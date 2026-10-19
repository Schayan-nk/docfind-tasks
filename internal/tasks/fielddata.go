package tasks

// Questions whose options or value come from somewhere else: the server's own
// lists, or a database through a query saved on the question. Source says
// where:
//
//	people       the people with an account here   (a choice or several)
//	departments  the departments
//	db:<source>  a database the host server names: SQL is the query, its
//	             first column the value, its second the label; for a text,
//	             number or amount question its first row's first column
//	             fills the answer. This module's server names none, so its
//	             form maker never offers one (find/fielddata.go).
//
// "@@key" in a query is the answer to question key, bound as a parameter, and
// the page asks again when one of those answers changes (Deps), so one
// dropdown can narrow another. The query runs only as saved -- the page names
// the question, never sends SQL -- and only an administrator may add or
// change one.

import (
	"context"
	"regexp"
	"slices"
	"strings"
)

var listSources = []string{"people", "departments"}

// FieldData answers a sourced question.
type FieldData interface {
	// Choices are what the question's source gives for these answers: its
	// options, or -- a filled question -- the value as the first. q narrows
	// a long list to what matches it.
	Choices(ctx context.Context, f FormField, vals map[string]any, q string) ([]Choice, error)
}

// sourced says a question's options or value really come from a source.
func sourced(f FormField) bool {
	return slices.Contains(listSources, f.Source) || strings.HasPrefix(f.Source, "db:")
}

var sqlRef = regexp.MustCompile(`@@([A-Za-z_][A-Za-z0-9_]{0,63})`)

// sqlDeps are the answers a query names.
func sqlDeps(sql string) []string {
	var out []string
	for _, m := range sqlRef.FindAllStringSubmatch(sql, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// cleanSource checks where a question's options or value come from.
func cleanSource(f *FormField) error {
	f.Source, f.SQL = strings.TrimSpace(f.Source), strings.TrimSpace(f.SQL)
	f.Deps = nil
	switch {
	case f.Source == "":
		f.SQL = ""
	case slices.Contains(listSources, f.Source):
		if f.Kind != "choice" && f.Kind != "multi" {
			return Invalid{"«" + f.Label + "»: فهرست‌های docfind فقط برای پرسش انتخابی است."}
		}
		f.SQL = ""
	case strings.HasPrefix(f.Source, "db:") && len(f.Source) > 3 && len(f.Source) <= 80:
		switch f.Kind {
		case "choice", "multi", "text", "textarea", "number", "money":
		default:
			return Invalid{"«" + f.Label + "»: این نوع پرسش از پایگاه داده پر نمی‌شود."}
		}
		if f.SQL == "" {
			return Invalid{"«" + f.Label + "»: پرس‌وجوی پایگاه داده ندارد."}
		}
		if len(f.SQL) > 4000 {
			return Invalid{"«" + f.Label + "»: پرس‌وجو بیش از ۴۰۰۰ نویسه است."}
		}
		f.Deps = sqlDeps(f.SQL)
	default:
		return Invalid{"«" + f.Label + "»: منبع داده شناخته نیست."}
	}
	if f.Kind != "choice" || (f.Display != "radio" && f.Display != "search") {
		f.Display = ""
	}
	return nil
}

// sqlEdited says whether questions add or change a database query against
// those saved before; only docfind's administrators may.
func sqlEdited(old, new map[string]FormField) bool {
	for k, f := range new {
		if f.SQL == "" {
			continue
		}
		if o, ok := old[k]; !ok || o.SQL != f.SQL || o.Source != f.Source {
			return true
		}
	}
	return false
}

func formFieldsByKey(b Board) map[string]FormField {
	out := map[string]FormField{}
	for _, f := range b.Forms {
		for _, q := range f.Fields {
			out[f.ID+"/"+q.Key] = q
		}
	}
	return out
}

func nodeFieldsByKey(p *Process) map[string]FormField {
	out := map[string]FormField{}
	if p == nil {
		return out
	}
	for _, n := range p.Nodes {
		for _, q := range n.Fields {
			out[n.ID+"/"+q.Key] = q
		}
	}
	return out
}

var errSQLAdmin = Invalid{"پرس‌وجوی پایگاه داده را فقط مدیر docfind می‌نویسد یا تغییر می‌دهد."}

// checkSourced checks what was picked on a sourced choice against what its
// source gives now: a pick the list does not hold is refused, as one off a
// fixed list is.
func (s *Service) checkSourced(ctx context.Context, fields []FormField, raw, have map[string]any) error {
	vals := map[string]any{}
	for k, v := range have {
		vals[k] = v
	}
	for k, v := range raw {
		vals[k] = v
	}
	for _, f := range fields {
		if !sourced(f) || f.Mode != "" || (f.Kind != "choice" && f.Kind != "multi") {
			continue
		}
		if !rulesMatch(f.ShowIf, f.ShowAny, vals) {
			continue
		}
		v, sent := raw[f.Key]
		if !sent || blank(v) {
			continue
		}
		if s.Data == nil {
			return Invalid{"«" + f.Label + "»: منبع گزینه‌هایش در دسترس نیست."}
		}
		for _, x := range asList(v) {
			pick := strings.TrimSpace(latinDigits(asString(x)))
			if pick == "" {
				continue
			}
			cs, err := s.Data.Choices(ctx, f, vals, pick)
			if err != nil {
				return Invalid{"«" + f.Label + "»: گزینه‌هایش خوانده نشد؛ کمی بعد دوباره بفرستید."}
			}
			if !slices.ContainsFunc(cs, func(c Choice) bool { return c.Value == pick }) {
				return Invalid{"«" + f.Label + "» یکی از گزینه‌ها نیست."}
			}
		}
	}
	return nil
}

// FieldFor is a question of a board's intake form (formID) or of its
// process's step (nodeID), for somebody who may fill it: who sees the board,
// anybody for an open form, whoever may start the process for its first step,
// and whoever a case's step at that node waits for.
func (s *Service) FieldFor(ctx context.Context, a Actor, boardID, formID, nodeID, key string) (FormField, error) {
	b, err := s.Store.Board(ctx, boardID)
	if err != nil {
		return FormField{}, err
	}
	w, err := s.workspace(ctx, b.WorkspaceID)
	if err != nil {
		return FormField{}, err
	}
	var fields []FormField
	switch {
	case formID != "":
		i := slices.IndexFunc(b.Forms, func(f Form) bool { return f.ID == formID })
		if i < 0 || (!b.Forms[i].Open && !a.sees(w)) {
			return FormField{}, ErrNotFound
		}
		fields = b.Forms[i].Fields
	case nodeID != "" && b.Process != nil:
		n, ok := b.Process.node(nodeID)
		if !ok {
			return FormField{}, ErrNotFound
		}
		may := a.sees(w) || (nodeID == b.Process.Start && mayStart(n.Assign, a.User))
		if !may {
			items, err := s.Store.Items(ctx, Query{BoardIDs: []string{b.ID}})
			if err != nil {
				return FormField{}, err
			}
			for _, it := range items {
				if it.Flow == nil {
					continue
				}
				for _, t := range it.Flow.Threads {
					if t.Node == nodeID && (slices.Contains(t.Assignees, a.User) || slices.Contains(t.Pool, a.User)) {
						may = true
					}
				}
			}
		}
		if !may {
			return FormField{}, ErrNotFound
		}
		fields = n.Fields
	default:
		return FormField{}, ErrNotFound
	}
	i := slices.IndexFunc(fields, func(f FormField) bool { return f.Key == key })
	if i < 0 || !sourced(fields[i]) {
		return FormField{}, ErrNotFound
	}
	return fields[i], nil
}

// PublicFields are questions as a page that fills them gets them: the query
// stays here, only what it depends on goes.
func PublicFields(in []FormField) []FormField {
	out := make([]FormField, len(in))
	for i, f := range in {
		if f.SQL != "" {
			f.Deps = sqlDeps(f.SQL)
			f.SQL = ""
		}
		out[i] = f
	}
	return out
}
