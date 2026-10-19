package find

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Schayan-nk/docfind-tasks/internal/pgstore"
)

// The users page's departments: created, renamed, moved and deleted here,
// each person in any number of them and one of them primary. A process step
// can go to a department, or to the head of the requester's primary one. All
// for whoever manages accounts.

// departmentStore is what this needs of pgstore.Departments.
type departmentStore interface {
	List(ctx context.Context) ([]pgstore.Department, error)
	Save(ctx context.Context, x pgstore.Department) (pgstore.Department, error)
	Delete(ctx context.Context, id int) error
	Memberships(ctx context.Context) ([]pgstore.Membership, error)
	SetMemberships(ctx context.Context, username string, ids []int, primary int) error
}

// UseDepartments turns departments on.
func (s *Server) UseDepartments(st departmentStore) { s.departments = st }

func (s *Server) departmentsReady(w http.ResponseWriter) bool {
	if s.departments == nil {
		writeJSONError(w, http.StatusNotFound, `{"error":"برای واحدها، پایگاه دادهٔ PostgreSQL لازم است."}`)
		return false
	}
	return true
}

// handleDepartments is GET /api/departments: the tree and who is in each.
func (s *Server) handleDepartments(w http.ResponseWriter, r *http.Request) {
	if !s.departmentsReady(w) {
		return
	}
	deps, err := s.departments.List(r.Context())
	if err != nil {
		logReportError("departments", err)
		writeJSONError(w, http.StatusBadGateway, `{"error":"واحدها خوانده نشد."}`)
		return
	}
	mem, err := s.departments.Memberships(r.Context())
	if err != nil {
		logReportError("departments: memberships", err)
		writeJSONError(w, http.StatusBadGateway, `{"error":"عضویت‌ها خوانده نشد."}`)
		return
	}
	if deps == nil {
		deps = []pgstore.Department{}
	}
	if mem == nil {
		mem = []pgstore.Membership{}
	}
	writeJSON(w, map[string]any{"departments": deps, "memberships": mem})
}

// handleSaveDepartment is POST (new) or PUT (change) /api/departments.
func (s *Server) handleSaveDepartment(w http.ResponseWriter, r *http.Request) {
	if !s.departmentsReady(w) {
		return
	}
	var x pgstore.Department
	if err := readJSON(r, &x); err != nil || strings.TrimSpace(x.Name) == "" || len([]rune(x.Name)) > 150 {
		writeJSONError(w, http.StatusBadRequest, `{"error":"نام واحد لازم است (تا ۱۵۰ نویسه)."}`)
		return
	}
	// A head is a docfind account that may sign in (taskflow.go's "the
	// requester's manager").
	x.Head = strings.ToLower(strings.TrimSpace(x.Head))
	if x.Head != "" && !s.isActiveUser(x.Head) {
		writeJSONError(w, http.StatusBadRequest, `{"error":"سرپرست باید یکی از کاربران فعال docfind باشد."}`)
		return
	}
	if r.Method == http.MethodPost {
		x.ID = 0
		x.Active = true
	} else if x.ID <= 0 {
		writeJSONError(w, http.StatusBadRequest, `{"error":"کدام واحد؟"}`)
		return
	}
	saved, err := s.departments.Save(r.Context(), x)
	if errors.Is(err, pgstore.ErrNoDepartment) {
		writeJSONError(w, http.StatusNotFound, `{"error":"این واحد نیست."}`)
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, `{"error":`+jsonQuote("ذخیره نشد: "+err.Error())+`}`)
		return
	}
	s.auditBPM(actor(r), "department.save", "department:"+strconv.Itoa(saved.ID), map[string]any{"name": saved.Name})
	writeJSON(w, map[string]any{"ok": true, "department": saved})
}

// handleDeleteDepartment is DELETE /api/departments?id=: its children move up,
// its members simply leave it.
func (s *Server) handleDeleteDepartment(w http.ResponseWriter, r *http.Request) {
	if !s.departmentsReady(w) {
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	if id <= 0 {
		writeJSONError(w, http.StatusBadRequest, `{"error":"کدام واحد؟"}`)
		return
	}
	if err := s.departments.Delete(r.Context(), id); err != nil {
		if errors.Is(err, pgstore.ErrNoDepartment) {
			writeJSONError(w, http.StatusNotFound, `{"error":"این واحد نیست."}`)
			return
		}
		writeJSONError(w, http.StatusInternalServerError, `{"error":"حذف نشد."}`)
		return
	}
	s.auditBPM(actor(r), "department.delete", "department:"+strconv.Itoa(id), nil)
	writeJSON(w, map[string]any{"ok": true})
}

// handleUserDepartments is PUT /api/users/departments {username, ids, primary}.
func (s *Server) handleUserDepartments(w http.ResponseWriter, r *http.Request) {
	if !s.departmentsReady(w) {
		return
	}
	var req struct {
		Username string `json:"username"`
		IDs      []int  `json:"ids"`
		Primary  int    `json:"primary"`
	}
	if err := readJSON(r, &req); err != nil || len(req.IDs) > 50 {
		writeJSONError(w, http.StatusBadRequest, `{"error":"درخواست نامعتبر است."}`)
		return
	}
	if _, err := s.guard.Users.Get(req.Username); err != nil {
		writeUserError(w, err)
		return
	}
	if err := s.departments.SetMemberships(r.Context(), req.Username, req.IDs, req.Primary); err != nil {
		logReportError("user departments", err)
		writeJSONError(w, http.StatusInternalServerError, `{"error":"ذخیره نشد."}`)
		return
	}
	s.auditBPM(actor(r), "user.departments", "user:"+req.Username, map[string]any{"count": len(req.IDs)})
	writeJSON(w, map[string]any{"ok": true})
}
