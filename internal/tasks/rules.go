package tasks

import (
	"errors"
	"slices"
	"strconv"
	"strings"
)

// Conditions on a question (FormField.ShowIf, RequiredIf): show it, or make
// it required, only when other answers say so. The page tests the same rules
// as people type (taskflow.js); the server tests them again on what is sent,
// so a question hidden on the page cannot be made to skip a requirement, and
// one that should not have been shown cannot slip an answer in.

// FieldRule is one test on another answer. Field is its key -- on a process step,
// any answer the case already holds -- Op one of RuleOps, Value what it is
// compared with (several for in and nin; none for set and unset).
type FieldRule struct {
	Field string   `json:"field"`
	Op    string   `json:"op"`
	Value []string `json:"value,omitempty"`
}

// RuleOps: equal, not equal, one of, none of, answered, not answered,
// greater than, less than.
var RuleOps = []string{"eq", "ne", "in", "nin", "set", "unset", "gt", "lt"}

// cleanRules checks a question's conditions; self is its own key, which no
// condition may test.
func cleanRules(in []FieldRule, self string) ([]FieldRule, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > 10 {
		return nil, errors.New("بیش از ۱۰ شرط")
	}
	var out []FieldRule
	for _, r := range in {
		r.Field = strings.TrimSpace(r.Field)
		if !fieldKey.MatchString(r.Field) || strings.EqualFold(r.Field, self) {
			return nil, errors.New("شرطی به پرسشی بسته است که نیست")
		}
		if !slices.Contains(RuleOps, r.Op) {
			return nil, errors.New("شرطی شناخته نیست")
		}
		var vals []string
		for _, v := range r.Value {
			if v = strings.TrimSpace(v); v != "" && len(vals) < 50 {
				vals = append(vals, clipText(v, 200))
			}
		}
		switch r.Op {
		case "set", "unset":
			vals = nil
		case "in", "nin":
			if len(vals) == 0 {
				return nil, errors.New("شرطی مقداری ندارد")
			}
		default:
			if len(vals) == 0 {
				return nil, errors.New("شرطی مقداری ندارد")
			}
			if r.Op == "gt" || r.Op == "lt" {
				if _, ok := ruleNumber(vals[0]); !ok {
					return nil, errors.New("شرط «بیشتر از / کمتر از» عدد می‌خواهد")
				}
			}
			vals = vals[:1]
		}
		r.Value = vals
		out = append(out, r)
	}
	return out, nil
}

// rulesMatch says whether the rules hold on vals: every one, or -- any --
// at least one. No rules always hold.
func rulesMatch(rules []FieldRule, any bool, vals map[string]any) bool {
	if !any || len(rules) == 0 {
		return rulesHold(rules, vals)
	}
	for _, r := range rules {
		if ruleHolds(r, vals[r.Field]) {
			return true
		}
	}
	return false
}

// rulesHold says whether every rule holds on vals; no rules always hold.
func rulesHold(rules []FieldRule, vals map[string]any) bool {
	for _, r := range rules {
		if !ruleHolds(r, vals[r.Field]) {
			return false
		}
	}
	return true
}

func ruleHolds(r FieldRule, v any) bool {
	switch r.Op {
	case "set":
		return !blank(v)
	case "unset":
		return blank(v)
	}
	have := ruleTexts(v)
	match := func(want string) bool {
		for _, h := range have {
			if ruleSame(h, want) {
				return true
			}
		}
		return false
	}
	switch r.Op {
	case "eq":
		return match(r.Value[0])
	case "ne":
		return !match(r.Value[0])
	case "in", "nin":
		in := slices.ContainsFunc(r.Value, match)
		return in == (r.Op == "in")
	case "gt", "lt":
		want, ok := ruleNumber(r.Value[0])
		if !ok || len(have) == 0 {
			return false
		}
		n, ok := ruleNumber(have[0])
		if !ok {
			return false
		}
		if r.Op == "gt" {
			return n > want
		}
		return n < want
	}
	return false
}

// ruleTexts is an answer as the texts a rule compares: each of a multiple
// choice's picks, or the one value; a tick is "1" or "0".
func ruleTexts(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, strings.TrimSpace(latinDigits(asString(e))))
		}
		return out
	case []string:
		var out []string
		for _, e := range x {
			out = append(out, strings.TrimSpace(latinDigits(e)))
		}
		return out
	case bool:
		if x {
			return []string{"1"}
		}
		return []string{"0"}
	}
	return []string{strings.TrimSpace(latinDigits(asString(v)))}
}

// ruleSame is equal as text, or as numbers ("1" and "1.0", "5,000" and 5000).
func ruleSame(a, b string) bool {
	b = strings.TrimSpace(latinDigits(b))
	if strings.EqualFold(a, b) {
		return true
	}
	x, ok1 := ruleNumber(a)
	y, ok2 := ruleNumber(b)
	return ok1 && ok2 && x == y
}

func ruleNumber(s string) (float64, bool) {
	s = strings.NewReplacer(",", "", "٬", "", " ", "", "_", "").Replace(strings.TrimSpace(latinDigits(s)))
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(s, 64)
	return n, err == nil
}
