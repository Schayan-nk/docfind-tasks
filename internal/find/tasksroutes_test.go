package find

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Schayan-nk/docfind-tasks/internal/auth"
	"github.com/Schayan-nk/docfind-tasks/internal/tasks"
)

// The task manager over HTTP: a person makes a workspace, a board and an item;
// a colleague outside the workspace finds none of it; the administrator sees
// it all; a stale save is answered with the current item rather than
// overwriting it; and "my work" and the palette find what they should.
func TestTheTaskManagerOverHTTP(t *testing.T) {
	s, boss, reader, _ := accountsTestServer(t)
	if rec := asUser(t, s, "GET", "/api/tasks", reader, ""); rec.Code != http.StatusNotFound ||
		!strings.Contains(rec.Body.String(), "PostgreSQL") {
		t.Fatalf("without a store: %d %s", rec.Code, rec.Body.String())
	}
	s.UseTasks(tasks.NewMemStore())
	if _, err := s.guard.Users.Create("other", "Other", "correct horse battery", auth.RoleViewer); err != nil {
		t.Fatal(err)
	}
	other := signFor(t, s.guard.Signer, s.guard.Users, "other")

	decode := func(body string, v any) {
		t.Helper()
		if err := json.Unmarshal([]byte(body), v); err != nil {
			t.Fatalf("%v: %s", err, body)
		}
	}
	var ws tasks.Workspace
	rec := asUser(t, s, "POST", "/api/tasks/workspaces", reader, `{"name":"فروش"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("workspace: %d %s", rec.Code, rec.Body.String())
	}
	decode(rec.Body.String(), &ws)
	var b tasks.Board
	rec = asUser(t, s, "POST", "/api/tasks/boards", reader, `{"workspace_id":"`+ws.ID+`","name":"استعلام‌ها"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("board: %d %s", rec.Code, rec.Body.String())
	}
	decode(rec.Body.String(), &b)
	if len(b.Statuses) != 7 {
		t.Errorf("the board's columns: %+v", b.Statuses)
	}
	var it tasks.Item
	rec = asUser(t, s, "POST", "/api/tasks/items", reader, `{"board_id":"`+b.ID+`","title":"پیشنهاد فنی","assignees":["boss"],"due":"2026-10-05"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("item: %d %s", rec.Code, rec.Body.String())
	}
	decode(rec.Body.String(), &it)

	// Outside the workspace: nothing, not even that it exists.
	for _, path := range []string{"/api/tasks/boards/" + b.ID, "/api/tasks/items/" + it.ID} {
		if rec := asUser(t, s, "GET", path, other, ""); rec.Code != http.StatusNotFound {
			t.Errorf("an outsider reading %s: %d", path, rec.Code)
		}
	}
	if rec := asUser(t, s, "POST", "/api/tasks/items", other, `{"board_id":"`+b.ID+`","title":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("an outsider adding an item: %d %s", rec.Code, rec.Body.String())
	}
	if rec := asUser(t, s, "GET", "/api/tasks/search?q=فنی", other, ""); strings.Contains(rec.Body.String(), it.ID) {
		t.Error("an outsider's search found the item")
	}

	// The administrator sees the workspace, and has the item as their work.
	var home struct {
		Workspaces []struct {
			Role   string        `json:"role"`
			Boards []tasks.Board `json:"boards"`
		} `json:"workspaces"`
	}
	decode(asUser(t, s, "GET", "/api/tasks", boss, "").Body.String(), &home)
	if len(home.Workspaces) != 1 || home.Workspaces[0].Role != "owner" || len(home.Workspaces[0].Boards) != 1 {
		t.Errorf("the administrator's home: %+v", home)
	}
	if rec := asUser(t, s, "GET", "/api/tasks/my", boss, ""); !strings.Contains(rec.Body.String(), it.ID) {
		t.Errorf("my work: %s", rec.Body.String())
	}
	if rec := asUser(t, s, "GET", "/api/tasks/search?q=فنی", reader, ""); !strings.Contains(rec.Body.String(), it.ID) {
		t.Errorf("the palette: %s", rec.Body.String())
	}

	// A change at the version read; then the same stale version again.
	it.Status = "doing"
	body, _ := json.Marshal(it)
	if rec := asUser(t, s, "POST", "/api/tasks/items", reader, string(body)); rec.Code != http.StatusOK {
		t.Fatalf("moving: %d %s", rec.Code, rec.Body.String())
	}
	rec = asUser(t, s, "POST", "/api/tasks/items", reader, string(body))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"current"`) {
		t.Errorf("a stale save: %d %s", rec.Code, rec.Body.String())
	}

	if rec := asUser(t, s, "POST", "/api/tasks/items/"+it.ID+"/comments", reader, `{"body":"@boss تأمین‌کننده پاسخ داد"}`); rec.Code != http.StatusOK {
		t.Errorf("a comment: %d %s", rec.Code, rec.Body.String())
	}
	rec = asUser(t, s, "GET", "/api/tasks/items/"+it.ID, reader, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "تأمین‌کننده") || !strings.Contains(rec.Body.String(), "ستون") {
		t.Errorf("the item with its updates and history: %d %s", rec.Code, rec.Body.String())
	}
	if rec := asUser(t, s, "POST", "/api/tasks/items", reader, `{"board_id":"`+b.ID+`","title":""}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "عنوان") {
		t.Errorf("an item with no title: %d %s", rec.Code, rec.Body.String())
	}
}
