package find

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Schayan-nk/docfind-tasks/internal/auth"
	"github.com/Schayan-nk/docfind-tasks/internal/tasks"
)

func uploadAs(t *testing.T, s *Server, token, name, content string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write([]byte(content))
	mw.Close()
	r := httptest.NewRequest("POST", "/api/tasks/files", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: token})
	r.Header.Set("Origin", "http://example.test")
	r.Host = "example.test"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	return rec
}

// A form's file: kept for whoever sent it, then the item's -- downloaded
// only as an attachment, by whoever may see that item, and never taken from
// somebody else's hands.
func TestAFormsFilesAreTheSendersThenTheItems(t *testing.T) {
	s, _, reader, _ := accountsTestServer(t)
	s.UseTasks(tasks.NewMemStore())
	if err := s.UseTaskFiles(filepath.Join(tempDir(t), "task-files")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.guard.Users.Create("other", "Other", "correct horse battery", auth.RoleViewer); err != nil {
		t.Fatal(err)
	}
	other := signFor(t, s.guard.Signer, s.guard.Users, "other")

	if rec := uploadAs(t, s, other, "setup.exe", "MZ"); rec.Code != http.StatusBadRequest || !hasPersian(rec.Body.String()) {
		t.Errorf("a program was taken: %d %s", rec.Code, rec.Body)
	}
	rec := uploadAs(t, s, other, `..\..\پیش‌فاکتور.pdf`, "%PDF-1.4 hello")
	var up struct {
		ID   string
		Name string
		Size int64
	}
	json.Unmarshal(rec.Body.Bytes(), &up)
	if rec.Code != http.StatusOK || len(up.ID) != 24 || up.Name != "پیش‌فاکتور.pdf" || up.Size != 14 {
		t.Fatalf("the upload: %d %s", rec.Code, rec.Body)
	}
	get := func(token string) *httptest.ResponseRecorder {
		return asUser(t, s, "GET", "/api/tasks/files/"+up.ID, token, "")
	}
	if rec := get(other); rec.Code != http.StatusOK || rec.Body.String() != "%PDF-1.4 hello" ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("the sender's own file: %d %v", rec.Code, rec.Header())
	}
	if rec := get(reader); rec.Code != http.StatusNotFound {
		t.Errorf("somebody else read a file still being filled in: %d", rec.Code)
	}

	// An open form with a file question, on reader's board.
	var ws tasks.Workspace
	json.Unmarshal(asUser(t, s, "POST", "/api/tasks/workspaces", reader, `{"name":"خرید"}`).Body.Bytes(), &ws)
	var b tasks.Board
	json.Unmarshal(asUser(t, s, "POST", "/api/tasks/boards", reader, `{"workspace_id":"`+ws.ID+`","name":"درخواست‌ها","forms":[{"name":"پیش‌فاکتور","open":true,
		"fields":[{"key":"subject","label":"موضوع","kind":"text","required":true},{"key":"docs","label":"پیوست","kind":"file"}]}]}`).Body.Bytes(), &b)
	if len(b.Forms) != 1 {
		t.Fatalf("the board: %+v", b)
	}
	send := func(token, id string) *httptest.ResponseRecorder {
		return asUser(t, s, "POST", "/api/tasks/forms/"+b.ID+"/"+b.Forms[0].ID, token,
			`{"answers":{"subject":"پمپ","docs":[{"id":"`+id+`","name":"x.pdf","size":14}]}}`)
	}
	// reader may not hand in other's file as their own.
	if rec := send(reader, up.ID); rec.Code != http.StatusBadRequest {
		t.Errorf("somebody else's file was sent: %d %s", rec.Code, rec.Body)
	}
	if rec := send(other, up.ID); rec.Code != http.StatusOK {
		t.Fatalf("the form: %d %s", rec.Code, rec.Body)
	}
	if rec := get(reader); rec.Code != http.StatusOK {
		t.Errorf("the board's owner cannot read its item's file: %d", rec.Code)
	}
	if rec := get(other); rec.Code != http.StatusOK {
		t.Errorf("the sender cannot read the file they sent: %d", rec.Code)
	}
	// Once the item's, it cannot be sent again on another.
	if rec := send(other, up.ID); rec.Code != http.StatusBadRequest {
		t.Errorf("a file of one item went on another: %d", rec.Code)
	}
	if rec := asUser(t, s, "GET", "/api/tasks/files/ffffffffffffffffffffffff", reader, ""); rec.Code != http.StatusNotFound {
		t.Errorf("a file that is not there: %d", rec.Code)
	}
}
