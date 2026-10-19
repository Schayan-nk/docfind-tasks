package tasks

import (
	"context"
	"math"
	"slices"
	"sort"
	"time"
)

// Time: a timer each person starts and stops on an
// item, and time entered by hand, kept on the item as its work log. The
// item's actual time is the log's sum; a timesheet is the logs of every item
// a person can see, by person, day and board.

// TimeEntry is one stretch of work: running while End is nil.
type TimeEntry struct {
	ID      string     `json:"id"`
	User    string     `json:"user"`
	Day     string     `json:"day"` // 2006-01-02
	Start   time.Time  `json:"start"`
	End     *time.Time `json:"end,omitempty"`
	Minutes int        `json:"minutes"`
	Note    string     `json:"note,omitempty"`
}

func sumMinutes(it *Item) {
	total := 0
	for _, e := range it.Time {
		total += e.Minutes
	}
	it.ActualMin = total
}

// startTimer starts user's timer on it, unless one is running.
func startTimer(it *Item, user string, now time.Time) bool {
	if slices.ContainsFunc(it.Time, func(e TimeEntry) bool { return e.User == user && e.End == nil }) || user == "" || user == "automation" {
		return false
	}
	it.Time = append(it.Time, TimeEntry{ID: NewID(), User: user, Day: now.Format("2006-01-02"), Start: now})
	return true
}

// stopTimers stops user's running timer on it, or everybody's for "".
func stopTimers(it *Item, user string, now time.Time) bool {
	changed := false
	for i := range it.Time {
		e := &it.Time[i]
		if e.End != nil || (user != "" && user != "automation" && e.User != user) {
			continue
		}
		end := now
		e.End = &end
		e.Minutes = max(1, int(math.Ceil(end.Sub(e.Start).Minutes())))
		changed = true
	}
	if changed {
		sumMinutes(it)
	}
	return changed
}

// Timer starts or stops a's timer on an item, or adds minutes a worked
// ("add", with note).
func (s *Service) Timer(ctx context.Context, a Actor, itemID, action string, minutes int, note string) (Item, error) {
	for attempt := 0; ; attempt++ {
		it, _, w, err := s.Item(ctx, a, itemID)
		if err != nil {
			return it, err
		}
		if !a.edits(w) {
			return it, ErrForbidden
		}
		now := s.now()
		changed := false
		switch action {
		case "start":
			changed = startTimer(&it, a.User, now)
		case "stop":
			changed = stopTimers(&it, a.User, now)
		case "add":
			if minutes <= 0 || minutes > 24*60 {
				return it, Invalid{"زمان باید بین ۱ دقیقه و ۲۴ ساعت باشد."}
			}
			end := now
			it.Time = append(it.Time, TimeEntry{ID: NewID(), User: a.User, Day: now.Format("2006-01-02"),
				Start: now.Add(-time.Duration(minutes) * time.Minute), End: &end, Minutes: minutes, Note: oneLine(note, 200)})
			sumMinutes(&it)
			changed = true
		default:
			return it, Invalid{"کار زمان‌سنج شناخته نیست."}
		}
		if !changed {
			return it, nil
		}
		if len(it.Time) > 2000 {
			return it, Invalid{"دفتر زمان این کار پر است."}
		}
		it.Updated, it.UpdatedBy = now, a.User
		saved, err := s.Store.SaveItem(ctx, it)
		if err == ErrConflict && attempt < 2 {
			continue // somebody saved it meanwhile: read it again and redo
		}
		if err == nil {
			detail := map[string]string{"start": "زمان‌سنج روشن شد", "stop": "زمان‌سنج خاموش شد", "add": "ثبت زمان: " + itoa(minutes) + " دقیقه"}[action]
			s.log(ctx, saved, a, "time", detail)
		}
		return saved, err
	}
}

// TimesheetRow is one person's minutes on one day on one item.
type TimesheetRow struct {
	User    string `json:"user"`
	Day     string `json:"day"`
	Board   string `json:"board"`
	ItemID  string `json:"item_id"`
	Title   string `json:"title"`
	Minutes int    `json:"minutes"`
	Running bool   `json:"running,omitempty"`
}

// Timesheet is the time logged between from and to (days, inclusive) on
// every item a can see; only a's own unless a owns the workspace.
func (s *Service) Timesheet(ctx context.Context, a Actor, from, to string) ([]TimesheetRow, error) {
	ws, err := s.Workspaces(ctx, a)
	if err != nil {
		return nil, err
	}
	out := []TimesheetRow{}
	for _, w := range ws {
		everybody := a.owns(w)
		boards, err := s.Store.Boards(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		for _, b := range boards {
			items, err := s.Store.Items(ctx, Query{BoardIDs: []string{b.ID}})
			if err != nil {
				return nil, err
			}
			for _, it := range items {
				for _, e := range it.Time {
					if (from != "" && e.Day < from) || (to != "" && e.Day > to) || (!everybody && e.User != a.User) {
						continue
					}
					m := e.Minutes
					if e.End == nil {
						m = int(s.now().Sub(e.Start).Minutes())
					}
					out = append(out, TimesheetRow{User: e.User, Day: e.Day, Board: b.Name, ItemID: it.ID, Title: it.Title,
						Minutes: m, Running: e.End == nil})
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day > out[j].Day
		}
		return out[i].User < out[j].User
	})
	return out, nil
}
