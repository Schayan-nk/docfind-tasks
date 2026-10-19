package tasks

import (
	"strings"
	"testing"
)

// The conditions a process map carries parse, and mean what PHP would make
// of them.
func TestAMapsConditionsAreRead(t *testing.T) {
	conds := []string{
		"(@@ship_to==2) && (@@carrier_ok==1)",
		"(@@ship_to==2) && (@@carrier_ok==2)",
		"(@@req_type == 2) or (@@tender_kind == 1)",
		"1", "@@QC_Check==1", "@@market_check==2", "@@req_type == 1",
		`in_array("1",@@buy_via)`, `in_array("2",@@buy_via)`,
	}
	for _, src := range conds {
		if _, err := ParseExpr(src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
	data := map[string]any{"ship_to": "2", "carrier_ok": 1.0, "req_type": "1", "tender_kind": "1",
		"buy_via": []any{"1", "3"}, "QC_Check": "1"}
	for src, want := range map[string]bool{
		"(@@ship_to==2) && (@@carrier_ok==1)":       true,
		"(@@ship_to==2) && (@@carrier_ok==2)":       false,
		"(@@req_type == 2) or (@@tender_kind == 1)": true,
		"(@@req_type == 2) || (@@tender_kind == 2)": false,
		`in_array("1",@@buy_via)`:                   true,
		`in_array("2",@@buy_via)`:                   false,
		"1":                                         true,
		"":                                          true,
		"0":                                         false,
		"@@QC_Check==1":                             true,
		"@@QC_Check===1":                            true,
		"@@QC_Check!=1":                             false,
		"@@missing==1":                              false,
		"@@missing==''":                             true,
		"empty(@@missing)":                          true,
		"!empty(@@buy_via)":                         true,
		"not (@@req_type == 2)":                     true,
		"@@req_type > 0 and @@req_type <= 1":        true,
		"price > 500000000":                         false,
	} {
		if got := ExprTrue(src, data); got != want {
			t.Errorf("%q = %v, want %v", src, got, want)
		}
	}
}

// What is not the language is refused, never run: no function but the two,
// no half an expression.
func TestAConditionThatIsNotTheLanguageIsRefused(t *testing.T) {
	for _, src := range []string{"shout('x')", "@@a ==", "(@@a == 1", "@@a = 1", "@@a == 'x", "double(1)", "@@a == 1 1"} {
		if _, err := ParseExpr(src); err == nil {
			t.Errorf("%q was taken", src)
		}
		if ExprTrue(src, map[string]any{"a": "1"}) {
			t.Errorf("%q was true", src)
		}
	}
	if got := strings.Join(ExprVars(`(@@ship_to==2) && in_array("1",@@buy_via)`), ","); got != "ship_to,buy_via" {
		t.Errorf("the variables read: %s", got)
	}
}
