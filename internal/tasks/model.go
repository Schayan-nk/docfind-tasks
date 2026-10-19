// Package tasks is the task manager: workspaces, boards and the work items
// on them (monday's workspaces -> boards -> groups -> items -> columns, and
// Trello's cards and lists), each item tied to what it is about outside the
// board.
//
// This package is the model and the rules -- what a workspace, board and item
// are, what makes one valid, who may see and change what, and what is written
// to the activity log when they do. Storage is behind Store (PostgreSQL in
// internal/pgstore, a map in tests); the HTTP side is internal/find.
package tasks

import (
	"errors"
	"time"
)

// Member is one person in a workspace, and what they may do there:
// "owner" runs it (its settings, members and boards), "member" works in it,
// "viewer" reads it.
type Member struct {
	User string `json:"user"`
	Role string `json:"role"`
}

// Workspace is one department's or team's space: its boards, and who is in it.
// Open means everyone who holds the task manager may see it and work in it,
// as a member; the listed members still decide who owns it and who only reads.
type Workspace struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Department  string    `json:"department,omitempty"`
	Team        string    `json:"team,omitempty"`
	Open        bool      `json:"open,omitempty"`
	Members     []Member  `json:"members"`
	CreatedBy   string    `json:"created_by"`
	Created     time.Time `json:"created"`
}

// Status is one column of a board's Kanban: its key, what it is called, its
// colour, its work-in-progress limit (0 for none), and whether an item in it
// is finished.
type Status struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
	WIP   int    `json:"wip,omitempty"`
	Done  bool   `json:"done,omitempty"`
}

// Field is a custom column (monday's columns): its key, its name, what kind
// of value it holds, and the choices for a choice field.
type Field struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // text, number, date, choice, person, check, link
	Options []string `json:"options,omitempty"`
}

// Label is a coloured tag a board offers its items.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// Board is one board or project in a workspace: its columns (Statuses), its
// custom fields, its labels and its groups.
type Board struct {
	ID          string   `json:"id"`
	WorkspaceID string   `json:"workspace_id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Kind        string   `json:"kind"` // board or project
	Statuses    []Status `json:"statuses"`
	Fields      []Field  `json:"fields"`
	Labels      []Label  `json:"labels"`
	Groups      []string `json:"groups"`
	// Rules are the board's automations (automation.go), Buttons its quick
	// actions (none: Start, Complete and Escalate), Forms its intake forms,
	// Webhooks where its events are posted.
	Rules   []Rule   `json:"rules"`
	Buttons []Button `json:"buttons"`
	Forms   []Form   `json:"forms"`
	// Process is the board's workflow, when its items are cases moving
	// through steps (process.go); ProcessHistory the maps older cases are
	// still on. Both change only through SaveProcess.
	Process        *Process  `json:"process,omitempty"`
	ProcessHistory []Process `json:"process_history,omitempty"`
	Webhooks       []Webhook `json:"webhooks"`
	// Template offers the board as a starting point for new ones.
	Template  bool      `json:"template,omitempty"`
	Archived  bool      `json:"archived,omitempty"`
	CreatedBy string    `json:"created_by"`
	Created   time.Time `json:"created"`
}

// CheckItem is one line of a Trello-style checklist.
type CheckItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done,omitempty"`
}

// Link ties an item to something outside the board: a customer, an order, an
// email, a document, a tender, a supplier or a contract, by the reference
// people know it by (Ref) and what it is called (Title); or a page of this
// site, Ref being its path.
type Link struct {
	Kind  string `json:"kind"`
	Ref   string `json:"ref"`
	Title string `json:"title,omitempty"`
}

// Dep is a dependency on another item: fs, ss, ff and sf are the four
// schedule kinds (finish->start and so on); blocks, related and duplicate are
// the others. The item this is on is the one that depends.
type Dep struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// Item is one work item: a task, a card, a row. Dates are days, written
// 2006-01-02; the page shows them in the Jalali calendar.
type Item struct {
	ID          string         `json:"id"`
	BoardID     string         `json:"board_id"`
	ParentID    string         `json:"parent_id,omitempty"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"`
	Priority    string         `json:"priority"` // low, normal, high, critical
	Assignees   []string       `json:"assignees"`
	Watchers    []string       `json:"watchers"`
	Creator     string         `json:"creator"`
	Department  string         `json:"department,omitempty"`
	Team        string         `json:"team,omitempty"`
	Group       string         `json:"group,omitempty"`
	Start       string         `json:"start,omitempty"`
	Due         string         `json:"due,omitempty"`
	Completed   *time.Time     `json:"completed,omitempty"`
	EstimateMin int            `json:"estimate_min,omitempty"`
	ActualMin   int            `json:"actual_min,omitempty"`
	Labels      []string       `json:"labels"`
	Fields      map[string]any `json:"fields"`
	Checklist   []CheckItem    `json:"checklist"`
	Links       []Link         `json:"links"`
	Deps        []Dep          `json:"deps"`
	Color       string         `json:"color,omitempty"`
	// Approval is the item's sign-off, when one was asked for (approval.go);
	// Time its work log, timers included (timelog.go).
	Approval *Approval `json:"approval,omitempty"`
	// Flow is a case's state on its board's process (flow.go); Form the
	// typed answers of the intake form it came from (answers.go). Each
	// changes only through its own door, never through an edit of the item.
	Flow      *Flow       `json:"flow,omitempty"`
	Form      *FormFill   `json:"form,omitempty"`
	Time      []TimeEntry `json:"time"`
	Position  float64     `json:"position"`
	Created   time.Time   `json:"created"`
	Updated   time.Time   `json:"updated"`
	UpdatedBy string      `json:"updated_by,omitempty"`
	Version   int         `json:"version"`
}

// Comment is one entry in an item's updates: a comment, an internal note, a
// decision, or an approval's words. Mentions are the usernames it names.
type Comment struct {
	ID       string    `json:"id"`
	ItemID   string    `json:"item_id"`
	Author   string    `json:"author"`
	Body     string    `json:"body"`
	Kind     string    `json:"kind"` // comment, note, decision, approval
	Mentions []string  `json:"mentions"`
	Created  time.Time `json:"created"`
}

// Activity is one line of the log: who did what to which item, and when.
// Nothing important happens without one.
type Activity struct {
	ID      string    `json:"id"`
	BoardID string    `json:"board_id"`
	ItemID  string    `json:"item_id,omitempty"`
	Actor   string    `json:"actor"`
	Action  string    `json:"action"`
	Detail  string    `json:"detail,omitempty"`
	At      time.Time `json:"at"`
}

// Query is which items to read. Every non-empty field narrows it.
type Query struct {
	BoardIDs []string
	Assignee string
	ParentID string
	Text     string
	DueFrom  string
	DueTo    string
	Open     bool // only items not in a finished column
	Limit    int
}

// The errors the HTTP side turns into answers.
var (
	ErrNotFound  = errors.New("tasks: not found")
	ErrForbidden = errors.New("tasks: not allowed")
	ErrConflict  = errors.New("tasks: changed by somebody else")
)

// Invalid is input that cannot be stored, with the reason in the page's
// language.
type Invalid struct{ Msg string }

func (e Invalid) Error() string { return e.Msg }

// Priorities, lowest first.
var Priorities = []string{"low", "normal", "high", "critical"}

// DefaultStatuses are a new board's columns, as the owner listed them:
// BACKLOG -> TODO -> IN PROGRESS -> WAITING -> REVIEW -> APPROVED -> DONE.
func DefaultStatuses() []Status {
	return []Status{
		{Key: "backlog", Name: "بک‌لاگ", Color: "#6B7C8F"},
		{Key: "todo", Name: "برای انجام", Color: "#3B82F6"},
		{Key: "doing", Name: "در حال انجام", Color: "#E5641D", WIP: 0},
		{Key: "waiting", Name: "در انتظار", Color: "#A855F7"},
		{Key: "review", Name: "بازبینی", Color: "#EAB308"},
		{Key: "approved", Name: "تأییدشده", Color: "#14B8A6"},
		{Key: "done", Name: "انجام‌شده", Color: "#22C55E", Done: true},
	}
}
