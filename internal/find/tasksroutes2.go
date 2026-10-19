package find

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Schayan-nk/docfind-tasks/internal/netguard"
	"github.com/Schayan-nk/docfind-tasks/internal/tasks"
)

// The task manager's second half: buttons, timers and timesheets, approvals,
// forms, templates, the dashboard, and the board's webhooks and clock
// (tasksroutes.go is the first). Everything goes through tasks.Service, which
// decides who may.

// redactBoard is a board as a page may see it: a webhook's secret stays on
// the server, and of a process only its name and version -- a large map runs
// to hundreds of fields, which only its owner's GET .../process needs, and the
// older versions nobody's page does.
func redactBoard(b tasks.Board) tasks.Board {
	hooks := make([]tasks.Webhook, len(b.Webhooks))
	for i, h := range b.Webhooks {
		h.Secret = ""
		hooks[i] = h
	}
	b.Webhooks = hooks
	if b.Process != nil {
		b.Process = &tasks.Process{Name: b.Process.Name, Version: b.Process.Version, Source: b.Process.Source}
	}
	b.ProcessHistory = nil
	return b
}

func (s *Server) handleTaskPress(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var req struct {
		Button string `json:"button"`
	}
	if !readTaskJSON(w, r, &req) {
		return
	}
	it, err := s.tasks.Press(r.Context(), s.taskActor(r), r.PathValue("id"), req.Button)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, it)
}

func (s *Server) handleTaskTimer(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var req struct {
		Action  string `json:"action"`
		Minutes int    `json:"minutes"`
		Note    string `json:"note"`
	}
	if !readTaskJSON(w, r, &req) {
		return
	}
	it, err := s.tasks.Timer(r.Context(), s.taskActor(r), r.PathValue("id"), req.Action, req.Minutes, req.Note)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, it)
}

var dayShape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (s *Server) handleTaskTimesheet(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if (from != "" && !dayShape.MatchString(from)) || (to != "" && !dayShape.MatchString(to)) {
		writeJSONError(w, http.StatusBadRequest, `{"error":"تاریخ درست نیست."}`)
		return
	}
	rows, err := s.tasks.Timesheet(r.Context(), s.taskActor(r), from, to)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, map[string]any{"rows": rows})
}

func (s *Server) handleTaskApprovalStart(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var req struct {
		Steps []tasks.Step `json:"steps"`
		Due   string       `json:"due"`
	}
	if !readTaskJSON(w, r, &req) {
		return
	}
	it, err := s.tasks.StartApproval(r.Context(), s.taskActor(r), r.PathValue("id"), req.Steps, req.Due)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, it)
}

func (s *Server) handleTaskDecide(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var req struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
		To       string `json:"to"`
	}
	if !readTaskJSON(w, r, &req) {
		return
	}
	it, err := s.tasks.Decide(r.Context(), s.taskActor(r), r.PathValue("id"), req.Decision, req.Note, req.To)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, it)
}

func (s *Server) handleTaskApprovals(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	items, boards, err := s.tasks.MyApprovals(r.Context(), s.taskActor(r))
	if err != nil {
		taskError(w, err)
		return
	}
	names := map[string]string{}
	for _, it := range items {
		names[it.BoardID] = boards[it.BoardID].Name
	}
	writeJSON(w, map[string]any{"items": items, "boards": names})
}

func (s *Server) handleTaskForms(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	forms, err := s.tasks.Forms(r.Context(), s.taskActor(r))
	if err != nil {
		taskError(w, err)
		return
	}
	// Filling a form needs what its database questions depend on, not their SQL.
	for i := range forms {
		forms[i].Form.Fields = tasks.PublicFields(forms[i].Form.Fields)
	}
	writeJSON(w, map[string]any{"forms": forms})
}

func (s *Server) handleTaskFormSubmit(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var req struct {
		Answers map[string]any `json:"answers"`
	}
	if !readTaskJSON(w, r, &req) {
		return
	}
	it, err := s.tasks.Submit(r.Context(), s.taskActor(r), r.PathValue("board"), r.PathValue("form"), req.Answers)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": it.ID, "title": it.Title})
}

func (s *Server) handleTaskTemplates(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	refs, err := s.tasks.Templates(r.Context(), s.taskActor(r))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, map[string]any{"templates": refs})
}

func (s *Server) handleTaskFromTemplate(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	var req struct {
		Workspace string `json:"workspace_id"`
		Template  string `json:"template"`
		Name      string `json:"name"`
	}
	if !readTaskJSON(w, r, &req) {
		return
	}
	b, err := s.tasks.FromTemplate(r.Context(), s.taskActor(r), req.Workspace, req.Template, req.Name)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, redactBoard(b))
}

func (s *Server) handleTaskDashboard(w http.ResponseWriter, r *http.Request) {
	if s.tasksOff(w) {
		return
	}
	d, err := s.tasks.Dashboard(r.Context(), s.taskActor(r))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, d)
}

// postTaskHook posts a board event to one webhook, in the background: signed
// with the webhook's secret (X-Docfind-Signature: sha256=<hex of the body's
// HMAC>), and never to this machine, a link-local address (the cloud metadata
// service) or a multicast one, unless the host is named in
// DOCFIND_ALLOWED_HOSTS -- a board's settings must not become a way to make
// this server call itself. The internet and the office network are allowed
// (netguard.LAN).
func (s *Server) postTaskHook(_ context.Context, h tasks.Webhook, event string, body []byte) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		u, err := url.Parse(h.URL)
		if err != nil || u.Hostname() == "" {
			return
		}
		if err := netguard.CheckHost(ctx, u.Hostname(), netguard.LAN, s.allowedHosts...); err != nil {
			logReportError("tasks webhook "+u.Hostname(), err)
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Docfind-Event", event)
		if h.Secret != "" {
			mac := hmac.New(sha256.New, []byte(h.Secret))
			mac.Write(body)
			req.Header.Set("X-Docfind-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		// Checked again at the dial, so a name that resolves to the office
		// network when checked cannot resolve elsewhere when connected -- for
		// every host but one an administrator named, which may be outside.
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		if !slices.ContainsFunc(s.allowedHosts, func(a string) bool { return strings.EqualFold(a, u.Hostname()) }) {
			dialer.Control = netguard.Control(netguard.LAN)
		}
		client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DialContext: dialer.DialContext},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(req)
		if err != nil {
			logReportError("tasks webhook "+u.Hostname(), err)
			return
		}
		resp.Body.Close()
	}()
}

// taskSweep runs the task manager's clock every ten minutes: reminders the
// day before and the day after a due day, and the time-based rules.
func (s *Server) taskSweep() {
	time.Sleep(time.Minute)
	for {
		if s.tasks != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			s.tasks.Sweep(ctx, time.Now())
			cancel()
		}
		time.Sleep(10 * time.Minute)
	}
}
