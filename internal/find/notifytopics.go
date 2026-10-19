package find

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/Schayan-nk/docfind-tasks/internal/auth"
	"github.com/Schayan-nk/docfind-tasks/internal/notify"
)

// What a person can choose to be told about.
//
// Everything is on until someone switches it off, so a new topic reaches
// everybody the day it ships and a person who never opens this card loses
// nothing. The choice is per person, not per device: a phone and a laptop
// that both buzz for a new item are one decision, made once.
//
// Switching a topic off stops its notices, not the work: what happened is
// still on the board, and switching it back on brings what happens from
// then on, not a backlog of everything missed.

// topicTasks is the task manager's: an item given to you, your name in a
// comment, an item you made or follow moved.
const topicTasks = "tasks"

type notifyTopic struct {
	ID, Label, Hint string
}

var notifyTopics = []notifyTopic{
	{topicTasks, "کارهای من", "کاری که به شما سپرده شد، نام شما در یادداشتی، و جابه‌جایی کاری که شما ساخته‌اید یا دنبالش می‌کنید."},
}

// quieten makes n arrive without sound when now is inside u's quiet hours.
// It is still sent and still listed: quiet hours stop a phone buzzing at
// night, they do not hide what happened.
func quieten(u *auth.User, n notify.Notification, now time.Time) notify.Notification {
	if u.Prefs.Quiet(now) {
		n.Quiet = true
	}
	return n
}

// enqueueFor queues n for u and returns how many devices it reached: none
// when the same notice was queued already.
func (s *Server) enqueueFor(ctx context.Context, u *auth.User, now time.Time, n notify.Notification) int {
	n = quieten(u, n, now)
	n.Username = u.Username
	_, created, devices, err := s.push.store.Enqueue(ctx, n)
	if err != nil {
		logReportError("notification enqueue", err)
		return 0
	}
	if !created {
		return 0
	}
	return devices
}

// topicReaches reports whether anything under topic can ever be sent to u:
// a person a topic cannot reach gets nothing from it, whatever they choose.
func (s *Server) topicReaches(u *auth.User, id string) bool {
	if id == topicTasks {
		return s.tasks != nil && s.userCan(u, auth.PageTasks)
	}
	// The task manager is the only thing here that notifies.
	return false
}

// handlePushTopics lists the topics and whether each is on for the caller.
func (s *Server) handlePushTopics(w http.ResponseWriter, r *http.Request) {
	if s.pushOff(w) {
		return
	}
	s.writeTopics(w, auth.UserFrom(r), auth.UserFrom(r).Prefs)
}

// handleSetPushTopics replaces the caller's switched-off topics. The body
// names every topic that is off, {"off": [...]}; a name that is not a topic
// is refused rather than stored, so a typo cannot silently mute nothing.
func (s *Server) handleSetPushTopics(w http.ResponseWriter, r *http.Request) {
	if s.pushOff(w) {
		return
	}
	var req struct {
		Off []string `json:"off"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, `{"error":"درخواست نامعتبر است."}`)
		return
	}
	for _, id := range req.Off {
		if !slices.ContainsFunc(notifyTopics, func(t notifyTopic) bool { return t.ID == id }) {
			writeJSONError(w, http.StatusBadRequest, `{"error":"موضوع اعلان ناشناخته است."}`)
			return
		}
	}
	u := auth.UserFrom(r)
	prefs, err := s.guard.Users.SetMuted(u.Username, req.Off)
	if err != nil {
		writeUserError(w, err)
		return
	}
	s.writeTopics(w, u, prefs)
}

func (s *Server) writeTopics(w http.ResponseWriter, u *auth.User, p auth.Prefs) {
	type item struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		Hint      string `json:"hint"`
		On        bool   `json:"on"`
		Available bool   `json:"available"`
		SMS       bool   `json:"sms"`
	}
	out := make([]item, 0, len(notifyTopics))
	for _, t := range notifyTopics {
		out = append(out, item{t.ID, t.Label, t.Hint, !p.MutedTopic(t.ID), s.topicReaches(u, t.ID), p.SMSTopic(t.ID)})
	}
	phone := ""
	if cur, err := s.guard.Users.Get(u.Username); err == nil {
		phone = cur.Phone
	}
	writeJSON(w, map[string]any{"topics": out, "quiet": map[string]int{"from": p.QuietFrom, "to": p.QuietTo},
		// Whether a topic can be asked for by SMS at all: a panel, and a phone.
		"sms_available": s.smsConfig().Enabled() && phone != "", "phone": maskPhone(phone)})
}

// handleSetPushQuiet sets the caller's quiet hours: {"from": 22, "to": 7}.
// Equal hours switch them off.
func (s *Server) handleSetPushQuiet(w http.ResponseWriter, r *http.Request) {
	if s.pushOff(w) {
		return
	}
	var req struct {
		From *int `json:"from"`
		To   *int `json:"to"`
	}
	if err := readJSON(r, &req); err != nil || req.From == nil || req.To == nil {
		writeJSONError(w, http.StatusBadRequest, `{"error":"درخواست نامعتبر است."}`)
		return
	}
	u := auth.UserFrom(r)
	prefs, err := s.guard.Users.SetQuiet(u.Username, *req.From, *req.To)
	if errors.Is(err, auth.ErrQuietHours) {
		writeJSONError(w, http.StatusBadRequest, `{"error":"ساعت آرام باید عددی از ۰ تا ۲۳ باشد."}`)
		return
	}
	if err != nil {
		writeUserError(w, err)
		return
	}
	s.writeTopics(w, u, prefs)
}

// userCan is the permission decision for a person outside any request.
func (s *Server) userCan(u *auth.User, id string) bool {
	return auth.Can(s.perms.Load(), u, id)
}
