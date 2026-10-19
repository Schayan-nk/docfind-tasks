package tasks

import (
	"context"
	"slices"
)

// Templates: a new board started from a ready one. Three are built in -- a
// new tender, a technical offer with its seven steps, and a sales pipeline --
// and any board marked Template is one
// too. A board made from a template takes its columns, fields, labels, rules,
// buttons and forms, and its items without their dates, people or history.

// TemplateRef is a template as the page lists it.
type TemplateRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Builtin     bool   `json:"builtin,omitempty"`
}

type tmplItem struct {
	title string
	subs  []string
	dep   bool // depends (finish -> start) on the item before it
}

type builtinTemplate struct {
	ref    TemplateRef
	kind   string
	fields []Field
	labels []Label
	items  []tmplItem
}

var builtinTemplates = []builtinTemplate{
	{TemplateRef{ID: "tender", Name: "مناقصهٔ تازه", Description: "از تحلیل مناقصه تا ارسال پیشنهاد، هر گام پس از گام پیش", Builtin: true},
		"project", []Field{{Key: "customer", Name: "مشتری", Kind: "text"}, {Key: "tender_no", Name: "شمارهٔ مناقصه", Kind: "text"}, {Key: "amount", Name: "مبلغ", Kind: "number"}},
		[]Label{{Name: "مناقصه", Color: "#7C3AED"}},
		[]tmplItem{{title: "تحلیل مناقصه"}, {title: "نیازمندی‌های فنی", dep: true}, {title: "استعلام از تأمین‌کنندگان", dep: true},
			{title: "محاسبهٔ هزینه", dep: true}, {title: "تحلیل رقبا", dep: true}, {title: "تصمیم شرکت یا عدم شرکت", dep: true},
			{title: "پیشنهاد بازرگانی", dep: true}, {title: "تأیید مدیریت", dep: true}, {title: "ارسال پیشنهاد", dep: true}}},
	{TemplateRef{ID: "offer", Name: "پیشنهاد فنی", Description: "یک پیشنهاد فنی با هفت گام آن، از خواندن استعلام تا ارسال به مشتری", Builtin: true},
		"board", []Field{{Key: "customer", Name: "مشتری", Kind: "text"}},
		nil,
		[]tmplItem{{title: "تهیهٔ پیشنهاد فنی", subs: []string{"خواندن استعلام", "بررسی مشخصات فنی", "تماس با تأمین‌کننده",
			"دریافت داده‌های فنی", "تهیهٔ سند", "بازبینی داخلی", "ارسال به مشتری"}}}},
	{TemplateRef{ID: "sales", Name: "خط فروش", Description: "استعلام‌ها و سفارش‌ها با مشتری و مبلغ", Builtin: true},
		"board", []Field{{Key: "customer", Name: "مشتری", Kind: "text"}, {Key: "amount", Name: "مبلغ", Kind: "number"},
			{Key: "stage", Name: "مرحله", Kind: "choice", Options: []string{"استعلام", "پیش‌فاکتور", "مذاکره", "سفارش"}}},
		[]Label{{Name: "فوری", Color: "#DC2626"}, {Name: "صادراتی", Color: "#0EA5E9"}},
		nil},
}

// Templates is the built-in templates and the template boards a can see.
func (s *Service) Templates(ctx context.Context, a Actor) ([]TemplateRef, error) {
	out := []TemplateRef{}
	for _, t := range builtinTemplates {
		out = append(out, t.ref)
	}
	boards, err := s.visibleBoards(ctx, a)
	if err != nil {
		return nil, err
	}
	for _, b := range boards {
		if b.Template {
			out = append(out, TemplateRef{ID: b.ID, Name: b.Name, Description: b.Description})
		}
	}
	return out, nil
}

// FromTemplate makes a new board, name, in a workspace a works in.
func (s *Service) FromTemplate(ctx context.Context, a Actor, workspaceID, templateID, name string) (Board, error) {
	nb := Board{WorkspaceID: workspaceID, Name: name}
	var items []Item
	if i := slices.IndexFunc(builtinTemplates, func(t builtinTemplate) bool { return t.ref.ID == templateID }); i >= 0 {
		t := builtinTemplates[i]
		nb.Kind, nb.Fields, nb.Labels, nb.Description = t.kind, slices.Clone(t.fields), slices.Clone(t.labels), t.ref.Description
		if nb.Name == "" {
			nb.Name = t.ref.Name
		}
		prev := ""
		for k, ti := range t.items {
			id := NewID()
			it := Item{ID: id, Title: ti.title, Position: float64(k+1) * 1024}
			if ti.dep && prev != "" {
				it.Deps = []Dep{{ID: prev, Kind: "fs"}}
			}
			items = append(items, it)
			for j, sub := range ti.subs {
				items = append(items, Item{ID: NewID(), ParentID: id, Title: sub, Position: float64(j+1) * 1024})
			}
			prev = id
		}
	} else {
		src, _, err := s.Board(ctx, a, templateID)
		if err != nil {
			return Board{}, err
		}
		nb.Kind, nb.Statuses, nb.Fields, nb.Labels, nb.Groups = src.Kind, src.Statuses, src.Fields, src.Labels, src.Groups
		// Not its webhooks: where one board posts is not where a copy should.
		nb.Rules, nb.Buttons, nb.Forms, nb.Description = src.Rules, src.Buttons, src.Forms, src.Description
		if nb.Name == "" {
			nb.Name = src.Name
		}
		old, err := s.Store.Items(ctx, Query{BoardIDs: []string{src.ID}})
		if err != nil {
			return Board{}, err
		}
		ids := map[string]string{}
		for _, it := range old {
			ids[it.ID] = NewID()
		}
		for _, it := range old {
			n := Item{ID: ids[it.ID], ParentID: ids[it.ParentID], Title: it.Title, Description: it.Description,
				Priority: it.Priority, Labels: it.Labels, Fields: it.Fields, Links: it.Links, Group: it.Group,
				EstimateMin: it.EstimateMin, Position: it.Position, Color: it.Color}
			for _, c := range it.Checklist {
				n.Checklist = append(n.Checklist, CheckItem{Text: c.Text})
			}
			for _, d := range it.Deps {
				if ids[d.ID] != "" {
					n.Deps = append(n.Deps, Dep{ID: ids[d.ID], Kind: d.Kind})
				}
			}
			items = append(items, n)
		}
	}
	saved, err := s.SaveBoard(ctx, a, nb)
	if err != nil {
		return saved, err
	}
	// Parents first, so a subtask's parent is there when it is checked.
	slices.SortStableFunc(items, func(x, y Item) int {
		if (x.ParentID == "") == (y.ParentID == "") {
			return 0
		}
		if x.ParentID == "" {
			return -1
		}
		return 1
	})
	for _, it := range items {
		it.BoardID, it.Creator, it.Created, it.Updated, it.UpdatedBy = saved.ID, a.User, s.now(), s.now(), a.User
		it.Status = saved.Statuses[0].Key
		clean, err := cleanItem(it, saved)
		if err != nil {
			continue
		}
		_, _ = s.Store.SaveItem(ctx, clean)
	}
	s.log(ctx, Item{BoardID: saved.ID}, a, "board", "بورد از الگو، با "+itoa(len(items))+" کار")
	return saved, nil
}
