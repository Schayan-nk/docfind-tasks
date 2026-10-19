package find

// Questions whose options come from this server's own lists: a form's
// question may offer the people with an account here, or the departments.
// The task manager's side is internal/tasks/fielddata.go.
//
//	POST /api/tasks/fielddata          {board, form|node, field, values, q}: a question's
//	                                   options (or value) for somebody filling it
//	GET  /api/tasks/fielddata/sources  where a question may draw its options from
//	POST /api/tasks/fielddata/test     {source, values, q}: a list as the form maker
//	                                   previews it
//
// The page names a question; the list it reads is the one saved on it.

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Schayan-nk/docfind-tasks/internal/tasks"
)

// fieldData answers the task manager's sourced questions.
type fieldData struct{ s *Server }

func (d fieldData) Choices(ctx context.Context, f tasks.FormField, vals map[string]any, q string) ([]tasks.Choice, error) {
	q = strings.TrimSpace(q)
	keep := func(value, label string) bool {
		return q == "" || value == q || strings.Contains(strings.ToLower(label), strings.ToLower(q))
	}
	var out []tasks.Choice
	switch f.Source {
	case "people":
		for _, p := range d.s.flowPeople() {
			if keep(p["user"], p["name"]+" "+p["user"]) {
				out = append(out, tasks.Choice{Value: p["user"], Label: firstNonEmpty(p["name"], p["user"])})
			}
		}
	case "departments":
		by, _, err := deptOrg{d.s}.load(ctx)
		if err != nil {
			return nil, err
		}
		for id, dp := range by {
			v := strconv.Itoa(id)
			if keep(v, dp.Name) {
				out = append(out, tasks.Choice{Value: v, Label: dp.Name})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	default:
		return nil, errNoData
	}
	return out, nil
}

type dataErr string

func (e dataErr) Error() string { return string(e) }

const errNoData = dataErr("این منبع داده روی این سرور نیست")

func (s *Server) handleFieldData(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var req struct {
		Board  string         `json:"board"`
		Form   string         `json:"form"`
		Node   string         `json:"node"`
		Field  string         `json:"field"`
		Values map[string]any `json:"values"`
		Q      string         `json:"q"`
	}
	if !readTaskJSON(w, r, &req) {
		return
	}
	f, err := s.tasks.FieldFor(r.Context(), s.taskActor(r), req.Board, req.Form, req.Node, req.Field)
	if err != nil {
		taskError(w, err)
		return
	}
	if req.Values == nil {
		req.Values = map[string]any{}
	}
	cs, err := fieldData{s}.Choices(r.Context(), f, req.Values, req.Q)
	if err != nil {
		logReportError("task field data", err)
		writeJSONError(w, http.StatusBadGateway, mustJSON(map[string]string{"error": "«" + f.Label + "» خوانده نشد."}))
		return
	}
	if cs == nil {
		cs = []tasks.Choice{}
	}
	writeJSON(w, map[string]any{"choices": cs})
}

// fieldLists are this server's own lists, for every form maker.
var fieldLists = []map[string]string{
	{"id": "people", "name": "همکاران"},
	{"id": "departments", "name": "واحدها"},
}

func (s *Server) handleFieldSources(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	writeJSON(w, map[string]any{"lists": fieldLists, "databases": []any{}, "may_sql": false})
}

// handleFieldTest shows one of the lists as the form maker previews it.
func (s *Server) handleFieldTest(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var req struct {
		Source string         `json:"source"`
		Values map[string]any `json:"values"`
		Q      string         `json:"q"`
	}
	if !readTaskJSON(w, r, &req) {
		return
	}
	cs, err := fieldData{s}.Choices(r.Context(), tasks.FormField{Source: req.Source, Kind: "choice"}, req.Values, req.Q)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "choices": cs})
}

// firstNonEmpty is a, unless it is blank.
func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
