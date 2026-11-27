package store

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// Digest limits: the digest rides on every model request of a turn, so it is
// a fixed small window onto the graph rather than a dump of it.
const (
	DigestTodos    = 12
	DigestSubjects = 12
)

// GraphDigest is the compact state the model sees on each request.
type GraphDigest struct {
	Revision int64
	// Subjects counts every subject by kind and by status. CurrentByKind omits
	// retracted subjects, which are still in ByKind.
	Subjects      int
	ByKind        map[string]int
	CurrentByKind map[string]int
	Retracted     int
	Todos         []GraphTodo
	OpenTodoTotal int
	Recent        []GraphSubject
	// Unrecorded counts this turn's tool results produced after the last graph
	// write of the turn: results the graph has not been told about yet.
	Unrecorded int
}

// Empty reports whether there is nothing to show: no subjects, no open work and
// no unrecorded results.
func (d GraphDigest) Empty() bool {
	return d.Subjects == 0 && d.OpenTodoTotal == 0 && d.Unrecorded == 0
}

// ThreadGraphOwner is the owner of the conversation's graph as of now. Turns
// freeze their own owner when they start; this is for readers outside a turn.
func (s *Store) ThreadGraphOwner(ctx context.Context, threadID string) (string, error) {
	var th Thread
	if err := s.db.WithContext(ctx).Select("id", "project_id").Take(&th, "id = ?", threadID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	return GraphOwnerID(th.ProjectID, th.ID), nil
}

// ReadGraphDigest builds the per-request digest in one snapshot.
func (s *Store) ReadGraphDigest(ctx context.Context, ownerID, threadID, turnID string) (GraphDigest, error) {
	d := GraphDigest{ByKind: map[string]int{}, CurrentByKind: map[string]int{}}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if d.Revision, err = graphRevision(tx, ownerID); err != nil {
			return err
		}
		var kinds []struct {
			Kind   string
			Status string
			N      int
		}
		if err := tx.Model(&GraphSubject{}).Where("owner_id = ?", ownerID).
			Select("kind, status, COUNT(*) AS n").Group("kind, status").Scan(&kinds).Error; err != nil {
			return err
		}
		for _, k := range kinds {
			d.Subjects += k.N
			d.ByKind[k.Kind] += k.N
			if k.Status == "retracted" {
				d.Retracted += k.N
				continue
			}
			d.CurrentByKind[k.Kind] += k.N
		}
		open := tx.Model(&GraphTodo{}).Where("owner_id = ? AND status IN ?", ownerID, []string{"open", "doing"})
		var total int64
		if err := open.Count(&total).Error; err != nil {
			return err
		}
		d.OpenTodoTotal = int(total)
		if err := open.Order("position, created_at, id").Limit(DigestTodos).Find(&d.Todos).Error; err != nil {
			return err
		}
		if err := tx.Where("owner_id = ?", ownerID).Order("updated_at DESC, id").
			Limit(DigestSubjects).Find(&d.Recent).Error; err != nil {
			return err
		}
		if turnID == "" {
			return nil
		}
		d.Unrecorded, err = graphUnrecorded(tx, threadID, turnID)
		return err
	})
	return d, err
}

// graphUnrecorded counts tool results of the turn after its last graph tool
// result. Graph tool traffic is tagged work_record_payload, so the count does
// not depend on parsing a tool name out of stored text.
func graphUnrecorded(tx *gorm.DB, threadID, turnID string) (int, error) {
	var last int64
	if err := tx.Model(&Event{}).
		Where("thread_id = ? AND turn_id = ? AND kind = ? AND work_record_payload = ?", threadID, turnID, "tool_result", true).
		Select("COALESCE(MAX(seq), 0)").Scan(&last).Error; err != nil {
		return 0, err
	}
	var n int64
	err := tx.Model(&Event{}).
		Where("thread_id = ? AND turn_id = ? AND kind = ? AND work_record_payload = ? AND seq > ?", threadID, turnID, "tool_result", false, last).
		Count(&n).Error
	return int(n), err
}

// GraphUnrecorded is the same count for callers outside a digest, such as the
// end-of-turn Trace note.
func (s *Store) GraphUnrecorded(ctx context.Context, threadID, turnID string) (int, error) {
	return graphUnrecorded(s.db.WithContext(ctx), threadID, turnID)
}
