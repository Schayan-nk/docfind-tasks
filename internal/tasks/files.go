package tasks

// Files a question takes (kind "file"): a form's or a step's
// attachments. The page sends each file first (find/taskfiles.go keeps it,
// for whoever sent it, attached to nothing); the answer is then a list of
// {id, name, size}. Saving checks every file is the sender's own and still
// free -- or already this item's -- and attaches them to the item, after
// which they are anybody's who may see it.

import (
	"context"
	"regexp"
	"strings"
)

// FileStore keeps the files questions take.
type FileStore interface {
	// Usable is nil when every file was sent by user and is attached to
	// nothing, or is already itemID's own.
	Usable(ctx context.Context, user, itemID string, ids []string) error
	// Attach makes the files itemID's.
	Attach(ctx context.Context, itemID string, ids []string) error
}

var fileID = regexp.MustCompile(`^[a-f0-9]{24}$`)

// maxFiles is the most files one question holds.
const maxFiles = 20

// checkFiles is a file question's answer as kept: each file's id, name and size.
func checkFiles(f FormField, v any) (any, error) {
	var out []any
	for _, x := range asList(v) {
		m, ok := x.(map[string]any)
		if !ok {
			return nil, Invalid{"«" + f.Label + "»: پیوستی درست فرستاده نشد."}
		}
		id := strings.TrimSpace(asString(m["id"]))
		if !fileID.MatchString(id) {
			return nil, Invalid{"«" + f.Label + "»: پیوستی درست فرستاده نشد."}
		}
		size, _ := number(m["size"])
		out = append(out, map[string]any{"id": id, "name": oneLine(asString(m["name"]), 200), "size": size})
		if len(out) > maxFiles {
			return nil, Invalid{"«" + f.Label + "» بیش از ۲۰ پیوست دارد."}
		}
	}
	if out == nil {
		out = []any{}
	}
	return out, nil
}

// fileIDs are the files a set of answers holds.
func fileIDs(fields []FormField, typed map[string]any) []string {
	var ids []string
	for _, f := range fields {
		if f.Kind != "file" {
			continue
		}
		for _, x := range asList(typed[f.Key]) {
			if m, ok := x.(map[string]any); ok {
				ids = append(ids, asString(m["id"]))
			}
		}
	}
	return ids
}

// filesUsable checks a's files may go on itemID ("" for one being made).
func (s *Service) filesUsable(ctx context.Context, a Actor, itemID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if s.Files == nil {
		return Invalid{"پیوست روی این سرور نگه داشته نمی‌شود."}
	}
	return s.Files.Usable(ctx, a.User, itemID, ids)
}

func (s *Service) attachFiles(ctx context.Context, itemID string, ids []string) {
	if len(ids) > 0 && s.Files != nil {
		_ = s.Files.Attach(ctx, itemID, ids)
	}
}

// SeesItem says whether a may see an item: its board's people, whoever made
// it (somebody who sent a form to a board they are not on), and everybody a
// case has involved.
func (s *Service) SeesItem(ctx context.Context, a Actor, itemID string) bool {
	if _, _, _, err := s.Item(ctx, a, itemID); err == nil {
		return true
	}
	it, err := s.Store.Item(ctx, itemID)
	if err != nil {
		return false
	}
	return it.Creator == a.User || (it.Flow != nil && involved(it.Flow, a.User))
}
