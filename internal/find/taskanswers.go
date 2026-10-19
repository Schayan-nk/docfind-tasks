package find

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Schayan-nk/docfind-tasks/internal/jalali"
)

// handleTaskFormAnswers is GET /api/tasks/boards/{id}/forms/{form}/answers:
// one intake form's answers as a table, for somebody who may see the board;
// ?format=csv as a file Excel opens in Persian (UTF-8 with its byte-order
// mark), the dates both ways.
func (s *Server) handleTaskFormAnswers(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	form, cols, rows, err := s.tasks.FormAnswers(r.Context(), s.taskActor(r), r.PathValue("id"), r.PathValue("form"))
	if err != nil {
		taskError(w, err)
		return
	}
	if r.URL.Query().Get("format") != "csv" {
		writeJSON(w, map[string]any{"form": map[string]string{"id": form.ID, "name": form.Name}, "columns": cols, "rows": rows})
		return
	}
	name := strings.Map(func(c rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, c) || c < ' ' {
			return '_'
		}
		return c
	}, form.Name)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name+" "+time.Now().Format("2006-01-02")+".csv"))
	w.Write([]byte("\uFEFF"))
	cw := csv.NewWriter(w)
	head := []string{"تاریخ", "تاریخ (میلادی)", "فرستنده", "کار", "وضعیت"}
	for _, c := range cols {
		head = append(head, c.Label)
	}
	cw.Write(head)
	for _, row := range rows {
		line := []string{jalaliOf(row.Created), row.Created.Format("2006-01-02 15:04"), row.By, row.Title, row.Status}
		cw.Write(append(line, row.Cells...))
	}
	cw.Flush()
	s.auditBPM(s.taskActor(r).User, "tasks.form_export", "form:"+form.ID, map[string]any{"rows": len(rows)})
}

// tehran is the zone an export's dates are written in.
var tehran = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Tehran"); err == nil {
		return loc
	}
	return time.FixedZone("IRST", 3*3600+1800)
}()

// jalaliOf is t's date in Tehran, in the Jalali calendar: 1405/07/26.
func jalaliOf(t time.Time) string {
	t = t.In(tehran)
	y, m, d := jalali.ToJalali(t.Year(), int(t.Month()), t.Day())
	return fmt.Sprintf("%04d/%02d/%02d", y, m, d)
}
