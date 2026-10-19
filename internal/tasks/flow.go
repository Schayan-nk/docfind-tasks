package tasks

// Cases moving through a board's process (process.go): starting one,
// finishing a step, claiming a pooled one, and the inbox of open steps. A
// step done saves its answers into the case, then the case follows the
// routes until every branch waits at a step, parks at a join, or ends. If no
// route fits the answers nothing is saved and the person is told why.
//
// The inbox is docfind's: a step reaches its assignee whether or not they are
// a member of the board's workspace, so a workflow can run across teams.

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"
)

// Org is who heads whom and who is in which department (docfind's
// departments page).
type Org interface {
	// Head is the head of the person's primary department, or of the one
	// above when they head it themselves; "" when there is none.
	Head(ctx context.Context, username string) (string, error)
	// Members are a department's people.
	Members(ctx context.Context, department int) ([]string, error)
}

// maxHops stops a map that routes round in circles without a step.
const maxHops = 300

type router struct {
	s       *Service
	ctx     context.Context
	p       Process
	f       *Flow
	by      string
	now     time.Time
	out     []Thread
	pending map[string]int // a split's branches not yet placed
	hops    int
	effects []effect // what the steps' actions send once the case is saved
}

// leave follows the routes out of from, for the branch t that is done there.
func (r *router) leave(t Thread, from string) error {
	if r.hops++; r.hops > maxHops {
		return Invalid{"مسیر فرآیند در دوری بی‌پایان افتاد؛ نقشهٔ فرآیند را بررسی کنید."}
	}
	edges := r.p.out(from)
	if len(edges) == 0 {
		return nil // nowhere to go: the branch ends
	}
	name := from
	if n, ok := r.p.node(from); ok && n.Name != "" {
		name = n.Name
	}
	switch edges[0].Kind {
	case "join":
		return r.join(t, edges[0].To, from)
	case "xor":
		for _, e := range edges {
			if e.holds(r.f.Data) {
				return r.arrive(t, e.To)
			}
		}
		return Invalid{"«" + name + "»: هیچ مسیری با پاسخ‌ها جور نیست."}
	case "par":
		var picked []Edge
		for _, e := range edges {
			if e.holds(r.f.Data) {
				picked = append(picked, e)
			}
		}
		if len(picked) == 0 {
			return Invalid{"«" + name + "»: هیچ مسیری با پاسخ‌ها جور نیست."}
		}
		split := NewID()
		r.pending[split] = len(picked)
		for _, e := range picked {
			child := t
			child.ID = NewID()
			child.Fork = append(slices.Clone(t.Fork), split)
			r.pending[split]--
			if err := r.arrive(child, e.To); err != nil {
				return err
			}
		}
		return nil
	}
	// seq: the first route, whatever its condition says.
	return r.arrive(t, edges[0].To)
}

// arrive places the branch at a node: a step waits there, a gateway routes on
// at once, an end ends it.
func (r *router) arrive(t Thread, to string) error {
	if to == "" {
		return nil
	}
	n, ok := r.p.node(to)
	if !ok {
		return Invalid{"مسیری به گامی که نیست."}
	}
	switch n.Kind {
	case "end":
		return nil
	case "gateway":
		return r.leave(t, to)
	}
	t.Node, t.Since, t.Previous, t.At = to, r.now, r.by, ""
	r.act(n, "before", n.Before)
	t.Assignees, t.Pool = r.assign(n.Assign)
	r.out = append(r.out, t)
	return nil
}

// join parks the branch at the meeting point until no sibling from its split
// is still on its way; the last to arrive goes on, alone, for all of them.
func (r *router) join(t Thread, at, from string) error {
	if len(t.Fork) == 0 {
		return r.arrive(t, at)
	}
	split := t.Fork[len(t.Fork)-1]
	t.At, t.Node = at, from
	r.f.Waiting = append(r.f.Waiting, t)
	if r.live(split) {
		return nil
	}
	var keep []Thread
	for _, w := range r.f.Waiting {
		if !(w.At == at && slices.Contains(w.Fork, split)) {
			keep = append(keep, w)
		}
	}
	r.f.Waiting = keep
	merged := t
	merged.ID, merged.At = NewID(), ""
	merged.Fork = slices.Clone(t.Fork[:len(t.Fork)-1])
	return r.arrive(merged, at)
}

// settle releases every join whose branches have all arrived or ended. A
// branch that ends instead of reaching the join (a rejection on one side, say)
// must not leave the others parked for ever, so after each pass every parked
// group is looked at again.
func (r *router) settle() error {
	for changed := true; changed; {
		changed = false
		for _, w := range r.f.Waiting {
			split := w.Fork[len(w.Fork)-1]
			if r.live(split) {
				continue
			}
			var keep []Thread
			for _, x := range r.f.Waiting {
				if !(x.At == w.At && slices.Contains(x.Fork, split)) {
					keep = append(keep, x)
				}
			}
			r.f.Waiting = keep
			merged := w
			merged.ID, merged.At = NewID(), ""
			merged.Fork = slices.Clone(w.Fork[:len(w.Fork)-1])
			if err := r.arrive(merged, w.At); err != nil {
				return err
			}
			changed = true
			break
		}
	}
	return nil
}

// live says a branch of the split is still on its way somewhere: open at a
// step, placed in this pass, or not placed yet.
func (r *router) live(split string) bool {
	if r.pending[split] > 0 {
		return true
	}
	for _, list := range [][]Thread{r.f.Threads, r.out} {
		for _, x := range list {
			if slices.Contains(x.Fork, split) {
				return true
			}
		}
	}
	return false
}

// assign is whom a step goes to: assignees, or a pool to claim from. Whatever
// cannot be worked out falls to the requester, so no step is lost.
func (r *router) assign(a Assign) ([]string, []string) {
	one := func(u string) ([]string, []string) {
		u = strings.ToLower(strings.TrimSpace(u))
		if u == "" || !userShape.MatchString(u) {
			return []string{r.f.Requester}, nil
		}
		return []string{u}, nil
	}
	pool := func(us []string) ([]string, []string) {
		switch len(us) {
		case 0:
			return []string{r.f.Requester}, nil
		case 1:
			return one(us[0])
		}
		return nil, us
	}
	switch a.Mode {
	case "users":
		return pool(a.Users)
	case "field":
		return one(asString(r.f.Data[a.Field]))
	case "department":
		if r.s.Org != nil && a.Department > 0 {
			if ms, err := r.s.Org.Members(r.ctx, a.Department); err == nil {
				return pool(ms)
			}
		}
		return pool(nil)
	case "manager":
		if r.s.Org != nil {
			if h, err := r.s.Org.Head(r.ctx, r.f.Requester); err == nil && h != "" {
				return one(h)
			}
		}
		return one("")
	case "previous":
		return one(r.by)
	}
	return one(r.f.Requester)
}

// ------------------------------------------------------------- doors

// StartCase starts a case on a board's process: the start step's answers,
// then the routes from it.
func (s *Service) StartCase(ctx context.Context, a Actor, boardID string, answers map[string]any) (Item, error) {
	b, err := s.Store.Board(ctx, boardID)
	if err != nil {
		return Item{}, err
	}
	if b.Process == nil {
		return Item{}, ErrNotFound
	}
	w, err := s.workspace(ctx, b.WorkspaceID)
	if err != nil {
		return Item{}, err
	}
	p := *b.Process
	start, _ := p.node(p.Start)
	if !a.sees(w) && !mayStart(start.Assign, a.User) {
		return Item{}, ErrForbidden
	}
	if err := s.checkSourced(ctx, start.Fields, answers, nil); err != nil {
		return Item{}, err
	}
	typed, err := checkAnswers(start.Fields, answers, nil, s.isPerson)
	if err != nil {
		return Item{}, err
	}
	files := fileIDs(start.Fields, typed)
	if err := s.filesUsable(ctx, a, "", files); err != nil {
		return Item{}, err
	}
	items, err := s.Store.Items(ctx, Query{BoardIDs: []string{b.ID}})
	if err != nil {
		return Item{}, err
	}
	num := 0
	for _, x := range items {
		if x.Flow != nil && x.Flow.Number > num {
			num = x.Flow.Number
		}
	}
	now := s.now()
	f := &Flow{Version: p.Version, Requester: a.User, Data: typed, Number: num + 1,
		Steps: []StepRecord{{Node: start.ID, Name: start.Name, By: a.User, At: now, Values: typed}}}
	r := &router{s: s, ctx: ctx, p: p, f: f, by: a.User, now: now, pending: map[string]int{}}
	r.act(start, "before", start.Before)
	r.act(start, "after", start.After)
	if err := r.leave(Thread{ID: NewID(), Node: p.Start, Since: now}, p.Start); err != nil {
		return Item{}, err
	}
	if err := r.settle(); err != nil {
		return Item{}, err
	}
	f.Threads = r.out
	title := caseLabel(p, f.Number)
	for _, fl := range start.Fields {
		if v, ok := typed[fl.Key]; ok && (fl.Kind == "text" || fl.Kind == "choice") && !blank(v) {
			title += ": " + oneLine(answerText(fl, v), 120)
			break
		}
	}
	it := Item{ID: NewID(), BoardID: b.ID, Title: title, Priority: "normal", Creator: a.User,
		Watchers: []string{a.User}, Created: now, Updated: now, UpdatedBy: a.User, Position: float64(now.UnixMilli()) / 1000}
	settle(&it, b, f)
	clean, err := cleanItem(it, b)
	if err != nil {
		return it, err
	}
	clean.Flow = f
	saved, err := s.Store.SaveItem(ctx, clean)
	if err != nil {
		return saved, err
	}
	s.attachFiles(ctx, saved.ID, files)
	s.log(ctx, saved, a, "case", "پرونده آغاز شد: «"+start.Name+"»")
	s.announce(ctx, saved, p, r.out, a.User)
	s.sendEffects(ctx, saved, b, r.effects, a.User)
	return saved, nil
}

// CompleteStep finishes one open step of a case with its answers.
func (s *Service) CompleteStep(ctx context.Context, a Actor, itemID, threadID string, answers map[string]any) (Item, error) {
	it, b, err := s.caseFor(ctx, a, itemID)
	if err != nil {
		return it, err
	}
	f := cloneFlow(it.Flow)
	ti := slices.IndexFunc(f.Threads, func(t Thread) bool { return t.ID == threadID })
	if ti < 0 {
		return it, Invalid{"این گام دیگر باز نیست؛ صفحه را تازه کنید."}
	}
	t := f.Threads[ti]
	// A pooled step is claimed first (Claim), so two people never fill the
	// same step at once.
	if !a.Admin && !slices.Contains(t.Assignees, a.User) {
		return it, ErrForbidden
	}
	p, ok := versionOf(b, f.Version)
	if !ok {
		return it, Invalid{"نقشهٔ فرآیندی که این پرونده با آن آغاز شد دیگر نیست."}
	}
	n, _ := p.node(t.Node)
	if err := s.checkSourced(ctx, n.Fields, answers, f.Data); err != nil {
		return it, err
	}
	typed, err := checkAnswers(n.Fields, answers, f.Data, s.isPerson)
	if err != nil {
		return it, err
	}
	files := fileIDs(n.Fields, typed)
	if err := s.filesUsable(ctx, a, it.ID, files); err != nil {
		return it, err
	}
	for k, v := range typed {
		f.Data[k] = v
	}
	now := s.now()
	f.Steps = append(f.Steps, StepRecord{Node: n.ID, Name: n.Name, By: a.User, At: now, Values: typed})
	f.Threads = slices.Delete(f.Threads, ti, ti+1)
	r := &router{s: s, ctx: ctx, p: p, f: f, by: a.User, now: now, pending: map[string]int{}}
	r.act(n, "after", n.After)
	if err := r.leave(t, t.Node); err != nil {
		return it, err
	}
	if err := r.settle(); err != nil {
		return it, err
	}
	f.Threads = append(f.Threads, r.out...)
	settle(&it, b, f)
	it.Flow, it.Updated, it.UpdatedBy = f, now, a.User
	saved, err := s.Store.SaveItem(ctx, it)
	if err != nil {
		return saved, err
	}
	s.attachFiles(ctx, saved.ID, files)
	s.log(ctx, saved, a, "step", "«"+n.Name+"» انجام شد")
	s.announce(ctx, saved, p, r.out, a.User)
	s.sendEffects(ctx, saved, b, r.effects, a.User)
	return saved, nil
}

// Claim takes a pooled step for a, who is in its pool.
func (s *Service) Claim(ctx context.Context, a Actor, itemID, threadID string) (Item, error) {
	it, b, err := s.caseFor(ctx, a, itemID)
	if err != nil {
		return it, err
	}
	f := cloneFlow(it.Flow)
	ti := slices.IndexFunc(f.Threads, func(t Thread) bool { return t.ID == threadID })
	if ti < 0 {
		return it, Invalid{"این گام دیگر باز نیست؛ صفحه را تازه کنید."}
	}
	if len(f.Threads[ti].Assignees) > 0 {
		return it, Invalid{"این کار را کس دیگری برداشته است."}
	}
	if !slices.Contains(f.Threads[ti].Pool, a.User) && !a.Admin {
		return it, ErrForbidden
	}
	f.Threads[ti].Assignees, f.Threads[ti].Pool = []string{a.User}, nil
	settle(&it, b, f)
	it.Flow, it.Updated, it.UpdatedBy = f, s.now(), a.User
	saved, err := s.Store.SaveItem(ctx, it)
	if err != nil {
		return saved, err
	}
	s.log(ctx, saved, a, "claim", "کار را برداشت")
	return saved, nil
}

// InboxEntry is one open step waiting for a: theirs, or theirs to claim.
type InboxEntry struct {
	ItemID    string    `json:"item_id"`
	BoardID   string    `json:"board_id"`
	Board     string    `json:"board"`
	Title     string    `json:"title"`
	ThreadID  string    `json:"thread_id"`
	Step      string    `json:"step"`
	Since     time.Time `json:"since"`
	Pool      bool      `json:"pool,omitempty"`
	Requester string    `json:"requester"`
}

// Inbox is every open step across the processes that waits for a.
func (s *Service) Inbox(ctx context.Context, a Actor) ([]InboxEntry, error) {
	boards, err := s.processBoards(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for id := range boards {
		ids = append(ids, id)
	}
	out := []InboxEntry{}
	if len(ids) == 0 {
		return out, nil
	}
	items, err := s.Store.Items(ctx, Query{BoardIDs: ids})
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.Flow == nil || it.Flow.Done {
			continue
		}
		b := boards[it.BoardID]
		p, _ := versionOf(b, it.Flow.Version)
		for _, t := range it.Flow.Threads {
			mine := slices.Contains(t.Assignees, a.User)
			pooled := len(t.Assignees) == 0 && slices.Contains(t.Pool, a.User)
			if !mine && !pooled {
				continue
			}
			n, _ := p.node(t.Node)
			out = append(out, InboxEntry{ItemID: it.ID, BoardID: b.ID, Board: b.Name, Title: it.Title, ThreadID: t.ID,
				Step: n.Name, Since: t.Since, Pool: pooled, Requester: it.Flow.Requester})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out, nil
}

// Case is a case and its board's map, for somebody involved in it or who sees
// its workspace.
func (s *Service) Case(ctx context.Context, a Actor, itemID string) (Item, Process, error) {
	it, b, err := s.caseFor(ctx, a, itemID)
	if err != nil {
		return it, Process{}, err
	}
	p, _ := versionOf(b, it.Flow.Version)
	return it, p, nil
}

// StartableProcess is a process a may start a case on.
type StartableProcess struct {
	BoardID string `json:"board_id"`
	Board   string `json:"board"`
	Name    string `json:"name"`
}

// Startable are the processes a may start.
func (s *Service) Startable(ctx context.Context, a Actor) ([]StartableProcess, error) {
	boards, err := s.processBoards(ctx)
	if err != nil {
		return nil, err
	}
	out := []StartableProcess{}
	for _, b := range boards {
		w, err := s.workspace(ctx, b.WorkspaceID)
		if err != nil || b.Archived {
			continue
		}
		start, _ := b.Process.node(b.Process.Start)
		if a.sees(w) || mayStart(start.Assign, a.User) {
			out = append(out, StartableProcess{BoardID: b.ID, Board: b.Name, Name: b.Process.Name})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// SaveProcess sets a board's process, for its workspace's owner. A map that
// changes keeps the old one for the cases already on it.
func (s *Service) SaveProcess(ctx context.Context, a Actor, boardID string, p Process) (Board, error) {
	b, w, err := s.Board(ctx, a, boardID)
	if err != nil {
		return b, err
	}
	if !a.owns(w) {
		return b, ErrForbidden
	}
	if p, err = cleanProcess(p); err != nil {
		return b, err
	}
	if !a.Admin && sqlEdited(nodeFieldsByKey(b.Process), nodeFieldsByKey(&p)) {
		return b, errSQLAdmin
	}
	if b.Process != nil {
		p.Version = b.Process.Version + 1
		b.ProcessHistory = append(b.ProcessHistory, *b.Process)
		if len(b.ProcessHistory) > 20 {
			b.ProcessHistory = b.ProcessHistory[len(b.ProcessHistory)-20:]
		}
	} else if p.Version <= 0 {
		p.Version = 1
	}
	b.Process = &p
	if err := s.Store.SaveBoard(ctx, b); err != nil {
		return b, err
	}
	_ = s.Store.AddActivity(ctx, Activity{ID: NewID(), BoardID: b.ID, Actor: a.User, Action: "process",
		Detail: "نقشهٔ فرآیند «" + p.Name + "»، نسخهٔ " + itoa(p.Version), At: s.now()})
	return b, nil
}

// ------------------------------------------------------------- helpers

func (s *Service) caseFor(ctx context.Context, a Actor, itemID string) (Item, Board, error) {
	it, err := s.Store.Item(ctx, itemID)
	if err != nil {
		return it, Board{}, err
	}
	if it.Flow == nil {
		return it, Board{}, ErrNotFound
	}
	b, err := s.Store.Board(ctx, it.BoardID)
	if err != nil {
		return it, b, err
	}
	w, err := s.workspace(ctx, b.WorkspaceID)
	if err != nil {
		return it, b, err
	}
	if !a.sees(w) && !involved(it.Flow, a.User) {
		return it, b, ErrNotFound
	}
	return it, b, nil
}

func (s *Service) processBoards(ctx context.Context) (map[string]Board, error) {
	all, err := s.Store.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]Board{}
	for _, w := range all {
		bs, err := s.Store.Boards(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		for _, b := range bs {
			if b.Process != nil {
				out[b.ID] = b
			}
		}
	}
	return out, nil
}

// announce tells each new step's people that it waits for them.
func (s *Service) announce(ctx context.Context, it Item, p Process, steps []Thread, actor string) {
	for _, t := range steps {
		n, _ := p.node(t.Node)
		to := t.Assignees
		if len(to) == 0 {
			to = t.Pool
		}
		s.tell(ctx, to, actor, "کار تازه: «"+n.Name+"»", it.Title, ItemLink(it))
	}
}

// settle keeps the item's own fields in step with its case: who it is with,
// and whether it is done.
func settle(it *Item, b Board, f *Flow) {
	var who []string
	for _, t := range f.Threads {
		for _, u := range t.Assignees {
			if !slices.Contains(who, u) {
				who = append(who, u)
			}
		}
	}
	if who == nil {
		who = []string{}
	}
	it.Assignees = who
	f.Done = len(f.Threads) == 0 && len(f.Waiting) == 0
	open, done := "", ""
	for _, st := range b.Statuses {
		if st.Done && done == "" {
			done = st.Key
		}
		if !st.Done && open == "" {
			open = st.Key
		}
	}
	if f.Done && done != "" {
		it.Status = done
	} else if open != "" && (it.Status == "" || isDone(b, it.Status)) {
		it.Status = open
	}
}

func involved(f *Flow, u string) bool {
	if f.Requester == u {
		return true
	}
	for _, t := range append(slices.Clone(f.Threads), f.Waiting...) {
		if slices.Contains(t.Assignees, u) || slices.Contains(t.Pool, u) {
			return true
		}
	}
	return slices.ContainsFunc(f.Steps, func(r StepRecord) bool { return r.By == u })
}

func mayStart(a Assign, u string) bool {
	return a.Mode == "users" && slices.Contains(a.Users, u)
}

func cloneFlow(f *Flow) *Flow {
	c := *f
	c.Data = map[string]any{}
	for k, v := range f.Data {
		c.Data[k] = v
	}
	c.Threads = slices.Clone(f.Threads)
	c.Waiting = slices.Clone(f.Waiting)
	c.Steps = slices.Clone(f.Steps)
	return &c
}

// NodeByID is the map's node by id; the zero node when there is none.
func (p Process) NodeByID(id string) Node {
	n, _ := p.node(id)
	return n
}

// StartInfo is a process's first step: what a new case asks.
type StartInfo struct {
	Name     string
	StepName string
	Node     string
	Fields   []FormField
}

// StartForm is the start step's form, for somebody who may start a case.
func (s *Service) StartForm(ctx context.Context, a Actor, boardID string) (StartInfo, error) {
	b, err := s.Store.Board(ctx, boardID)
	if err != nil {
		return StartInfo{}, err
	}
	if b.Process == nil {
		return StartInfo{}, ErrNotFound
	}
	w, err := s.workspace(ctx, b.WorkspaceID)
	if err != nil {
		return StartInfo{}, err
	}
	start, _ := b.Process.node(b.Process.Start)
	if !a.sees(w) && !mayStart(start.Assign, a.User) {
		return StartInfo{}, ErrForbidden
	}
	return StartInfo{Name: b.Process.Name, StepName: start.Name, Node: start.ID, Fields: start.Fields}, nil
}
