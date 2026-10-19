package tasks

// Fields and their answers, for a board's intake forms and for a
// process's steps alike. A field has a kind, a Persian label, whether it must
// be filled, and -- on a process step -- whether it is filled there, only
// shown, or hidden. Answers are checked here, on the server, whatever the page
// already checked, and kept as typed values: a number as a number, money as
// whole rials, a date as 2006-01-02 (the page shows it in the Jalali
// calendar), a choice as its value, a table as rows.

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// FieldKinds are the kinds a field may be. heading only shows its label.
// calc is worked out from the others (calc.go), never filled.
// file takes attachments (files.go).
var FieldKinds = []string{"text", "textarea", "number", "money", "date", "choice", "multi", "check", "person", "department", "grid", "heading", "calc", "file"}

// columnKinds are what a table's columns may be.
var columnKinds = []string{"text", "number", "money", "date", "choice", "check", "person"}

// Choice is one option of a choice: what is stored, and what is shown.
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// FormFill is an intake form's answers kept on the item it made: the
// questions as they were asked, so the answers read right whatever the form
// becomes.
type FormFill struct {
	FormID    string         `json:"form_id"`
	Name      string         `json:"name"`
	Questions []FormField    `json:"questions"`
	Answers   map[string]any `json:"answers"`
}

// cleanFields checks a list of fields: keys unique and shaped, labels there,
// kinds known, choices and columns sound.
func cleanFields(in []FormField, max int) ([]FormField, error) {
	if len(in) > max {
		return nil, Invalid{"بیش از " + itoa(max) + " پرسش."}
	}
	seen := map[string]bool{}
	out := []FormField{}
	for i, f := range in {
		f.Key = strings.TrimSpace(f.Key)
		if f.Kind == "heading" && f.Key == "" {
			f.Key = "h" + strconv.Itoa(i+1)
		}
		if !fieldKey.MatchString(f.Key) || seen[strings.ToLower(f.Key)] {
			f.Key = "q" + strconv.Itoa(i+1)
			for seen[f.Key] {
				f.Key += "_"
			}
		}
		seen[strings.ToLower(f.Key)] = true
		if f.Label = oneLine(f.Label, 160); f.Label == "" {
			continue
		}
		if !slices.Contains(FieldKinds, f.Kind) {
			f.Kind = "text"
		}
		switch f.Mode {
		case "", "view", "hidden":
		default:
			f.Mode = ""
		}
		f.Hint = clipText(f.Hint, 400)
		var err error
		if f.ShowIf, err = cleanRules(f.ShowIf, f.Key); err != nil {
			return nil, Invalid{"«" + f.Label + "»: " + err.Error()}
		}
		if f.RequiredIf, err = cleanRules(f.RequiredIf, f.Key); err != nil {
			return nil, Invalid{"«" + f.Label + "»: " + err.Error()}
		}
		if f.ReadOnlyIf, err = cleanRules(f.ReadOnlyIf, f.Key); err != nil {
			return nil, Invalid{"«" + f.Label + "»: " + err.Error()}
		}
		f.ShowAny = f.ShowAny && len(f.ShowIf) > 1
		f.RequiredAny = f.RequiredAny && len(f.RequiredIf) > 1
		f.ReadOnlyAny = f.ReadOnlyAny && len(f.ReadOnlyIf) > 1
		if f.Kind == "calc" {
			if f.Formula, err = cleanFormula(f.Formula); err != nil || f.Formula == "" {
				if err == nil {
					err = Invalid{"فرمول ندارد."}
				}
				return nil, Invalid{"«" + f.Label + "»: " + err.Error()}
			}
			if f.Decimals < 0 {
				f.Decimals = 0
			} else if f.Decimals > 6 {
				f.Decimals = 6
			}
			f.Required, f.RequiredIf, f.ReadOnlyIf = false, nil, nil
		} else {
			f.Formula, f.Decimals = "", 0
		}
		if f.Kind != "number" && f.Kind != "money" {
			f.Min, f.Max = nil, nil
		}
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			return nil, Invalid{"«" + f.Label + "»: کمینه از بیشینه بزرگ‌تر است."}
		}
		f.Pattern = strings.TrimSpace(f.Pattern)
		if f.Kind != "text" && f.Kind != "textarea" {
			f.Pattern = ""
		}
		if f.Pattern != "" {
			if len(f.Pattern) > 300 {
				return nil, Invalid{"«" + f.Label + "»: الگو بیش از ۳۰۰ نویسه است."}
			}
			if _, err := patternOf(f.Pattern); err != nil {
				return nil, Invalid{"«" + f.Label + "»: الگو خوانده نشد."}
			}
		}
		f.PatternMsg = oneLine(f.PatternMsg, 200)
		if f.Pattern == "" {
			f.PatternMsg = ""
		}
		if f.Default = strings.TrimSpace(f.Default); len(f.Default) > 500 {
			f.Default = f.Default[:500]
		}
		if strings.HasPrefix(f.Default, "=") {
			if _, err := cleanFormula(f.Default); err != nil {
				return nil, Invalid{"«" + f.Label + "»، مقدار پیش‌فرض: " + err.Error()}
			}
		}
		if err := cleanSource(&f); err != nil {
			return nil, err
		}
		f.Choices = cleanChoices(f.Choices, f.Options)
		f.Options = nil
		if sourced(f) && f.Kind != "text" && f.Kind != "textarea" && f.Kind != "number" && f.Kind != "money" {
			f.Choices = nil
		} else if (f.Kind == "choice" || f.Kind == "multi") && len(f.Choices) == 0 {
			return nil, Invalid{"«" + f.Label + "» گزینه‌ای ندارد."}
		}
		if f.Kind == "grid" {
			if len(f.Columns) == 0 || len(f.Columns) > 20 {
				return nil, Invalid{"جدول «" + f.Label + "» باید بین ۱ و ۲۰ ستون داشته باشد."}
			}
			cols, err := cleanFields(f.Columns, 20)
			if err != nil {
				return nil, err
			}
			for i := range cols {
				if !slices.Contains(columnKinds, cols[i].Kind) {
					cols[i].Kind = "text"
				}
				cols[i].Mode, cols[i].Columns = "", nil
				cols[i].Source, cols[i].SQL, cols[i].Deps, cols[i].Display = "", "", nil, ""
				cols[i].ShowIf, cols[i].RequiredIf, cols[i].ReadOnlyIf = nil, nil, nil
				cols[i].ShowAny, cols[i].RequiredAny, cols[i].ReadOnlyAny = false, false, false
			}
			f.Columns = cols
		} else {
			f.Columns = nil
		}
		if f.Kind == "heading" {
			f.Required = false
		}
		out = append(out, f)
	}
	return out, nil
}

// cleanChoices are a field's options as value and label; plain options (the
// older forms') are both.
func cleanChoices(cs []Choice, opts []string) []Choice {
	for _, o := range opts {
		cs = append(cs, Choice{Value: o, Label: o})
	}
	var out []Choice
	for _, c := range cs {
		c.Value, c.Label = oneLine(c.Value, 120), oneLine(c.Label, 120)
		if c.Label == "" {
			c.Label = c.Value
		}
		if c.Value == "" || slices.ContainsFunc(out, func(x Choice) bool { return x.Value == c.Value }) || len(out) >= 300 {
			continue
		}
		out = append(out, c)
	}
	return out
}

// Answers are what a person sent for a set of fields: checked against them,
// kept typed. Only the fields filled here count; a field sent empty is
// cleared, one not sent is left as it was. person checks a username is one
// of docfind's.
func checkAnswers(fields []FormField, raw, have map[string]any, person func(string) bool) (map[string]any, error) {
	// What the conditions are tested against: what the case already holds,
	// with what was just sent over it.
	vals := map[string]any{}
	for k, v := range have {
		vals[k] = v
	}
	for k, v := range raw {
		vals[k] = v
	}
	out := map[string]any{}
	for _, f := range fields {
		if f.Mode != "" || f.Kind == "heading" || f.Kind == "calc" {
			continue
		}
		// Not shown: not asked, not required, and whatever came is not kept.
		if !rulesMatch(f.ShowIf, f.ShowAny, vals) {
			continue
		}
		// Read-only here: what the case holds stays, whatever was sent.
		if len(f.ReadOnlyIf) > 0 && rulesMatch(f.ReadOnlyIf, f.ReadOnlyAny, vals) {
			continue
		}
		required := f.Required || (len(f.RequiredIf) > 0 && rulesMatch(f.RequiredIf, f.RequiredAny, vals))
		v, sent := raw[f.Key]
		if !sent || blank(v) {
			if required {
				return nil, Invalid{"«" + f.Label + "» را پر کنید."}
			}
			if sent {
				out[f.Key] = ""
			}
			continue
		}
		val, err := checkOne(f, v, person)
		if err != nil {
			return nil, err
		}
		out[f.Key] = val
	}
	// The calculated ones, in order, over the answers as they were kept:
	// each sees the ones before it.
	typed := map[string]any{}
	for k, v := range have {
		typed[k] = v
	}
	for k, v := range out {
		typed[k] = v
	}
	for _, f := range fields {
		if f.Kind != "calc" || f.Mode != "" || !rulesMatch(f.ShowIf, f.ShowAny, typed) {
			continue
		}
		if v := calcValue(f, typed); v != nil {
			out[f.Key], typed[f.Key] = v, v
		}
	}
	return out, nil
}

// bounds checks a number against a question's Min and Max.
func bounds(f FormField, n float64) error {
	if f.Min != nil && n < *f.Min {
		return Invalid{"«" + f.Label + "» باید دست‌کم " + numText(*f.Min) + " باشد."}
	}
	if f.Max != nil && n > *f.Max {
		return Invalid{"«" + f.Label + "» باید حداکثر " + numText(*f.Max) + " باشد."}
	}
	return nil
}

func numText(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }

func checkOne(f FormField, v any, person func(string) bool) (any, error) {
	bad := func(what string) error { return Invalid{"«" + f.Label + "» " + what} }
	if f.Kind == "file" {
		return checkFiles(f, v)
	}
	s := strings.TrimSpace(latinDigits(asString(v)))
	switch f.Kind {
	case "text", "textarea":
		if f.Kind == "text" {
			s = oneLine(s, 500)
		} else {
			s = clipText(s, 8000)
		}
		if f.Pattern != "" {
			if re, err := patternOf(f.Pattern); err == nil && !re.MatchString(s) {
				if f.PatternMsg != "" {
					return nil, bad(f.PatternMsg)
				}
				return nil, bad("شکل درستی ندارد.")
			}
		}
		return s, nil
	case "number":
		n, err := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(s, ",", ""), "٬", ""), 64)
		if err != nil {
			return nil, bad("باید عدد باشد.")
		}
		if err := bounds(f, n); err != nil {
			return nil, err
		}
		return n, nil
	case "money":
		digits := strings.NewReplacer(",", "", "٬", "", " ", "", "_", "").Replace(s)
		n, err := strconv.ParseInt(digits, 10, 64)
		if err != nil || n < 0 {
			return nil, bad("باید مبلغی به ریال، بی ممیز، باشد.")
		}
		if err := bounds(f, float64(n)); err != nil {
			return nil, err
		}
		return n, nil
	case "date":
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return nil, bad("تاریخ درستی نیست.")
		}
		return s, nil
	case "choice":
		// A sourced one was checked against its source (checkSourced).
		if !sourced(f) && !slices.ContainsFunc(f.Choices, func(c Choice) bool { return c.Value == s }) {
			return nil, bad("یکی از گزینه‌ها نیست.")
		}
		return s, nil
	case "multi":
		var picked []any
		for _, x := range asList(v) {
			xs := strings.TrimSpace(latinDigits(asString(x)))
			if xs == "" {
				continue
			}
			if !sourced(f) && !slices.ContainsFunc(f.Choices, func(c Choice) bool { return c.Value == xs }) {
				return nil, bad("یکی از گزینه‌ها نیست.")
			}
			if !slices.Contains(picked, any(xs)) {
				picked = append(picked, xs)
			}
		}
		if picked == nil {
			picked = []any{}
		}
		return picked, nil
	case "check":
		switch strings.ToLower(s) {
		case "1", "true", "on", "yes", "بله":
			return true, nil
		}
		return false, nil
	case "person":
		u := strings.ToLower(s)
		if !userShape.MatchString(u) || (person != nil && !person(u)) {
			return nil, bad("کاربر docfind نیست.")
		}
		return u, nil
	case "department":
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return nil, bad("واحد درستی نیست.")
		}
		return n, nil
	case "grid":
		rows, ok := v.([]any)
		if !ok {
			return nil, bad("جدول درستی نیست.")
		}
		if len(rows) > 300 {
			return nil, bad("بیش از ۳۰۰ ردیف دارد.")
		}
		var out []any
		for _, r := range rows {
			m, ok := r.(map[string]any)
			if !ok {
				return nil, bad("ردیفی درست نیست.")
			}
			row, err := checkAnswers(f.Columns, m, nil, person)
			if err != nil {
				return nil, err
			}
			if len(row) > 0 {
				out = append(out, row)
			}
		}
		if out == nil {
			out = []any{}
		}
		return out, nil
	}
	return oneLine(s, 500), nil
}

// blank is an answer with nothing in it.
func blank(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	}
	return false
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		if x {
			return "1"
		}
		return "0"
	case nil:
		return ""
	}
	return ""
}

// latinDigits writes Persian and Arabic digits as 0-9.
func latinDigits(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '۰' && r <= '۹':
			return '0' + (r - '۰')
		case r >= '٠' && r <= '٩':
			return '0' + (r - '٠')
		}
		return r
	}, s)
}

// answerText is an answer as a person reads it: a choice's label, money with
// its thousands marked, yes or no, how many rows a table has.
func answerText(f FormField, v any) string {
	label := func(val string) string {
		for _, c := range f.Choices {
			if c.Value == val {
				return c.Label
			}
		}
		return val
	}
	switch f.Kind {
	case "choice":
		return label(asString(v))
	case "multi":
		var parts []string
		for _, x := range asList(v) {
			parts = append(parts, label(asString(x)))
		}
		return strings.Join(parts, "، ")
	case "money":
		n, _ := number(v)
		return groupThousands(strconv.FormatInt(int64(n), 10)) + " ریال"
	case "check":
		if truthy(v) {
			return "بله"
		}
		return "خیر"
	case "grid":
		return itoa(len(asList(v))) + " ردیف"
	}
	return asString(v)
}

// groupThousands puts a comma between every three digits.
func groupThousands(s string) string {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
