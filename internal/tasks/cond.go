package tasks

// Conditions on a process's routes (process.go), in the expression language
// PHP-based BPM tools write on their maps, read here instead of by PHP. The
// whole language: a case variable (@@name, or the bare name), a
// number or a quoted string, the comparisons == != === !== < > <= >=, the
// logic && || ! and its words and/or/not, brackets, and the functions
// in_array(needle, haystack) and empty(value). Comparison is PHP's loose kind:
// "1" == 1, and two numbers compare as numbers. Nothing here runs code; an
// expression that does not parse is false, and says why.
//
// Formulas (a calculated question, a step's "set" action) are the
// same language with arithmetic: + - * / %, a leading minus, and round(x, d),
// floor, ceil, abs, min(...), max(...) and if(test, then, else). A value that
// is not a number counts as 0 in arithmetic; dividing by 0 gives 0.

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Expr is a parsed route condition.
type Expr struct {
	src  string
	root condNode
}

type condNode interface {
	eval(data map[string]any) any
}

// ParseExpr reads a condition. "" and "1" are always true.
func ParseExpr(src string) (Expr, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return Expr{src: src, root: condLit{true}}, nil
	}
	p := &condParser{toks: nil}
	if err := p.lex(src); err != nil {
		return Expr{src: src}, err
	}
	n, err := p.or()
	if err != nil {
		return Expr{src: src}, err
	}
	if p.i < len(p.toks) {
		return Expr{src: src}, fmt.Errorf("condition %q: unexpected %q", src, p.toks[p.i].s)
	}
	return Expr{src: src, root: n}, nil
}

// True is the condition over a case's data. A condition that never parsed is
// false.
func (c Expr) True(data map[string]any) bool {
	if c.root == nil {
		return false
	}
	return truthy(c.root.eval(data))
}

// String is the condition as written.
func (c Expr) String() string { return c.src }

// Value is what the expression gives over a case's data: a number for
// arithmetic, true or false for a test. One that never parsed gives nil.
func (c Expr) Value(data map[string]any) any {
	if c.root == nil {
		return nil
	}
	return c.root.eval(data)
}

// ExprTrue parses and evaluates in one go; an unreadable condition is false.
func ExprTrue(src string, data map[string]any) bool {
	c, err := ParseExpr(src)
	if err != nil {
		return false
	}
	return c.True(data)
}

// ExprVars are the variables a condition reads, for checking a map against
// its forms.
func ExprVars(src string) []string {
	p := &condParser{}
	if p.lex(strings.TrimSpace(src)) != nil {
		return nil
	}
	var out []string
	for i, t := range p.toks {
		if t.k == tkIdent && !(i+1 < len(p.toks) && p.toks[i+1].s == "(") {
			name := t.s
			if !containsStr(out, name) {
				out = append(out, name)
			}
		}
	}
	return out
}

func containsStr(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------- lexing

type tokKind int

const (
	tkIdent tokKind = iota
	tkNum
	tkStr
	tkOp
)

type tok struct {
	k tokKind
	s string
}

type condParser struct {
	toks []tok
	i    int
}

func (p *condParser) lex(s string) error {
	r := []rune(s)
	for i := 0; i < len(r); {
		c := r[i]
		switch {
		case unicode.IsSpace(c):
			i++
		case c == '@' && i+2 < len(r) && strings.ContainsRune("@#=%?$&", r[i+1]):
			// @@name, @#name, @=name...: a case variable.
			j := i + 2
			for j < len(r) && (unicode.IsLetter(r[j]) || unicode.IsDigit(r[j]) || r[j] == '_') {
				j++
			}
			if j == i+2 {
				return fmt.Errorf("a variable with no name at %d", i)
			}
			p.toks = append(p.toks, tok{tkIdent, string(r[i+2 : j])})
			i = j
		case unicode.IsLetter(c) || c == '_':
			j := i
			for j < len(r) && (unicode.IsLetter(r[j]) || unicode.IsDigit(r[j]) || r[j] == '_') {
				j++
			}
			w := string(r[i:j])
			switch strings.ToLower(w) {
			case "and":
				p.toks = append(p.toks, tok{tkOp, "&&"})
			case "or":
				p.toks = append(p.toks, tok{tkOp, "||"})
			case "not":
				p.toks = append(p.toks, tok{tkOp, "!"})
			case "true":
				p.toks = append(p.toks, tok{tkNum, "1"})
			case "false":
				p.toks = append(p.toks, tok{tkNum, "0"})
			default:
				p.toks = append(p.toks, tok{tkIdent, w})
			}
			i = j
		case unicode.IsDigit(c) || (c == '.' && i+1 < len(r) && unicode.IsDigit(r[i+1])):
			j := i
			for j < len(r) && (unicode.IsDigit(r[j]) || r[j] == '.') {
				j++
			}
			p.toks = append(p.toks, tok{tkNum, string(r[i:j])})
			i = j
		case c == '"' || c == '\'':
			j := i + 1
			var b strings.Builder
			for j < len(r) && r[j] != c {
				if r[j] == '\\' && j+1 < len(r) {
					j++
				}
				b.WriteRune(r[j])
				j++
			}
			if j >= len(r) {
				return errors.New("a string with no end")
			}
			p.toks = append(p.toks, tok{tkStr, b.String()})
			i = j + 1
		default:
			// Longest first, so === is not read as == and =.
			matched := false
			for _, op := range []string{"===", "!==", "==", "!=", "<>", "<=", ">=", "&&", "||", "<", ">", "!", "(", ")", ",", "+", "-", "*", "/", "%"} {
				n := len(op) // every operator is ASCII
				if i+n <= len(r) && string(r[i:i+n]) == op {
					norm := op
					switch op {
					case "===":
						norm = "=="
					case "!==", "<>":
						norm = "!="
					}
					p.toks = append(p.toks, tok{tkOp, norm})
					i += n
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("%q is not understood", string(c))
			}
		}
	}
	return nil
}

// ------------------------------------------------------------- parsing

func (p *condParser) peek() string {
	if p.i < len(p.toks) && p.toks[p.i].k == tkOp {
		return p.toks[p.i].s
	}
	return ""
}

func (p *condParser) or() (condNode, error) {
	l, err := p.and()
	if err != nil {
		return nil, err
	}
	for p.peek() == "||" {
		p.i++
		r, err := p.and()
		if err != nil {
			return nil, err
		}
		l = condLogic{"||", l, r}
	}
	return l, nil
}

func (p *condParser) and() (condNode, error) {
	l, err := p.cmp()
	if err != nil {
		return nil, err
	}
	for p.peek() == "&&" {
		p.i++
		r, err := p.cmp()
		if err != nil {
			return nil, err
		}
		l = condLogic{"&&", l, r}
	}
	return l, nil
}

func (p *condParser) cmp() (condNode, error) {
	l, err := p.sum()
	if err != nil {
		return nil, err
	}
	switch op := p.peek(); op {
	case "==", "!=", "<", ">", "<=", ">=":
		p.i++
		r, err := p.sum()
		if err != nil {
			return nil, err
		}
		return condCmp{op, l, r}, nil
	}
	return l, nil
}

func (p *condParser) sum() (condNode, error) {
	l, err := p.product()
	if err != nil {
		return nil, err
	}
	for op := p.peek(); op == "+" || op == "-"; op = p.peek() {
		p.i++
		r, err := p.product()
		if err != nil {
			return nil, err
		}
		l = condArith{op, l, r}
	}
	return l, nil
}

func (p *condParser) product() (condNode, error) {
	l, err := p.unary()
	if err != nil {
		return nil, err
	}
	for op := p.peek(); op == "*" || op == "/" || op == "%"; op = p.peek() {
		p.i++
		r, err := p.unary()
		if err != nil {
			return nil, err
		}
		l = condArith{op, l, r}
	}
	return l, nil
}

func (p *condParser) unary() (condNode, error) {
	switch p.peek() {
	case "!":
		p.i++
		n, err := p.unary()
		if err != nil {
			return nil, err
		}
		return condNot{n}, nil
	case "-", "+":
		op := p.peek()
		p.i++
		n, err := p.unary()
		if err != nil {
			return nil, err
		}
		if op == "-" {
			return condArith{"-", condLit{0.0}, n}, nil
		}
		return condArith{"+", condLit{0.0}, n}, nil
	}
	return p.primary()
}

// condFuncs are the functions a condition or formula may call, with how many
// arguments each takes (-1: one or more; -2: one or two).
var condFuncs = map[string]int{"in_array": 2, "empty": 1, "round": -2, "floor": 1, "ceil": 1, "abs": 1, "min": -1, "max": -1, "if": 3}

func (p *condParser) primary() (condNode, error) {
	if p.i >= len(p.toks) {
		return nil, errors.New("the condition ends too soon")
	}
	t := p.toks[p.i]
	p.i++
	switch t.k {
	case tkNum:
		f, err := strconv.ParseFloat(t.s, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", t.s)
		}
		return condLit{f}, nil
	case tkStr:
		return condLit{t.s}, nil
	case tkIdent:
		if p.peek() == "(" {
			p.i++
			var args []condNode
			for p.peek() != ")" {
				a, err := p.or()
				if err != nil {
					return nil, err
				}
				args = append(args, a)
				if p.peek() == "," {
					p.i++
				} else if p.peek() != ")" {
					return nil, fmt.Errorf("%s(: a , or ) was expected", t.s)
				}
				if p.i >= len(p.toks) {
					return nil, errors.New("the condition ends too soon")
				}
			}
			p.i++
			fn := strings.ToLower(t.s)
			want, ok := condFuncs[fn]
			switch {
			case !ok:
				return nil, fmt.Errorf("%s() is not a function conditions may call", t.s)
			case want == -2 && (len(args) < 1 || len(args) > 2), want == -1 && len(args) < 1, want >= 0 && len(args) != want:
				return nil, fmt.Errorf("%s(): the wrong number of values", t.s)
			}
			return condCall{fn, args}, nil
		}
		return condVar{t.s}, nil
	case tkOp:
		if t.s == "(" {
			n, err := p.or()
			if err != nil {
				return nil, err
			}
			if p.peek() != ")" {
				return nil, errors.New("a ( with no )")
			}
			p.i++
			return n, nil
		}
	}
	return nil, fmt.Errorf("%q is out of place", t.s)
}

// ------------------------------------------------------------- evaluating

type condLit struct{ v any }
type condVar struct{ name string }
type condNot struct{ n condNode }
type condLogic struct {
	op   string
	l, r condNode
}
type condCmp struct {
	op   string
	l, r condNode
}
type condCall struct {
	fn   string
	args []condNode
}
type condArith struct {
	op   string
	l, r condNode
}

// num is a value as a number for arithmetic: 0 when it is none.
func num(v any) float64 {
	if n, ok := number(v); ok {
		return n
	}
	return 0
}

func (n condArith) eval(d map[string]any) any {
	a, b := num(n.l.eval(d)), num(n.r.eval(d))
	switch n.op {
	case "+":
		return a + b
	case "-":
		return a - b
	case "*":
		return a * b
	case "/":
		if b == 0 {
			return 0.0
		}
		return a / b
	case "%":
		if b == 0 {
			return 0.0
		}
		return math.Mod(a, b)
	}
	return 0.0
}

func (n condLit) eval(map[string]any) any { return n.v }
func (n condVar) eval(d map[string]any) any {
	if v, ok := d[n.name]; ok {
		return v
	}
	return nil
}
func (n condNot) eval(d map[string]any) any { return !truthy(n.n.eval(d)) }
func (n condLogic) eval(d map[string]any) any {
	if n.op == "&&" {
		return truthy(n.l.eval(d)) && truthy(n.r.eval(d))
	}
	return truthy(n.l.eval(d)) || truthy(n.r.eval(d))
}
func (n condCmp) eval(d map[string]any) any {
	c, ok := looseCompare(n.l.eval(d), n.r.eval(d))
	switch n.op {
	case "==":
		return ok && c == 0
	case "!=":
		return !ok || c != 0
	case "<":
		return ok && c < 0
	case ">":
		return ok && c > 0
	case "<=":
		return ok && c <= 0
	case ">=":
		return ok && c >= 0
	}
	return false
}
func (n condCall) eval(d map[string]any) any {
	switch n.fn {
	case "in_array":
		needle := n.args[0].eval(d)
		for _, v := range asList(n.args[1].eval(d)) {
			if c, ok := looseCompare(needle, v); ok && c == 0 {
				return true
			}
		}
		return false
	case "empty":
		return !truthy(n.args[0].eval(d))
	case "round":
		x, places := num(n.args[0].eval(d)), 0.0
		if len(n.args) > 1 {
			places = math.Max(0, math.Min(6, math.Round(num(n.args[1].eval(d)))))
		}
		f := math.Pow(10, places)
		return math.Round(x*f) / f
	case "floor":
		return math.Floor(num(n.args[0].eval(d)))
	case "ceil":
		return math.Ceil(num(n.args[0].eval(d)))
	case "abs":
		return math.Abs(num(n.args[0].eval(d)))
	case "min", "max":
		best := num(n.args[0].eval(d))
		for _, a := range n.args[1:] {
			x := num(a.eval(d))
			if (n.fn == "min" && x < best) || (n.fn == "max" && x > best) {
				best = x
			}
		}
		return best
	case "if":
		if truthy(n.args[0].eval(d)) {
			return n.args[1].eval(d)
		}
		return n.args[2].eval(d)
	}
	return false
}

// asList is a value as a list: a list as it is, anything else alone.
func asList(v any) []any {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		return x
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	}
	return []any{v}
}

// number reads a value as a number, when it is one.
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case string:
		s := strings.TrimSpace(strings.NewReplacer(",", "", "٬", "").Replace(latinDigits(x)))
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	}
	return 0, false
}

// looseCompare is PHP's ==: numbers as numbers, else as text. ok is false
// when one side is a list.
func looseCompare(a, b any) (int, bool) {
	if _, isList := a.([]any); isList {
		return 0, false
	}
	if _, isList := b.([]any); isList {
		return 0, false
	}
	if a == nil {
		a = ""
	}
	if b == nil {
		b = ""
	}
	if x, ok := number(a); ok {
		if y, ok := number(b); ok {
			switch {
			case x < y:
				return -1, true
			case x > y:
				return 1, true
			}
			return 0, true
		}
	}
	return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)), true
}

// truthy is PHP's: "", "0", 0, false, nothing and an empty list are false.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case int:
		return x != 0
	case string:
		return x != "" && x != "0"
	case []any:
		return len(x) > 0
	case []string:
		return len(x) > 0
	}
	return true
}
