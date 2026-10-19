package find

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Schayan-nk/docfind-tasks/internal/auth"
	"github.com/Schayan-nk/docfind-tasks/internal/tasks"
)

// A process over HTTP: its owner sets the map, starts a case, and the next
// step lands in the inbox of somebody who is not even in the workspace --
// docfind is the inbox -- who finishes it; a third person sees nothing.
func TestAProcessRunsOverHTTP(t *testing.T) {
	s, _, reader, _ := accountsTestServer(t)
	s.UseTasks(tasks.NewMemStore())
	for _, u := range []string{"other", "third"} {
		if _, err := s.guard.Users.Create(u, strings.ToUpper(u[:1])+u[1:], "correct horse battery", auth.RoleViewer); err != nil {
			t.Fatal(err)
		}
	}
	other := signFor(t, s.guard.Signer, s.guard.Users, "other")
	third := signFor(t, s.guard.Signer, s.guard.Users, "third")
	decode := func(body string, v any) {
		t.Helper()
		if err := json.Unmarshal([]byte(body), v); err != nil {
			t.Fatalf("%v: %s", err, body)
		}
	}
	var ws tasks.Workspace
	decode(asUser(t, s, "POST", "/api/tasks/workspaces", reader, `{"name":"فرآیندها"}`).Body.String(), &ws)
	var b tasks.Board
	decode(asUser(t, s, "POST", "/api/tasks/boards", reader, `{"workspace_id":"`+ws.ID+`","name":"درخواست خرید"}`).Body.String(), &b)

	process := `{"name":"درخواست خرید","start":"ask","nodes":[
		{"id":"ask","kind":"step","name":"ثبت درخواست","assign":{"mode":"users","users":["reader"]},"fields":[
			{"key":"subject","label":"موضوع","kind":"text","required":true},{"key":"amount","label":"مبلغ","kind":"money"}]},
		{"id":"ok","kind":"step","name":"تایید","assign":{"mode":"users","users":["other"]},"fields":[
			{"key":"ok","label":"نتیجه","kind":"choice","required":true,"choices":[{"value":"1","label":"تایید"},{"value":"2","label":"رد"}]}]},
		{"id":"end","kind":"end"}],
	"edges":[{"from":"ask","to":"ok","kind":"seq"},{"from":"ok","to":"end","kind":"seq"}]}`
	if rec := asUser(t, s, "PUT", "/api/tasks/boards/"+b.ID+"/process", reader, process); rec.Code != http.StatusOK {
		t.Fatalf("the map: %d %s", rec.Code, rec.Body)
	}
	var home struct {
		Inbox []tasks.InboxEntry       `json:"inbox"`
		Start []tasks.StartableProcess `json:"start"`
	}
	decode(asUser(t, s, "GET", "/api/tasks/flow", reader, "").Body.String(), &home)
	if len(home.Start) != 1 || home.Start[0].BoardID != b.ID {
		t.Fatalf("what the owner may start: %+v", home.Start)
	}
	if rec := asUser(t, s, "GET", "/api/tasks/flow/boards/"+b.ID+"/start", reader, ""); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"subject"`) {
		t.Fatalf("the start form: %d %s", rec.Code, rec.Body)
	}
	if rec := asUser(t, s, "POST", "/api/tasks/flow/boards/"+b.ID+"/cases", reader, `{"answers":{"amount":"5,000"}}`); rec.Code != http.StatusBadRequest ||
		!hasPersian(rec.Body.String()) {
		t.Errorf("a required answer missing: %d %s", rec.Code, rec.Body)
	}
	var started struct{ ID string }
	rec := asUser(t, s, "POST", "/api/tasks/flow/boards/"+b.ID+"/cases", reader, `{"answers":{"subject":"لپ‌تاپ","amount":"۵۰٬۰۰۰٬۰۰۰"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("starting: %d %s", rec.Code, rec.Body)
	}
	decode(rec.Body.String(), &started)

	// The approver is in no workspace, and still finds the step waiting.
	decode(asUser(t, s, "GET", "/api/tasks/flow", other, "").Body.String(), &home)
	if len(home.Inbox) != 1 || home.Inbox[0].Step != "تایید" || len(home.Start) != 0 {
		t.Fatalf("the approver's inbox: %+v", home)
	}
	var cv struct {
		Data map[string]any `json:"data"`
		Open []struct {
			ID     string            `json:"id"`
			Mine   bool              `json:"mine"`
			Fields []tasks.FormField `json:"fields"`
		} `json:"open"`
		Fields map[string]tasks.FormField `json:"fields"`
		Order  []string                   `json:"order"`
		Done   bool                       `json:"done"`
	}
	decode(asUser(t, s, "GET", "/api/tasks/flow/cases/"+started.ID, other, "").Body.String(), &cv)
	if len(cv.Open) != 1 || !cv.Open[0].Mine || len(cv.Open[0].Fields) != 1 || cv.Data["amount"] != 50000000.0 || cv.Fields["amount"].Label != "مبلغ" {
		t.Fatalf("the case as the approver sees it: %+v", cv)
	}
	if strings.Join(cv.Order, ",") != "subject,amount,ok" {
		t.Errorf("the fields' order: %v", cv.Order)
	}
	if rec := asUser(t, s, "GET", "/api/tasks/flow/cases/"+started.ID, third, ""); rec.Code != http.StatusNotFound {
		t.Errorf("a stranger read the case: %d", rec.Code)
	}
	if rec := asUser(t, s, "POST", "/api/tasks/flow/cases/"+started.ID+"/steps/"+cv.Open[0].ID, third, `{"answers":{"ok":"1"}}`); rec.Code != http.StatusNotFound {
		t.Errorf("a stranger did the step: %d", rec.Code)
	}
	rec = asUser(t, s, "POST", "/api/tasks/flow/cases/"+started.ID+"/steps/"+cv.Open[0].ID, other, `{"answers":{"ok":"1"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("the approval: %d %s", rec.Code, rec.Body)
	}
	decode(rec.Body.String(), &cv)
	if !cv.Done || len(cv.Open) != 0 {
		t.Errorf("after the approval: %+v", cv)
	}
	if rec := asUser(t, s, "PUT", "/api/tasks/boards/"+b.ID+"/process", other, process); rec.Code == http.StatusOK {
		t.Error("somebody who does not own the workspace changed its map")
	}
}
