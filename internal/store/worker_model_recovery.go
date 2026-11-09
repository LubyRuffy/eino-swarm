package store

import (
	"context"
	"fmt"

	swarm "github.com/LubyRuffy/eino-swarm"
	"gorm.io/gorm"
)

// SaveWorkerModelRecovery changes only the explicit recovery overlays. Original
// turn selections remain audit history. All owners must still match at commit.
func (s *Store) SaveWorkerModelRecovery(ctx context.Context, threadID, projectID string, changes map[string]map[string]swarm.ModelBinding) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var thread Thread
		if err := tx.First(&thread, "id = ?", threadID).Error; err != nil {
			return err
		}
		if thread.ProjectID != projectID {
			return fmt.Errorf("worker recovery project owner changed")
		}
		for tag, recovery := range changes {
			result := tx.Model(&Turn{}).Where("id = ? AND thread_id = ? AND project_id = ?", tag, threadID, projectID).
				Select("WorkerModelRecovery").Updates(&Turn{WorkerModelRecovery: recovery})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("worker recovery source is missing or changed owner")
			}
		}
		return nil
	})
}
