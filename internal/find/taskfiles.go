package find

// The files a task form's question takes (internal/tasks/files.go has the
// rules): kept on disk beside the accounts, one file and a small JSON
// note about it each, in a folder per two first letters of its id.
//
//	POST /api/tasks/files        one file (multipart "file"): kept for whoever sent it,
//	                             attached to nothing; answers {id, name, size}
//	GET  /api/tasks/files/{id}   the file, as an attachment: for whoever sent it
//	                             while it is free, then for anybody who may see the item
//	                             or case it went on
//
// Never shown inline and never sniffed: a file somebody attached is handed
// over to be saved, whatever it claims to be.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Schayan-nk/docfind-tasks/internal/tasks"
)

// taskFileMax is the most one file may be.
const taskFileMax = 25 << 20

// refusedExt are kinds of file a form does not take: what runs when opened.
var refusedExt = map[string]bool{".exe": true, ".msi": true, ".bat": true, ".cmd": true, ".com": true, ".scr": true, ".ps1": true,
	".vbs": true, ".vbe": true, ".js": true, ".jse": true, ".wsf": true, ".wsh": true, ".hta": true, ".jar": true, ".dll": true,
	".lnk": true, ".reg": true, ".sh": true, ".cpl": true, ".msc": true, ".pif": true, ".application": true, ".gadget": true}

var taskFileID = regexp.MustCompile(`^[a-f0-9]{24}$`)

// taskFileNote is what is kept about a file.
type taskFileNote struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Size int64     `json:"size"`
	By   string    `json:"by"`
	At   time.Time `json:"at"`
	Item string    `json:"item,omitempty"`
}

// taskFiles is the folder of a task form's files; it is tasks.FileStore.
type taskFiles struct {
	dir string
	mu  sync.Mutex
}

func (f *taskFiles) paths(id string) (string, string) {
	d := filepath.Join(f.dir, id[:2])
	return filepath.Join(d, id+".bin"), filepath.Join(d, id+".json")
}

func (f *taskFiles) note(id string) (taskFileNote, error) {
	var n taskFileNote
	if !taskFileID.MatchString(id) {
		return n, os.ErrNotExist
	}
	_, meta := f.paths(id)
	raw, err := os.ReadFile(meta)
	if err != nil {
		return n, err
	}
	return n, json.Unmarshal(raw, &n)
}

func (f *taskFiles) writeNote(n taskFileNote) error {
	_, meta := f.paths(n.ID)
	raw, _ := json.Marshal(n)
	tmp := meta + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, meta)
}

// Usable: every file is user's and free, or already itemID's.
func (f *taskFiles) Usable(_ context.Context, user, itemID string, ids []string) error {
	for _, id := range ids {
		n, err := f.note(id)
		if err != nil {
			return tasks.Invalid{Msg: "پیوستی پیدا نشد؛ دوباره بفرستید."}
		}
		mine := n.Item == "" && n.By == user
		if !mine && (itemID == "" || n.Item != itemID) {
			return tasks.Invalid{Msg: "پیوست «" + n.Name + "» از آنِ این پرونده یا شما نیست."}
		}
	}
	return nil
}

// Attach makes the files the item's.
func (f *taskFiles) Attach(_ context.Context, itemID string, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range ids {
		n, err := f.note(id)
		if err != nil || n.Item == itemID {
			continue
		}
		if n.Item == "" {
			n.Item = itemID
			if err := f.writeNote(n); err != nil {
				return err
			}
		}
	}
	return nil
}

// UseTaskFiles keeps the task forms' files in dir.
func (s *Server) UseTaskFiles(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	s.taskFiles = &taskFiles{dir: dir}
	if s.tasks != nil {
		s.tasks.Files = s.taskFiles
	}
	return nil
}

func (s *Server) handleTaskFileUpload(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	if s.taskFiles == nil {
		writeJSONError(w, http.StatusNotFound, `{"error":"پیوست روی این سرور نگه داشته نمی‌شود."}`)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, taskFileMax+1<<20)
	file, head, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, `{"error":"فایلی نرسید، یا بیش از ۲۵ مگابایت بود."}`)
		return
	}
	defer file.Close()
	name := strings.TrimSpace(filepath.Base(strings.ReplaceAll(head.Filename, "\\", "/")))
	if r := []rune(name); len(r) > 200 {
		name = string(r[:200])
	}
	if name == "" || name == "." {
		name = "file"
	}
	if refusedExt[strings.ToLower(filepath.Ext(name))] {
		writeJSONError(w, http.StatusBadRequest, mustJSON(map[string]string{"error": "«" + name + "» از نوع پرونده‌های اجرایی است و پذیرفته نمی‌شود."}))
		return
	}
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		writeJSONError(w, http.StatusInternalServerError, `{"error":"نگه داشته نشد."}`)
		return
	}
	id := hex.EncodeToString(b[:])
	bin, _ := s.taskFiles.paths(id)
	if err := os.MkdirAll(filepath.Dir(bin), 0o700); err != nil {
		writeJSONError(w, http.StatusInternalServerError, `{"error":"نگه داشته نشد."}`)
		return
	}
	tmp := bin + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, `{"error":"نگه داشته نشد."}`)
		return
	}
	n, err := io.Copy(out, io.LimitReader(file, taskFileMax+1))
	out.Close()
	if err != nil || n > taskFileMax {
		os.Remove(tmp)
		writeJSONError(w, http.StatusRequestEntityTooLarge, `{"error":"هر پیوست حداکثر ۲۵ مگابایت است."}`)
		return
	}
	if err := os.Rename(tmp, bin); err != nil {
		os.Remove(tmp)
		writeJSONError(w, http.StatusInternalServerError, `{"error":"نگه داشته نشد."}`)
		return
	}
	note := taskFileNote{ID: id, Name: name, Size: n, By: s.taskActor(r).User, At: time.Now()}
	if err := s.taskFiles.writeNote(note); err != nil {
		os.Remove(bin)
		writeJSONError(w, http.StatusInternalServerError, `{"error":"نگه داشته نشد."}`)
		return
	}
	writeJSON(w, map[string]any{"id": id, "name": name, "size": n})
}

func (s *Server) handleTaskFile(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	if s.taskFiles == nil {
		http.NotFound(w, r)
		return
	}
	n, err := s.taskFiles.note(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a := s.taskActor(r)
	if !(n.Item == "" && n.By == a.User) && !(n.Item != "" && s.tasks.SeesItem(r.Context(), a, n.Item)) {
		// Somebody else's: as if it were not there.
		http.NotFound(w, r)
		return
	}
	bin, _ := s.taskFiles.paths(n.ID)
	fh, err := os.Open(bin)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logReportError("task file", err)
		}
		http.NotFound(w, r)
		return
	}
	defer fh.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\"file\"; filename*=UTF-8''"+url.PathEscape(n.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", n.At, fh)
}
