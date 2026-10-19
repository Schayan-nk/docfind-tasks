package tasks

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// Forms: an inquiry, a tender, an IT or HR request,
// a complaint -- a board's intake form, whose submission becomes a work item
// on that board, in the form's column, with the form's assignees and labels.
// A form marked Open can be sent by anybody who holds the task manager, not
// only the workspace's members: the request comes from outside the team.

// Form is one intake form.
type Form struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Intro     string      `json:"intro,omitempty"`
	Fields    []FormField `json:"fields"`
	Status    string      `json:"status,omitempty"`
	Priority  string      `json:"priority,omitempty"`
	Assignees []string    `json:"assignees"`
	Labels    []string    `json:"labels"`
	Open      bool        `json:"open,omitempty"`
	// Assign, when set, decides who a request goes to instead of Assignees:
	// the sender's manager (the head of their department), the colleague a
	// person question names, or the members of a department. Assignees are
	// still who it goes to when the rule names nobody.
	Assign *Assign `json:"assign,omitempty"`
}

// FormField is one question (answers.go has the kinds). Its Key, when the
// board has a custom field of that key, puts the answer in that field too.
// Choices are a choice's options as value and label; Options, the older
// forms' plain list, is read into them. Mode, on a process step, is "" to
// fill it, "view" to only show it, "hidden" to keep it off the page.
type FormField struct {
	Key      string      `json:"key"`
	Label    string      `json:"label"`
	Kind     string      `json:"kind"`
	Required bool        `json:"required,omitempty"`
	Options  []string    `json:"options,omitempty"`
	Choices  []Choice    `json:"choices,omitempty"`
	Mode     string      `json:"mode,omitempty"`
	Columns  []FormField `json:"columns,omitempty"`
	Hint     string      `json:"hint,omitempty"`
	// Source is where a question's options or value come from when they are
	// not fixed -- docfind's lists or a database (fielddata.go); SQL is the
	// query for a database, Deps the answers it names (worked out, for the
	// page), Display how a choice is drawn: "" a dropdown, "radio", "search".
	Source  string   `json:"source,omitempty"`
	SQL     string   `json:"sql,omitempty"`
	Deps    []string `json:"deps,omitempty"`
	Display string   `json:"display,omitempty"`
	// ShowIf: the question is asked only while all of these hold, and is
	// otherwise not asked, not required, and not kept. RequiredIf: it must be
	// answered while all of these hold, as well as when Required. Both are
	// tested against the other answers (answers.go, rulesHold).
	ShowIf     []FieldRule `json:"show_if,omitempty"`
	RequiredIf []FieldRule `json:"required_if,omitempty"`
	// ShowAny, RequiredAny: those hold when any one of their rules does,
	// instead of all of them.
	ShowAny     bool `json:"show_any,omitempty"`
	RequiredAny bool `json:"required_any,omitempty"`
	// ReadOnlyIf: while it holds the question is shown but not changed here;
	// what the case holds for it stays (ReadOnlyAny: any one rule).
	ReadOnlyIf  []FieldRule `json:"readonly_if,omitempty"`
	ReadOnlyAny bool        `json:"readonly_any,omitempty"`
	// Formula and Decimals make a calculated question (kind "calc", calc.go).
	Formula  string `json:"formula,omitempty"`
	Decimals int    `json:"decimals,omitempty"`
	// Min and Max bound a number or an amount; Pattern is a text's shape, a
	// regular expression the whole answer must match, and PatternMsg what to
	// say when it does not.
	Min        *float64 `json:"min,omitempty"`
	Max        *float64 `json:"max,omitempty"`
	Pattern    string   `json:"pattern,omitempty"`
	PatternMsg string   `json:"pattern_msg,omitempty"`
	// Default fills the question when its form opens empty: "=formula" over
	// the case's answers, or plain text. Only the page uses it.
	Default string `json:"default,omitempty"`
}

func cleanForms(b Board) (Board, error) {
	if len(b.Forms) > 20 {
		return b, Invalid{"یک بورد حداکثر ۲۰ فرم دارد."}
	}
	out := []Form{}
	for _, f := range b.Forms {
		if f.ID == "" || !keyShape.MatchString(f.ID) {
			f.ID = NewID()
		}
		if f.Name = oneLine(f.Name, 80); f.Name == "" {
			continue
		}
		f.Intro = clipText(f.Intro, 2000)
		if f.Status != "" && !slices.ContainsFunc(b.Statuses, func(s Status) bool { return s.Key == f.Status }) {
			f.Status = ""
		}
		if !slices.Contains(Priorities, f.Priority) {
			f.Priority = "normal"
		}
		var err error
		if f.Assignees, err = users(f.Assignees, 20); err != nil {
			return b, err
		}
		var labels []string
		for _, l := range f.Labels {
			if l = oneLine(l, 40); l != "" && !slices.Contains(labels, l) {
				labels = append(labels, l)
			}
		}
		if labels == nil {
			labels = []string{}
		}
		f.Labels = labels
		if len(f.Fields) == 0 || len(f.Fields) > 40 {
			return b, Invalid{"فرم «" + f.Name + "» باید بین ۱ و ۴۰ پرسش داشته باشد."}
		}
		fields, err := cleanFields(f.Fields, 40)
		if err != nil {
			return b, Invalid{"فرم «" + f.Name + "»: " + err.Error()}
		}
		for i := range fields {
			fields[i].Mode = "" // an intake form asks everything it shows
		}
		f.Fields = fields
		if f.Assign, err = cleanFormAssign(f.Assign, fields); err != nil {
			return b, Invalid{"فرم «" + f.Name + "»: " + err.Error()}
		}
		out = append(out, f)
	}
	b.Forms = out
	return b, nil
}

// cleanFormAssign checks a form's rule for who its requests go to; the
// fixed list (Assignees) needs none.
func cleanFormAssign(a *Assign, fields []FormField) (*Assign, error) {
	if a == nil {
		return nil, nil
	}
	switch a.Mode {
	case "", "users":
		return nil, nil
	case "manager":
		return &Assign{Mode: "manager"}, nil
	case "field":
		if !slices.ContainsFunc(fields, func(q FormField) bool { return q.Key == a.Field && q.Kind == "person" }) {
			return nil, errors.New("پرسشی که مسئول را نام می‌برد باید از نوع «یکی از همکاران» باشد")
		}
		return &Assign{Mode: "field", Field: a.Field}, nil
	case "department":
		if a.Department <= 0 {
			return nil, errors.New("کدام واحد؟")
		}
		return &Assign{Mode: "department", Department: a.Department}, nil
	}
	return nil, errors.New("این شیوهٔ سپردن شناخته نیست")
}

// formAssignees is who a request goes to by its form's rule; nil when the
// rule names nobody (no head set, the question left empty), and then the
// form's Assignees are used.
func (s *Service) formAssignees(ctx context.Context, f Form, sender string, typed map[string]any) []string {
	if f.Assign == nil {
		return nil
	}
	switch f.Assign.Mode {
	case "manager":
		if s.Org != nil {
			if h, err := s.Org.Head(ctx, sender); err == nil && h != "" {
				return []string{h}
			}
		}
	case "field":
		if u := strings.ToLower(strings.TrimSpace(asString(typed[f.Assign.Field]))); u != "" {
			return []string{u}
		}
	case "department":
		if s.Org != nil {
			if ms, err := s.Org.Members(ctx, f.Assign.Department); err == nil && len(ms) > 0 {
				return ms[:min(len(ms), 30)]
			}
		}
	}
	return nil
}

// FormRef is a form and the board it files into.
type FormRef struct {
	BoardID   string `json:"board_id"`
	BoardName string `json:"board_name"`
	Form      Form   `json:"form"`
	// SeesBoard is whether this person may see the form's board, and so its
	// answers (FormAnswers); an open form is sent by people who may not.
	SeesBoard bool `json:"sees_board,omitempty"`
}

// Forms is every form a may send: those of the boards a can see, and the open
// ones of every board.
func (s *Service) Forms(ctx context.Context, a Actor) ([]FormRef, error) {
	all, err := s.Store.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	out := []FormRef{}
	for _, w := range all {
		boards, err := s.Store.Boards(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		for _, b := range boards {
			for _, f := range b.Forms {
				if a.sees(w) || f.Open {
					out = append(out, FormRef{BoardID: b.ID, BoardName: b.Name, Form: f, SeesBoard: a.sees(w)})
				}
			}
		}
	}
	return out, nil
}

// Submit files a's answers to a form as a new item, the answers checked
// and kept typed on it (Item.Form).
func (s *Service) Submit(ctx context.Context, a Actor, boardID, formID string, answers map[string]any) (Item, error) {
	b, err := s.Store.Board(ctx, boardID)
	if err != nil {
		return Item{}, err
	}
	w, err := s.workspace(ctx, b.WorkspaceID)
	if err != nil {
		return Item{}, err
	}
	i := slices.IndexFunc(b.Forms, func(f Form) bool { return f.ID == formID })
	if i < 0 || (!b.Forms[i].Open && !a.sees(w)) {
		return Item{}, ErrNotFound
	}
	f := b.Forms[i]
	if err := s.checkSourced(ctx, f.Fields, answers, nil); err != nil {
		return Item{}, err
	}
	typed, err := checkAnswers(f.Fields, answers, nil, s.isPerson)
	if err != nil {
		return Item{}, err
	}
	files := fileIDs(f.Fields, typed)
	if err := s.filesUsable(ctx, a, "", files); err != nil {
		return Item{}, err
	}
	var lines []string
	fields := map[string]any{}
	title := ""
	for _, q := range f.Fields {
		v, ok := typed[q.Key]
		if !ok || blank(v) {
			continue
		}
		shown := answerText(q, v)
		if title == "" && q.Kind != "check" && q.Kind != "grid" {
			title = oneLine(shown, 150)
		}
		lines = append(lines, q.Label+": "+shown)
		if bi := slices.IndexFunc(b.Fields, func(x Field) bool { return x.Key == q.Key }); bi >= 0 {
			switch b.Fields[bi].Kind {
			case "number":
				if n, ok := number(v); ok {
					fields[q.Key] = n
				}
			case "check":
				fields[q.Key] = truthy(v)
			default:
				fields[q.Key] = shown
			}
		}
	}
	if title == "" {
		title = f.Name
	}
	assignees := f.Assignees
	if who := s.formAssignees(ctx, f, a.User, typed); len(who) > 0 {
		assignees = who
	}
	it := Item{ID: NewID(), BoardID: b.ID, Title: f.Name + ": " + title, Description: strings.Join(lines, "\n"),
		Status: f.Status, Priority: f.Priority, Assignees: assignees, Watchers: []string{a.User}, Labels: f.Labels,
		Fields: fields, Creator: a.User, Created: s.now(), Updated: s.now(), UpdatedBy: a.User,
		Position: float64(s.now().UnixMilli()) / 1000}
	clean, err := cleanItem(it, b)
	if err != nil {
		return it, err
	}
	clean.Form = &FormFill{FormID: f.ID, Name: f.Name, Questions: f.Fields, Answers: typed}
	saved, err := s.Store.SaveItem(ctx, clean)
	if err != nil {
		return saved, err
	}
	s.attachFiles(ctx, saved.ID, files)
	s.log(ctx, saved, a, "form", "از فرم «"+f.Name+"»")
	s.tell(ctx, saved.Assignees, a.User, "درخواست تازه از فرم «"+f.Name+"»", saved.Title, ItemLink(saved))
	saved = s.fire(ctx, b, saved, "created", a.User)
	return saved, nil
}
