package tasks

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// fakeOrg: everybody's manager is boss.man; department 7 is k.hosseini and
// viewer.
type fakeOrg struct{}

func (fakeOrg) Head(context.Context, string) (string, error) { return "boss.man", nil }
func (fakeOrg) Members(_ context.Context, d int) ([]string, error) {
	if d == 7 {
		return []string{"k.hosseini", "viewer"}, nil
	}
	return nil, nil
}

// miniFlow is a purchase process in small: a start, a first-true gateway, a
// tender step that may end the case, a parallel split into a pooled step and
// a manager's step, a join, and a last step for whoever did the one before.
func miniFlow() Process {
	return Process{Name: "سفارش", Start: "reg", Nodes: []Node{
		{ID: "reg", Kind: "step", Name: "دریافت استعلام", Assign: Assign{Mode: "users", Users: []string{"r.ahmadi"}}, Fields: []FormField{
			{Key: "subject", Label: "موضوع", Kind: "text", Required: true},
			{Key: "req_type", Label: "نوع سفارش", Kind: "choice", Required: true, Choices: []Choice{{"1", "مناقصه"}, {"2", "استعلام"}}},
			{Key: "buy_via", Label: "خرید", Kind: "multi", Choices: []Choice{{"1", "تامین کننده"}, {"2", "آنلاین"}}},
			{Key: "amount", Label: "مبلغ", Kind: "money"},
		}},
		{ID: "g1", Kind: "gateway"},
		{ID: "tender", Kind: "step", Name: "تایید نهایی مناقصه", Assign: Assign{Mode: "field", Field: "expert"}, Fields: []FormField{
			{Key: "subject", Label: "موضوع", Kind: "text", Mode: "view"},
			{Key: "tender_result", Label: "نتیجه", Kind: "choice", Required: true, Choices: []Choice{{"1", "برنده"}, {"2", "بازنده"}}},
		}},
		{ID: "g2", Kind: "gateway"},
		{ID: "inquiry", Kind: "step", Name: "ارسال استعلام", Assign: Assign{Mode: "department", Department: 7}},
		{ID: "online", Kind: "step", Name: "خرید آنلاین", Assign: Assign{Mode: "manager"}},
		{ID: "g3", Kind: "gateway"},
		{ID: "final", Kind: "step", Name: "جمع‌بندی", Assign: Assign{Mode: "previous"}},
		{ID: "end", Kind: "end"},
	}, Edges: []Edge{
		{From: "reg", To: "g1", Kind: "seq"},
		{From: "g1", To: "tender", Kind: "xor", When: "@@req_type == 1", Order: 1},
		{From: "g1", To: "g2", Kind: "xor", When: "@@req_type == 2", Order: 2},
		{From: "tender", To: "", Kind: "xor", When: "@@tender_result==2", Order: 1},
		{From: "tender", To: "g2", Kind: "xor", When: "@@tender_result==1", Order: 2},
		{From: "g2", To: "inquiry", Kind: "par", When: `in_array("1",@@buy_via)`, Order: 1},
		{From: "g2", To: "online", Kind: "par", When: `in_array("2",@@buy_via)`, Order: 2},
		{From: "inquiry", To: "g3", Kind: "join"},
		{From: "online", To: "g3", Kind: "join"},
		{From: "g3", To: "final", Kind: "seq"},
		{From: "final", To: "end", Kind: "seq"},
	}}
}

func processRig(t *testing.T) (*rig, Board) {
	t.Helper()
	r := newRig()
	r.s.Org = fakeOrg{}
	r.s.People = func(u string) bool { return u != "ghost" }
	_, b := r.board(t)
	b, err := r.s.SaveProcess(ctx, owner, b.ID, miniFlow())
	if err != nil {
		t.Fatal(err)
	}
	return r, b
}

func threadAt(it Item, node string) Thread {
	for _, th := range it.Flow.Threads {
		if th.Node == node {
			return th
		}
	}
	return Thread{}
}

// The inquiry road: a parallel split, a pool claimed, a manager's step, the
// join waiting for both, and the last step for whoever finished before it.
func TestACaseRunsThroughSplitAndJoin(t *testing.T) {
	r, b := processRig(t)
	boss := Actor{User: "boss.man"}
	it, err := r.s.StartCase(ctx, owner, b.ID, map[string]any{"subject": "پمپ", "req_type": "2", "buy_via": []any{"1", "2"}, "amount": "۱۲٬۰۰۰"})
	if err != nil {
		t.Fatal(err)
	}
	f := it.Flow
	if f.Number != 1 || func() bool { n, _ := number(f.Data["amount"]); return n != 12000 }() || len(f.Threads) != 2 || !strings.Contains(it.Title, "پمپ") {
		t.Fatalf("started: %+v %+v", it.Title, f)
	}
	inq, onl := threadAt(it, "inquiry"), threadAt(it, "online")
	if len(inq.Assignees) != 0 || !slices.Equal(inq.Pool, []string{"k.hosseini", "viewer"}) || !slices.Equal(onl.Assignees, []string{"boss.man"}) {
		t.Fatalf("assigned: %+v %+v", inq, onl)
	}
	// The pool shows in its members' inboxes, to claim.
	in, _ := r.s.Inbox(ctx, mem)
	if len(in) != 1 || !in[0].Pool || in[0].Step != "ارسال استعلام" {
		t.Fatalf("k.hosseini's inbox: %+v", in)
	}
	if _, err := r.s.CompleteStep(ctx, mem, it.ID, inq.ID, nil); !IsForbidden(err) {
		t.Errorf("a pooled step done before it was claimed: %v", err)
	}
	if _, err := r.s.Claim(ctx, out, it.ID, inq.ID); !IsForbidden(err) && !IsNotFound(err) {
		t.Errorf("an outsider claimed: %v", err)
	}
	it, err = r.s.Claim(ctx, mem, it.ID, inq.ID)
	if err != nil || !slices.Equal(threadAt(it, "inquiry").Assignees, []string{"k.hosseini"}) {
		t.Fatalf("claimed: %v %+v", err, it.Flow.Threads)
	}
	if _, err := r.s.Claim(ctx, view, it.ID, inq.ID); err == nil {
		t.Error("a claimed step was claimed again")
	}
	it, err = r.s.CompleteStep(ctx, mem, it.ID, inq.ID, nil)
	if err != nil || len(it.Flow.Threads) != 1 || len(it.Flow.Waiting) != 1 || it.Flow.Done {
		t.Fatalf("after the inquiry, the join waits for the other branch: %v %+v", err, it.Flow)
	}
	it, err = r.s.CompleteStep(ctx, boss, it.ID, onl.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	fin := threadAt(it, "final")
	if len(it.Flow.Waiting) != 0 || len(it.Flow.Threads) != 1 || !slices.Equal(fin.Assignees, []string{"boss.man"}) {
		t.Fatalf("after both branches: %+v", it.Flow)
	}
	it, err = r.s.CompleteStep(ctx, boss, it.ID, fin.ID, nil)
	if err != nil || !it.Flow.Done || !isDone(b, it.Status) || len(it.Assignees) != 0 {
		t.Fatalf("finished: %v %+v %s", err, it.Flow, it.Status)
	}
	if n := len(it.Flow.Steps); n != 4 {
		t.Errorf("history: %d steps", n)
	}
}

// The tender road: the step goes to the person a field names (nobody named:
// the requester), and a lost tender ends the case there.
func TestATenderLostEndsTheCase(t *testing.T) {
	r, b := processRig(t)
	it, err := r.s.StartCase(ctx, owner, b.ID, map[string]any{"subject": "شیر", "req_type": "1"})
	if err != nil {
		t.Fatal(err)
	}
	ten := threadAt(it, "tender")
	if !slices.Equal(ten.Assignees, []string{"r.ahmadi"}) {
		t.Fatalf("the tender step: %+v", it.Flow.Threads)
	}
	if _, err := r.s.CompleteStep(ctx, owner, it.ID, ten.ID, map[string]any{}); err == nil {
		t.Error("a required answer was missing and the step was taken")
	}
	if _, err := r.s.CompleteStep(ctx, owner, it.ID, ten.ID, map[string]any{"tender_result": "3"}); err == nil {
		t.Error("an answer that is no option was taken")
	}
	// A shown-only field is not changed by the step.
	it, err = r.s.CompleteStep(ctx, owner, it.ID, ten.ID, map[string]any{"tender_result": "2", "subject": "دست‌کاری"})
	if err != nil || !it.Flow.Done || it.Flow.Data["subject"] != "شیر" {
		t.Fatalf("lost: %v %+v", err, it.Flow)
	}
}

// Answers that fit no route save nothing; answers that fit no form are
// refused; an outsider does no step and sees no case.
func TestWhatACaseRefuses(t *testing.T) {
	r, b := processRig(t)
	if _, err := r.s.StartCase(ctx, owner, b.ID, map[string]any{"subject": "x", "req_type": "2"}); err == nil ||
		!strings.Contains(err.Error(), "هیچ مسیری") {
		t.Errorf("a split with no branch to take: %v", err)
	}
	items, _ := r.s.Items(ctx, owner, b.ID)
	if len(items) != 0 {
		t.Errorf("a case that could not route was saved: %d", len(items))
	}
	if _, err := r.s.StartCase(ctx, out, b.ID, map[string]any{"subject": "x", "req_type": "1"}); !IsForbidden(err) {
		t.Errorf("an outsider started a case: %v", err)
	}
	it, _ := r.s.StartCase(ctx, owner, b.ID, map[string]any{"subject": "x", "req_type": "1"})
	if _, _, err := r.s.Case(ctx, out, it.ID); !IsNotFound(err) {
		t.Errorf("an outsider read a case: %v", err)
	}
	if _, err := r.s.CompleteStep(ctx, out, it.ID, it.Flow.Threads[0].ID, map[string]any{"tender_result": "2"}); !IsNotFound(err) {
		t.Errorf("an outsider did a step: %v", err)
	}
	for _, bad := range []map[string]any{
		{"subject": "x", "req_type": "1", "amount": "دوازده"},
		{"subject": "x", "req_type": "9"},
	} {
		if _, err := r.s.StartCase(ctx, owner, b.ID, bad); err == nil {
			t.Errorf("%v was taken", bad)
		}
	}
	// An edit of the item cannot touch its case; a save of the board cannot
	// drop its process.
	it.Flow = nil
	it.Title = "عنوان تازه"
	saved, err := r.s.SaveItem(ctx, owner, it)
	if err != nil || saved.Flow == nil || saved.Flow.Number == 0 {
		t.Errorf("an edit dropped the case: %v %+v", err, saved.Flow)
	}
	b.Process = nil
	b2, err := r.s.SaveBoard(ctx, owner, b)
	if err != nil || b2.Process == nil {
		t.Errorf("a board save dropped its process: %v", err)
	}
}

// A changed map is a new version; a case keeps the one it started on.
func TestACaseKeepsTheMapItStartedOn(t *testing.T) {
	r, b := processRig(t)
	it, _ := r.s.StartCase(ctx, owner, b.ID, map[string]any{"subject": "x", "req_type": "1"})
	p := miniFlow()
	p.Nodes[2].Name = "تایید مناقصه (تازه)"
	b, err := r.s.SaveProcess(ctx, owner, b.ID, p)
	if err != nil || b.Process.Version != 2 || len(b.ProcessHistory) != 1 {
		t.Fatalf("the new map: %v %+v", err, b.Process.Version)
	}
	_, old, err := r.s.Case(ctx, owner, it.ID)
	if err != nil || old.Version != 1 {
		t.Fatalf("the case's map: %v %d", err, old.Version)
	}
	if n, _ := old.node("tender"); n.Name != "تایید نهایی مناقصه" {
		t.Errorf("the case reads the new map: %q", n.Name)
	}
	if _, err := r.s.SaveProcess(ctx, mem, b.ID, p); !IsForbidden(err) {
		t.Errorf("a member who does not own the workspace changed its process: %v", err)
	}
	bad := miniFlow()
	bad.Edges = append(bad.Edges, Edge{From: "reg", To: "nowhere", Kind: "seq"})
	if _, err := r.s.SaveProcess(ctx, owner, b.ID, bad); err == nil {
		t.Error("a route to a node that is not there was taken")
	}
	bad = miniFlow()
	bad.Edges[1].When = "@@req_type ==="
	if _, err := r.s.SaveProcess(ctx, owner, b.ID, bad); err == nil {
		t.Error("an unreadable condition was taken")
	}
}

// Typed answers: a person must be one of docfind's; a date a real date; a
// table's rows each checked against its columns.
func TestAnswersAreCheckedByKind(t *testing.T) {
	people := func(u string) bool { return u == "k.hosseini" }
	fields := []FormField{
		{Key: "who", Label: "کارشناس", Kind: "person"},
		{Key: "when", Label: "مهلت", Kind: "date"},
		{Key: "lines", Label: "اقلام", Kind: "grid", Columns: []FormField{
			{Key: "item", Label: "کالا", Kind: "text", Required: true}, {Key: "qty", Label: "تعداد", Kind: "number"}}},
	}
	fields, err := cleanFields(fields, 10)
	if err != nil {
		t.Fatal(err)
	}
	got, err := checkAnswers(fields, map[string]any{"who": "K.Hosseini", "when": "2026-10-06",
		"lines": []any{map[string]any{"item": "پمپ", "qty": "۲"}, map[string]any{"item": "شیر"}}}, nil, people)
	if err != nil || got["who"] != "k.hosseini" || len(asList(got["lines"])) != 2 {
		t.Fatalf("%v %+v", err, got)
	}
	if row := asList(got["lines"])[0].(map[string]any); row["qty"] != 2.0 {
		t.Errorf("a row's number: %+v", row)
	}
	for _, bad := range []map[string]any{
		{"who": "ghost"}, {"when": "1405/07/14"}, {"lines": []any{map[string]any{"qty": "2"}}}, {"lines": "x"},
	} {
		if _, err := checkAnswers(fields, bad, nil, people); err == nil {
			t.Errorf("%v was taken", bad)
		}
	}
}

// A parallel branch that ends instead of reaching the join does not leave
// the other parked for ever, whichever finishes first.
func TestAJoinGoesOnWhenASiblingEnds(t *testing.T) {
	for _, first := range []string{"a", "b"} {
		r := newRig()
		_, b := r.board(t)
		p := Process{Name: "شاخه", Start: "s", Nodes: []Node{
			{ID: "s", Kind: "step", Name: "آغاز"},
			{ID: "split", Kind: "gateway"},
			{ID: "a", Kind: "step", Name: "شاخهٔ پایان‌یافته"},
			{ID: "b", Kind: "step", Name: "شاخهٔ به هم رسیده"},
			{ID: "meet", Kind: "gateway"},
			{ID: "last", Kind: "step", Name: "آخر"},
			{ID: "end", Kind: "end"},
		}, Edges: []Edge{
			{From: "s", To: "split", Kind: "seq"},
			{From: "split", To: "a", Kind: "par", Order: 1},
			{From: "split", To: "b", Kind: "par", Order: 2},
			{From: "a", To: "end", Kind: "seq"},
			{From: "b", To: "meet", Kind: "join"},
			{From: "meet", To: "last", Kind: "seq"},
			{From: "last", To: "end", Kind: "seq"},
		}}
		b, err := r.s.SaveProcess(ctx, owner, b.ID, p)
		if err != nil {
			t.Fatal(err)
		}
		it, err := r.s.StartCase(ctx, owner, b.ID, nil)
		if err != nil || len(it.Flow.Threads) != 2 {
			t.Fatalf("%s: started %v %+v", first, err, it.Flow)
		}
		order := []string{"a", "b"}
		if first == "b" {
			order = []string{"b", "a"}
		}
		for _, node := range order {
			if it, err = r.s.CompleteStep(ctx, owner, it.ID, threadAt(it, node).ID, nil); err != nil {
				t.Fatalf("%s first, %s: %v", first, node, err)
			}
		}
		if len(it.Flow.Waiting) != 0 || threadAt(it, "last").ID == "" {
			t.Errorf("%s first: the join did not go on: %+v", first, it.Flow)
		}
	}
}
