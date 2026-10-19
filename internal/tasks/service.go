package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Store keeps workspaces, boards, items, comments and the activity log. It
// stores what it is given: every rule is Service's.
type Store interface {
	Workspaces(ctx context.Context) ([]Workspace, error)
	SaveWorkspace(ctx context.Context, w Workspace) error
	DeleteWorkspace(ctx context.Context, id string) error // and its boards and their items
	Boards(ctx context.Context, workspaceID string) ([]Board, error)
	Board(ctx context.Context, id string) (Board, error)
	SaveBoard(ctx context.Context, b Board) error
	DeleteBoard(ctx context.Context, id string) error // and its items
	Items(ctx context.Context, q Query) ([]Item, error)
	Item(ctx context.Context, id string) (Item, error)
	// SaveItem stores it at version+1 when the stored one is at version
	// (0: a new item), and answers ErrConflict when it is not.
	SaveItem(ctx context.Context, it Item) (Item, error)
	DeleteItem(ctx context.Context, id string) error // and its subtasks
	Comments(ctx context.Context, itemID string) ([]Comment, error)
	AddComment(ctx context.Context, c Comment) error
	Activity(ctx context.Context, boardID, itemID string, limit int) ([]Activity, error)
	AddActivity(ctx context.Context, a Activity) error
}

// Actor is who is asking. Admin sees and runs every workspace.
type Actor struct {
	User  string
	Admin bool
}

// Notice is something to tell people: the HTTP side sends it through the
// notification centre, which pushes, mails or texts it by each person's
// choice.
type Notice struct {
	To    []string
	Title string
	Body  string
	Link  string
}

// Service is the task manager's rules over a Store.
type Service struct {
	Store  Store
	Notify func(ctx context.Context, n Notice) // nil: nobody is told
	// Hook posts a board's event to one of its webhooks (body is JSON); nil:
	// nothing is posted. It must not block.
	Hook func(ctx context.Context, h Webhook, event string, body []byte)
	Now  func() time.Time
	// People says a username is one of docfind's (a person field); nil takes
	// any well-formed one. Org is who heads whom and who is in which
	// department, for a process's assignments (flow.go); nil leaves a
	// manager's or a department's step with the requester.
	People func(username string) bool
	Org    Org
	// Data answers questions whose options come from docfind's lists or a
	// database (fielddata.go); nil: such questions cannot be sent.
	Data FieldData
	// Files keeps the files questions take (files.go); nil: none can be sent.
	Files FileStore
}

func (s *Service) isPerson(u string) bool { return s.People == nil || s.People(u) }

// webhook posts event on it to the board's webhooks that asked for it.
func (s *Service) webhook(ctx context.Context, b Board, event string, it Item) {
	if s.Hook == nil {
		return
	}
	for _, h := range b.Webhooks {
		if !slices.Contains(h.Events, event) {
			continue
		}
		body, err := json.Marshal(map[string]any{"event": event, "at": s.now(),
			"board": map[string]string{"id": b.ID, "name": b.Name}, "item": it})
		if err == nil {
			s.Hook(ctx, h, event, body)
		}
	}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) tell(ctx context.Context, to []string, actor, title, body, link string) {
	if s.Notify == nil {
		return
	}
	var who []string
	for _, u := range to {
		if u != "" && u != actor && !slices.Contains(who, u) {
			who = append(who, u)
		}
	}
	if len(who) > 0 {
		s.Notify(ctx, Notice{To: who, Title: title, Body: body, Link: link})
	}
}

// Role is what u is in w: owner, member, viewer, or "" for nothing. An open
// workspace makes everybody at least a member.
func Role(w Workspace, u string) string {
	for _, m := range w.Members {
		if m.User == u {
			return m.Role
		}
	}
	if w.Open {
		return "member"
	}
	return ""
}

func (a Actor) sees(w Workspace) bool { return a.Admin || Role(w, a.User) != "" }
func (a Actor) edits(w Workspace) bool {
	r := Role(w, a.User)
	return a.Admin || r == "owner" || r == "member"
}
func (a Actor) owns(w Workspace) bool { return a.Admin || Role(w, a.User) == "owner" }

// ------------------------------------------------------------- workspaces

// Workspaces are the ones a can see.
func (s *Service) Workspaces(ctx context.Context, a Actor) ([]Workspace, error) {
	all, err := s.Store.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	out := []Workspace{}
	for _, w := range all {
		if a.sees(w) {
			out = append(out, w)
		}
	}
	return out, nil
}

func (s *Service) workspace(ctx context.Context, id string) (Workspace, error) {
	all, err := s.Store.Workspaces(ctx)
	if err != nil {
		return Workspace{}, err
	}
	for _, w := range all {
		if w.ID == id {
			return w, nil
		}
	}
	return Workspace{}, ErrNotFound
}

// SaveWorkspace creates one (its maker owns it) or changes one a owns.
func (s *Service) SaveWorkspace(ctx context.Context, a Actor, w Workspace) (Workspace, error) {
	w, err := cleanWorkspace(w)
	if err != nil {
		return w, err
	}
	if w.ID == "" {
		w.ID, w.CreatedBy, w.Created = NewID(), a.User, s.now()
		if Role(Workspace{Members: w.Members}, a.User) != "owner" {
			w.Members = append([]Member{{User: a.User, Role: "owner"}},
				slices.DeleteFunc(w.Members, func(m Member) bool { return m.User == a.User })...)
		}
	} else {
		old, err := s.workspace(ctx, w.ID)
		if err != nil {
			return w, err
		}
		if !a.owns(old) {
			return w, ErrForbidden
		}
		w.CreatedBy, w.Created = old.CreatedBy, old.Created
		// Somebody must still run it: an owner who removes every owner,
		// themselves included, stays one.
		if !slices.ContainsFunc(w.Members, func(m Member) bool { return m.Role == "owner" }) {
			w.Members = append([]Member{{User: a.User, Role: "owner"}},
				slices.DeleteFunc(w.Members, func(m Member) bool { return m.User == a.User })...)
		}
	}
	if err := s.Store.SaveWorkspace(ctx, w); err != nil {
		return w, err
	}
	_ = s.Store.AddActivity(ctx, Activity{ID: NewID(), Actor: a.User, Action: "workspace", Detail: "فضای کار «" + w.Name + "»", At: s.now()})
	return w, nil
}

// DeleteWorkspace removes one a owns, with its boards and their items.
func (s *Service) DeleteWorkspace(ctx context.Context, a Actor, id string) error {
	w, err := s.workspace(ctx, id)
	if err != nil {
		return err
	}
	if !a.owns(w) {
		return ErrForbidden
	}
	return s.Store.DeleteWorkspace(ctx, id)
}

// ----------------------------------------------------------------- boards

// Boards are a workspace's, for somebody who can see it.
func (s *Service) Boards(ctx context.Context, a Actor, workspaceID string) ([]Board, error) {
	w, err := s.workspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if !a.sees(w) {
		return nil, ErrNotFound
	}
	return s.Store.Boards(ctx, workspaceID)
}

// Board is one board and its workspace, for somebody who can see it. One
// they cannot see is not found, not forbidden: its existence is not theirs.
func (s *Service) Board(ctx context.Context, a Actor, id string) (Board, Workspace, error) {
	b, err := s.Store.Board(ctx, id)
	if err != nil {
		return Board{}, Workspace{}, err
	}
	w, err := s.workspace(ctx, b.WorkspaceID)
	if err != nil {
		return Board{}, Workspace{}, err
	}
	if !a.sees(w) {
		return Board{}, Workspace{}, ErrNotFound
	}
	return b, w, nil
}

// SaveBoard creates or changes a board in a workspace a works in. A column
// that still holds items cannot be taken away.
func (s *Service) SaveBoard(ctx context.Context, a Actor, b Board) (Board, error) {
	b, err := cleanBoard(b)
	if err != nil {
		return b, err
	}
	if b, err = cleanAutomation(b); err != nil {
		return b, err
	}
	if b, err = cleanForms(b); err != nil {
		return b, err
	}
	w, err := s.workspace(ctx, b.WorkspaceID)
	if err != nil {
		return b, err
	}
	if !a.sees(w) {
		return b, ErrNotFound
	}
	if !a.edits(w) {
		return b, ErrForbidden
	}
	if b.ID == "" {
		if !a.Admin && sqlEdited(nil, formFieldsByKey(b)) {
			return b, errSQLAdmin
		}
		b.ID, b.CreatedBy, b.Created = NewID(), a.User, s.now()
	} else {
		old, _, err := s.Board(ctx, a, b.ID)
		if err != nil {
			return b, err
		}
		if old.WorkspaceID != b.WorkspaceID {
			return b, Invalid{"بورد را نمی‌شود به فضای کار دیگری برد."}
		}
		b.CreatedBy, b.Created = old.CreatedBy, old.Created
		if !a.Admin && sqlEdited(formFieldsByKey(old), formFieldsByKey(b)) {
			return b, errSQLAdmin
		}
		// The process changes only through SaveProcess.
		b.Process, b.ProcessHistory = old.Process, old.ProcessHistory
		// A webhook's secret is never sent to the page; one coming back
		// empty for the same address keeps the one stored.
		for i, h := range b.Webhooks {
			if h.Secret != "" {
				continue
			}
			for _, o := range old.Webhooks {
				if o.URL == h.URL {
					b.Webhooks[i].Secret = o.Secret
				}
			}
		}
		items, err := s.Store.Items(ctx, Query{BoardIDs: []string{b.ID}})
		if err != nil {
			return b, err
		}
		for _, it := range items {
			if !slices.ContainsFunc(b.Statuses, func(st Status) bool { return st.Key == it.Status }) {
				return b, Invalid{"ستونی که هنوز کار دارد را نمی‌شود برداشت؛ اول کارهایش را جابه‌جا کنید."}
			}
		}
	}
	if err := s.Store.SaveBoard(ctx, b); err != nil {
		return b, err
	}
	_ = s.Store.AddActivity(ctx, Activity{ID: NewID(), BoardID: b.ID, Actor: a.User, Action: "board", Detail: "بورد «" + b.Name + "»", At: s.now()})
	return b, nil
}

// DeleteBoard removes a board, with its items, for its workspace's owner.
func (s *Service) DeleteBoard(ctx context.Context, a Actor, id string) error {
	_, w, err := s.Board(ctx, a, id)
	if err != nil {
		return err
	}
	if !a.owns(w) {
		return ErrForbidden
	}
	return s.Store.DeleteBoard(ctx, id)
}

// ------------------------------------------------------------------ items

// Items are a board's, subtasks included.
func (s *Service) Items(ctx context.Context, a Actor, boardID string) ([]Item, error) {
	if _, _, err := s.Board(ctx, a, boardID); err != nil {
		return nil, err
	}
	return s.Store.Items(ctx, Query{BoardIDs: []string{boardID}})
}

// Item is one item with its board, for somebody who can see the board.
func (s *Service) Item(ctx context.Context, a Actor, id string) (Item, Board, Workspace, error) {
	it, err := s.Store.Item(ctx, id)
	if err != nil {
		return Item{}, Board{}, Workspace{}, err
	}
	b, w, err := s.Board(ctx, a, it.BoardID)
	if err != nil {
		return Item{}, Board{}, Workspace{}, err
	}
	return it, b, w, nil
}

// visibleBoards is every board a can see.
func (s *Service) visibleBoards(ctx context.Context, a Actor) (map[string]Board, error) {
	ws, err := s.Workspaces(ctx, a)
	if err != nil {
		return nil, err
	}
	out := map[string]Board{}
	for _, w := range ws {
		bs, err := s.Store.Boards(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		for _, b := range bs {
			out[b.ID] = b
		}
	}
	return out, nil
}

// MyWork is everything assigned to a across the boards a can see, not yet
// finished, soonest due first.
func (s *Service) MyWork(ctx context.Context, a Actor) ([]Item, map[string]Board, error) {
	boards, err := s.visibleBoards(ctx, a)
	if err != nil || len(boards) == 0 {
		return []Item{}, boards, err
	}
	ids := make([]string, 0, len(boards))
	for id := range boards {
		ids = append(ids, id)
	}
	items, err := s.Store.Items(ctx, Query{BoardIDs: ids, Assignee: a.User})
	if err != nil {
		return nil, nil, err
	}
	out := []Item{}
	for _, it := range items {
		if !isDone(boards[it.BoardID], it.Status) {
			out = append(out, it)
		}
	}
	slices.SortStableFunc(out, func(x, y Item) int { return strings.Compare(dueKey(x), dueKey(y)) })
	return out, boards, nil
}

// Search is the items whose title or description holds text, on the boards a
// can see -- for the palette.
func (s *Service) Search(ctx context.Context, a Actor, text string, limit int) ([]Item, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []Item{}, nil
	}
	boards, err := s.visibleBoards(ctx, a)
	if err != nil || len(boards) == 0 {
		return []Item{}, err
	}
	ids := make([]string, 0, len(boards))
	for id := range boards {
		ids = append(ids, id)
	}
	return s.Store.Items(ctx, Query{BoardIDs: ids, Text: text, Limit: limit})
}

func dueKey(it Item) string {
	if it.Due == "" {
		return "9999-99-99"
	}
	return it.Due
}

func isDone(b Board, status string) bool {
	i := slices.IndexFunc(b.Statuses, func(s Status) bool { return s.Key == status })
	return i >= 0 && b.Statuses[i].Done
}

func statusName(b Board, key string) string {
	if i := slices.IndexFunc(b.Statuses, func(s Status) bool { return s.Key == key }); i >= 0 {
		return b.Statuses[i].Name
	}
	return key
}

// ItemLink is where an item opens.
func ItemLink(it Item) string { return "/tasks?item=" + it.ID }

// SaveItem creates or changes an item on a board a works on. version is the
// one the page read (0 for a new item); a newer stored one is ErrConflict,
// never overwritten. Moving into a finished column stamps the completion,
// out of one clears it; a column at its WIP limit takes no more.
func (s *Service) SaveItem(ctx context.Context, a Actor, it Item) (Item, error) {
	b, w, err := s.Board(ctx, a, it.BoardID)
	if err != nil {
		return it, err
	}
	if !a.edits(w) {
		return it, ErrForbidden
	}
	var old *Item
	if it.ID != "" {
		o, err := s.Store.Item(ctx, it.ID)
		if err != nil {
			return it, err
		}
		if o.BoardID != it.BoardID {
			// Moving between boards is a new item on the other board; the
			// columns and fields of one say nothing about the other.
			return it, Invalid{"کار را نمی‌شود به بورد دیگری برد."}
		}
		old = &o
		it.Creator, it.Created = o.Creator, o.Created
		// The sign-off and the work log change through their own doors
		// (approval.go, timelog.go), never through an edit of the item.
		it.Approval, it.Time = o.Approval, o.Time
		it.Flow, it.Form = o.Flow, o.Form
		if len(o.Time) > 0 {
			sumMinutes(&it)
		}
	} else {
		it.ID, it.Creator, it.Created, it.Version = NewID(), a.User, s.now(), 0
		it.Approval, it.Time = nil, nil
		it.Flow, it.Form = nil, nil
	}
	if it, err = cleanItem(it, b); err != nil {
		return it, err
	}
	if it.ParentID != "" {
		p, err := s.Store.Item(ctx, it.ParentID)
		if err != nil || p.BoardID != it.BoardID || p.ParentID != "" || p.ID == it.ID {
			return it, Invalid{"کار بالادست باید کاری از همین بورد باشد."}
		}
	}
	if st := b.Statuses[slices.IndexFunc(b.Statuses, func(s Status) bool { return s.Key == it.Status })]; st.WIP > 0 &&
		it.ParentID == "" && (old == nil || old.Status != it.Status) {
		items, err := s.Store.Items(ctx, Query{BoardIDs: []string{b.ID}})
		if err != nil {
			return it, err
		}
		n := 0
		for _, x := range items {
			if x.Status == it.Status && x.ParentID == "" && x.ID != it.ID {
				n++
			}
		}
		if n >= st.WIP {
			return it, Invalid{"ستون «" + st.Name + "» به سقف " + itoa(st.WIP) + " کار رسیده است."}
		}
	}
	done := isDone(b, it.Status)
	switch {
	case done && (old == nil || old.Completed == nil):
		t := s.now()
		it.Completed = &t
	case done:
		it.Completed = old.Completed
	default:
		it.Completed = nil
	}
	it.Updated, it.UpdatedBy = s.now(), a.User
	saved, err := s.Store.SaveItem(ctx, it)
	if err != nil {
		return it, err
	}

	link := ItemLink(saved)
	if old == nil {
		s.log(ctx, saved, a, "create", "ساخت «"+saved.Title+"»")
		s.tell(ctx, saved.Assignees, a.User, "کار تازه برای شما", saved.Title, link)
		saved = s.fire(ctx, b, saved, "created", a.User)
		s.webhook(ctx, b, "created", saved)
		return saved, nil
	}
	for _, line := range changes(*old, saved, b) {
		s.log(ctx, saved, a, "update", line)
	}
	var added []string
	for _, u := range saved.Assignees {
		if !slices.Contains(old.Assignees, u) {
			added = append(added, u)
		}
	}
	s.tell(ctx, added, a.User, "کاری به شما سپرده شد", saved.Title, link)
	moved := old.Status != saved.Status
	if moved {
		s.tell(ctx, append(append([]string{saved.Creator}, saved.Watchers...), saved.Assignees...), a.User,
			"«"+saved.Title+"» به «"+statusName(b, saved.Status)+"» رفت", "", link)
		saved = s.fire(ctx, b, saved, "moved", a.User)
	}
	if len(added) > 0 {
		saved = s.fire(ctx, b, saved, "assigned", a.User)
	}
	if moved {
		s.webhook(ctx, b, "moved", saved)
	} else {
		s.webhook(ctx, b, "updated", saved)
	}
	return saved, nil
}

// DeleteItem removes an item, and its subtasks, from a board a works on.
func (s *Service) DeleteItem(ctx context.Context, a Actor, id string) error {
	it, b, w, err := s.Item(ctx, a, id)
	if err != nil {
		return err
	}
	if !a.edits(w) {
		return ErrForbidden
	}
	if err := s.Store.DeleteItem(ctx, id); err != nil {
		return err
	}
	s.log(ctx, it, a, "delete", "حذف «"+it.Title+"»")
	s.webhook(ctx, b, "deleted", it)
	return nil
}

func (s *Service) log(ctx context.Context, it Item, a Actor, action, detail string) {
	_ = s.Store.AddActivity(ctx, Activity{ID: NewID(), BoardID: it.BoardID, ItemID: it.ID, Actor: a.User,
		Action: action, Detail: detail, At: s.now()})
}

// changes is what an update did, a line each, in the log's words.
func changes(o, n Item, b Board) []string {
	var out []string
	if o.Title != n.Title {
		out = append(out, "عنوان: «"+n.Title+"»")
	}
	if o.Status != n.Status {
		out = append(out, "ستون: "+statusName(b, o.Status)+" ← "+statusName(b, n.Status))
	}
	if o.Priority != n.Priority {
		out = append(out, "اولویت: "+priorityName(n.Priority))
	}
	if !slices.Equal(o.Assignees, n.Assignees) {
		out = append(out, "مسئول: "+strings.Join(n.Assignees, "، "))
	}
	if o.Due != n.Due {
		out = append(out, "سررسید: "+n.Due)
	}
	if o.Start != n.Start {
		out = append(out, "شروع: "+n.Start)
	}
	if o.Description != n.Description {
		out = append(out, "شرح تغییر کرد")
	}
	if !slices.Equal(o.Labels, n.Labels) {
		out = append(out, "برچسب‌ها: "+strings.Join(n.Labels, "، "))
	}
	if doneOf(o.Checklist) != doneOf(n.Checklist) || len(o.Checklist) != len(n.Checklist) {
		out = append(out, "فهرست بررسی: "+itoa(doneOf(n.Checklist))+" از "+itoa(len(n.Checklist)))
	}
	if len(o.Links) != len(n.Links) {
		out = append(out, "پیوندها: "+itoa(len(n.Links)))
	}
	if len(o.Deps) != len(n.Deps) {
		out = append(out, "وابستگی‌ها: "+itoa(len(n.Deps)))
	}
	return out
}

func doneOf(c []CheckItem) int {
	n := 0
	for _, x := range c {
		if x.Done {
			n++
		}
	}
	return n
}

func priorityName(p string) string {
	switch p {
	case "low":
		return "کم"
	case "high":
		return "زیاد"
	case "critical":
		return "بحرانی"
	}
	return "عادی"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// ------------------------------------------------------ comments, the log

var mention = regexp.MustCompile(`@([\p{L}\p{N}._-]{1,64})`)

// Comments are an item's updates, oldest first.
func (s *Service) Comments(ctx context.Context, a Actor, itemID string) ([]Comment, error) {
	if _, _, _, err := s.Item(ctx, a, itemID); err != nil {
		return nil, err
	}
	return s.Store.Comments(ctx, itemID)
}

// AddComment adds one of a's updates to an item they work on; the people it
// @mentions, and those on the item, are told.
func (s *Service) AddComment(ctx context.Context, a Actor, itemID, body, kind string) (Comment, error) {
	it, b, w, err := s.Item(ctx, a, itemID)
	if err != nil {
		return Comment{}, err
	}
	if !a.edits(w) {
		return Comment{}, ErrForbidden
	}
	body = clipText(body, 10000)
	if body == "" {
		return Comment{}, Invalid{"متن خالی است."}
	}
	switch kind {
	case "note", "decision", "approval":
	default:
		kind = "comment"
	}
	c := Comment{ID: NewID(), ItemID: itemID, Author: a.User, Body: body, Kind: kind, Created: s.now(), Mentions: []string{}}
	for _, m := range mention.FindAllStringSubmatch(body, 20) {
		if u := strings.ToLower(m[1]); !slices.Contains(c.Mentions, u) {
			c.Mentions = append(c.Mentions, u)
		}
	}
	if err := s.Store.AddComment(ctx, c); err != nil {
		return Comment{}, err
	}
	s.log(ctx, it, a, "comment", clipText(body, 120))
	link := ItemLink(it)
	s.tell(ctx, c.Mentions, a.User, a.User+" از شما در «"+it.Title+"» نام برد", clipText(body, 200), link)
	var others []string
	for _, u := range append(append([]string{it.Creator}, it.Assignees...), it.Watchers...) {
		if !slices.Contains(c.Mentions, u) {
			others = append(others, u)
		}
	}
	s.tell(ctx, others, a.User, "یادداشت تازه در «"+it.Title+"»", clipText(body, 200), link)
	s.fire(ctx, b, it, "commented", a.User)
	s.webhook(ctx, b, "commented", it)
	return c, nil
}

// Activity is a board's log, or one item's, newest first.
func (s *Service) Activity(ctx context.Context, a Actor, boardID, itemID string, limit int) ([]Activity, error) {
	if itemID != "" {
		it, _, _, err := s.Item(ctx, a, itemID)
		if err != nil {
			return nil, err
		}
		boardID = it.BoardID
	} else if _, _, err := s.Board(ctx, a, boardID); err != nil {
		return nil, err
	}
	return s.Store.Activity(ctx, boardID, itemID, limit)
}

// IsNotFound and IsForbidden say what an error is, for the HTTP side.
func IsNotFound(err error) bool  { return errors.Is(err, ErrNotFound) }
func IsForbidden(err error) bool { return errors.Is(err, ErrForbidden) }
