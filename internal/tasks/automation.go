package tasks

import (
	"context"
	"slices"
	"strings"
	"time"
)

// Automations and quick-action buttons.
//
// A Rule is IF -> THEN on one board: when something happens to an item
// (When), and the item is so (If), do these (Then) -- "when an inquiry lands
// in New, assign it to the sales team, add the technical review, due in two
// days, and tell the manager". A Button is the same actions, pressed by a
// person on one item: Start, Complete, Escalate.
//
// What a rule does is done as the automation, in one save, and what a rule
// changes does not fire rules again: a rule that moves an item cannot start
// a chain that moves it back. The time-based triggers -- due soon, overdue --
// are Sweep's, which runs on the server's clock and fires each once per item
// and due day.

// Rule is one IF -> THEN.
type Rule struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Enabled bool     `json:"enabled"`
	When    Trigger  `json:"when"`
	If      []Cond   `json:"if"`
	Then    []Action `json:"then"`
}

// Trigger is what starts a rule: an item created, moved (into Status, or
// anywhere), given an assignee, commented on, due in Days, or overdue.
type Trigger struct {
	Kind   string `json:"kind"` // created, moved, assigned, commented, due_soon, overdue
	Status string `json:"status,omitempty"`
	Days   int    `json:"days,omitempty"`
}

// Cond is one condition on the item: its priority, a label, an assignee, its
// column, its group, or its title, compared with Value.
type Cond struct {
	Field string `json:"field"` // priority, label, assignee, status, group, title
	Op    string `json:"op"`    // is, is_not, has, lacks, contains
	Value string `json:"value"`
}

// Action is one thing to do. Users may name people or these: @creator,
// @assignees, @watchers, @owners (the workspace's), @actor (who pressed or
// changed it). Title and Value may hold {title}, {due} and {status}.
type Action struct {
	Kind  string   `json:"kind"`
	Value string   `json:"value,omitempty"`
	Days  int      `json:"days,omitempty"`
	Users []string `json:"users,omitempty"`
	Title string   `json:"title,omitempty"`
	Sub   bool     `json:"sub,omitempty"` // create_item: as a subtask of this one
}

var actionKinds = []string{"set_status", "set_priority", "assign", "unassign", "add_label", "remove_label", "set_group",
	"set_due", "add_watcher", "add_check", "complete_checklist", "notify", "create_item", "comment",
	"start_timer", "stop_timer"}
var triggerKinds = []string{"created", "moved", "assigned", "commented", "due_soon", "overdue"}

// Button is a quick action on an item.
type Button struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Color string   `json:"color,omitempty"`
	Then  []Action `json:"then"`
}

// Webhook is where a board's events are posted, signed with Secret.
type Webhook struct {
	URL    string   `json:"url"`
	Events []string `json:"events"` // created, updated, moved, deleted, commented, approved
	Secret string   `json:"secret,omitempty"`
}

// DefaultButtons are the owner's three: Start, Complete, Escalate.
func DefaultButtons(b Board) []Button {
	doing := ""
	for _, st := range b.Statuses {
		if st.Key == "doing" {
			doing = st.Key
		}
	}
	if doing == "" && len(b.Statuses) > 1 {
		doing = b.Statuses[1].Key
	}
	done := ""
	for _, st := range b.Statuses {
		if st.Done {
			done = st.Key
		}
	}
	return []Button{
		{ID: "start", Name: "شروع", Color: "#3B82F6", Then: []Action{
			{Kind: "set_status", Value: doing}, {Kind: "assign", Users: []string{"@actor"}}, {Kind: "start_timer"},
			{Kind: "notify", Users: []string{"@watchers", "@creator"}, Title: "«{title}» شروع شد"}}},
		{ID: "complete", Name: "انجام شد", Color: "#22C55E", Then: []Action{
			{Kind: "set_status", Value: done}, {Kind: "complete_checklist"}, {Kind: "stop_timer"},
			{Kind: "notify", Users: []string{"@creator", "@watchers"}, Title: "«{title}» انجام شد"}}},
		{ID: "escalate", Name: "ارجاع فوری", Color: "#DC2626", Then: []Action{
			{Kind: "set_priority", Value: "critical"}, {Kind: "assign", Users: []string{"@owners"}},
			{Kind: "notify", Users: []string{"@owners"}, Title: "ارجاع فوری: «{title}»"},
			{Kind: "comment", Value: "ارجاع فوری به مدیر فضای کار"}}},
	}
}

// ButtonsOf is a board's buttons: its own, or the three.
func ButtonsOf(b Board) []Button {
	if len(b.Buttons) > 0 {
		return b.Buttons
	}
	return DefaultButtons(b)
}

func cleanActions(in []Action, b Board) ([]Action, error) {
	if len(in) > 20 {
		return nil, Invalid{"یک قاعده یا دکمه حداکثر ۲۰ کار دارد."}
	}
	out := []Action{}
	for _, a := range in {
		if !slices.Contains(actionKinds, a.Kind) {
			return nil, Invalid{"کار «" + a.Kind + "» شناخته نیست."}
		}
		a.Value = clipText(a.Value, 500)
		a.Title = oneLine(a.Title, 200)
		a.Days = max(-3650, min(a.Days, 3650))
		var us []string
		for _, u := range a.Users {
			u = strings.ToLower(strings.TrimSpace(u))
			if u != "" && (strings.HasPrefix(u, "@") || userShape.MatchString(u)) && !slices.Contains(us, u) && len(us) < 30 {
				us = append(us, u)
			}
		}
		a.Users = us
		if a.Kind == "set_status" && a.Value != "" && !slices.ContainsFunc(b.Statuses, func(s Status) bool { return s.Key == a.Value }) {
			return nil, Invalid{"ستون «" + a.Value + "» در این بورد نیست."}
		}
		if a.Kind == "set_priority" && !slices.Contains(Priorities, a.Value) {
			return nil, Invalid{"اولویت «" + a.Value + "» شناخته نیست."}
		}
		out = append(out, a)
	}
	return out, nil
}

// cleanAutomation checks a board's rules, buttons and webhooks.
func cleanAutomation(b Board) (Board, error) {
	if len(b.Rules) > 50 || len(b.Buttons) > 12 || len(b.Webhooks) > 5 {
		return b, Invalid{"قاعده، دکمه یا وب‌هوک بیش از اندازه است."}
	}
	rules := []Rule{}
	for _, r := range b.Rules {
		if !slices.Contains(triggerKinds, r.When.Kind) {
			return b, Invalid{"رویداد «" + r.When.Kind + "» شناخته نیست."}
		}
		if r.ID == "" || !keyShape.MatchString(r.ID) {
			r.ID = NewID()
		}
		r.Name = oneLine(r.Name, 80)
		if r.Name == "" {
			r.Name = "قاعده"
		}
		r.When.Days = max(0, min(r.When.Days, 365))
		var conds []Cond
		for _, c := range r.If {
			c.Value = oneLine(c.Value, 200)
			if !slices.Contains([]string{"priority", "label", "assignee", "status", "group", "title"}, c.Field) ||
				!slices.Contains([]string{"is", "is_not", "has", "lacks", "contains"}, c.Op) || len(conds) >= 10 {
				continue
			}
			conds = append(conds, c)
		}
		if conds == nil {
			conds = []Cond{}
		}
		r.If = conds
		var err error
		if r.Then, err = cleanActions(r.Then, b); err != nil {
			return b, err
		}
		rules = append(rules, r)
	}
	b.Rules = rules
	buttons := []Button{}
	for _, bt := range b.Buttons {
		if bt.ID == "" || !keyShape.MatchString(bt.ID) {
			bt.ID = NewID()
		}
		if bt.Name = oneLine(bt.Name, 40); bt.Name == "" {
			continue
		}
		if !colorShape.MatchString(bt.Color) {
			bt.Color = ""
		}
		var err error
		if bt.Then, err = cleanActions(bt.Then, b); err != nil {
			return b, err
		}
		buttons = append(buttons, bt)
	}
	b.Buttons = buttons
	hooks := []Webhook{}
	for _, h := range b.Webhooks {
		h.URL = strings.TrimSpace(h.URL)
		if !(strings.HasPrefix(h.URL, "https://") || strings.HasPrefix(h.URL, "http://")) || strings.ContainsAny(h.URL, " \r\n\t\"'<>@") || len(h.URL) > 500 {
			return b, Invalid{"نشانی وب‌هوک باید http یا https باشد، بی نام کاربری."}
		}
		var ev []string
		for _, e := range h.Events {
			if slices.Contains([]string{"created", "updated", "moved", "deleted", "commented", "approved"}, e) && !slices.Contains(ev, e) {
				ev = append(ev, e)
			}
		}
		if ev == nil {
			ev = []string{"created", "moved"}
		}
		h.Events = ev
		h.Secret = clipText(h.Secret, 200)
		hooks = append(hooks, h)
	}
	b.Webhooks = hooks
	return b, nil
}

// matches reports whether the item meets every condition.
func matches(conds []Cond, it Item) bool {
	for _, c := range conds {
		v := strings.ToLower(c.Value)
		var ok bool
		switch c.Field {
		case "priority":
			ok = it.Priority == v
		case "status":
			ok = it.Status == v
		case "group":
			ok = strings.ToLower(it.Group) == v
		case "title":
			ok = strings.Contains(strings.ToLower(it.Title), v)
		case "label":
			ok = slices.ContainsFunc(it.Labels, func(l string) bool { return strings.ToLower(l) == v })
		case "assignee":
			ok = slices.Contains(it.Assignees, v)
		}
		switch c.Op {
		case "is_not", "lacks":
			ok = !ok
		}
		if !ok {
			return false
		}
	}
	return true
}

// run is one execution: the item being changed, who by, and what it told.
type run struct {
	s       *Service
	ctx     context.Context
	b       Board
	w       Workspace
	actor   string
	item    Item
	changed bool
	notices int
}

func (r *run) expand(s string) string {
	return strings.NewReplacer("{title}", r.item.Title, "{due}", r.item.Due, "{status}", statusName(r.b, r.item.Status)).Replace(s)
}

func (r *run) people(us []string) []string {
	var out []string
	add := func(xs ...string) {
		for _, x := range xs {
			if x != "" && !slices.Contains(out, x) {
				out = append(out, x)
			}
		}
	}
	for _, u := range us {
		switch u {
		case "@creator":
			add(r.item.Creator)
		case "@assignees":
			add(r.item.Assignees...)
		case "@watchers":
			add(r.item.Watchers...)
		case "@actor":
			add(r.actor)
		case "@owners":
			for _, m := range r.w.Members {
				if m.Role == "owner" {
					add(m.User)
				}
			}
		default:
			if !strings.HasPrefix(u, "@") {
				add(u)
			}
		}
	}
	return out
}

func (r *run) setStatus(key string) {
	if key == "" || r.item.Status == key || !slices.ContainsFunc(r.b.Statuses, func(s Status) bool { return s.Key == key }) {
		return
	}
	r.item.Status = key
	if isDone(r.b, key) {
		t := r.s.now()
		r.item.Completed = &t
	} else {
		r.item.Completed = nil
	}
	r.changed = true
}

// do carries out one action on the item being changed.
func (r *run) do(a Action) {
	it := &r.item
	switch a.Kind {
	case "set_status":
		r.setStatus(a.Value)
	case "set_priority":
		if slices.Contains(Priorities, a.Value) && it.Priority != a.Value {
			it.Priority, r.changed = a.Value, true
		}
	case "assign":
		var added []string
		for _, u := range r.people(a.Users) {
			if !slices.Contains(it.Assignees, u) {
				it.Assignees = append(it.Assignees, u)
				added = append(added, u)
			}
		}
		if len(added) > 0 {
			r.changed = true
			r.s.tell(r.ctx, added, r.actor, "کاری به شما سپرده شد", it.Title, ItemLink(*it))
		}
	case "unassign":
		if len(it.Assignees) > 0 {
			it.Assignees, r.changed = []string{}, true
		}
	case "add_watcher":
		for _, u := range r.people(a.Users) {
			if !slices.Contains(it.Watchers, u) {
				it.Watchers, r.changed = append(it.Watchers, u), true
			}
		}
	case "add_label":
		if v := oneLine(a.Value, 40); v != "" && !slices.Contains(it.Labels, v) {
			it.Labels, r.changed = append(it.Labels, v), true
		}
	case "remove_label":
		if i := slices.Index(it.Labels, a.Value); i >= 0 {
			it.Labels, r.changed = slices.Delete(it.Labels, i, i+1), true
		}
	case "set_group":
		if v := oneLine(a.Value, 60); it.Group != v {
			it.Group, r.changed = v, true
		}
	case "set_due":
		if d := r.s.now().AddDate(0, 0, a.Days).Format("2006-01-02"); it.Due != d {
			it.Due, r.changed = d, true
			if it.Start != "" && it.Start > d {
				it.Start = d
			}
		}
	case "add_check":
		if v := oneLine(r.expand(a.Value), 300); v != "" {
			it.Checklist, r.changed = append(it.Checklist, CheckItem{ID: NewID(), Text: v}), true
		}
	case "complete_checklist":
		for i := range it.Checklist {
			if !it.Checklist[i].Done {
				it.Checklist[i].Done, r.changed = true, true
			}
		}
	case "start_timer":
		if startTimer(it, r.actor, r.s.now()) {
			r.changed = true
		}
	case "stop_timer":
		if stopTimers(it, r.actor, r.s.now()) {
			r.changed = true
		}
	case "notify":
		title := r.expand(a.Title)
		if title == "" {
			title = "«" + it.Title + "»"
		}
		r.s.tell(r.ctx, r.people(a.Users), r.actor, title, r.expand(a.Value), ItemLink(*it))
		r.notices++
	case "comment":
		if v := clipText(r.expand(a.Value), 2000); v != "" {
			_ = r.s.Store.AddComment(r.ctx, Comment{ID: NewID(), ItemID: it.ID, Author: "automation", Body: v,
				Kind: "note", Mentions: []string{}, Created: r.s.now()})
		}
	case "create_item":
		title := oneLine(r.expand(a.Title), 200)
		if title == "" {
			return
		}
		n := Item{ID: NewID(), BoardID: it.BoardID, Title: title, Status: a.Value, Priority: "normal",
			Creator: "automation", Created: r.s.now(), Updated: r.s.now(), Position: float64(r.s.now().UnixMilli()) / 1000,
			Assignees: r.people(a.Users)}
		if a.Sub && it.ParentID == "" {
			n.ParentID = it.ID
		}
		if a.Days != 0 {
			n.Due = r.s.now().AddDate(0, 0, a.Days).Format("2006-01-02")
		}
		n.Links = []Link{{Kind: "page", Ref: ItemLink(*it), Title: it.Title}}
		clean, err := cleanItem(n, r.b)
		if err != nil {
			return
		}
		if saved, err := r.s.Store.SaveItem(r.ctx, clean); err == nil {
			r.s.log(r.ctx, saved, Actor{User: "automation"}, "create", "ساخت خودکار «"+saved.Title+"»")
			r.s.tell(r.ctx, saved.Assignees, r.actor, "کار تازه برای شما", saved.Title, ItemLink(saved))
		}
	}
}

// apply runs actions on it, as actor, and saves it once if they changed it.
func (s *Service) apply(ctx context.Context, b Board, w Workspace, it Item, actor, why string, actions []Action) (Item, error) {
	r := &run{s: s, ctx: ctx, b: b, w: w, actor: actor, item: it}
	for _, a := range actions {
		r.do(a)
	}
	s.log(ctx, it, Actor{User: actor}, "automation", why)
	if !r.changed {
		return it, nil
	}
	clean, err := cleanItem(r.item, b)
	if err != nil {
		return it, err
	}
	clean.Updated, clean.UpdatedBy = s.now(), actor
	return s.Store.SaveItem(ctx, clean)
}

// fire runs the rules of b that answer event on it. It never fails the save
// that caused it: an automation that cannot run is logged, not raised.
func (s *Service) fire(ctx context.Context, b Board, it Item, event, actor string) Item {
	w, err := s.workspace(ctx, b.WorkspaceID)
	if err != nil {
		return it
	}
	for _, rule := range b.Rules {
		if !rule.Enabled || rule.When.Kind != event {
			continue
		}
		if event == "moved" && rule.When.Status != "" && rule.When.Status != it.Status {
			continue
		}
		if !matches(rule.If, it) {
			continue
		}
		saved, err := s.apply(ctx, b, w, it, actor, "قاعدهٔ «"+rule.Name+"»", rule.Then)
		if err == nil {
			it = saved
		}
	}
	return it
}

// Press runs one of the board's buttons on an item, as a.
func (s *Service) Press(ctx context.Context, a Actor, itemID, buttonID string) (Item, error) {
	it, b, w, err := s.Item(ctx, a, itemID)
	if err != nil {
		return it, err
	}
	if !a.edits(w) {
		return it, ErrForbidden
	}
	i := slices.IndexFunc(ButtonsOf(b), func(x Button) bool { return x.ID == buttonID })
	if i < 0 {
		return it, ErrNotFound
	}
	bt := ButtonsOf(b)[i]
	before := it
	saved, err := s.apply(ctx, b, w, it, a.User, "دکمهٔ «"+bt.Name+"»", bt.Then)
	if err != nil {
		return saved, err
	}
	if before.Status != saved.Status {
		saved = s.fire(ctx, b, saved, "moved", a.User)
	}
	return saved, nil
}

// Sweep runs what the clock starts: the reminder the day before an item is
// due and the day it becomes overdue, to its assignees, and the rules that
// fire on "due soon" and "overdue". Each fires once per item and due day,
// remembered in the activity log.
func (s *Service) Sweep(ctx context.Context, now time.Time) int {
	all, err := s.Store.Workspaces(ctx)
	if err != nil {
		return 0
	}
	today := now.Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	fired := 0
	for _, w := range all {
		boards, err := s.Store.Boards(ctx, w.ID)
		if err != nil {
			continue
		}
		for _, b := range boards {
			items, err := s.Store.Items(ctx, Query{BoardIDs: []string{b.ID}})
			if err != nil {
				continue
			}
			for _, it := range items {
				if it.Due == "" || isDone(b, it.Status) {
					continue
				}
				once := func(key, detail string, do func()) {
					log, _ := s.Store.Activity(ctx, "", it.ID, 500)
					if slices.ContainsFunc(log, func(a Activity) bool { return a.Action == "sweep:"+key }) {
						return
					}
					do()
					_ = s.Store.AddActivity(ctx, Activity{ID: NewID(), BoardID: b.ID, ItemID: it.ID, Actor: "automation",
						Action: "sweep:" + key, Detail: detail, At: s.now()})
					fired++
				}
				if it.Due == tomorrow {
					once("soon:"+it.Due, "یادآوری: سررسید فردا", func() {
						s.tell(ctx, it.Assignees, "", "سررسید فردا: «"+it.Title+"»", "", ItemLink(it))
					})
				}
				if it.Due < today {
					once("late:"+it.Due, "یادآوری: سررسید گذشته است", func() {
						s.tell(ctx, append(append([]string{}, it.Assignees...), it.Creator), "", "دیرکرد: «"+it.Title+"»",
							"سررسید "+it.Due+" بود.", ItemLink(it))
					})
				}
				for _, rule := range b.Rules {
					if !rule.Enabled || !matches(rule.If, it) {
						continue
					}
					due := rule.When.Kind == "due_soon" && it.Due == now.AddDate(0, 0, rule.When.Days).Format("2006-01-02")
					late := rule.When.Kind == "overdue" && it.Due < today
					if !due && !late {
						continue
					}
					rule := rule
					once("rule:"+rule.ID+":"+it.Due, "قاعدهٔ «"+rule.Name+"»", func() {
						if saved, err := s.apply(ctx, b, w, it, "automation", "قاعدهٔ «"+rule.Name+"»", rule.Then); err == nil {
							it = saved
						}
					})
				}
			}
		}
	}
	return fired
}
