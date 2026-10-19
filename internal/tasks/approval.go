package tasks

import (
	"context"
	"slices"
	"time"
)

// Approvals: a quotation to the sales manager, then
// finance, then the managing director. An item's approval is a chain of
// steps; each step's approvers sign "any" (one of them is enough) or "all";
// an approver approves, rejects, asks for changes, or hands their place to a
// colleague. Everything decided is kept, and the item moves to its board's
// «تأییدشده» column when the last step signs.

// Approval is an item's sign-off.
type Approval struct {
	Steps     []Step     `json:"steps"`
	At        int        `json:"at"`    // the step waiting for a decision
	State     string     `json:"state"` // pending, approved, rejected, changes
	Due       string     `json:"due,omitempty"`
	By        string     `json:"by"` // who asked
	Started   time.Time  `json:"started"`
	Decisions []Decision `json:"decisions"`
}

// Step is one level of the chain.
type Step struct {
	Name      string   `json:"name"`
	Approvers []string `json:"approvers"`
	Mode      string   `json:"mode"` // any, all
}

// Decision is one approver's word.
type Decision struct {
	Step     int       `json:"step"`
	User     string    `json:"user"`
	Decision string    `json:"decision"` // approve, reject, changes, delegate
	Note     string    `json:"note,omitempty"`
	To       string    `json:"to,omitempty"`
	At       time.Time `json:"at"`
}

// Waiting reports whether u is one of the approvers the item waits on.
func (p *Approval) Waiting(u string) bool {
	if p == nil || p.State != "pending" || p.At >= len(p.Steps) {
		return false
	}
	if !slices.Contains(p.Steps[p.At].Approvers, u) {
		return false
	}
	return !slices.ContainsFunc(p.Decisions, func(d Decision) bool {
		return d.Step == p.At && d.User == u && d.Decision == "approve"
	})
}

// StartApproval asks for an item's sign-off: steps of approvers, each a
// member of the workspace. A running approval is replaced.
func (s *Service) StartApproval(ctx context.Context, a Actor, itemID string, steps []Step, due string) (Item, error) {
	it, _, w, err := s.Item(ctx, a, itemID)
	if err != nil {
		return it, err
	}
	if !a.edits(w) {
		return it, ErrForbidden
	}
	if len(steps) == 0 || len(steps) > 10 {
		return it, Invalid{"تأیید باید یک تا ده مرحله داشته باشد."}
	}
	if !validDay(due) {
		return it, Invalid{"تاریخ درست نیست."}
	}
	var clean []Step
	for i, st := range steps {
		us, err := users(st.Approvers, 20)
		if err != nil {
			return it, err
		}
		if len(us) == 0 {
			return it, Invalid{"مرحلهٔ " + itoa(i+1) + " تأییدکننده ندارد."}
		}
		for _, u := range us {
			if Role(w, u) == "" {
				return it, Invalid{"«" + u + "» عضو این فضای کار نیست و کار را نمی‌بیند."}
			}
		}
		if st.Mode != "all" {
			st.Mode = "any"
		}
		st.Name = oneLine(st.Name, 60)
		if st.Name == "" {
			st.Name = "مرحلهٔ " + itoa(i+1)
		}
		st.Approvers = us
		clean = append(clean, st)
	}
	it.Approval = &Approval{Steps: clean, State: "pending", Due: due, By: a.User, Started: s.now(), Decisions: []Decision{}}
	it.Updated, it.UpdatedBy = s.now(), a.User
	saved, err := s.Store.SaveItem(ctx, it)
	if err != nil {
		return saved, err
	}
	s.log(ctx, saved, a, "approval", "درخواست تأیید در "+itoa(len(clean))+" مرحله")
	s.tell(ctx, clean[0].Approvers, a.User, "درخواست تأیید: «"+saved.Title+"»", clean[0].Name, ItemLink(saved))
	return saved, nil
}

// Decide is one approver's decision on the step the item waits on.
func (s *Service) Decide(ctx context.Context, a Actor, itemID, decision, note, to string) (Item, error) {
	it, b, w, err := s.Item(ctx, a, itemID)
	if err != nil {
		return it, err
	}
	p := it.Approval
	if p == nil || p.State != "pending" {
		return it, Invalid{"این کار تأیید در انتظاری ندارد."}
	}
	if !p.Waiting(a.User) {
		return it, ErrForbidden
	}
	note = clipText(note, 2000)
	st := &p.Steps[p.At]
	d := Decision{Step: p.At, User: a.User, Decision: decision, Note: note, At: s.now()}
	link := ItemLink(it)
	switch decision {
	case "approve":
		p.Decisions = append(p.Decisions, d)
		done := st.Mode == "any"
		if st.Mode == "all" {
			done = true
			for _, u := range st.Approvers {
				if !slices.ContainsFunc(p.Decisions, func(x Decision) bool { return x.Step == p.At && x.User == u && x.Decision == "approve" }) {
					done = false
				}
			}
		}
		if done {
			p.At++
			if p.At >= len(p.Steps) {
				p.State = "approved"
				if slices.ContainsFunc(b.Statuses, func(s Status) bool { return s.Key == "approved" }) && !isDone(b, it.Status) {
					it.Status = "approved"
				}
				s.tell(ctx, []string{p.By, it.Creator}, a.User, "تأیید شد: «"+it.Title+"»", note, link)
			} else {
				next := p.Steps[p.At]
				s.tell(ctx, next.Approvers, a.User, "درخواست تأیید: «"+it.Title+"»", next.Name, link)
			}
		}
	case "reject":
		p.Decisions = append(p.Decisions, d)
		p.State = "rejected"
		s.tell(ctx, []string{p.By, it.Creator}, a.User, "رد شد: «"+it.Title+"»", note, link)
	case "changes":
		p.Decisions = append(p.Decisions, d)
		p.State = "changes"
		s.tell(ctx, []string{p.By, it.Creator}, a.User, "نیاز به اصلاح: «"+it.Title+"»", note, link)
	case "delegate":
		us, err := users([]string{to}, 1)
		if err != nil || len(us) == 0 || us[0] == a.User {
			return it, Invalid{"به چه کسی سپرده شود؟"}
		}
		if Role(w, us[0]) == "" {
			return it, Invalid{"«" + us[0] + "» عضو این فضای کار نیست."}
		}
		d.To = us[0]
		p.Decisions = append(p.Decisions, d)
		i := slices.Index(st.Approvers, a.User)
		st.Approvers[i] = us[0]
		st.Approvers = slices.Compact(st.Approvers)
		s.tell(ctx, us, a.User, "درخواست تأیید (سپرده‌شده): «"+it.Title+"»", note, link)
	default:
		return it, Invalid{"تصمیم شناخته نیست."}
	}
	it.Updated, it.UpdatedBy = s.now(), a.User
	saved, err := s.Store.SaveItem(ctx, it)
	if err != nil {
		return saved, err
	}
	if saved.Approval.State == "approved" {
		s.webhook(ctx, b, "approved", saved)
	}
	words := map[string]string{"approve": "تأیید کرد", "reject": "رد کرد", "changes": "اصلاح خواست", "delegate": "به " + to + " سپرد"}[decision]
	s.log(ctx, saved, a, "approval", "تأیید، مرحلهٔ «"+st.Name+"»: "+words)
	if note != "" {
		_ = s.Store.AddComment(ctx, Comment{ID: NewID(), ItemID: saved.ID, Author: a.User, Body: note, Kind: "approval",
			Mentions: []string{}, Created: s.now()})
	}
	return saved, nil
}

// MyApprovals is what waits on a's decision, on the boards a can see.
func (s *Service) MyApprovals(ctx context.Context, a Actor) ([]Item, map[string]Board, error) {
	boards, err := s.visibleBoards(ctx, a)
	if err != nil || len(boards) == 0 {
		return []Item{}, boards, err
	}
	ids := make([]string, 0, len(boards))
	for id := range boards {
		ids = append(ids, id)
	}
	items, err := s.Store.Items(ctx, Query{BoardIDs: ids})
	if err != nil {
		return nil, nil, err
	}
	out := []Item{}
	for _, it := range items {
		if it.Approval.Waiting(a.User) {
			out = append(out, it)
		}
	}
	return out, boards, nil
}
