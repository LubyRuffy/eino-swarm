package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ClearGraph deletes one owner's subjects, links, to-dos and old batches, then
// records a newer revision so a client that already has the previous picture
// refetches the empty one. An owner that has never had a graph is left
// untouched. A later clear still advances the revision.
func (s *Store) ClearGraph(ctx context.Context, caller GraphCaller) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := graphOwnerCheck(tx, caller); err != nil {
			return err
		}
		return clearGraph(tx, caller.OwnerID(), caller.ThreadID)
	})
	if err != nil {
		return fmt.Errorf("store: clear graph: %w", err)
	}
	return nil
}

func clearGraph(tx *gorm.DB, ownerID, threadID string) error {
	var subjects, todos int64
	if err := tx.Model(&GraphSubject{}).Where("owner_id = ?", ownerID).Count(&subjects).Error; err != nil {
		return err
	}
	if err := tx.Model(&GraphTodo{}).Where("owner_id = ?", ownerID).Count(&todos).Error; err != nil {
		return err
	}
	rev, err := graphRevision(tx, ownerID)
	if err != nil {
		return err
	}
	if subjects == 0 && todos == 0 && rev == 0 {
		return nil
	}
	for _, table := range []any{&GraphLink{}, &GraphTodo{}, &GraphSubjectKey{}, &GraphSubject{}, &GraphRevision{}} {
		if err := tx.Where("owner_id = ?", ownerID).Delete(table).Error; err != nil {
			return err
		}
	}
	return tx.Create(&GraphRevision{
		OwnerID: ownerID, Revision: rev + 1, Payload: "{}", ThreadID: threadID, CreatedAt: time.Now().UTC(),
	}).Error
}
