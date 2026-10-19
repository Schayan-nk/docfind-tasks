package find

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Schayan-nk/docfind-tasks/internal/auth"
	"github.com/Schayan-nk/docfind-tasks/internal/tasks"
)

// An intake form's answers come back as a table and as a file for Excel:
// typed answers shown as text, newest first, a question the form no longer
// asks still in its column, and nothing for somebody who cannot see the
// board.
func TestAFormsAnswersAreATableAndAFile(t *testing.T) {
	s, _, reader, _ := accountsTestServer(t)
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
	decode(asUser(t, s, "POST", "/api/tasks/workspaces", reader, `{"name":"خرید"}`).Body.String(), &ws)
	board := `{"workspace_id":"` + ws.ID + `","name":"درخواست‌ها","forms":[{"name":"درخواست خرید","fields":[
		{"key":"subject","label":"موضوع","kind":"text","required":true},
		{"key":"amount","label":"مبلغ","kind":"money"},
		{"key":"urgent","label":"فوری","kind":"choice","choices":[{"value":"1","label":"بله"},{"value":"0","label":"خیر"}]}]}]}`
	var b tasks.Board
	rec := asUser(t, s, "POST", "/api/tasks/boards", reader, board)
	decode(rec.Body.String(), &b)
	if len(b.Forms) != 1 {
		t.Fatalf("the board's form: %s", rec.Body)
	}
	form := b.Forms[0].ID
	submit := func(body string) {
		t.Helper()
		if rec := asUser(t, s, "POST", "/api/tasks/forms/"+b.ID+"/"+form, reader, body); rec.Code != http.StatusOK {
			t.Fatalf("a submission: %d %s", rec.Code, rec.Body)
		}
	}
	submit(`{"answers":{"subject":"لپ‌تاپ","amount":"۵۰٬۰۰۰٬۰۰۰","urgent":"1"}}`)
	submit(`{"answers":{"subject":"میز","amount":"12000000","urgent":"0"}}`)

	// The form changes: "urgent" is no longer asked.
	b.Forms[0].Fields = b.Forms[0].Fields[:2]
	raw, _ := json.Marshal(b)
	if rec := asUser(t, s, "POST", "/api/tasks/boards", reader, string(raw)); rec.Code != http.StatusOK {
		t.Fatalf("changing the form: %d %s", rec.Code, rec.Body)
	}

	path := "/api/tasks/boards/" + b.ID + "/forms/" + form + "/answers"
	var got struct {
		Columns []tasks.AnswerColumn `json:"columns"`
		Rows    []tasks.AnswerRow    `json:"rows"`
	}
	decode(asUser(t, s, "GET", path, reader, "").Body.String(), &got)
	if len(got.Columns) != 3 || got.Columns[2].Label != "فوری" || len(got.Rows) != 2 {
		t.Fatalf("the table: %+v", got)
	}
	if got.Rows[0].Cells[0] != "میز" || got.Rows[1].Cells[0] != "لپ‌تاپ" {
		t.Errorf("not newest first: %+v", got.Rows)
	}
	if got.Rows[1].Cells[2] != "بله" || !strings.Contains(got.Rows[1].Cells[1], "50") {
		t.Errorf("the answers as text: %q", got.Rows[1].Cells)
	}

	rec = asUser(t, s, "GET", path+"?format=csv", reader, "")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "\uFEFF") || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("the file: %d %q %s", rec.Code, rec.Header().Get("Content-Disposition"), rec.Body.String()[:min(80, rec.Body.Len())])
	}
	lines, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(rec.Body.String(), "\uFEFF"))).ReadAll()
	if err != nil || len(lines) != 3 || lines[0][5] != "موضوع" || lines[1][5] != "میز" || !strings.HasPrefix(lines[1][0], "14") {
		t.Fatalf("the file's rows: %v %q", err, lines)
	}

	if rec := asUser(t, s, "GET", path, other, ""); rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
		t.Errorf("somebody who cannot see the board read its answers: %d", rec.Code)
	}
	var forms struct {
		Forms []tasks.FormRef `json:"forms"`
	}
	decode(asUser(t, s, "GET", "/api/tasks/forms", reader, "").Body.String(), &forms)
	if len(forms.Forms) != 1 || !forms.Forms[0].SeesBoard {
		t.Errorf("the forms list does not say the reader sees the board: %+v", forms.Forms)
	}
}

// A form's requests go to whoever its rule names: here the colleague a
// question names, and the form's own list when the question is left empty;
// a rule pointing at a question that is not a person is refused.
func TestAFormsRequestGoesToTheColleagueItNames(t *testing.T) {
	s, _, reader, _ := accountsTestServer(t)
	s.UseTasks(tasks.NewMemStore())
	if _, err := s.guard.Users.Create("other", "Other", "correct horse battery", auth.RoleViewer); err != nil {
		t.Fatal(err)
	}
	var ws tasks.Workspace
	json.Unmarshal(asUser(t, s, "POST", "/api/tasks/workspaces", reader, `{"name":"فنی"}`).Body.Bytes(), &ws)
	bad := `{"workspace_id":"` + ws.ID + `","name":"درخواست‌ها","forms":[{"name":"بازدید","assignees":["reader"],
		"assign":{"mode":"field","field":"subject"},"fields":[{"key":"subject","label":"موضوع","kind":"text","required":true}]}]}`
	if rec := asUser(t, s, "POST", "/api/tasks/boards", reader, bad); rec.Code != http.StatusBadRequest || !hasPersian(rec.Body.String()) {
		t.Fatalf("a rule naming a question that is not a person: %d %s", rec.Code, rec.Body)
	}
	good := strings.Replace(bad, `"fields":[`, `"fields":[{"key":"expert","label":"کارشناس","kind":"person"},`, 1)
	good = strings.Replace(good, `"field":"subject"`, `"field":"expert"`, 1)
	var b tasks.Board
	rec := asUser(t, s, "POST", "/api/tasks/boards", reader, good)
	json.Unmarshal(rec.Body.Bytes(), &b)
	if len(b.Forms) != 1 || b.Forms[0].Assign == nil || b.Forms[0].Assign.Field != "expert" {
		t.Fatalf("the form with its rule: %d %s", rec.Code, rec.Body)
	}
	send := func(body string) tasks.Item {
		t.Helper()
		rec := asUser(t, s, "POST", "/api/tasks/forms/"+b.ID+"/"+b.Forms[0].ID, reader, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("a request: %d %s", rec.Code, rec.Body)
		}
		var r struct{ ID string }
		json.Unmarshal(rec.Body.Bytes(), &r)
		it, _, _, err := s.tasks.Item(context.Background(), tasks.Actor{User: "reader"}, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	if it := send(`{"answers":{"subject":"پمپ","expert":"other"}}`); len(it.Assignees) != 1 || it.Assignees[0] != "other" {
		t.Errorf("the colleague named: %v", it.Assignees)
	}
	if it := send(`{"answers":{"subject":"شیر"}}`); len(it.Assignees) != 1 || it.Assignees[0] != "reader" {
		t.Errorf("nobody named, the form's own list: %v", it.Assignees)
	}
}

// An intake form's conditions hold on the server: a question shown only for
// goods is not required, and not kept, for a service; one required only for
// goods is refused empty there.
func TestAFormsConditionsHoldOnTheServer(t *testing.T) {
	s, _, reader, _ := accountsTestServer(t)
	s.UseTasks(tasks.NewMemStore())
	var ws tasks.Workspace
	json.Unmarshal(asUser(t, s, "POST", "/api/tasks/workspaces", reader, `{"name":"خرید"}`).Body.Bytes(), &ws)
	board := `{"workspace_id":"` + ws.ID + `","name":"درخواست‌ها","forms":[{"name":"درخواست","fields":[
		{"key":"kind","label":"نوع","kind":"choice","required":true,"choices":[{"value":"1","label":"کالا"},{"value":"2","label":"خدمت"}]},
		{"key":"qty","label":"تعداد","kind":"number","required":true,"show_if":[{"field":"kind","op":"eq","value":["1"]}]},
		{"key":"spec","label":"مشخصات","kind":"textarea","required_if":[{"field":"kind","op":"eq","value":["1"]}]}]}]}`
	var b tasks.Board
	rec := asUser(t, s, "POST", "/api/tasks/boards", reader, board)
	json.Unmarshal(rec.Body.Bytes(), &b)
	if len(b.Forms) != 1 || len(b.Forms[0].Fields[1].ShowIf) != 1 || len(b.Forms[0].Fields[2].RequiredIf) != 1 {
		t.Fatalf("the form and its conditions: %d %s", rec.Code, rec.Body)
	}
	path := "/api/tasks/forms/" + b.ID + "/" + b.Forms[0].ID
	if rec := asUser(t, s, "POST", path, reader, `{"answers":{"kind":"1","qty":"3"}}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "مشخصات") {
		t.Errorf("goods without their specification: %d %s", rec.Code, rec.Body)
	}
	rec = asUser(t, s, "POST", path, reader, `{"answers":{"kind":"2","qty":"99"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("a service, the quantity not asked: %d %s", rec.Code, rec.Body)
	}
	var r struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &r)
	it, _, _, err := s.tasks.Item(context.Background(), tasks.Actor{User: "reader"}, r.ID)
	if err != nil || it.Form == nil {
		t.Fatal(err)
	}
	if _, kept := it.Form.Answers["qty"]; kept {
		t.Errorf("a question that was not shown kept its answer: %v", it.Form.Answers)
	}
}
