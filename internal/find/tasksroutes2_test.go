package find

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Schayan-nk/docfind-tasks/internal/tasks"
)

// The second half over HTTP: a webhook's secret never goes back to a page;
// the buttons, the timer, a form and a template board do what they say; and
// the dashboard counts.
func TestTheTaskManagersSecondHalfOverHTTP(t *testing.T) {
	s, _, reader, _ := accountsTestServer(t)
	s.UseTasks(tasks.NewMemStore())
	s.tasks.Hook = nil // no posting from a test
	post := func(path, body string) (int, string) {
		t.Helper()
		rec := asUser(t, s, "POST", path, reader, body)
		return rec.Code, rec.Body.String()
	}
	get := func(path string) (int, string) {
		t.Helper()
		rec := asUser(t, s, "GET", path, reader, "")
		return rec.Code, rec.Body.String()
	}
	var ws tasks.Workspace
	_, body := post("/api/tasks/workspaces", `{"name":"فنی"}`)
	_ = json.Unmarshal([]byte(body), &ws)
	code, body := post("/api/tasks/boards", `{"workspace_id":"`+ws.ID+`","name":"پروژه","webhooks":[{"url":"https://erp.lan/hook","events":["created"],"secret":"TOPSECRET"}]}`)
	if code != http.StatusOK || strings.Contains(body, "TOPSECRET") {
		t.Fatalf("saving a board with a webhook: %d %s", code, body)
	}
	var b tasks.Board
	_ = json.Unmarshal([]byte(body), &b)
	for _, path := range []string{"/api/tasks", "/api/tasks/boards/" + b.ID} {
		if _, body := get(path); strings.Contains(body, "TOPSECRET") {
			t.Errorf("%s gave the webhook's secret away", path)
		}
	}

	_, body = post("/api/tasks/items", `{"board_id":"`+b.ID+`","title":"نصب"}`)
	var it tasks.Item
	_ = json.Unmarshal([]byte(body), &it)
	if code, body := post("/api/tasks/items/"+it.ID+"/press", `{"button":"start"}`); code != http.StatusOK || !strings.Contains(body, `"status":"doing"`) {
		t.Errorf("start: %d %s", code, body)
	}
	if code, body := post("/api/tasks/items/"+it.ID+"/timer", `{"action":"add","minutes":45,"note":"نصب"}`); code != http.StatusOK || !strings.Contains(body, `"minutes":45`) {
		t.Errorf("time: %d %s", code, body)
	}
	if code, body := get("/api/tasks/timesheet?from=2000-01-01"); code != http.StatusOK || !strings.Contains(body, it.ID) {
		t.Errorf("timesheet: %d %s", code, body)
	}
	if code, _ := get("/api/tasks/timesheet?from=yesterday"); code != http.StatusBadRequest {
		t.Errorf("a timesheet from a bad date: %d", code)
	}
	if code, body := post("/api/tasks/items/"+it.ID+"/approval", `{"steps":[{"approvers":["reader"]}]}`); code != http.StatusOK || !strings.Contains(body, `"state":"pending"`) {
		t.Errorf("approval: %d %s", code, body)
	}
	if code, body := get("/api/tasks/approvals"); code != http.StatusOK || !strings.Contains(body, it.ID) {
		t.Errorf("my approvals: %d %s", code, body)
	}
	if code, body := post("/api/tasks/items/"+it.ID+"/decide", `{"decision":"approve"}`); code != http.StatusOK || !strings.Contains(body, `"state":"approved"`) {
		t.Errorf("decide: %d %s", code, body)
	}

	code, body = post("/api/tasks/boards/from-template", `{"workspace_id":"`+ws.ID+`","template":"tender"}`)
	if code != http.StatusOK || !strings.Contains(body, "مناقصهٔ تازه") {
		t.Fatalf("from a template: %d %s", code, body)
	}
	if code, body := get("/api/tasks/templates"); code != http.StatusOK || !strings.Contains(body, `"offer"`) {
		t.Errorf("templates: %d %s", code, body)
	}
	if code, body := get("/api/tasks/dashboard"); code != http.StatusOK || !strings.Contains(body, `"total":10`) {
		t.Errorf("dashboard: %d %s", code, body)
	}
}
