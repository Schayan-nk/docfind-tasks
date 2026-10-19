package tasks

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// What makes a workspace, board or item storable. Everything a page sends is
// trimmed, bounded and checked here, so the store never holds a status that is
// not a column, a date that is not a day, or a link that leads off the
// application.

// NewID is a random id, 16 hex characters.
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var (
	keyShape = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	// fieldKey is a field's key. The variables of maps drawn in other tools
	// keep their case (QC_Check), so this one does too.
	fieldKey   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	colorShape = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	userShape  = regexp.MustCompile(`^[\p{L}\p{N}._@-]{1,64}$`)
)

func clipText(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n])
	}
	return s
}

// oneLine is a name: one line, its runs of space made one.
func oneLine(s string, n int) string {
	return clipText(strings.Join(strings.Fields(s), " "), n)
}

func validDay(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// users is a list of usernames, each once, lowercased.
func users(in []string, max int) ([]string, error) {
	out := []string{}
	for _, u := range in {
		u = strings.ToLower(strings.TrimSpace(u))
		if u == "" || slices.Contains(out, u) {
			continue
		}
		if !userShape.MatchString(u) {
			return nil, Invalid{"نام کاربری «" + u + "» درست نیست."}
		}
		out = append(out, u)
	}
	if len(out) > max {
		return nil, Invalid{"بیش از اندازه نفر."}
	}
	return out, nil
}

func cleanWorkspace(w Workspace) (Workspace, error) {
	w.Name = oneLine(w.Name, 80)
	if w.Name == "" {
		return w, Invalid{"فضای کار نامی می‌خواهد."}
	}
	w.Description = clipText(w.Description, 1000)
	w.Department = oneLine(w.Department, 80)
	w.Team = oneLine(w.Team, 80)
	var ms []Member
	seen := map[string]bool{}
	for _, m := range w.Members {
		u := strings.ToLower(strings.TrimSpace(m.User))
		if u == "" || seen[u] {
			continue
		}
		if !userShape.MatchString(u) {
			return w, Invalid{"نام کاربری «" + u + "» درست نیست."}
		}
		switch m.Role {
		case "owner", "member", "viewer":
		default:
			m.Role = "member"
		}
		seen[u] = true
		ms = append(ms, Member{User: u, Role: m.Role})
	}
	if len(ms) > 500 {
		return w, Invalid{"بیش از اندازه عضو."}
	}
	if ms == nil {
		ms = []Member{}
	}
	w.Members = ms
	return w, nil
}

func cleanBoard(b Board) (Board, error) {
	b.Name = oneLine(b.Name, 80)
	if b.Name == "" {
		return b, Invalid{"بورد نامی می‌خواهد."}
	}
	b.Description = clipText(b.Description, 2000)
	if b.Kind != "project" {
		b.Kind = "board"
	}
	if len(b.Statuses) == 0 {
		b.Statuses = DefaultStatuses()
	}
	if len(b.Statuses) > 20 {
		return b, Invalid{"یک بورد حداکثر ۲۰ ستون دارد."}
	}
	seen := map[string]bool{}
	for i, s := range b.Statuses {
		s.Key = strings.ToLower(strings.TrimSpace(s.Key))
		if !keyShape.MatchString(s.Key) || seen[s.Key] {
			return b, Invalid{"شناسهٔ ستون «" + s.Key + "» درست یا یکتا نیست."}
		}
		seen[s.Key] = true
		s.Name = oneLine(s.Name, 40)
		if s.Name == "" {
			s.Name = s.Key
		}
		if !colorShape.MatchString(s.Color) {
			s.Color = ""
		}
		s.WIP = max(0, min(s.WIP, 999))
		b.Statuses[i] = s
	}
	if len(b.Fields) > 40 {
		return b, Invalid{"یک بورد حداکثر ۴۰ ستون سفارشی دارد."}
	}
	fseen := map[string]bool{}
	for i, f := range b.Fields {
		f.Key = strings.ToLower(strings.TrimSpace(f.Key))
		if !keyShape.MatchString(f.Key) || fseen[f.Key] {
			return b, Invalid{"شناسهٔ ستون سفارشی «" + f.Key + "» درست یا یکتا نیست."}
		}
		fseen[f.Key] = true
		f.Name = oneLine(f.Name, 40)
		switch f.Kind {
		case "text", "number", "date", "choice", "person", "check", "link":
		default:
			f.Kind = "text"
		}
		var opts []string
		for _, o := range f.Options {
			if o = oneLine(o, 60); o != "" && !slices.Contains(opts, o) && len(opts) < 50 {
				opts = append(opts, o)
			}
		}
		f.Options = opts
		b.Fields[i] = f
	}
	if b.Fields == nil {
		b.Fields = []Field{}
	}
	var labels []Label
	for _, l := range b.Labels {
		if l.Name = oneLine(l.Name, 40); l.Name != "" && len(labels) < 60 {
			if !colorShape.MatchString(l.Color) {
				l.Color = ""
			}
			labels = append(labels, l)
		}
	}
	if labels == nil {
		labels = []Label{}
	}
	b.Labels = labels
	var groups []string
	for _, g := range b.Groups {
		if g = oneLine(g, 60); g != "" && !slices.Contains(groups, g) && len(groups) < 40 {
			groups = append(groups, g)
		}
	}
	if groups == nil {
		groups = []string{}
	}
	b.Groups = groups
	return b, nil
}

var linkKinds = []string{"customer", "order", "email", "document", "tender", "supplier", "contract", "page"}
var depKinds = []string{"fs", "ss", "ff", "sf", "blocks", "related", "duplicate"}

// cleanItem checks an item against its board.
func cleanItem(it Item, b Board) (Item, error) {
	it.Title = oneLine(it.Title, 200)
	if it.Title == "" {
		return it, Invalid{"کار عنوانی می‌خواهد."}
	}
	it.Description = clipText(it.Description, 20000)
	if it.Status == "" {
		it.Status = b.Statuses[0].Key
	}
	if !slices.ContainsFunc(b.Statuses, func(s Status) bool { return s.Key == it.Status }) {
		return it, Invalid{"ستون «" + it.Status + "» در این بورد نیست."}
	}
	if !slices.Contains(Priorities, it.Priority) {
		it.Priority = "normal"
	}
	var err error
	if it.Assignees, err = users(it.Assignees, 30); err != nil {
		return it, err
	}
	if it.Watchers, err = users(it.Watchers, 100); err != nil {
		return it, err
	}
	it.Department = oneLine(it.Department, 80)
	it.Team = oneLine(it.Team, 80)
	it.Group = oneLine(it.Group, 60)
	if !validDay(it.Start) || !validDay(it.Due) {
		return it, Invalid{"تاریخ باید یک روز باشد."}
	}
	if it.Start != "" && it.Due != "" && it.Due < it.Start {
		return it, Invalid{"سررسید پیش از شروع است."}
	}
	it.EstimateMin = max(0, min(it.EstimateMin, 1_000_000))
	it.ActualMin = max(0, min(it.ActualMin, 1_000_000))
	var labels []string
	for _, l := range it.Labels {
		if l = oneLine(l, 40); l != "" && !slices.Contains(labels, l) && len(labels) < 20 {
			labels = append(labels, l)
		}
	}
	if labels == nil {
		labels = []string{}
	}
	it.Labels = labels
	fields := map[string]any{}
	for k, v := range it.Fields {
		i := slices.IndexFunc(b.Fields, func(f Field) bool { return f.Key == k })
		if i < 0 || v == nil {
			continue
		}
		switch x := v.(type) {
		case string:
			fields[k] = clipText(x, 2000)
		case float64, bool:
			fields[k] = x
		}
	}
	it.Fields = fields
	if len(it.Checklist) > 200 {
		return it, Invalid{"یک فهرست بررسی حداکثر ۲۰۰ مورد دارد."}
	}
	var checks []CheckItem
	for _, c := range it.Checklist {
		if c.Text = oneLine(c.Text, 300); c.Text == "" {
			continue
		}
		if c.ID == "" || !keyShape.MatchString(c.ID) {
			c.ID = NewID()
		}
		checks = append(checks, c)
	}
	if checks == nil {
		checks = []CheckItem{}
	}
	it.Checklist = checks
	var links []Link
	for _, l := range it.Links {
		l.Ref = oneLine(l.Ref, 300)
		l.Title = oneLine(l.Title, 200)
		if !slices.Contains(linkKinds, l.Kind) || l.Ref == "" || len(links) >= 50 {
			continue
		}
		if l.Kind == "page" && (!strings.HasPrefix(l.Ref, "/") || strings.HasPrefix(l.Ref, "//") ||
			strings.ContainsAny(l.Ref, "\\\"'<> ")) {
			return it, Invalid{"پیوند صفحه باید نشانی‌ای از همین سامانه باشد."}
		}
		links = append(links, l)
	}
	if links == nil {
		links = []Link{}
	}
	it.Links = links
	var deps []Dep
	for _, d := range it.Deps {
		if !slices.Contains(depKinds, d.Kind) || !keyShape.MatchString(d.ID) || d.ID == it.ID || len(deps) >= 50 {
			continue
		}
		if slices.ContainsFunc(deps, func(x Dep) bool { return x.ID == d.ID }) {
			continue
		}
		deps = append(deps, d)
	}
	if deps == nil {
		deps = []Dep{}
	}
	it.Deps = deps
	if !colorShape.MatchString(it.Color) {
		it.Color = ""
	}
	return it, nil
}
