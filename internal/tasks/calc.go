package tasks

// Calculated questions and formulas. A question of kind "calc" is
// not filled by anybody: its Formula, in the condition language with
// arithmetic (cond.go), is worked out over the other answers -- on the page as
// people type, and again here when the answers are saved, and the value
// worked out here is the one kept. A step's "set" action (actions.go) and a
// question's Default use the same formulas.
//
// A formula may start with "=", as in a spreadsheet; a Default or a "set"
// value that does not is plain text.

import (
	"math"
	"regexp"
	"strings"
	"sync"
)

// formulaOf is a formula's text without its leading "=".
func formulaOf(s string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "="))
}

// cleanFormula checks a formula reads; it is kept as written.
func cleanFormula(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return "", Invalid{"فرمول بیش از ۵۰۰ نویسه است."}
	}
	if _, err := ParseExpr(formulaOf(s)); err != nil {
		return "", Invalid{"فرمول خوانده نشد: " + err.Error()}
	}
	return s, nil
}

// calcValue is a calculated question's value over the answers: a number,
// rounded to its decimals. A formula that does not read gives nothing.
func calcValue(f FormField, vals map[string]any) any {
	e, err := ParseExpr(formulaOf(f.Formula))
	if err != nil || f.Formula == "" {
		return nil
	}
	n, ok := number(e.Value(vals))
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	p := math.Pow(10, float64(f.Decimals))
	return math.Round(n*p) / p
}

// valueOf is what a "set" action or a Default gives: a formula's value when
// it starts with "=", else the text itself.
func valueOf(src string, vals map[string]any) any {
	src = strings.TrimSpace(src)
	if !strings.HasPrefix(src, "=") {
		return src
	}
	e, err := ParseExpr(formulaOf(src))
	if err != nil {
		return nil
	}
	v := e.Value(vals)
	if n, ok := v.(float64); ok && (math.IsNaN(n) || math.IsInf(n, 0)) {
		return nil
	}
	return v
}

// A question's pattern, compiled once: matched against the whole answer.
var patterns sync.Map // string -> *regexp.Regexp

func patternOf(p string) (*regexp.Regexp, error) {
	if re, ok := patterns.Load(p); ok {
		return re.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(`^(?:` + p + `)$`)
	if err != nil {
		return nil, err
	}
	patterns.Store(p, re)
	return re, nil
}
