package tasks

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// The owner's example: IF an inquiry is created as urgent THEN assign it to
// the workspace's owners, due in two days, add the technical review as a
// subtask, and tell them -- and what the rule changed does not fire it again.
func TestARuleRunsOnceWhenItsTriggerHappens(t *testing.T) {
	r := newRig()
	w, b := r.board(t)
	b.Rules = []Rule{{Name: "استعلام فوری", Enabled: true, When: Trigger{Kind: "created"},
		If: []Cond{{Field: "label", Op: "has", Value: "فوری"}},
		Then: []Action{{Kind: "assign", Users: []string{"@owners"}}, {Kind: "set_due", Days: 2},
			{Kind: "add_check", Value: "تماس با مشتری"}, {Kind: "create_item", Title: "بررسی فنی {title}", Sub: true},
			{Kind: "notify", Users: []string{"@owners"}, Title: "استعلام فوری: {title}"},
			{Kind: "set_status", Value: "todo"}}}}
	b, err := r.s.SaveBoard(ctx, owner, b)
	if err != nil {
		t.Fatal(err)
	}
	_ = w
	it, err := r.s.SaveItem(ctx, mem, Item{BoardID: b.ID, Title: "پمپ", Labels: []string{"فوری"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(it.Assignees, "r.ahmadi") || it.Due == "" || it.Status != "todo" || len(it.Checklist) != 1 {
		t.Errorf("after the rule: %+v", it)
	}
	subs, _ := r.s.Store.Items(ctx, Query{ParentID: it.ID})
	if len(subs) != 1 || subs[0].Title != "بررسی فنی پمپ" {
		t.Errorf("the created subtask: %+v", subs)
	}
	if !slices.ContainsFunc(r.notices, func(n Notice) bool { return n.Title == "استعلام فوری: پمپ" }) {
		t.Errorf("not told: %+v", r.notices)
	}
	// Not an urgent one: nothing.
	plain, _ := r.s.SaveItem(ctx, mem, Item{BoardID: b.ID, Title: "عادی"})
	if len(plain.Assignees) != 0 || plain.Status != "backlog" {
		t.Errorf("a rule ran on an item that does not meet it: %+v", plain)
	}
}

func TestTheButtonsStartCompleteAndEscalate(t *testing.T) {
	r := newRig()
	_, b := r.board(t)
	it, _ := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, Title: "x", Checklist: []CheckItem{{Text: "a"}, {Text: "b"}}})
	it, err := r.s.Press(ctx, mem, it.ID, "start")
	if err != nil || it.Status != "doing" || !slices.Contains(it.Assignees, "k.hosseini") || len(it.Time) != 1 || it.Time[0].End != nil {
		t.Fatalf("start: %+v %v", it, err)
	}
	it, err = r.s.Press(ctx, mem, it.ID, "complete")
	if err != nil || it.Status != "done" || it.Completed == nil || it.Time[0].End == nil || it.ActualMin < 1 {
		t.Fatalf("complete: %+v %v", it, err)
	}
	for _, c := range it.Checklist {
		if !c.Done {
			t.Error("the checklist was not completed")
		}
	}
	it, err = r.s.Press(ctx, mem, it.ID, "escalate")
	if err != nil || it.Priority != "critical" || !slices.Contains(it.Assignees, "r.ahmadi") {
		t.Errorf("escalate: %+v %v", it, err)
	}
	if _, err := r.s.Press(ctx, view, it.ID, "start"); !IsForbidden(err) {
		t.Errorf("a viewer pressed a button: %v", err)
	}
	// A plain save of what the page read keeps the work log.
	it.Title = "y"
	saved, err := r.s.SaveItem(ctx, owner, Item{ID: it.ID, BoardID: b.ID, Title: "y", Status: it.Status, Version: it.Version})
	if err != nil || len(saved.Time) != 1 || saved.ActualMin != it.ActualMin {
		t.Errorf("the work log after a plain save: %+v %v", saved, err)
	}
}

func TestTheSweepRemindsOnce(t *testing.T) {
	r := newRig()
	_, b := r.board(t)
	b.Rules = []Rule{{Name: "دیرکرد", Enabled: true, When: Trigger{Kind: "overdue"}, Then: []Action{{Kind: "set_priority", Value: "high"}}}}
	b, _ = r.s.SaveBoard(ctx, owner, b)
	now := r.s.now()
	soon, _ := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, Title: "فردا", Assignees: []string{"k.hosseini"}, Due: now.AddDate(0, 0, 1).Format("2006-01-02")})
	late, _ := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, Title: "دیر", Assignees: []string{"k.hosseini"}, Due: now.AddDate(0, 0, -3).Format("2006-01-02")})
	r.notices = nil
	if n := r.s.Sweep(ctx, r.s.now()); n != 3 {
		t.Errorf("the first sweep fired %d, want 3 (tomorrow, late, the rule)", n)
	}
	if n := r.s.Sweep(ctx, r.s.now()); n != 0 {
		t.Errorf("the second sweep fired %d again", n)
	}
	got, _, _, _ := r.s.Item(ctx, owner, late.ID)
	if got.Priority != "high" {
		t.Errorf("the overdue rule: %+v", got)
	}
	var titles []string
	for _, n := range r.notices {
		titles = append(titles, n.Title)
	}
	if !slices.Contains(titles, "سررسید فردا: «فردا»") || !slices.Contains(titles, "دیرکرد: «دیر»") {
		t.Errorf("reminders: %v", titles)
	}
	_ = soon
}

func TestTimeIsLogged(t *testing.T) {
	r := newRig()
	_, b := r.board(t)
	it, _ := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, Title: "x"})
	if _, err := r.s.Timer(ctx, mem, it.ID, "start", 0, ""); err != nil {
		t.Fatal(err)
	}
	it, err := r.s.Timer(ctx, mem, it.ID, "stop", 0, "")
	if err != nil || len(it.Time) != 1 || it.Time[0].Minutes < 1 {
		t.Fatalf("stop: %+v %v", it.Time, err)
	}
	it, _ = r.s.Timer(ctx, mem, it.ID, "add", 90, "جلسه")
	if it.ActualMin != it.Time[0].Minutes+90 {
		t.Errorf("actual %d", it.ActualMin)
	}
	rows, _ := r.s.Timesheet(ctx, mem, "", "")
	if len(rows) != 2 {
		t.Errorf("the member's timesheet: %+v", rows)
	}
	if rows, _ := r.s.Timesheet(ctx, out, "", ""); len(rows) != 0 {
		t.Error("an outsider saw the timesheet")
	}
	if _, err := r.s.Timer(ctx, mem, it.ID, "add", 0, ""); err == nil {
		t.Error("zero minutes were logged")
	}
}

func TestAnApprovalGoesThroughItsSteps(t *testing.T) {
	r := newRig()
	w, b := r.board(t)
	w.Members = append(w.Members, Member{User: "ceo", Role: "member"}, Member{User: "fin", Role: "member"})
	if _, err := r.s.SaveWorkspace(ctx, owner, w); err != nil {
		t.Fatal(err)
	}
	it, _ := r.s.SaveItem(ctx, mem, Item{BoardID: b.ID, Title: "پیش‌فاکتور"})
	if _, err := r.s.StartApproval(ctx, mem, it.ID, []Step{{Approvers: []string{"stranger"}}}, ""); err == nil {
		t.Error("a non-member was made an approver")
	}
	it, err := r.s.StartApproval(ctx, mem, it.ID, []Step{
		{Name: "مدیر فروش", Approvers: []string{"r.ahmadi"}},
		{Name: "مالی و مدیرعامل", Approvers: []string{"fin", "ceo"}, Mode: "all"}}, "")
	if err != nil || it.Approval.State != "pending" {
		t.Fatalf("start: %+v %v", it.Approval, err)
	}
	if _, err := r.s.Decide(ctx, Actor{User: "ceo"}, it.ID, "approve", "", ""); !IsForbidden(err) {
		t.Errorf("the second step decided first: %v", err)
	}
	it, _ = r.s.Decide(ctx, owner, it.ID, "approve", "خوب است", "")
	if it.Approval.At != 1 {
		t.Fatalf("after the first step: %+v", it.Approval)
	}
	it, _ = r.s.Decide(ctx, Actor{User: "fin"}, it.ID, "delegate", "", "k.hosseini")
	if !slices.Contains(it.Approval.Steps[1].Approvers, "k.hosseini") || slices.Contains(it.Approval.Steps[1].Approvers, "fin") {
		t.Fatalf("delegated: %+v", it.Approval.Steps[1])
	}
	mine, _, _ := r.s.MyApprovals(ctx, mem)
	if len(mine) != 1 {
		t.Errorf("my approvals: %d", len(mine))
	}
	it, _ = r.s.Decide(ctx, mem, it.ID, "approve", "", "")
	if it.Approval.State != "pending" {
		t.Fatalf("one of two in an all step closed it: %+v", it.Approval)
	}
	it, _ = r.s.Decide(ctx, Actor{User: "ceo"}, it.ID, "approve", "", "")
	if it.Approval.State != "approved" || it.Status != "approved" {
		t.Errorf("approved: %+v status %s", it.Approval, it.Status)
	}
	if _, err := r.s.Decide(ctx, owner, it.ID, "approve", "", ""); err == nil {
		t.Error("a decision after the end was taken")
	}
}

func TestAnOpenFormTakesRequestsFromOutside(t *testing.T) {
	r := newRig()
	_, b := r.board(t)
	b.Fields = []Field{{Key: "qty", Name: "تعداد", Kind: "number"}}
	b.Forms = []Form{{Name: "درخواست فناوری اطلاعات", Open: true, Assignees: []string{"k.hosseini"}, Fields: []FormField{
		{Key: "what", Label: "چه لازم است", Required: true}, {Key: "qty", Label: "تعداد", Kind: "number"}}}}
	b, err := r.s.SaveBoard(ctx, owner, b)
	if err != nil {
		t.Fatal(err)
	}
	forms, _ := r.s.Forms(ctx, out)
	if len(forms) != 1 {
		t.Fatalf("the outsider's forms: %+v", forms)
	}
	f := forms[0].Form
	if _, err := r.s.Submit(ctx, out, b.ID, f.ID, map[string]any{"qty": "2"}); err == nil {
		t.Error("a required answer was missing and the form was taken")
	}
	it, err := r.s.Submit(ctx, out, b.ID, f.ID, map[string]any{"what": "لپ‌تاپ", "qty": "2"})
	if err != nil || !strings.Contains(it.Title, "لپ‌تاپ") || it.Fields["qty"] != 2.0 || it.Creator != "outsider" ||
		!slices.Contains(it.Assignees, "k.hosseini") {
		t.Errorf("the request: %+v %v", it, err)
	}
	// The answers are kept typed on the item, with the questions as asked.
	if it.Form == nil || it.Form.Answers["qty"] != 2.0 || it.Form.Answers["what"] != "لپ‌تاپ" || len(it.Form.Questions) != 2 {
		t.Errorf("the answers kept: %+v", it.Form)
	}
	b.Forms[0].Open = false
	b, _ = r.s.SaveBoard(ctx, owner, b)
	if _, err := r.s.Submit(ctx, out, b.ID, f.ID, map[string]any{"what": "x"}); !IsNotFound(err) {
		t.Errorf("a closed form took an outsider's request: %v", err)
	}
}

func TestBoardsFromTemplates(t *testing.T) {
	r := newRig()
	w, _ := r.board(t)
	tender, err := r.s.FromTemplate(ctx, owner, w.ID, "tender", "")
	if err != nil {
		t.Fatal(err)
	}
	items, _ := r.s.Items(ctx, owner, tender.ID)
	if len(items) != 9 || tender.Name != "مناقصهٔ تازه" {
		t.Fatalf("tender: %d items", len(items))
	}
	chained := 0
	for _, it := range items {
		if len(it.Deps) == 1 && it.Deps[0].Kind == "fs" {
			chained++
		}
	}
	if chained != 8 {
		t.Errorf("the tender's steps are not chained: %d", chained)
	}
	offer, _ := r.s.FromTemplate(ctx, owner, w.ID, "offer", "پیشنهاد پمپ")
	items, _ = r.s.Items(ctx, owner, offer.ID)
	if len(items) != 8 {
		t.Errorf("offer: %d items", len(items))
	}
	offer.Template = true
	if _, err := r.s.SaveBoard(ctx, owner, offer); err != nil {
		t.Fatal(err)
	}
	copyB, err := r.s.FromTemplate(ctx, owner, w.ID, offer.ID, "نسخه")
	if err != nil {
		t.Fatal(err)
	}
	items, _ = r.s.Items(ctx, owner, copyB.ID)
	subs := 0
	for _, it := range items {
		if it.ParentID != "" {
			subs++
		}
	}
	if len(items) != 8 || subs != 7 {
		t.Errorf("the copy: %d items, %d subtasks", len(items), subs)
	}
	refs, _ := r.s.Templates(ctx, owner)
	if !slices.ContainsFunc(refs, func(x TemplateRef) bool { return x.ID == offer.ID }) || len(refs) != 4 {
		t.Errorf("templates: %+v", refs)
	}
}

func TestTheDashboardAndTheWebhooks(t *testing.T) {
	r := newRig()
	var posted []string
	r.s.Hook = func(_ context.Context, h Webhook, event string, body []byte) {
		posted = append(posted, event+":"+h.Secret+":"+string(body))
	}
	_, b := r.board(t)
	b.Webhooks = []Webhook{{URL: "https://erp.example/hook", Events: []string{"created", "moved"}, Secret: "s3"}}
	b, err := r.s.SaveBoard(ctx, owner, b)
	if err != nil {
		t.Fatal(err)
	}
	// The page never sees the secret; sending the hook back without it keeps it.
	b.Webhooks[0].Secret = ""
	if b, err = r.s.SaveBoard(ctx, owner, b); err != nil || b.Webhooks[0].Secret != "s3" {
		t.Fatalf("the secret after a save without it: %+v %v", b.Webhooks, err)
	}
	b.Webhooks = []Webhook{{URL: "ftp://x"}}
	if _, err := r.s.SaveBoard(ctx, owner, b); err == nil {
		t.Error("an ftp webhook was accepted")
	}
	now := r.s.now()
	it, _ := r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, Title: "یک", Assignees: []string{"k.hosseini"}, Due: now.AddDate(0, 0, -1).Format("2006-01-02")})
	_, _ = r.s.SaveItem(ctx, owner, Item{BoardID: b.ID, Title: "دو", Status: "done"})
	if len(posted) != 2 || !strings.HasPrefix(posted[0], "created:s3:") || !strings.Contains(posted[0], "یک") {
		t.Errorf("posted: %v", posted)
	}
	d, err := r.s.Dashboard(ctx, owner)
	if err != nil || d.Total != 2 || d.Open != 1 || d.Overdue != 1 || d.DoneWeek != 1 || d.Rate != 0.5 {
		t.Errorf("dashboard: %+v %v", d, err)
	}
	if len(d.ByPerson) != 1 || d.ByPerson[0].Name != "k.hosseini" {
		t.Errorf("by person: %+v", d.ByPerson)
	}
	if d, _ := r.s.Dashboard(ctx, out); d.Total != 0 {
		t.Error("an outsider's dashboard counted the board")
	}
	_ = it
}
