package tasks

import "testing"

func TestEachConditionTestsWhatItSays(t *testing.T) {
	vals := map[string]any{
		"kind": "2", "parts": []any{"a", "c"}, "amount": "۱٬۵۰۰٬۰۰۰", "urgent": "1", "note": "", "zero": "0",
	}
	for _, c := range []struct {
		r    FieldRule
		want bool
	}{
		{FieldRule{"kind", "eq", []string{"2"}}, true},
		{FieldRule{"kind", "eq", []string{"2.0"}}, true}, // as numbers
		{FieldRule{"kind", "ne", []string{"2"}}, false},
		{FieldRule{"kind", "in", []string{"1", "2"}}, true},
		{FieldRule{"kind", "nin", []string{"1", "2"}}, false},
		{FieldRule{"parts", "eq", []string{"c"}}, true}, // one of several picks
		{FieldRule{"parts", "nin", []string{"b"}}, true},
		{FieldRule{"amount", "gt", []string{"1000000"}}, true}, // Persian digits and separators
		{FieldRule{"amount", "lt", []string{"1,000,000"}}, false},
		{FieldRule{"urgent", "eq", []string{"1"}}, true},
		{FieldRule{"note", "unset", nil}, true},
		{FieldRule{"missing", "unset", nil}, true},
		{FieldRule{"zero", "set", nil}, true}, // an unticked box was answered
		{FieldRule{"missing", "eq", []string{"x"}}, false},
		{FieldRule{"missing", "gt", []string{"1"}}, false},
	} {
		if got := ruleHolds(c.r, vals[c.r.Field]); got != c.want {
			t.Errorf("%+v: %v, want %v", c.r, got, c.want)
		}
	}
	if !rulesHold(nil, vals) {
		t.Error("no conditions do not hold")
	}
}

func TestConditionsAreCheckedWhenTheFormIsSaved(t *testing.T) {
	for _, bad := range [][]FieldRule{
		{{"other", "between", []string{"1"}}},
		{{"self", "eq", []string{"1"}}},
		{{"1st", "eq", []string{"1"}}},
		{{"other", "eq", nil}},
		{{"other", "gt", []string{"many"}}},
	} {
		if _, err := cleanRules(bad, "self"); err == nil {
			t.Errorf("%+v was taken", bad)
		}
	}
	got, err := cleanRules([]FieldRule{{" other ", "set", []string{"ignored"}}, {"other", "eq", []string{" a ", "b"}}}, "self")
	if err != nil || len(got) != 2 || got[0].Field != "other" || got[0].Value != nil || len(got[1].Value) != 1 || got[1].Value[0] != "a" {
		t.Errorf("cleaned: %+v %v", got, err)
	}
}

// A question not shown is not asked and not kept; one required only when
// another answer says so is required only then; the case's earlier answers
// count as much as the ones just sent.
func TestAnswersFollowTheirConditions(t *testing.T) {
	fields, err := cleanFields([]FormField{
		{Key: "kind", Label: "نوع", Kind: "choice", Choices: []Choice{{"1", "کالا"}, {"2", "خدمت"}}, Required: true},
		{Key: "qty", Label: "تعداد", Kind: "number", ShowIf: []FieldRule{{"kind", "eq", []string{"1"}}}, Required: true},
		{Key: "why", Label: "دلیل", Kind: "textarea", RequiredIf: []FieldRule{{"amount", "gt", []string{"1000"}}}},
	}, 40)
	if err != nil {
		t.Fatal(err)
	}
	person := func(string) bool { return true }
	// A service: the quantity is not asked, so not required -- and not kept.
	got, err := checkAnswers(fields, map[string]any{"kind": "2", "qty": "5"}, nil, person)
	if err != nil || got["qty"] != nil {
		t.Errorf("a hidden question: %v %v", got, err)
	}
	// Goods: the quantity is asked, and required.
	if _, err := checkAnswers(fields, map[string]any{"kind": "1"}, nil, person); err == nil {
		t.Error("a shown required question was left empty")
	}
	// The reason is required once the case's amount (an earlier step) is large.
	if _, err := checkAnswers(fields, map[string]any{"kind": "2"}, map[string]any{"amount": 5000.0}, person); err == nil {
		t.Error("a question required by an earlier answer was left empty")
	}
	if _, err := checkAnswers(fields, map[string]any{"kind": "2"}, map[string]any{"amount": 500.0}, person); err != nil {
		t.Errorf("not required below the amount: %v", err)
	}
}
