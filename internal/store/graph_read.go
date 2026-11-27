package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"gorm.io/gorm"
)

// Page limits for graph reads. The model reads pages; the panel reads the
// whole graph up to GraphMaxRows and is told when there is more.
const (
	GraphDefaultPage = 50
	GraphMaxRows     = 2000
)

// ErrGraphNotFound means a subject id is not in this owner's graph.
var ErrGraphNotFound = fmt.Errorf("%w: no such subject here", ErrNotFound)

// GraphQuery selects what ReadGraph returns. With nothing set it returns a
// page of subjects, newest change first.
type GraphQuery struct {
	// ID returns one subject with its links, the subjects they reach and the
	// to-dos about it.
	ID string
	// All returns subjects, their links and the to-dos in one snapshot, as the
	// panel draws them.
	All bool
	// Todos returns the ordered list, optionally filtered by Status.
	Todos  bool
	Status string
	// Text matches subject names and identity keys.
	Text string
	// Offset and Limit page subjects or to-dos. A zero Limit means the default
	// page; a negative Limit means everything up to GraphMaxRows.
	Offset, Limit int
}

// GraphView is a read result. Attribute values are exactly as stored,
// including credentials; callers redact for every reader but the model's
// by-id read.
type GraphView struct {
	OwnerID  string         `json:"owner_id"`
	Revision int64          `json:"revision"`
	Subjects []GraphSubject `json:"subjects"`
	Links    []GraphLink    `json:"links"`
	Todos    []GraphTodo    `json:"todos"`
	// Total is the number of rows the query matched before paging.
	Total int `json:"total"`
	// More is true when rows remain after this page.
	More bool `json:"more"`
}

// GraphRevisionOf is the owner's latest revision, 0 for an empty graph.
func (s *Store) GraphRevisionOf(ctx context.Context, ownerID string) (int64, error) {
	return graphRevision(s.db.WithContext(ctx), ownerID)
}

func graphRevision(tx *gorm.DB, ownerID string) (int64, error) {
	var rev int64
	err := tx.Model(&GraphRevision{}).Where("owner_id = ?", ownerID).Select("COALESCE(MAX(revision), 0)").Scan(&rev).Error
	return rev, err
}

func pageOf(q GraphQuery) (offset, limit int) {
	offset, limit = max(q.Offset, 0), q.Limit
	switch {
	case limit == 0:
		limit = GraphDefaultPage
	case limit < 0 || limit > GraphMaxRows:
		limit = GraphMaxRows
	}
	return offset, limit
}

// ReadGraph reads one owner's graph in a single consistent snapshot.
func (s *Store) ReadGraph(ctx context.Context, ownerID string, q GraphQuery) (GraphView, error) {
	view := GraphView{OwnerID: ownerID, Subjects: []GraphSubject{}, Links: []GraphLink{}, Todos: []GraphTodo{}}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if view.Revision, err = graphRevision(tx, ownerID); err != nil {
			return err
		}
		switch {
		case q.ID != "":
			return readGraphSubject(tx, ownerID, q.ID, &view)
		case q.Todos:
			return readGraphTodos(tx, ownerID, q, &view)
		case q.All:
			if err := readGraphSubjects(tx, ownerID, q, &view); err != nil {
				return err
			}
			subjects, more, total := view.Subjects, view.More, view.Total
			if err := readGraphTodos(tx, ownerID, GraphQuery{Limit: q.Limit}, &view); err != nil {
				return err
			}
			// Total and More describe the subjects; the to-do page is bounded alike.
			view.Total, view.More = total, more || view.More
			view.Subjects = subjects
			return nil
		}
		return readGraphSubjects(tx, ownerID, q, &view)
	})
	if err != nil {
		return GraphView{}, err
	}
	if err := s.markGraphEvidence(ctx, &view); err != nil {
		return GraphView{}, err
	}
	return view, nil
}

func readGraphTodos(tx *gorm.DB, ownerID string, q GraphQuery, view *GraphView) error {
	query := tx.Model(&GraphTodo{}).Where("owner_id = ?", ownerID)
	if q.Status != "" {
		query = query.Where("status = ?", q.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return err
	}
	offset, limit := pageOf(q)
	if err := query.Order("position, created_at, id").Offset(offset).Limit(limit).Find(&view.Todos).Error; err != nil {
		return err
	}
	view.Total, view.More = int(total), offset+len(view.Todos) < int(total)
	return nil
}

func readGraphSubjects(tx *gorm.DB, ownerID string, q GraphQuery, view *GraphView) error {
	query := tx.Model(&GraphSubject{}).Where("owner_id = ?", ownerID)
	if text := strings.ToLower(strings.TrimSpace(q.Text)); text != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text) + "%"
		query = query.Where(`LOWER(name) LIKE ? ESCAPE '\' OR id IN (
			SELECT subject_id FROM graph_subject_keys WHERE owner_id = ? AND LOWER(value) LIKE ? ESCAPE '\')`, like, ownerID, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return err
	}
	offset, limit := pageOf(q)
	if err := query.Order("updated_at DESC, id").Offset(offset).Limit(limit).Find(&view.Subjects).Error; err != nil {
		return err
	}
	view.Total, view.More = int(total), offset+len(view.Subjects) < int(total)
	if len(view.Subjects) == 0 {
		return nil
	}
	ids := make([]string, len(view.Subjects))
	for i, sj := range view.Subjects {
		ids[i] = sj.ID
	}
	return tx.Where("owner_id = ? AND from_id IN ? AND to_id IN ?", ownerID, ids, ids).
		Order("created_at, id").Find(&view.Links).Error
}

func readGraphSubject(tx *gorm.DB, ownerID, id string, view *GraphView) error {
	var subject GraphSubject
	if err := tx.Take(&subject, "id = ? AND owner_id = ?", id, ownerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrGraphNotFound
		}
		return err
	}
	if err := tx.Where("owner_id = ? AND (from_id = ? OR to_id = ?)", ownerID, id, id).
		Order("created_at, id").Limit(GraphMaxRows).Find(&view.Links).Error; err != nil {
		return err
	}
	ids := []string{id}
	for _, l := range view.Links {
		ids = append(ids, l.FromID, l.ToID)
	}
	if err := tx.Where("owner_id = ? AND id IN ?", ownerID, ids).Order("id").Find(&view.Subjects).Error; err != nil {
		return err
	}
	view.Total = len(view.Subjects)
	return tx.Where("owner_id = ? AND about = ?", ownerID, id).Order("position, created_at, id").Find(&view.Todos).Error
}

// markGraphEvidence flags evidence whose event no longer exists, for example
// after a rewind, so readers can show it as unavailable instead of trusting it.
func (s *Store) markGraphEvidence(ctx context.Context, view *GraphView) error {
	wanted := map[string]map[int64]bool{}
	collect := func(ev []graph.Evidence) {
		for _, e := range ev {
			if e.URL == "" {
				if wanted[e.ThreadID] == nil {
					wanted[e.ThreadID] = map[int64]bool{}
				}
				wanted[e.ThreadID][e.Seq] = false
			}
		}
	}
	for i := range view.Subjects {
		collect(view.Subjects[i].Evidence)
	}
	for i := range view.Links {
		collect(view.Links[i].Evidence)
	}
	for i := range view.Todos {
		collect(view.Todos[i].Evidence)
	}
	for thread, seqs := range wanted {
		list := make([]int64, 0, len(seqs))
		for seq := range seqs {
			list = append(list, seq)
		}
		var found []int64
		if err := s.db.WithContext(ctx).Model(&Event{}).Where("thread_id = ? AND seq IN ?", thread, list).Pluck("seq", &found).Error; err != nil {
			return err
		}
		for _, seq := range found {
			seqs[seq] = true
		}
	}
	mark := func(ev []graph.Evidence) {
		for i := range ev {
			if ev[i].URL == "" && !wanted[ev[i].ThreadID][ev[i].Seq] {
				ev[i].Unavailable = true
			}
		}
	}
	for i := range view.Subjects {
		mark(view.Subjects[i].Evidence)
	}
	for i := range view.Links {
		mark(view.Links[i].Evidence)
	}
	for i := range view.Todos {
		mark(view.Todos[i].Evidence)
	}
	return nil
}
