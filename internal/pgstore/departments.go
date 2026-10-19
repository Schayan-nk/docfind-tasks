package pgstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Departments (migration 038; find/departments.go): made and edited on the
// users page, and which of them each person belongs to -- several at once,
// one primary. OAID is a department's id in a directory it was imported
// from, if any.

// Department is one unit or post.
type Department struct {
	ID       int    `json:"id"`
	OAID     *int64 `json:"oa_id,omitempty"`
	ParentID *int   `json:"parent_id,omitempty"`
	Name     string `json:"name"`
	Code     string `json:"code"`
	Kind     string `json:"kind"`
	Active   bool   `json:"active"`
	Members  int    `json:"members"`
	// Head is who heads it, a docfind username; "" for nobody set
	// (migration 040).
	Head string `json:"head"`
}

// Membership is one person in one department.
type Membership struct {
	Username     string `json:"username"`
	DepartmentID int    `json:"department_id"`
	Primary      bool   `json:"primary"`
}

// ErrNoDepartment is a department that is not there.
var ErrNoDepartment = errors.New("pgstore: no such department")

// Departments is the store.
type Departments struct{ db *DB }

// NewDepartments returns a store over db.
func NewDepartments(db *DB) *Departments { return &Departments{db: db} }

// List is every department with how many people are in each.
func (d *Departments) List(ctx context.Context) ([]Department, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	rows, err := d.db.Pool().Query(ctx, `
		SELECT dp.id, dp.oa_id, dp.parent_id, dp.name, dp.code, dp.kind, dp.active,
		       (SELECT COUNT(*) FROM user_departments ud WHERE ud.department_id = dp.id), dp.head
		  FROM departments dp ORDER BY dp.name`)
	if err != nil {
		return nil, fmt.Errorf("pgstore: listing departments: %w", err)
	}
	defer rows.Close()
	var out []Department
	for rows.Next() {
		var x Department
		if err := rows.Scan(&x.ID, &x.OAID, &x.ParentID, &x.Name, &x.Code, &x.Kind, &x.Active, &x.Members, &x.Head); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Save creates a department (ID 0) or changes one; a parent that would make
// it its own ancestor is refused.
func (d *Departments) Save(ctx context.Context, x Department) (Department, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	x.Name = strings.TrimSpace(x.Name)
	if x.Name == "" {
		return x, fmt.Errorf("pgstore: a department needs a name")
	}
	if x.Kind != "position" {
		x.Kind = "unit"
	}
	if x.ID != 0 && x.ParentID != nil {
		// Walk up from the new parent; meeting this department is a cycle.
		cur := *x.ParentID
		for i := 0; i < 64 && cur != 0; i++ {
			if cur == x.ID {
				return x, fmt.Errorf("pgstore: a department cannot sit under itself")
			}
			var next *int
			if err := d.db.Pool().QueryRow(ctx, `SELECT parent_id FROM departments WHERE id = $1`, cur).Scan(&next); err != nil {
				break
			}
			if next == nil {
				break
			}
			cur = *next
		}
	}
	if x.ID == 0 {
		err := d.db.Pool().QueryRow(ctx, `
			INSERT INTO departments (parent_id, name, code, kind, active, head) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			x.ParentID, x.Name, strings.TrimSpace(x.Code), x.Kind, x.Active, strings.TrimSpace(x.Head)).Scan(&x.ID)
		return x, err
	}
	tag, err := d.db.Pool().Exec(ctx, `
		UPDATE departments SET parent_id = $2, name = $3, code = $4, kind = $5, active = $6, head = $7, updated_at = now()
		 WHERE id = $1`, x.ID, x.ParentID, x.Name, strings.TrimSpace(x.Code), x.Kind, x.Active, strings.TrimSpace(x.Head))
	if err != nil {
		return x, err
	}
	if tag.RowsAffected() == 0 {
		return x, ErrNoDepartment
	}
	return x, nil
}

// Delete removes a department; its children move up to its parent and its
// members simply leave it.
func (d *Departments) Delete(ctx context.Context, id int) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	return pgx.BeginFunc(ctx, d.db.Pool(), func(tx pgx.Tx) error {
		var parent *int
		if err := tx.QueryRow(ctx, `SELECT parent_id FROM departments WHERE id = $1`, id).Scan(&parent); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoDepartment
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE departments SET parent_id = $2 WHERE parent_id = $1`, id, parent); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM departments WHERE id = $1`, id)
		return err
	})
}

// Memberships are everybody's departments.
func (d *Departments) Memberships(ctx context.Context) ([]Membership, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	rows, err := d.db.Pool().Query(ctx, `SELECT username, department_id, is_primary FROM user_departments ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Membership
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.Username, &m.DepartmentID, &m.Primary); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMemberships replaces one person's departments; primary names the one
// marked primary (0 for the first given).
func (d *Departments) SetMemberships(ctx context.Context, username string, ids []int, primary int) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	username = strings.ToLower(strings.TrimSpace(username))
	if primary == 0 && len(ids) > 0 {
		primary = ids[0]
	}
	return pgx.BeginFunc(ctx, d.db.Pool(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM user_departments WHERE username = $1`, username); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := tx.Exec(ctx, `INSERT INTO user_departments (username, department_id, is_primary)
				VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, username, id, id == primary); err != nil {
				return err
			}
		}
		return nil
	})
}
