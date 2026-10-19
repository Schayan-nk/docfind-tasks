package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Schayan-nk/docfind-tasks/internal/tasks"
)

// Tasks is the task manager's store (migrations/031_tasks.sql). It stores
// what it is given; every rule -- who may, what is valid -- is internal/tasks.
type Tasks struct{ db *DB }

// NewTasks wraps a connection.
func NewTasks(db *DB) *Tasks { return &Tasks{db: db} }

var _ tasks.Store = (*Tasks)(nil)

const tasksTimeout = 15 * time.Second

func (t *Tasks) ctx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, tasksTimeout)
}

func (t *Tasks) Workspaces(ctx context.Context) ([]tasks.Workspace, error) {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	rows, err := t.db.Pool().Query(ctx, `SELECT body FROM tm_workspaces ORDER BY body->>'name'`)
	if err != nil {
		return nil, err
	}
	return scanJSON[tasks.Workspace](rows)
}

func (t *Tasks) SaveWorkspace(ctx context.Context, w tasks.Workspace) error {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	body, err := json.Marshal(w)
	if err != nil {
		return err
	}
	_, err = t.db.Pool().Exec(ctx, `
		INSERT INTO tm_workspaces (id, body) VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET body = EXCLUDED.body, updated_at = now()`, w.ID, body)
	return err
}

func (t *Tasks) DeleteWorkspace(ctx context.Context, id string) error {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	_, err := t.db.Pool().Exec(ctx, `DELETE FROM tm_workspaces WHERE id = $1`, id)
	return err
}

func (t *Tasks) Boards(ctx context.Context, workspaceID string) ([]tasks.Board, error) {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	rows, err := t.db.Pool().Query(ctx, `
		SELECT body FROM tm_boards WHERE $1 = '' OR workspace_id = $1 ORDER BY body->>'created'`, workspaceID)
	if err != nil {
		return nil, err
	}
	return scanJSON[tasks.Board](rows)
}

func (t *Tasks) Board(ctx context.Context, id string) (tasks.Board, error) {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	var b tasks.Board
	var body []byte
	err := t.db.Pool().QueryRow(ctx, `SELECT body FROM tm_boards WHERE id = $1`, id).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, tasks.ErrNotFound
	}
	if err != nil {
		return b, err
	}
	return b, json.Unmarshal(body, &b)
}

func (t *Tasks) SaveBoard(ctx context.Context, b tasks.Board) error {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	body, err := json.Marshal(b)
	if err != nil {
		return err
	}
	_, err = t.db.Pool().Exec(ctx, `
		INSERT INTO tm_boards (id, workspace_id, body) VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET body = EXCLUDED.body, updated_at = now()`, b.ID, b.WorkspaceID, body)
	return err
}

func (t *Tasks) DeleteBoard(ctx context.Context, id string) error {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	_, err := t.db.Pool().Exec(ctx, `DELETE FROM tm_boards WHERE id = $1`, id)
	return err
}

// likeText escapes what a person typed for ILIKE.
func likeText(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

func (t *Tasks) Items(ctx context.Context, q tasks.Query) ([]tasks.Item, error) {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	limit := q.Limit
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	text := ""
	if q.Text != "" {
		text = likeText(q.Text)
	}
	boards := q.BoardIDs
	if boards == nil {
		boards = []string{}
	}
	rows, err := t.db.Pool().Query(ctx, `
		SELECT body FROM tm_items
		 WHERE (cardinality($1::text[]) = 0 OR board_id = ANY($1))
		   AND ($2 = '' OR $2 = ANY(assignees))
		   AND ($3 = '' OR parent_id = $3)
		   AND ($4 = '' OR title ILIKE $4 OR body->>'description' ILIKE $4)
		   AND ($5 = '' OR (due <> '' AND due >= $5))
		   AND ($6 = '' OR (due <> '' AND due <= $6))
		 ORDER BY position, created_at
		 LIMIT $7`, boards, q.Assignee, q.ParentID, text, q.DueFrom, q.DueTo, limit)
	if err != nil {
		return nil, err
	}
	return scanJSON[tasks.Item](rows)
}

func (t *Tasks) Item(ctx context.Context, id string) (tasks.Item, error) {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	var it tasks.Item
	var body []byte
	err := t.db.Pool().QueryRow(ctx, `SELECT body FROM tm_items WHERE id = $1`, id).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, tasks.ErrNotFound
	}
	if err != nil {
		return it, err
	}
	return it, json.Unmarshal(body, &it)
}

// SaveItem writes it at version+1, in one statement that matches the version
// it was read at: two people saving the same item at once cannot both win.
func (t *Tasks) SaveItem(ctx context.Context, it tasks.Item) (tasks.Item, error) {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	read := it.Version
	it.Version++
	body, err := json.Marshal(it)
	if err != nil {
		return it, err
	}
	var parent any
	if it.ParentID != "" {
		parent = it.ParentID
	}
	assignees := it.Assignees
	if assignees == nil {
		assignees = []string{}
	}
	var tag interface{ RowsAffected() int64 }
	if read == 0 {
		ct, err := t.db.Pool().Exec(ctx, `
			INSERT INTO tm_items (id, board_id, parent_id, title, assignees, due, position, version, created_at, body)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) ON CONFLICT (id) DO NOTHING`,
			it.ID, it.BoardID, parent, it.Title, assignees, it.Due, it.Position, it.Version, it.Created, body)
		if err != nil {
			return it, err
		}
		tag = ct
	} else {
		ct, err := t.db.Pool().Exec(ctx, `
			UPDATE tm_items SET parent_id = $3, title = $4, assignees = $5, due = $6, position = $7,
			       version = $8, body = $9
			 WHERE id = $1 AND board_id = $2 AND version = $10`,
			it.ID, it.BoardID, parent, it.Title, assignees, it.Due, it.Position, it.Version, body, read)
		if err != nil {
			return it, err
		}
		tag = ct
	}
	if tag.RowsAffected() == 0 {
		return it, tasks.ErrConflict
	}
	return it, nil
}

func (t *Tasks) DeleteItem(ctx context.Context, id string) error {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	_, err := t.db.Pool().Exec(ctx, `DELETE FROM tm_items WHERE id = $1`, id)
	return err
}

func (t *Tasks) Comments(ctx context.Context, itemID string) ([]tasks.Comment, error) {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	rows, err := t.db.Pool().Query(ctx, `
		SELECT body FROM tm_comments WHERE item_id = $1 ORDER BY created_at LIMIT 2000`, itemID)
	if err != nil {
		return nil, err
	}
	return scanJSON[tasks.Comment](rows)
}

func (t *Tasks) AddComment(ctx context.Context, c tasks.Comment) error {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = t.db.Pool().Exec(ctx, `
		INSERT INTO tm_comments (id, item_id, created_at, body) VALUES ($1, $2, $3, $4)`, c.ID, c.ItemID, c.Created, body)
	return err
}

func (t *Tasks) Activity(ctx context.Context, boardID, itemID string, limit int) ([]tasks.Activity, error) {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := t.db.Pool().Query(ctx, `
		SELECT id, board_id, item_id, actor, action, detail, at FROM tm_activity
		 WHERE ($1 = '' OR board_id = $1) AND ($2 = '' OR item_id = $2)
		 ORDER BY at DESC LIMIT $3`, boardID, itemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []tasks.Activity{}
	for rows.Next() {
		var a tasks.Activity
		if err := rows.Scan(&a.ID, &a.BoardID, &a.ItemID, &a.Actor, &a.Action, &a.Detail, &a.At); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (t *Tasks) AddActivity(ctx context.Context, a tasks.Activity) error {
	ctx, cancel := t.ctx(ctx)
	defer cancel()
	_, err := t.db.Pool().Exec(ctx, `
		INSERT INTO tm_activity (id, board_id, item_id, actor, action, detail, at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		a.ID, a.BoardID, a.ItemID, a.Actor, a.Action, a.Detail, a.At)
	return err
}

// scanJSON reads rows of one JSON column into values.
func scanJSON[T any](rows pgx.Rows) ([]T, error) {
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var v T
		if err := json.Unmarshal(body, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
