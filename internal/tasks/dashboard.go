package tasks

import (
	"context"
	"sort"
)

// The dashboard: the figures across every
// board a person can see -- what is open, late, due this week, finished this
// week, how long things take to finish, and by board, person and column --
// with what waits on their approval and what happened last.

// Count is one row of a breakdown.
type Count struct {
	Name    string  `json:"name"`
	Open    int     `json:"open"`
	Overdue int     `json:"overdue"`
	Done    int     `json:"done"`
	Hours   float64 `json:"hours,omitempty"`
}

// Dashboard is the task manager's figures for one reader.
type Dashboard struct {
	Open      int        `json:"open"`
	Overdue   int        `json:"overdue"`
	DueWeek   int        `json:"due_week"`
	DoneWeek  int        `json:"done_week"`
	Total     int        `json:"total"`
	Rate      float64    `json:"rate"`     // finished share of everything, 0..1
	AvgDays   float64    `json:"avg_days"` // created to finished, over the last 30 days
	Approvals int        `json:"approvals"`
	ByBoard   []Count    `json:"by_board"`
	ByPerson  []Count    `json:"by_person"`
	ByStatus  []Count    `json:"by_status"`
	Recent    []Activity `json:"recent"`
}

// Dashboard counts the items on the boards a can see.
func (s *Service) Dashboard(ctx context.Context, a Actor) (Dashboard, error) {
	d := Dashboard{ByBoard: []Count{}, ByPerson: []Count{}, ByStatus: []Count{}, Recent: []Activity{}}
	boards, err := s.visibleBoards(ctx, a)
	if err != nil || len(boards) == 0 {
		return d, err
	}
	now := s.now()
	today := now.Format("2006-01-02")
	week := now.AddDate(0, 0, 7).Format("2006-01-02")
	weekAgo := now.AddDate(0, 0, -7)
	monthAgo := now.AddDate(0, 0, -30)
	people := map[string]*Count{}
	statuses := map[string]*Count{}
	var days float64
	finished, done := 0, 0
	var recent []Activity
	for _, b := range boards {
		items, err := s.Store.Items(ctx, Query{BoardIDs: []string{b.ID}})
		if err != nil {
			return d, err
		}
		bc := Count{Name: b.Name}
		for _, it := range items {
			if it.ParentID != "" {
				continue
			}
			d.Total++
			if it.Approval.Waiting(a.User) {
				d.Approvals++
			}
			sc := statuses[statusName(b, it.Status)]
			if sc == nil {
				sc = &Count{Name: statusName(b, it.Status)}
				statuses[sc.Name] = sc
			}
			if isDone(b, it.Status) {
				done++
				bc.Done++
				sc.Done++
				if it.Completed != nil {
					if it.Completed.After(weekAgo) {
						d.DoneWeek++
					}
					if it.Completed.After(monthAgo) {
						days += it.Completed.Sub(it.Created).Hours() / 24
						finished++
					}
				}
				continue
			}
			d.Open++
			bc.Open++
			sc.Open++
			late := it.Due != "" && it.Due < today
			if late {
				d.Overdue++
				bc.Overdue++
			}
			if it.Due != "" && it.Due >= today && it.Due <= week {
				d.DueWeek++
			}
			who := it.Assignees
			if len(who) == 0 {
				who = []string{""}
			}
			for _, u := range who {
				pc := people[u]
				if pc == nil {
					pc = &Count{Name: u}
					people[u] = pc
				}
				pc.Open++
				if late {
					pc.Overdue++
				}
				pc.Hours += float64(it.EstimateMin) / 60
			}
		}
		d.ByBoard = append(d.ByBoard, bc)
		if log, err := s.Store.Activity(ctx, b.ID, "", 15); err == nil {
			recent = append(recent, log...)
		}
	}
	if d.Total > 0 {
		d.Rate = float64(done) / float64(d.Total)
	}
	if finished > 0 {
		d.AvgDays = days / float64(finished)
	}
	for _, c := range people {
		d.ByPerson = append(d.ByPerson, *c)
	}
	for _, c := range statuses {
		d.ByStatus = append(d.ByStatus, *c)
	}
	sort.Slice(d.ByBoard, func(i, j int) bool { return d.ByBoard[i].Open > d.ByBoard[j].Open })
	sort.Slice(d.ByPerson, func(i, j int) bool { return d.ByPerson[i].Open > d.ByPerson[j].Open })
	sort.Slice(d.ByStatus, func(i, j int) bool {
		return d.ByStatus[i].Open+d.ByStatus[i].Done > d.ByStatus[j].Open+d.ByStatus[j].Done
	})
	sort.Slice(recent, func(i, j int) bool { return recent[i].At.After(recent[j].At) })
	if len(recent) > 20 {
		recent = recent[:20]
	}
	d.Recent = recent
	return d, nil
}
