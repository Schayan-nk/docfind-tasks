package find

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Schayan-nk/docfind-tasks/internal/audit"
	"github.com/Schayan-nk/docfind-tasks/internal/auth"
	"github.com/Schayan-nk/docfind-tasks/internal/notify"
	"github.com/Schayan-nk/docfind-tasks/internal/tasks"
)

// The task manager's routes (internal/tasks holds the model and every rule;
// tasks.js is the page). All behind page:tasks; within it, what a person may
// see and change is their workspaces' business, decided by tasks.Service --
// a board somebody is not in answers as if it did not exist.

// UseTasks turns the task manager on over a store (PostgreSQL in docfind).
func (s *Server) UseTasks(st tasks.Store) {
	first := s.tasks == nil
	s.tasks = &tasks.Service{Store: st, Notify: s.notifyTasks, Hook: s.postTaskHook,
		People: s.isActiveUser, Org: deptOrg{s}, Data: fieldData{s}}
	if s.taskFiles != nil {
		s.tasks.Files = s.taskFiles
	}
	if first {
		go s.taskSweep()
	}
}

// TasksEnabled reports whether the task manager has somewhere to keep things.
func (s *Server) TasksEnabled() bool { return s.tasks != nil }

func (s *Server) taskActor(r *http.Request) tasks.Actor {
	u := auth.UserFrom(r)
	if u == nil {
		// Only when accounts are off altogether: the one person there is.
		return tasks.Actor{User: "local", Admin: true}
	}
	return tasks.Actor{User: strings.ToLower(u.Username), Admin: u.Role == auth.RoleAdmin}
}

// tasksOff answers for a server with no task manager.
func (s *Server) tasksOff(w http.ResponseWriter) bool {
	if s.tasks == nil {
		writeJSONError(w, http.StatusNotFound, `{"error":"مدیریت کارها به پایگاه دادهٔ PostgreSQL نیاز دارد که روی این سرور وصل نیست."}`)
		return true
	}
	return false
}

// taskError answers an error from tasks.Service in the page's language.
func taskError(w http.ResponseWriter, err error) {
	var inv tasks.Invalid
	switch {
	case errors.As(err, &inv):
		writeJSONError(w, http.StatusBadRequest, mustJSON(map[string]string{"error": inv.Msg}))
	case tasks.IsNotFound(err):
		writeJSONError(w, http.StatusNotFound, `{"error":"پیدا نشد."}`)
	case tasks.IsForbidden(err):
		writeJSONError(w, http.StatusForbidden, `{"error":"برای این کار دسترسی ندارید."}`)
	default:
		logReportError("tasks", err)
		writeJSONError(w, http.StatusBadGateway, `{"error":"انجام نشد؛ شرح در لاگ سرور است."}`)
	}
}

func readTaskJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return readTaskJSONMax(w, r, v, 256<<10)
}

// readTaskJSONMax is readTaskJSON for a bigger body: a process map.
func readTaskJSONMax(w http.ResponseWriter, r *http.Request, v any, max int64) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, max)).Decode(v); err != nil {
		writeJSONError(w, http.StatusBadRequest, `{"error":"درخواست نامعتبر است."}`)
		return false
	}
	return true
}

// taskPerson is a colleague who can be put on a workspace or an item.
type taskPerson struct {
	User string `json:"user"`
	Name string `json:"name,omitempty"`
}

// taskPeople is everybody who holds the task manager, by name: who can be
// made a member or given an item.
func (s *Server) taskPeople() []taskPerson {
	out := []taskPerson{}
	if s.guard == nil || s.guard.Users == nil {
		return out
	}
	for _, u := range s.guard.Users.List() {
		if u.Disabled || !s.userCan(u, auth.PageTasks) {
			continue
		}
		out = append(out, taskPerson{User: strings.ToLower(u.Username), Name: u.DisplayName})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].User < out[j].User })
	return out
}

// handleTasksHome is GET /api/tasks: the workspaces this person can see with
// their boards and their role in each, and the colleagues' names.
func (s *Server) handleTasksHome(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	a := s.taskActor(r)
	ws, err := s.tasks.Workspaces(r.Context(), a)
	if err != nil {
		taskError(w, err)
		return
	}
	type withBoards struct {
		tasks.Workspace
		Role   string        `json:"role"`
		Boards []tasks.Board `json:"boards"`
	}
	out := []withBoards{}
	for _, x := range ws {
		bs, err := s.tasks.Boards(r.Context(), a, x.ID)
		if err != nil {
			taskError(w, err)
			return
		}
		role := tasks.Role(x, a.User)
		if a.Admin && role == "" {
			role = "owner"
		}
		for i := range bs {
			bs[i] = redactBoard(bs[i])
		}
		out = append(out, withBoards{x, role, bs})
	}
	writeJSON(w, map[string]any{"me": a.User, "admin": a.Admin, "workspaces": out, "people": s.taskPeople(),
		"departments": s.taskDepartments(r)})
}

// taskDepartments are the departments a form's requests can go to: their
// names and ids, nothing about who is in them.
func (s *Server) taskDepartments(r *http.Request) []map[string]any {
	out := []map[string]any{}
	if s.departments == nil {
		return out
	}
	deps, err := s.departments.List(r.Context())
	if err != nil {
		return out
	}
	for _, d := range deps {
		if d.Active {
			out = append(out, map[string]any{"id": d.ID, "name": d.Name})
		}
	}
	return out
}

func (s *Server) handleTaskWorkspaceSave(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var ws tasks.Workspace
	if !readTaskJSON(w, r, &ws) {
		return
	}
	saved, err := s.tasks.SaveWorkspace(r.Context(), s.taskActor(r), ws)
	if err != nil {
		taskError(w, err)
		return
	}
	audit.Note(r.Context(), "tasks:workspace:"+saved.ID, map[string]any{"name": saved.Name, "members": len(saved.Members)})
	writeJSON(w, saved)
}

func (s *Server) handleTaskWorkspaceDelete(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	id := r.PathValue("id")
	if err := s.tasks.DeleteWorkspace(r.Context(), s.taskActor(r), id); err != nil {
		taskError(w, err)
		return
	}
	audit.Note(r.Context(), "tasks:workspace:"+id, map[string]any{"deleted": true})
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleTaskBoardSave(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var b tasks.Board
	if !readTaskJSON(w, r, &b) {
		return
	}
	saved, err := s.tasks.SaveBoard(r.Context(), s.taskActor(r), b)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, redactBoard(saved))
}

func (s *Server) handleTaskBoardDelete(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	id := r.PathValue("id")
	if err := s.tasks.DeleteBoard(r.Context(), s.taskActor(r), id); err != nil {
		taskError(w, err)
		return
	}
	audit.Note(r.Context(), "tasks:board:"+id, map[string]any{"deleted": true})
	writeJSON(w, map[string]any{"ok": true})
}

// handleTaskBoard is GET /api/tasks/boards/{id}: the board, its workspace,
// the caller's role there, and every item on it.
func (s *Server) handleTaskBoard(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	a := s.taskActor(r)
	b, ws, err := s.tasks.Board(r.Context(), a, r.PathValue("id"))
	if err != nil {
		taskError(w, err)
		return
	}
	items, err := s.tasks.Items(r.Context(), a, b.ID)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, map[string]any{"board": redactBoard(b), "buttons": tasks.ButtonsOf(b), "workspace": ws,
		"role": roleFor(ws, a), "items": items})
}

func roleFor(ws tasks.Workspace, a tasks.Actor) string {
	if r := tasks.Role(ws, a.User); r != "" || !a.Admin {
		return r
	}
	return "owner"
}

func (s *Server) handleTaskBoardActivity(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	log, err := s.tasks.Activity(r.Context(), s.taskActor(r), r.PathValue("id"), "", 300)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, map[string]any{"activity": log})
}

// handleTaskItemSave is POST /api/tasks/items: a new item, or a change to one
// at the version the page read. A newer stored version is answered 409 with
// that version, so the page can show it instead of overwriting it.
func (s *Server) handleTaskItemSave(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var it tasks.Item
	if !readTaskJSON(w, r, &it) {
		return
	}
	a := s.taskActor(r)
	saved, err := s.tasks.SaveItem(r.Context(), a, it)
	if errors.Is(err, tasks.ErrConflict) {
		cur, _, _, gerr := s.tasks.Item(r.Context(), a, it.ID)
		body := map[string]any{"error": "کسی دیگر همین کار را تغییر داده بود."}
		if gerr == nil {
			body["current"] = cur
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(body)
		return
	}
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, saved)
}

func (s *Server) handleTaskItemDelete(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	if err := s.tasks.DeleteItem(r.Context(), s.taskActor(r), r.PathValue("id")); err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleTaskItem is GET /api/tasks/items/{id}: the item with its board, the
// board's other items (for subtasks and dependencies), its updates and its
// history.
func (s *Server) handleTaskItem(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	a := s.taskActor(r)
	it, b, ws, err := s.tasks.Item(r.Context(), a, r.PathValue("id"))
	if err != nil {
		taskError(w, err)
		return
	}
	items, err := s.tasks.Items(r.Context(), a, b.ID)
	if err != nil {
		taskError(w, err)
		return
	}
	comments, err := s.tasks.Comments(r.Context(), a, it.ID)
	if err != nil {
		taskError(w, err)
		return
	}
	log, err := s.tasks.Activity(r.Context(), a, "", it.ID, 200)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, map[string]any{"item": it, "board": redactBoard(b), "buttons": tasks.ButtonsOf(b), "workspace": ws,
		"role": roleFor(ws, a), "items": items, "comments": comments, "activity": log})
}

func (s *Server) handleTaskComment(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var req struct {
		Body string `json:"body"`
		Kind string `json:"kind"`
	}
	if !readTaskJSON(w, r, &req) {
		return
	}
	c, err := s.tasks.AddComment(r.Context(), s.taskActor(r), r.PathValue("id"), req.Body, req.Kind)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, c)
}

// handleTaskMine is GET /api/tasks/my: everything assigned to the caller and
// not finished, with the names and columns of the boards it is on.
func (s *Server) handleTaskMine(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	items, boards, err := s.tasks.MyWork(r.Context(), s.taskActor(r))
	if err != nil {
		taskError(w, err)
		return
	}
	type boardName struct {
		Name     string         `json:"name"`
		Statuses []tasks.Status `json:"statuses"`
	}
	names := map[string]boardName{}
	for _, it := range items {
		if b, ok := boards[it.BoardID]; ok {
			names[b.ID] = boardName{b.Name, b.Statuses}
		}
	}
	writeJSON(w, map[string]any{"items": items, "boards": names})
}

// handleTaskSearch is GET /api/tasks/search?q=: the palette's «کارها» group.
func (s *Server) handleTaskSearch(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 || len(q) > 200 {
		writeJSON(w, map[string]any{"items": []any{}})
		return
	}
	items, err := s.tasks.Search(r.Context(), s.taskActor(r), q, 8)
	if err != nil {
		taskError(w, err)
		return
	}
	type hit struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Due   string `json:"due,omitempty"`
	}
	out := []hit{}
	for _, it := range items {
		out = append(out, hit{it.ID, it.Title, it.Due})
	}
	writeJSON(w, map[string]any{"items": out})
}

// notifyTasks sends a task notice through the notification centre, to each
// person who has not switched «کارهای من» off: pushed to their devices.
func (s *Server) notifyTasks(ctx context.Context, n tasks.Notice) {
	if !s.NotificationsEnabled() {
		return
	}
	ctx = context.WithoutCancel(ctx)
	now := time.Now()
	queued := 0
	for _, name := range n.To {
		u := s.accountFor(name)
		if u == nil || u.Disabled || u.Prefs.MutedTopic(topicTasks) || !s.userCan(u, auth.PageTasks) {
			continue
		}
		queued += s.enqueueFor(ctx, u, now, notify.Notification{
			Kind: topicTasks, DedupeKey: "task:" + n.Link + ":" + n.Title + ":" + now.Format(time.RFC3339Nano),
			Title: n.Title, Body: n.Body, URL: n.Link, Tag: "task-" + strings.TrimPrefix(n.Link, "/tasks?item="),
		})
	}
	if queued > 0 {
		s.push.dispatcher.Wake()
	}
}

// The task manager's pages and scripts.

//go:embed tasks.html
var tasksHTML []byte

//go:embed tasks.js
var tasksJS []byte

// A process's inbox, cases and forms on the same page (taskflow.go).
//
//go:embed taskflow.js
var taskFlowJS []byte

// The process designer: the map of a board's process and its editor.
//
//go:embed processedit.js
var processEditJS []byte

//go:embed processmap.js
var processMapJS []byte

// The expression language calculated fields and conditions are written in.
//
//go:embed expr.js
var exprJS []byte

// The form maker.
//
//go:embed formmaker.js
var formMakerJS []byte

// routeTasks registers the task manager: its page and scripts, and its API.
// Within page:tasks, each workspace's members decide the rest
// (internal/tasks); a process's steps reach every signed-in person they are
// assigned to, and the service decides the rest.
func (s *Server) routeTasks(mux *http.ServeMux) {
	mux.HandleFunc("GET /tasks", s.signedInPage(s.handlePage(injectAssets(tasksHTML))))
	mux.HandleFunc("GET /tasks.js", s.handleAsset(tasksJS, "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /taskflow.js", s.handleAsset(taskFlowJS, "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /processedit.js", s.handleAsset(processEditJS, "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /processmap.js", s.handleAsset(processMapJS, "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /expr.js", s.handleAsset(exprJS, "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /formmaker.js", s.handleAsset(formMakerJS, "text/javascript; charset=utf-8"))

	mux.HandleFunc("GET /api/tasks", s.requirePermission(auth.PageTasks, s.handleTasksHome))
	mux.HandleFunc("GET /api/tasks/flow", s.guard.RequireAuth(s.handleFlowHome))
	mux.HandleFunc("GET /api/tasks/flow/boards/{board}/start", s.guard.RequireAuth(s.handleFlowStartForm))
	mux.HandleFunc("POST /api/tasks/flow/boards/{board}/cases", s.mutating(s.guard.RequireAuth(s.handleFlowStart)))
	mux.HandleFunc("GET /api/tasks/flow/cases/{id}", s.guard.RequireAuth(s.handleFlowCase))
	mux.HandleFunc("POST /api/tasks/flow/cases/{id}/steps/{thread}", s.mutating(s.guard.RequireAuth(s.handleFlowStep)))
	mux.HandleFunc("POST /api/tasks/flow/cases/{id}/claim/{thread}", s.mutating(s.guard.RequireAuth(s.handleFlowClaim)))
	mux.HandleFunc("GET /api/tasks/boards/{id}/process", s.requirePermission(auth.PageTasks, s.handleProcessGet))
	mux.HandleFunc("PUT /api/tasks/boards/{id}/process", s.mutating(s.requirePermission(auth.PageTasks, s.handleProcessSave)))
	mux.HandleFunc("POST /api/tasks/process/bpmn/import", s.mutating(s.requirePermission(auth.PageTasks, s.handleProcessBPMNImport)))
	mux.HandleFunc("POST /api/tasks/process/bpmn/export", s.mutating(s.requirePermission(auth.PageTasks, s.handleProcessBPMNExport)))
	mux.HandleFunc("POST /api/tasks/fielddata", s.mutating(s.guard.RequireAuth(s.handleFieldData)))
	mux.HandleFunc("POST /api/tasks/files", s.mutating(s.guard.RequireAuth(s.handleTaskFileUpload)))
	mux.HandleFunc("GET /api/tasks/files/{id}", s.guard.RequireAuth(s.handleTaskFile))
	mux.HandleFunc("GET /api/tasks/fielddata/sources", s.requirePermission(auth.PageTasks, s.handleFieldSources))
	mux.HandleFunc("POST /api/tasks/fielddata/test", s.mutating(s.requirePermission(auth.PageTasks, s.handleFieldTest)))
	mux.HandleFunc("GET /api/tasks/my", s.requirePermission(auth.PageTasks, s.handleTaskMine))
	mux.HandleFunc("GET /api/tasks/search", s.requirePermission(auth.PageTasks, s.handleTaskSearch))
	mux.HandleFunc("POST /api/tasks/workspaces", s.mutating(s.requirePermission(auth.PageTasks, s.handleTaskWorkspaceSave)))
	mux.HandleFunc("DELETE /api/tasks/workspaces/{id}", s.mutating(s.requirePermission(auth.PageTasks, s.handleTaskWorkspaceDelete)))
	mux.HandleFunc("POST /api/tasks/boards", s.mutating(s.requirePermission(auth.PageTasks, s.handleTaskBoardSave)))
	mux.HandleFunc("GET /api/tasks/boards/{id}", s.requirePermission(auth.PageTasks, s.handleTaskBoard))
	mux.HandleFunc("GET /api/tasks/boards/{id}/activity", s.requirePermission(auth.PageTasks, s.handleTaskBoardActivity))
	mux.HandleFunc("DELETE /api/tasks/boards/{id}", s.mutating(s.requirePermission(auth.PageTasks, s.handleTaskBoardDelete)))
	mux.HandleFunc("POST /api/tasks/items", s.mutating(s.requirePermission(auth.PageTasks, s.handleTaskItemSave)))
	mux.HandleFunc("GET /api/tasks/items/{id}", s.requirePermission(auth.PageTasks, s.handleTaskItem))
	mux.HandleFunc("DELETE /api/tasks/items/{id}", s.mutating(s.requirePermission(auth.PageTasks, s.handleTaskItemDelete)))
	mux.HandleFunc("POST /api/tasks/items/{id}/comments", s.mutating(s.requirePermission(auth.PageTasks, s.handleTaskComment)))
	// Its second half (tasksroutes2.go): buttons, time, approvals, forms,
	// templates and the dashboard.
	mux.HandleFunc("POST /api/tasks/items/{id}/press", s.mutating(s.requirePermission(auth.PageTasks, s.handleTaskPress)))
	mux.HandleFunc("POST /api/tasks/items/{id}/timer", s.mutating(s.requirePermission(auth.PageTasks, s.handleTaskTimer)))
	mux.HandleFunc("POST /api/tasks/items/{id}/approval", s.mutating(s.requirePermission(auth.PageTasks, s.handleTaskApprovalStart)))
	mux.HandleFunc("POST /api/tasks/items/{id}/decide", s.mutating(s.requirePermission(auth.PageTasks, s.handleTaskDecide)))
	mux.HandleFunc("GET /api/tasks/timesheet", s.requirePermission(auth.PageTasks, s.handleTaskTimesheet))
	mux.HandleFunc("GET /api/tasks/approvals", s.requirePermission(auth.PageTasks, s.handleTaskApprovals))
	mux.HandleFunc("GET /api/tasks/forms", s.requirePermission(auth.PageTasks, s.handleTaskForms))
	mux.HandleFunc("POST /api/tasks/forms/{board}/{form}", s.mutating(s.requirePermission(auth.PageTasks, s.handleTaskFormSubmit)))
	mux.HandleFunc("GET /api/tasks/boards/{id}/forms/{form}/answers", s.requirePermission(auth.PageTasks, s.handleTaskFormAnswers))
	mux.HandleFunc("GET /api/tasks/templates", s.requirePermission(auth.PageTasks, s.handleTaskTemplates))
	mux.HandleFunc("POST /api/tasks/boards/from-template", s.mutating(s.requirePermission(auth.PageTasks, s.handleTaskFromTemplate)))
	mux.HandleFunc("GET /api/tasks/dashboard", s.requirePermission(auth.PageTasks, s.handleTaskDashboard))
}

// allowedHostsFromEnv reads DOCFIND_ALLOWED_HOSTS: hosts a board's webhook
// may reach whatever they resolve to, comma separated.
func allowedHostsFromEnv() []string {
	var out []string
	for _, h := range strings.Split(os.Getenv("DOCFIND_ALLOWED_HOSTS"), ",") {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// auditBPM records a change made through the task manager or the departments.
func (s *Server) auditBPM(actor, action, target string, detail map[string]any) {
	if s.audit == nil {
		return
	}
	s.audit.Record(context.Background(), audit.Event{
		Actor: actor, Action: action, Target: target, Result: "ok", Detail: detail,
	})
}
