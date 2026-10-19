package tasks

import (
	"context"
	"slices"
	"sort"
	"time"
)

// A form's answers as a table: one row per request it made, one column per
// question -- the form's questions as they are now, then any question only
// older requests were asked, so nothing that was answered goes missing when
// the form changes.

// AnswerColumn is one question of the table.
type AnswerColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

// AnswerRow is one request: the item it made and its answers as text.
type AnswerRow struct {
	ItemID  string    `json:"item_id"`
	Title   string    `json:"title"`
	Status  string    `json:"status"`
	By      string    `json:"by"`
	Created time.Time `json:"created"`
	Cells   []string  `json:"cells"`
}

// FormAnswers is one intake form's answers, newest first, for somebody who
// may see its board.
func (s *Service) FormAnswers(ctx context.Context, a Actor, boardID, formID string) (Form, []AnswerColumn, []AnswerRow, error) {
	b, _, err := s.Board(ctx, a, boardID)
	if err != nil {
		return Form{}, nil, nil, err
	}
	i := slices.IndexFunc(b.Forms, func(f Form) bool { return f.ID == formID })
	if i < 0 {
		return Form{}, nil, nil, ErrNotFound
	}
	form := b.Forms[i]
	items, err := s.Items(ctx, a, boardID)
	if err != nil {
		return form, nil, nil, err
	}
	var fills []Item
	for _, it := range items {
		if it.Form != nil && it.Form.FormID == formID {
			fills = append(fills, it)
		}
	}
	sort.Slice(fills, func(i, j int) bool { return fills[i].Created.After(fills[j].Created) })

	var cols []AnswerColumn
	byKey := map[string]FormField{}
	add := func(q FormField) {
		if q.Kind == "heading" || q.Key == "" {
			return
		}
		if _, ok := byKey[q.Key]; ok {
			return
		}
		byKey[q.Key] = q
		cols = append(cols, AnswerColumn{Key: q.Key, Label: q.Label, Kind: q.Kind})
	}
	for _, q := range form.Fields {
		add(q)
	}
	for _, it := range fills {
		for _, q := range it.Form.Questions {
			add(q)
		}
	}
	status := map[string]string{}
	for _, st := range b.Statuses {
		status[st.Key] = st.Name
	}
	rows := make([]AnswerRow, 0, len(fills))
	for _, it := range fills {
		asked := map[string]FormField{}
		for _, q := range it.Form.Questions {
			asked[q.Key] = q
		}
		row := AnswerRow{ItemID: it.ID, Title: it.Title, Status: status[it.Status], By: it.Creator, Created: it.Created}
		for _, c := range cols {
			v, ok := it.Form.Answers[c.Key]
			q, was := asked[c.Key]
			if !was {
				q = byKey[c.Key]
			}
			if !ok || blank(v) {
				row.Cells = append(row.Cells, "")
				continue
			}
			row.Cells = append(row.Cells, answerText(q, v))
		}
		rows = append(rows, row)
	}
	return form, cols, rows, nil
}
