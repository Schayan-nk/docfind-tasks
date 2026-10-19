package tasks

// Processes: a board whose items are cases moving through steps -- a team's
// own workflows, drawn here or brought in as BPMN (bpmn.go). A process
// is a map: steps (each with what it asks and who does it), gateways that only
// route, and ends; and the routes between them, each with an optional
// condition (cond.go). Its route kinds are the ones BPM tools' maps use:
//
//	seq   the one route on
//	xor   the first route, in order, whose condition holds
//	par   every route whose condition holds, in parallel
//	join  to a meeting point that waits for the other parallel branches
//
// A case's state is its Flow (flow.go): the data every step has filled, the
// branches open now and whom each waits for, and a step-by-step history.
// This server is the inbox: an open branch is a task in its assignee's list.

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Process is a board's workflow.
type Process struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	// Start is the step a new case begins at.
	Start string `json:"start"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
	// Source names where the map came from, when another tool's map was read
	// across.
	Source string `json:"source,omitempty"`
}

// Node is one place on the map.
type Node struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // step, gateway, end, subprocess
	Name string `json:"name"`
	// Fields are what the step shows and asks, in order; each field's Mode
	// says whether it is filled here, only shown, or hidden.
	Fields []FormField `json:"fields,omitempty"`
	Assign Assign      `json:"assign"`
	// X, Y are the node's centre on the designer's map (BPMN diagram
	// coordinates); none yet means the designer lays the map out itself.
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	// Gate is a gateway's BPMN kind as drawn: "xor" (exclusive, X) or "par"
	// (parallel, +). The routes' own kinds are what the engine follows;
	// this only says how to draw it and what a new route from it is.
	Gate string `json:"gate,omitempty"`
	// Before and After are what a step does by itself when it opens and when
	// it is done (actions.go).
	Before []StepAction `json:"before,omitempty"`
	After  []StepAction `json:"after,omitempty"`
}

// Assign is who a step goes to.
//
//	users      the one person named, or a pool of them when several
//	field      the person whose username a field of the case holds
//	department a pool of a docfind department's members
//	manager    the head of the requester's department
//	requester  the person who started the case
//	previous   whoever did the step before
//
// A pool's task is anybody's in it until one of them claims it.
type Assign struct {
	Mode       string   `json:"mode"`
	Users      []string `json:"users,omitempty"`
	Field      string   `json:"field,omitempty"`
	Department int      `json:"department,omitempty"`
	// Group is what an imported map named (a BPMN lane), kept so an
	// administrator can see what to map it to.
	Group string `json:"group,omitempty"`
}

// Edge is one route.
type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"` // a node; "" is the end
	Kind  string `json:"kind"`
	When  string `json:"when,omitempty"`
	Order int    `json:"order"`
	// Rules are a route's condition made in the designer: tests on the case's
	// answers, all of which must hold (rules.go) -- the same ones a question's
	// show_if makes. A route with rules has no When; an imported one keeps
	// its expression.
	Rules []FieldRule `json:"rules,omitempty"`
	// Any: the route is taken when any one of its rules holds.
	Any bool `json:"any,omitempty"`
}

// holds says whether a route's condition holds on a case's data.
func (e Edge) holds(data map[string]any) bool {
	if len(e.Rules) > 0 {
		return rulesMatch(e.Rules, e.Any, data)
	}
	return ExprTrue(e.When, data)
}

// Flow is a case's state on its board's process.
type Flow struct {
	Version   int            `json:"version"`
	Requester string         `json:"requester"`
	Data      map[string]any `json:"data"`
	Threads   []Thread       `json:"threads"`
	// Waiting are branches parked at a join until their siblings arrive.
	Waiting []Thread     `json:"waiting,omitempty"`
	Steps   []StepRecord `json:"steps"`
	Done    bool         `json:"done,omitempty"`
	// Number is the case's readable number on its board: 1, 2, 3...
	Number int `json:"number,omitempty"`
}

// Thread is one open branch: the step it waits at and for whom.
type Thread struct {
	ID        string    `json:"id"`
	Node      string    `json:"node"`
	Assignees []string  `json:"assignees,omitempty"`
	Pool      []string  `json:"pool,omitempty"`
	Fork      []string  `json:"fork,omitempty"` // the splits it came out of, outermost first
	Since     time.Time `json:"since"`
	Previous  string    `json:"previous,omitempty"` // who did the step before
	At        string    `json:"at,omitempty"`       // a parked branch's join
}

// StepRecord is one step done: by whom, when, with what.
type StepRecord struct {
	Node   string         `json:"node"`
	Name   string         `json:"name"`
	By     string         `json:"by"`
	At     time.Time      `json:"at"`
	Values map[string]any `json:"values,omitempty"`
}

var nodeKinds = []string{"step", "gateway", "end", "subprocess"}
var edgeKinds = []string{"seq", "xor", "par", "join"}
var assignModes = []string{"users", "field", "department", "manager", "requester", "previous"}

// cleanProcess checks a map: every route between nodes it has, every
// condition readable, every step's fields sound, a start that is a step.
func cleanProcess(p Process) (Process, error) {
	p.Name = oneLine(p.Name, 120)
	if len(p.Nodes) == 0 || len(p.Nodes) > 400 || len(p.Edges) > 1200 {
		return p, Invalid{"فرآیند باید بین ۱ و ۴۰۰ گام داشته باشد."}
	}
	ids := map[string]Node{}
	for i, n := range p.Nodes {
		n.ID = strings.TrimSpace(n.ID)
		if n.ID == "" || len(n.ID) > 64 || ids[n.ID].ID != "" {
			return p, Invalid{"شناسهٔ گام «" + n.ID + "» درست یا یکتا نیست."}
		}
		if !slices.Contains(nodeKinds, n.Kind) {
			n.Kind = "step"
		}
		n.Name = oneLine(n.Name, 160)
		if n.Name == "" && (n.Kind == "step" || n.Kind == "subprocess") {
			return p, Invalid{"گام «" + n.ID + "» نامی می‌خواهد."}
		}
		fields, err := cleanFields(n.Fields, 200)
		if err != nil {
			return p, Invalid{"«" + n.Name + "»: " + err.Error()}
		}
		n.Fields = fields
		if n.Kind == "step" || n.Kind == "subprocess" {
			if !slices.Contains(assignModes, n.Assign.Mode) {
				n.Assign.Mode = "requester"
			}
			if n.Assign.Users, err = users(n.Assign.Users, 200); err != nil {
				return p, err
			}
			n.Assign.Field = strings.TrimSpace(n.Assign.Field)
			n.Assign.Group = oneLine(n.Assign.Group, 80)
		} else {
			n.Assign = Assign{}
		}
		if n.Kind != "gateway" || (n.Gate != "xor" && n.Gate != "par") {
			n.Gate = ""
		}
		if n.Kind == "step" || n.Kind == "subprocess" {
			if n.Before, err = cleanStepActions(n.Before, n.Name); err != nil {
				return p, err
			}
			if n.After, err = cleanStepActions(n.After, n.Name); err != nil {
				return p, err
			}
		} else {
			n.Before, n.After = nil, nil
		}
		n.X, n.Y = mapCoord(n.X), mapCoord(n.Y)
		ids[n.ID] = n
		p.Nodes[i] = n
	}
	if s, ok := ids[p.Start]; !ok || s.Kind != "step" {
		return p, Invalid{"فرآیند باید از یک گام آغاز شود."}
	}
	for i, e := range p.Edges {
		if _, ok := ids[e.From]; !ok {
			return p, Invalid{"مسیری از گامی که نیست: «" + e.From + "»."}
		}
		if e.To != "" {
			if _, ok := ids[e.To]; !ok {
				return p, Invalid{"مسیری به گامی که نیست: «" + e.To + "»."}
			}
		}
		if !slices.Contains(edgeKinds, e.Kind) {
			e.Kind = "seq"
		}
		rules, err := cleanRules(e.Rules, "")
		if err != nil {
			return p, Invalid{"شرط مسیر «" + nodeName(ids[e.From]) + "»: " + err.Error()}
		}
		if e.Rules = rules; len(rules) > 0 {
			e.When = ""
		}
		e.Any = e.Any && len(rules) > 1
		e.When = strings.TrimSpace(e.When)
		if _, err := ParseExpr(e.When); err != nil {
			return p, Invalid{"شرط مسیر «" + nodeName(ids[e.From]) + "» خوانده نشد: " + err.Error()}
		}
		p.Edges[i] = e
	}
	return p, nil
}

// nodeName is a node as a message names it: its name, or its id.
func nodeName(n Node) string {
	if n.Name != "" {
		return n.Name
	}
	return n.ID
}

// mapCoord keeps a map coordinate a plain, bounded number.
func mapCoord(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(math.Max(-100000, math.Min(100000, v)))
}

// node is a map's node by id.
func (p Process) node(id string) (Node, bool) {
	i := slices.IndexFunc(p.Nodes, func(n Node) bool { return n.ID == id })
	if i < 0 {
		return Node{}, false
	}
	return p.Nodes[i], true
}

// out are the routes leaving a node, in their order.
func (p Process) out(id string) []Edge {
	var es []Edge
	for _, e := range p.Edges {
		if e.From == id {
			es = append(es, e)
		}
	}
	slices.SortStableFunc(es, func(a, b Edge) int { return a.Order - b.Order })
	return es
}

// versionOf is the map a case started on: the board's own, or one of the
// older ones it keeps for cases still on them.
func versionOf(b Board, v int) (Process, bool) {
	if b.Process != nil && (v == 0 || b.Process.Version == v) {
		return *b.Process, true
	}
	for _, p := range b.ProcessHistory {
		if p.Version == v {
			return p, true
		}
	}
	return Process{}, false
}

// caseLabel is a case's readable name: the board's process and its number.
func caseLabel(p Process, n int) string {
	if n <= 0 {
		return p.Name
	}
	return p.Name + " #" + strconv.Itoa(n)
}

// CheckProcess is the map as it would be saved, or why it would not be.
func CheckProcess(p Process) (Process, error) { return cleanProcess(p) }
