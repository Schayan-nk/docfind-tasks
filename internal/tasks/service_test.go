package tasks

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

type rig struct {
	s       *Service
	notices []Notice
}

func newRig() *rig {
	r := &rig{}
	clock := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	r.s = &Service{Store: NewMemStore(), Now: func() time.Time { clock = clock.Add(time.Minute); return clock },
		Notify: func(_ context.Context, n Notice) { r.notices = append(r.notices, n) }}
	return r
}

var (
	ctx   = context.Background()
	owner = Actor{User: "r.ahmadi"}
	mem   = Actor{User: "k.hosseini"}
	view  = Actor{User: "viewer"}
	out   = Actor{User: "outsider"}
	admin = Actor{User: "root", Admin: true}
)

func (r *rig) board(t *testing.T) (Workspace, Board) {
	t.Helper()
	w, err := r.s.SaveWorkspace(ctx, owner, Workspace{Name: " فروش  ", Members: []Member{{User: "K.Hosseini"}, {User: "viewer", Role: "viewer"}}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.s.SaveBoard(ctx, owner, Board{WorkspaceID: w.ID, Name: "استعلام‌ها"})
	if err != nil {
		t.Fatal(err)
	}
	return w, b
}

func TestAWorkspaceIsItsMembersOnly(t *testing.T) {
	r := newRig()
	w, b := r.board(t)
	if w.Name != "فروش" || Role(w, "r.ahmadi") != "owner" || Role(w, "k.hosseini") != "member" {
		t.Fatalf("workspace: %+v", w)
	}
	for _, a := range []Actor{owner, mem, view, admin} {
		if ws, _ := r.s.Workspaces(ctx, a); len(ws) != 1 {
			t.Errorf("%s sees %d workspaces", a.User, len(ws))
		}
	}
	if ws, _ := r.s.Workspaces(ctx, out); len(ws) != 0 {
		t.Error("an outsider sees the workspace")
	}
	// Somebody else's board is not found rather than forbidden.
	if _, _, err := r.s.Board(ctx, out, b.ID); !IsNotFound(err) {
		t.Errorf("an outsider opening the board: %v", err)
	}
	if b.Statuses[0].Key != "backlog" || b.Statuses[len(b.Statuses)-1].Key != "done" || !b.Statuses[len(b.Statuses)-1].Done {
		t.Errorf("the default columns: %+v", b.Statuses)
	}
	// An open workspace is everybody's, as members.
	w.Open = true
	if _, err := r.s.SaveWorkspace(ctx, owner, w); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.SaveItem(ctx, out, Item{BoardID: b.ID, Title: "x"}); err != nil {
		t.Errorf("an open workspace refused work: %v", err)
	}
	// Only an owner changes the workspace; a member cannot make themselves one.
	w.Members = append(w.Members, Member{User: "k.hosseini", Role: "owner"})
	if _, err := r.s.SaveWorkspace(ctx, mem, w); !IsForbidden(err) {
		t.Errorf("a member changed the workspace: %v", err)
	}
}

func TestAViewerReadsAndDoesNotWrite(t *testing.T) {
	r := newRig()
	_, b := r.board(t)
	if _, err := r.s.SaveItem(ctx, view, Item{BoardID: b.ID, Title: "x"}); !IsForbidden(err) {
		t.Errorf("a viewer made an item: %v", err)
	}
	it, err := r.s.SaveItem(ctx, mem, Item{BoardID: b.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if items, err := r.s.Items(ctx, view, b.ID); err != nil || len(items) != 1 {
		t.Errorf("a viewer reading: %v %v", items, err)
	}
	if _, err := r.s.AddComment(ctx, view, it.ID, "hi", ""); !IsForbidden(err) {
		t.Errorf("a viewer commented: %v", err)
	}
}

func TestAnItemMovesThroughTheColumns(t *testing.T) {
	r := newRig()
	_, b := r.board(t)
	it, err := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, Title: "  پیشنهاد فنی  ", Assignees: []string{"K.Hosseini"},
		Due: "2026-10-05", Priority: "urgent"})
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "پیشنهاد فنی" || it.Status != "backlog" || it.Priority != "normal" || it.Version != 1 ||
		it.Creator != "r.ahmadi" || !slices.Equal(it.Assignees, []string{"k.hosseini"}) {
		t.Fatalf("created: %+v", it)
	}
	if len(r.notices) != 1 || r.notices[0].To[0] != "k.hosseini" || !strings.Contains(r.notices[0].Link, it.ID) {
		t.Errorf("the assignee was not told: %+v", r.notices)
	}

	it.Status = "done"
	done, err := r.s.SaveItem(ctx, mem, it)
	if err != nil || done.Completed == nil || done.Version != 2 {
		t.Fatalf("done: %+v %v", done, err)
	}
	// The page that read version 1 does not overwrite version 2.
	it.Title = "stale"
	if _, err := r.s.SaveItem(ctx, owner, it); !errors.Is(err, ErrConflict) {
		t.Errorf("a stale save: %v", err)
	}
	done.Status = "todo"
	back, err := r.s.SaveItem(ctx, mem, done)
	if err != nil || back.Completed != nil {
		t.Errorf("reopened: %+v %v", back, err)
	}
	log, _ := r.s.Activity(ctx, owner, "", it.ID, 0)
	var lines []string
	for _, a := range log {
		lines = append(lines, a.Detail)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"ساخت «پیشنهاد فنی»", "ستون: بک‌لاگ ← انجام‌شده", "ستون: انجام‌شده ← برای انجام"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the log lacks %q:\n%s", want, joined)
		}
	}
	if _, err := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, Title: "x", Status: "nope"}); err == nil {
		t.Error("an item was put in a column the board does not have")
	}
	if _, err := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, Title: "x", Start: "2026-10-05", Due: "2026-10-01"}); err == nil {
		t.Error("a due date before the start was accepted")
	}
}

func TestAColumnAtItsLimitTakesNoMore(t *testing.T) {
	r := newRig()
	_, b := r.board(t)
	b.Statuses[2].WIP = 1 // در حال انجام
	b, err := r.s.SaveBoard(ctx, owner, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, Title: "a", Status: "doing"}); err != nil {
		t.Fatal(err)
	}
	second, _ := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, Title: "b"})
	second.Status = "doing"
	var inv Invalid
	if _, err := r.s.SaveItem(ctx, owner, second); !errors.As(err, &inv) || !strings.Contains(inv.Msg, "سقف") {
		t.Errorf("past the limit: %v", err)
	}
	// A column still holding work cannot be taken off the board.
	b.Statuses = slices.DeleteFunc(b.Statuses, func(s Status) bool { return s.Key == "doing" })
	if _, err := r.s.SaveBoard(ctx, owner, b); err == nil {
		t.Error("a column with work in it was removed")
	}
}

func TestSubtasksCommentsAndMyWork(t *testing.T) {
	r := newRig()
	_, b := r.board(t)
	parent, _ := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, Title: "Prepare Technical Offer", Watchers: []string{"viewer"}})
	child, err := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, ParentID: parent.ID, Title: "read inquiry",
		Assignees: []string{"k.hosseini"}, Due: "2026-10-02"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, ParentID: child.ID, Title: "too deep"}); err == nil {
		t.Error("a subtask of a subtask was accepted")
	}
	later, _ := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, Title: "later", Assignees: []string{"k.hosseini"}, Due: "2026-11-01"})
	mine, _, err := r.s.MyWork(ctx, mem)
	if err != nil || len(mine) != 2 || mine[0].ID != child.ID || mine[1].ID != later.ID {
		t.Errorf("my work: %+v %v", mine, err)
	}

	r.notices = nil
	c, err := r.s.AddComment(ctx, mem, parent.ID, "@R.Ahmadi the supplier replied; @nobody", "")
	if err != nil || !slices.Equal(c.Mentions, []string{"r.ahmadi", "nobody"}) {
		t.Fatalf("comment: %+v %v", c, err)
	}
	var mentioned, others []string
	for _, n := range r.notices {
		if strings.Contains(n.Title, "نام برد") {
			mentioned = n.To
		} else {
			others = n.To
		}
	}
	if !slices.Equal(mentioned, []string{"r.ahmadi", "nobody"}) || !slices.Equal(others, []string{"viewer"}) {
		t.Errorf("told: mentioned %v, others %v", mentioned, others)
	}

	if err := r.s.DeleteItem(ctx, owner, parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := r.s.Item(ctx, owner, child.ID); !IsNotFound(err) {
		t.Errorf("a subtask outlived its parent: %v", err)
	}
	if found, _ := r.s.Search(ctx, mem, "LATER", 10); len(found) != 1 {
		t.Errorf("search: %+v", found)
	}
	if found, _ := r.s.Search(ctx, out, "later", 10); len(found) != 0 {
		t.Error("an outsider found an item")
	}
}

func TestOnlyAnOwnerDeletes(t *testing.T) {
	r := newRig()
	w, b := r.board(t)
	if err := r.s.DeleteBoard(ctx, mem, b.ID); !IsForbidden(err) {
		t.Errorf("a member deleted the board: %v", err)
	}
	if err := r.s.DeleteWorkspace(ctx, mem, w.ID); !IsForbidden(err) {
		t.Errorf("a member deleted the workspace: %v", err)
	}
	if err := r.s.DeleteWorkspace(ctx, owner, w.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.s.Board(ctx, admin, b.ID); !IsNotFound(err) {
		t.Errorf("the board outlived its workspace: %v", err)
	}
}
