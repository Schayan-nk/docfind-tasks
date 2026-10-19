package find

// Processes in the task manager: this server is the inbox. Every open
// step of every case waits in its assignee's inbox here, whatever workspace
// its board is in; the service (internal/tasks/flow.go) decides who may see,
// start, claim and finish what, so these doors need only a signed-in person.
//
//	GET  /api/tasks/flow                         my inbox and what I may start
//	POST /api/tasks/flow/boards/{board}/cases    start a case {answers}
//	GET  /api/tasks/flow/cases/{id}              a case: its data, history, my steps
//	POST /api/tasks/flow/cases/{id}/steps/{t}    finish my step {answers}
//	POST /api/tasks/flow/cases/{id}/claim/{t}    take a pooled step
//	GET  /api/tasks/boards/{id}/process          a board's map (its owner)
//	PUT  /api/tasks/boards/{id}/process          change it -- a new version
//	POST /api/tasks/process/bpmn/import          a BPMN 2.0 file read as a map (not saved)
//	POST /api/tasks/process/bpmn/export          a map written as BPMN 2.0

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/Schayan-nk/docfind-tasks/internal/pgstore"
	"github.com/Schayan-nk/docfind-tasks/internal/tasks"
)

// deptOrg answers the process engine from docfind's departments page.
type deptOrg struct{ s *Server }

func (o deptOrg) load(ctx context.Context) (map[int]pgstore.Department, []pgstore.Membership, error) {
	if o.s.departments == nil {
		return nil, nil, nil
	}
	deps, err := o.s.departments.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	ms, err := o.s.departments.Memberships(ctx)
	if err != nil {
		return nil, nil, err
	}
	by := map[int]pgstore.Department{}
	for _, d := range deps {
		by[d.ID] = d
	}
	return by, ms, nil
}

// Head is the head of the person's primary department (their first, when
// none is marked), or of the nearest one above it that has a head other than
// themselves.
func (o deptOrg) Head(ctx context.Context, username string) (string, error) {
	by, ms, err := o.load(ctx)
	if err != nil || by == nil {
		return "", err
	}
	home := 0
	for _, m := range ms {
		if m.Username == username && (m.Primary || home == 0) {
			home = m.DepartmentID
		}
	}
	for i, id := 0, home; i < 64 && id != 0; i++ {
		d, ok := by[id]
		if !ok {
			break
		}
		if d.Head != "" && d.Head != username {
			return d.Head, nil
		}
		if d.ParentID == nil {
			break
		}
		id = *d.ParentID
	}
	return "", nil
}

// Members are a department's people.
func (o deptOrg) Members(ctx context.Context, id int) ([]string, error) {
	_, ms, err := o.load(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range ms {
		if m.DepartmentID == id && !slices.Contains(out, m.Username) {
			out = append(out, m.Username)
		}
	}
	sort.Strings(out)
	return out, nil
}

// isActiveUser is a username of a docfind account that may sign in.
func (s *Server) isActiveUser(u string) bool {
	if s.guard == nil || s.guard.Users == nil {
		return true
	}
	x, err := s.guard.Users.Get(u)
	return err == nil && x != nil && !x.Disabled
}

// flowPeople are everybody a person field may name: active accounts.
func (s *Server) flowPeople() []map[string]string {
	out := []map[string]string{}
	if s.guard == nil || s.guard.Users == nil {
		return out
	}
	for _, u := range s.guard.Users.List() {
		if !u.Disabled {
			out = append(out, map[string]string{"user": u.Username, "name": u.DisplayName})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"] < out[j]["name"] })
	return out
}

func (s *Server) handleFlowHome(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	a := s.taskActor(r)
	in, err := s.tasks.Inbox(r.Context(), a)
	if err != nil {
		taskError(w, err)
		return
	}
	start, err := s.tasks.Startable(r.Context(), a)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, map[string]any{"inbox": in, "start": start, "me": a.User})
}

// handleFlowStartForm is GET /api/tasks/flow/boards/{board}/start: the start
// step's form, for somebody who may start one.
func (s *Server) handleFlowStartForm(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	a := s.taskActor(r)
	list, err := s.tasks.Startable(r.Context(), a)
	if err != nil {
		taskError(w, err)
		return
	}
	board := r.PathValue("board")
	if !slices.ContainsFunc(list, func(p tasks.StartableProcess) bool { return p.BoardID == board }) {
		writeJSONError(w, http.StatusNotFound, `{"error":"این فرآیند نیست یا آغازش با شما نیست."}`)
		return
	}
	p, err := s.tasks.StartForm(r.Context(), a, board)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, map[string]any{"process": p.Name, "step": p.StepName, "node": p.Node, "fields": tasks.PublicFields(p.Fields), "people": s.flowPeople()})
}

func (s *Server) handleFlowStart(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var req struct {
		Answers map[string]any `json:"answers"`
	}
	if !readTaskJSON(w, r, &req) {
		return
	}
	it, err := s.tasks.StartCase(r.Context(), s.taskActor(r), r.PathValue("board"), req.Answers)
	if err != nil {
		taskError(w, err)
		return
	}
	s.auditBPM(s.taskActor(r).User, "flow.start", "case:"+it.ID, map[string]any{"title": it.Title})
	writeJSON(w, map[string]any{"ok": true, "id": it.ID, "title": it.Title})
}

func (s *Server) handleFlowCase(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	a := s.taskActor(r)
	it, p, err := s.tasks.Case(r.Context(), a, r.PathValue("id"))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, caseView(it, p, a.User, s.flowPeople()))
}

func (s *Server) handleFlowStep(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var req struct {
		Answers map[string]any `json:"answers"`
	}
	if !readTaskJSON(w, r, &req) {
		return
	}
	a := s.taskActor(r)
	it, err := s.tasks.CompleteStep(r.Context(), a, r.PathValue("id"), r.PathValue("thread"), req.Answers)
	if err != nil {
		taskError(w, err)
		return
	}
	_, p, _ := s.tasks.Case(r.Context(), a, it.ID)
	writeJSON(w, caseView(it, p, a.User, s.flowPeople()))
}

func (s *Server) handleFlowClaim(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	a := s.taskActor(r)
	it, err := s.tasks.Claim(r.Context(), a, r.PathValue("id"), r.PathValue("thread"))
	if err != nil {
		taskError(w, err)
		return
	}
	_, p, _ := s.tasks.Case(r.Context(), a, it.ID)
	writeJSON(w, caseView(it, p, a.User, s.flowPeople()))
}

func (s *Server) handleProcessGet(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	b, _, err := s.tasks.Board(r.Context(), s.taskActor(r), r.PathValue("id"))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, map[string]any{"process": b.Process})
}

func (s *Server) handleProcessSave(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var p tasks.Process
	if !readTaskJSONMax(w, r, &p, 8<<20) {
		return
	}
	a := s.taskActor(r)
	b, err := s.tasks.SaveProcess(r.Context(), a, r.PathValue("id"), p)
	if err != nil {
		taskError(w, err)
		return
	}
	s.auditBPM(a.User, "flow.process", "board:"+b.ID, map[string]any{"version": b.Process.Version, "name": b.Process.Name})
	writeJSON(w, map[string]any{"ok": true, "version": b.Process.Version})
}

// handleProcessBPMNImport reads a BPMN file drawn anywhere as a process map,
// for the designer to show; nothing is saved until the designer saves it.
// What the map would be refused for comes back as problem.
func (s *Server) handleProcessBPMNImport(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, `{"error":"فایل BPMN بیش از ۸ مگابایت است."}`)
		return
	}
	p, warnings, err := tasks.ImportBPMN(data)
	if err != nil {
		taskError(w, err)
		return
	}
	problem := ""
	if _, err := tasks.CheckProcess(p); err != nil {
		problem = err.Error()
	}
	writeJSON(w, map[string]any{"process": p, "warnings": warnings, "problem": problem})
}

// handleProcessBPMNExport writes the map the designer holds -- saved or not
// -- as a BPMN 2.0 file.
func (s *Server) handleProcessBPMNExport(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var p tasks.Process
	if !readTaskJSONMax(w, r, &p, 8<<20) {
		return
	}
	out, err := tasks.ExportBPMN(p)
	if err != nil {
		taskError(w, err)
		return
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = "process"
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"process.bpmn\"; filename*=UTF-8''"+url.PathEscape(name+".bpmn"))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}

// caseView is a case as its page shows it: the data with every field's label,
// the history, the open steps, and the forms of those that are the reader's.
func caseView(it tasks.Item, p tasks.Process, me string, people []map[string]string) map[string]any {
	// The keys in the order this case met them: its first step, the steps it
	// has been through, then the rest of the map.
	visit := []string{p.Start}
	if it.Flow != nil {
		for _, r := range it.Flow.Steps {
			visit = append(visit, r.Node)
		}
	}
	for _, n := range p.Nodes {
		visit = append(visit, n.ID)
	}
	labels := map[string]tasks.FormField{}
	order := []string{}
	seen := map[string]bool{}
	for _, id := range visit {
		if seen[id] {
			continue
		}
		seen[id] = true
		for _, f := range p.NodeByID(id).Fields {
			if _, ok := labels[f.Key]; !ok && f.Kind != "heading" {
				g := f
				g.Mode, g.Required, g.SQL = "", false, ""
				labels[f.Key] = g
				order = append(order, f.Key)
			}
		}
	}
	type openStep struct {
		ID        string            `json:"id"`
		Step      string            `json:"step"`
		Assignees []string          `json:"assignees"`
		Pool      []string          `json:"pool,omitempty"`
		Since     any               `json:"since"`
		Mine      bool              `json:"mine,omitempty"`
		Claimable bool              `json:"claimable,omitempty"`
		Node      string            `json:"node"`
		Fields    []tasks.FormField `json:"fields,omitempty"`
	}
	var open []openStep
	f := it.Flow
	for _, t := range f.Threads {
		n := p.NodeByID(t.Node)
		o := openStep{ID: t.ID, Step: n.Name, Assignees: t.Assignees, Pool: t.Pool, Since: t.Since, Node: n.ID}
		o.Mine = slices.Contains(t.Assignees, me)
		o.Claimable = len(t.Assignees) == 0 && slices.Contains(t.Pool, me)
		if o.Mine {
			o.Fields = tasks.PublicFields(n.Fields)
		}
		open = append(open, o)
	}
	if open == nil {
		open = []openStep{}
	}
	return map[string]any{
		"id": it.ID, "title": it.Title, "board_id": it.BoardID, "created": it.Created, "process": p.Name, "version": f.Version,
		"number": f.Number, "requester": f.Requester, "done": f.Done, "data": f.Data, "steps": f.Steps,
		"open": open, "fields": labels, "order": order, "people": people,
	}
}
