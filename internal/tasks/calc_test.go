package tasks

import (
	"context"
	"strings"
	"testing"
)

// Formulas: arithmetic in the order it is written in school, the functions,
// Persian digits and separators in the answers, and nothing that breaks on
// a missing answer or a division by zero.
func TestFormulasWorkOutWhatTheySay(t *testing.T) {
	data := map[string]any{"sales": "۱۳٬۳۳۸٬۰۰۰٬۰۰۰", "buy": 3718000000.0, "costs": int64(3927900000), "rate": "7", "zero": 0.0, "type": "MGMT"}
	for src, want := range map[string]float64{
		"1 + 2 * 3":           7,
		"(1 + 2) * 3":         9,
		"-2 + 5":              3,
		"10 % 4":              2,
		"sales - buy - costs": 5692100000,
		"max(0, sales - buy - costs) * rate / 100": 398447000,
		"round(10 / 3, 2)":                         3.33,
		"floor(2.7) + ceil(2.1) + abs(-1)":         6,
		"min(4, 2, 9)":                             2,
		"if(type == \"MGMT\", 7, 12)":              7,
		"if(missing > 0, 1, 2)":                    2,
		"sales / zero":                             0,
		"missing * 3":                              0,
	} {
		e, err := ParseExpr(src)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		if got := num(e.Value(data)); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
	for _, bad := range []string{"1 +", "round()", "round(1, 2, 3)", "system(1)", "if(1, 2)"} {
		if _, err := ParseExpr(bad); err == nil {
			t.Errorf("%q was read", bad)
		}
	}
	// Conditions written before formulas existed read as they did.
	if !ExprTrue("@@req_type == 1 && !empty(@@buy)", map[string]any{"req_type": "1", "buy": "x"}) {
		t.Error("an old condition changed meaning")
	}
}

// A calculated question is worked out from the answers as kept, in order,
// and whatever the page sent for it is not what counts.
func TestACalculatedQuestionIsWorkedOutHere(t *testing.T) {
	asked := []FormField{
		{Key: "qty", Label: "تعداد", Kind: "number", Required: true},
		{Key: "price", Label: "قیمت واحد", Kind: "money", Required: true},
		{Key: "total", Label: "جمع", Kind: "calc", Formula: "= qty * price"},
		{Key: "vat", Label: "مالیات", Kind: "calc", Formula: "total * 0.1", Decimals: 0},
	}
	if _, err := cleanFields(append(asked, FormField{Key: "bad", Label: "بد", Kind: "calc"}), 40); err == nil {
		t.Fatal("a calculated question with no formula was taken")
	}
	fields, err := cleanFields(asked, 40)
	if err != nil {
		t.Fatal(err)
	}
	got, err := checkAnswers(fields, map[string]any{"qty": "۳", "price": "1,250,000", "total": "1"}, nil, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if got["total"] != 3750000.0 || got["vat"] != 375000.0 {
		t.Errorf("worked out: %v", got)
	}
	if _, err := cleanFields([]FormField{{Key: "x", Label: "x", Kind: "calc", Formula: "1 +"}}, 5); err == nil {
		t.Error("a formula that does not read was taken")
	}
}

// Any, read-only when, bounds and patterns.
func TestTheRicherConditionsHold(t *testing.T) {
	lo, hi := 1.0, 100.0
	fields, err := cleanFields([]FormField{
		{Key: "kind", Label: "نوع", Kind: "choice", Choices: []Choice{{"1", "کالا"}, {"2", "خدمت"}, {"3", "هر دو"}}},
		{Key: "qty", Label: "تعداد", Kind: "number", Min: &lo, Max: &hi,
			ShowIf: []FieldRule{{"kind", "eq", []string{"1"}}, {"kind", "eq", []string{"3"}}}, ShowAny: true},
		{Key: "mobile", Label: "همراه", Kind: "text", Pattern: `09\d{9}`, PatternMsg: "شمارهٔ همراه ۱۱ رقمی است."},
		{Key: "price", Label: "قیمت", Kind: "money", ReadOnlyIf: []FieldRule{{"locked", "set", nil}}},
	}, 40)
	if err != nil {
		t.Fatal(err)
	}
	ok := func(string) bool { return true }
	// "هر دو" shows the quantity through the second rule.
	if got, err := checkAnswers(fields, map[string]any{"kind": "3", "qty": "5"}, nil, ok); err != nil || got["qty"] != 5.0 {
		t.Errorf("any: %v %v", got, err)
	}
	if got, _ := checkAnswers(fields, map[string]any{"kind": "2", "qty": "5"}, nil, ok); got["qty"] != nil {
		t.Errorf("a hidden quantity was kept: %v", got)
	}
	if _, err := checkAnswers(fields, map[string]any{"kind": "1", "qty": "500"}, nil, ok); err == nil || !strings.Contains(err.Error(), "۱۰۰") && !strings.Contains(err.Error(), "100") {
		t.Errorf("over the maximum: %v", err)
	}
	if _, err := checkAnswers(fields, map[string]any{"mobile": "0912"}, nil, ok); err == nil || !strings.Contains(err.Error(), "۱۱ رقمی") {
		t.Errorf("a pattern: %v", err)
	}
	if _, err := checkAnswers(fields, map[string]any{"mobile": "۰۹۱۲۱۲۳۴۵۶۷"}, nil, ok); err != nil {
		t.Errorf("a mobile in Persian digits: %v", err)
	}
	// Locked: the price sent is not taken; the case keeps its own.
	if got, _ := checkAnswers(fields, map[string]any{"price": "99"}, map[string]any{"locked": "1", "price": int64(5)}, ok); got["price"] != nil {
		t.Errorf("a read-only price was changed: %v", got)
	}
	if _, err := cleanFields([]FormField{{Key: "x", Label: "x", Kind: "text", Pattern: "(("}}, 5); err == nil {
		t.Error("a pattern that does not read was taken")
	}
}

// A step's actions: a set before routing (so the routes see it), its rules,
// a notice to the requester with the answers in it, and a webhook after the
// save.
func TestAStepsActionsRun(t *testing.T) {
	p := Process{Name: "خرید", Start: "ask", Nodes: []Node{
		{ID: "ask", Kind: "step", Name: "درخواست", Assign: Assign{Mode: "requester"},
			Fields: []FormField{{Key: "amount", Label: "مبلغ", Kind: "money", Required: true}},
			After: []StepAction{
				{Kind: "set", Field: "big", Formula: "= amount > 1000"},
				{Kind: "set", Field: "fee", Formula: "= round(amount * 0.07)"},
				{Kind: "set", Field: "note", Formula: "بزرگ", If: []FieldRule{{"big", "eq", []string{"1"}}}},
				{Kind: "notify", To: &Assign{Mode: "manager"}, Text: "درخواست {amount} ریال؛ کارمزد {fee}"},
				{Kind: "webhook", URL: "https://hooks.example/step"},
			}},
		{ID: "ceo", Kind: "step", Name: "مدیرعامل", Assign: Assign{Mode: "users", Users: []string{"boss.man"}},
			Before: []StepAction{{Kind: "set", Field: "seen", Formula: "بله"}}},
		{ID: "end", Kind: "end"},
	}, Edges: []Edge{
		{From: "ask", To: "ceo", Kind: "xor", Rules: []FieldRule{{"big", "eq", []string{"1"}}}, Order: 1},
		{From: "ask", To: "end", Kind: "xor", Order: 2},
	}}
	r := newRig()
	r.s.Org = fakeOrg{}
	var told []Notice
	var posted []string
	r.s.Notify = func(_ context.Context, n Notice) { told = append(told, n) }
	r.s.Hook = func(_ context.Context, h Webhook, event string, _ []byte) { posted = append(posted, h.URL+" "+event) }
	_, b := r.board(t)
	b, err := r.s.SaveProcess(ctx, owner, b.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	it, err := r.s.StartCase(ctx, owner, b.ID, map[string]any{"amount": "5000"})
	if err != nil {
		t.Fatal(err)
	}
	d := it.Flow.Data
	if d["big"] != true || d["fee"] != 350.0 || d["note"] != "بزرگ" || d["seen"] != "بله" || threadAt(it, "ceo").ID == "" {
		t.Fatalf("after the first step: %v %+v", d, it.Flow.Threads)
	}
	var notice *Notice
	for i := range told {
		if strings.Contains(told[i].Body, "کارمزد") {
			notice = &told[i]
		}
	}
	if notice == nil || notice.To[0] != "boss.man" || !strings.Contains(notice.Body, "5000") || !strings.Contains(notice.Body, "350") {
		t.Errorf("the notice: %+v", told)
	}
	if len(posted) != 1 || posted[0] != "https://hooks.example/step step.after" {
		t.Errorf("the webhook: %v", posted)
	}
	// A small amount: the rule keeps the note out, and the case ends.
	it, err = r.s.StartCase(ctx, owner, b.ID, map[string]any{"amount": "10"})
	if err != nil || it.Flow.Data["note"] != nil || !it.Flow.Done {
		t.Errorf("a small amount: %v %+v", err, it.Flow)
	}
	bad := p
	bad.Nodes = append([]Node{}, p.Nodes...)
	bad.Nodes[0].After = []StepAction{{Kind: "webhook", URL: "ftp://x"}}
	if _, err := r.s.SaveProcess(ctx, owner, b.ID, bad); err == nil {
		t.Error("an action with a bad address was saved")
	}
}
