package tasks

import (
	"context"
	"errors"
	"testing"
)

// fakeData: customers by city, from "the database"; a failing source.
type fakeData struct{ fail bool }

func (d fakeData) Choices(_ context.Context, f FormField, vals map[string]any, q string) ([]Choice, error) {
	if d.fail {
		return nil, errors.New("down")
	}
	if f.Source == "db:reporting" {
		if asString(vals["city"]) == "tehran" {
			return []Choice{{"c1", "پارس"}, {"c2", "آریا"}}, nil
		}
		return []Choice{{"c9", "کیش"}}, nil
	}
	return []Choice{{"r.ahmadi", "شایان"}}, nil
}

func TestSourcedQuestionsAreCheckedWhereTheyCameFrom(t *testing.T) {
	sql := "SELECT id, name FROM customers WHERE city = @@city"
	fields, err := cleanFields([]FormField{
		{Key: "city", Label: "شهر", Kind: "choice", Choices: []Choice{{"tehran", "تهران"}, {"kish", "کیش"}}},
		{Key: "customer", Label: "مشتری", Kind: "choice", Source: "db:reporting", SQL: sql, Display: "search"},
		{Key: "who", Label: "همکار", Kind: "multi", Source: "people"},
		{Key: "phone", Label: "تلفن", Kind: "text", Source: "db:reporting", SQL: "SELECT phone FROM customers WHERE id = @@customer"},
	}, 40)
	if err != nil {
		t.Fatal(err)
	}
	if got := fields[1]; got.Display != "search" || len(got.Deps) != 1 || got.Deps[0] != "city" || got.Choices != nil {
		t.Errorf("the customer question: %+v", got)
	}
	for _, bad := range []FormField{
		{Key: "x", Label: "x", Kind: "date", Source: "db:reporting", SQL: "SELECT 1"},
		{Key: "x", Label: "x", Kind: "choice", Source: "db:reporting"},
		{Key: "x", Label: "x", Kind: "text", Source: "people"},
		{Key: "x", Label: "x", Kind: "choice", Source: "ftp:x", SQL: "SELECT 1"},
	} {
		if _, err := cleanFields([]FormField{bad}, 5); err == nil {
			t.Errorf("%+v was taken", bad)
		}
	}
	s := &Service{Data: fakeData{}}
	ok := map[string]any{"city": "tehran", "customer": "c2", "who": []any{"r.ahmadi"}}
	if err := s.checkSourced(ctx, fields, ok, nil); err != nil {
		t.Errorf("good picks refused: %v", err)
	}
	// c2 is a Tehran customer; for Kish it is not on the list.
	if err := s.checkSourced(ctx, fields, map[string]any{"city": "kish", "customer": "c2"}, nil); err == nil {
		t.Error("a customer from another city's list was taken")
	}
	if err := s.checkSourced(ctx, fields, map[string]any{"who": []any{"ghost"}}, nil); err == nil {
		t.Error("somebody not among docfind's people was taken")
	}
	if err := (&Service{Data: fakeData{fail: true}}).checkSourced(ctx, fields, ok, nil); err == nil {
		t.Error("a pick was taken while its source was down")
	}
	// The pick passes checkAnswers too, though the question has no fixed list.
	if got, err := checkAnswers(fields, ok, nil, func(string) bool { return true }); err != nil || got["customer"] != "c2" {
		t.Errorf("checked: %v %v", got, err)
	}
	// The page that fills it gets what it depends on, not the SQL.
	pub := PublicFields(fields)
	if pub[1].SQL != "" || len(pub[1].Deps) != 1 || fields[1].SQL == "" {
		t.Errorf("public: %+v / kept: %q", pub[1], fields[1].SQL)
	}
}

// Only docfind's administrators add or change a question's SQL; anybody who
// edits the form may keep one that is there.
func TestOnlyAdministratorsWriteSQL(t *testing.T) {
	r := newRig()
	r.s.Data = fakeData{}
	_, b := r.board(t)
	b.Forms = []Form{{ID: "f1", Name: "درخواست", Fields: []FormField{{Key: "c", Label: "مشتری", Kind: "choice", Source: "db:reporting", SQL: "SELECT 1"}}}}
	if _, err := r.s.SaveBoard(ctx, owner, b); err == nil {
		t.Fatal("a board owner who is not an administrator wrote SQL")
	}
	admin := Actor{User: owner.User, Admin: true}
	b, err := r.s.SaveBoard(ctx, admin, b)
	if err != nil {
		t.Fatal(err)
	}
	b.Forms[0].Name = "درخواست خرید"
	if _, err := r.s.SaveBoard(ctx, owner, b); err != nil {
		t.Errorf("the owner could not rename a form whose SQL they left alone: %v", err)
	}
	b.Forms[0].Fields[0].SQL = "SELECT 2"
	if _, err := r.s.SaveBoard(ctx, owner, b); err == nil {
		t.Error("the owner changed the SQL")
	}
	// The same on a process's steps.
	p := miniFlow()
	p.Nodes[0].Fields = append(p.Nodes[0].Fields, FormField{Key: "cust", Label: "مشتری", Kind: "choice", Source: "db:reporting", SQL: "SELECT id FROM c"})
	if _, err := r.s.SaveProcess(ctx, owner, b.ID, p); err == nil {
		t.Error("a process step's SQL was written by somebody not an administrator")
	}
	if _, err := r.s.SaveProcess(ctx, admin, b.ID, p); err != nil {
		t.Fatal(err)
	}
	// Who may ask for its options: the board's people, and nobody else.
	if f, err := r.s.FieldFor(ctx, owner, b.ID, "", "reg", "cust"); err != nil || f.SQL == "" {
		t.Errorf("the owner's own process: %v", err)
	}
	if _, err := r.s.FieldFor(ctx, out, b.ID, "", "reg", "cust"); err == nil {
		t.Error("an outsider read a step's options")
	}
	if _, err := r.s.FieldFor(ctx, owner, b.ID, "", "reg", "subject"); err == nil {
		t.Error("a question with no source answered as one")
	}
}
