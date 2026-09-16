package store

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ErrInvalidReorder means the client sent an empty or repeated id. Unknown
// ids are ErrNotFound so the HTTP layer can tell a typo from a missing row.
var ErrInvalidReorder = errors.New("store: reorder ids must be distinct")

// rankStep leaves gaps so a later insert can sit between two rows without
// rewriting the whole list. Zero stays the "never dragged" sentinel.
const rankStep = 1000

// ReorderThreads pins the given conversations in that order. Rows not named
// keep whatever rank they already had, so a project-filtered drag does not
// scramble conversations the sidebar was not showing.
func (s *Store) ReorderThreads(ids []string) error {
	return s.reorder("threads", ids)
}

// ReorderProjects pins the given projects in that order.
func (s *Store) ReorderProjects(ids []string) error {
	return s.reorder("projects", ids)
}

func (s *Store) reorder(table string, ids []string) error {
	clean, err := uniqueIDs(ids)
	if err != nil {
		return err
	}
	if len(clean) == 0 {
		return nil
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		for i, id := range clean {
			res := tx.Table(table).Where("id = ?", id).Update("sort_rank", (i+1)*rankStep)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return ErrNotFound
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store: reorder %s: %w", table, err)
	}
	return nil
}

// interleaveByTime keeps dragged rows in rank order and lets unranked
// rows in by last activity. Rank 0 is "never dragged", not "always first":
// a stale unranked conversation sitting above one that just ran is the
// All-conversations list after a drag inside a project.
func interleaveByTime[T any](items []T, rank func(T) int, at func(T) time.Time) []T {
	unpinned := make([]T, 0, len(items))
	pinned := make([]T, 0, len(items))
	for _, item := range items {
		if rank(item) == 0 {
			unpinned = append(unpinned, item)
		} else {
			pinned = append(pinned, item)
		}
	}
	if len(unpinned) == 0 || len(pinned) == 0 {
		return items
	}
	out := make([]T, 0, len(items))
	i, j := 0, 0
	for i < len(unpinned) && j < len(pinned) {
		if !at(unpinned[i]).Before(at(pinned[j])) {
			out = append(out, unpinned[i])
			i++
		} else {
			out = append(out, pinned[j])
			j++
		}
	}
	out = append(out, unpinned[i:]...)
	out = append(out, pinned[j:]...)
	return out
}

func uniqueIDs(ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, ErrInvalidReorder
		}
		if _, dup := seen[id]; dup {
			return nil, ErrInvalidReorder
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

// bumpProject floats a project to the top of the auto-sorted list. A
// conversation can outlive its project; that is not a failed turn.
func (s *Store) bumpProject(id string) error {
	if id == "" {
		return nil
	}
	if err := s.touchProject(id); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

func (s *Store) touchProject(id string) error {
	res := s.db.Model(&Project{}).Where("id = ?", id).Update("updated_at", time.Now().UTC())
	if res.Error != nil {
		return fmt.Errorf("store: touch project: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
