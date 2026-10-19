package tasks

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
)

// MemStore is a Store in memory: for tests, and nothing else -- the task
// manager's records live in PostgreSQL (internal/pgstore/tasks.go). Values
// are copied in and out, as a database would, so a caller changing what it
// was given changes nothing stored.
type MemStore struct {
	mu         sync.Mutex
	workspaces []Workspace
	boards     []Board
	items      []Item
	comments   []Comment
	activity   []Activity
}

// NewMemStore is an empty store.
func NewMemStore() *MemStore { return &MemStore{} }

func clone[T any](v T) T {
	b, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(b, &out)
	return out
}

func (m *MemStore) Workspaces(context.Context) ([]Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return clone(append([]Workspace{}, m.workspaces...)), nil
}

func (m *MemStore) SaveWorkspace(_ context.Context, w Workspace) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := slices.IndexFunc(m.workspaces, func(x Workspace) bool { return x.ID == w.ID }); i >= 0 {
		m.workspaces[i] = clone(w)
	} else {
		m.workspaces = append(m.workspaces, clone(w))
	}
	return nil
}

func (m *MemStore) DeleteWorkspace(_ context.Context, id string) error {
	m.mu.Lock()
	var boards []string
	for _, b := range m.boards {
		if b.WorkspaceID == id {
			boards = append(boards, b.ID)
		}
	}
	m.workspaces = slices.DeleteFunc(m.workspaces, func(x Workspace) bool { return x.ID == id })
	m.mu.Unlock()
	for _, b := range boards {
		_ = m.DeleteBoard(context.Background(), b)
	}
	return nil
}

func (m *MemStore) Boards(_ context.Context, workspaceID string) ([]Board, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Board{}
	for _, b := range m.boards {
		if workspaceID == "" || b.WorkspaceID == workspaceID {
			out = append(out, clone(b))
		}
	}
	return out, nil
}

func (m *MemStore) Board(_ context.Context, id string) (Board, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := slices.IndexFunc(m.boards, func(x Board) bool { return x.ID == id }); i >= 0 {
		return clone(m.boards[i]), nil
	}
	return Board{}, ErrNotFound
}

func (m *MemStore) SaveBoard(_ context.Context, b Board) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := slices.IndexFunc(m.boards, func(x Board) bool { return x.ID == b.ID }); i >= 0 {
		m.boards[i] = clone(b)
	} else {
		m.boards = append(m.boards, clone(b))
	}
	return nil
}

func (m *MemStore) DeleteBoard(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.boards = slices.DeleteFunc(m.boards, func(x Board) bool { return x.ID == id })
	m.items = slices.DeleteFunc(m.items, func(x Item) bool { return x.BoardID == id })
	return nil
}

func (m *MemStore) Items(_ context.Context, q Query) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Item{}
	text := strings.ToLower(q.Text)
	for _, it := range m.items {
		switch {
		case len(q.BoardIDs) > 0 && !slices.Contains(q.BoardIDs, it.BoardID),
			q.Assignee != "" && !slices.Contains(it.Assignees, q.Assignee),
			q.ParentID != "" && it.ParentID != q.ParentID,
			text != "" && !strings.Contains(strings.ToLower(it.Title+" "+it.Description), text),
			q.DueFrom != "" && (it.Due == "" || it.Due < q.DueFrom),
			q.DueTo != "" && (it.Due == "" || it.Due > q.DueTo):
			continue
		}
		out = append(out, clone(it))
		if q.Limit > 0 && len(out) >= q.Limit {
			break
		}
	}
	slices.SortStableFunc(out, func(a, b Item) int {
		switch {
		case a.Position < b.Position:
			return -1
		case a.Position > b.Position:
			return 1
		}
		return a.Created.Compare(b.Created)
	})
	return out, nil
}

func (m *MemStore) Item(_ context.Context, id string) (Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := slices.IndexFunc(m.items, func(x Item) bool { return x.ID == id }); i >= 0 {
		return clone(m.items[i]), nil
	}
	return Item{}, ErrNotFound
}

func (m *MemStore) SaveItem(_ context.Context, it Item) (Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.items, func(x Item) bool { return x.ID == it.ID })
	switch {
	case i < 0 && it.Version != 0, i >= 0 && m.items[i].Version != it.Version:
		return Item{}, ErrConflict
	}
	it.Version++
	if i >= 0 {
		m.items[i] = clone(it)
	} else {
		m.items = append(m.items, clone(it))
	}
	return clone(it), nil
}

func (m *MemStore) DeleteItem(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = slices.DeleteFunc(m.items, func(x Item) bool { return x.ID == id || x.ParentID == id })
	return nil
}

func (m *MemStore) Comments(_ context.Context, itemID string) ([]Comment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Comment{}
	for _, c := range m.comments {
		if c.ItemID == itemID {
			out = append(out, clone(c))
		}
	}
	return out, nil
}

func (m *MemStore) AddComment(_ context.Context, c Comment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.comments = append(m.comments, clone(c))
	return nil
}

func (m *MemStore) Activity(_ context.Context, boardID, itemID string, limit int) ([]Activity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Activity{}
	for i := len(m.activity) - 1; i >= 0; i-- {
		a := m.activity[i]
		if (boardID != "" && a.BoardID != boardID) || (itemID != "" && a.ItemID != itemID) {
			continue
		}
		out = append(out, a)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *MemStore) AddActivity(_ context.Context, a Activity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activity = append(m.activity, a)
	return nil
}
