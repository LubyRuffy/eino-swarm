package store

import "gorm.io/gorm"

// deleteGraphOwners removes everything the given owners (projects or
// conversations) hold in the graph tables. Deleting a conversation or project
// must not leave its findings behind.
func deleteGraphOwners(tx *gorm.DB, ownerIDs []string) error {
	if len(ownerIDs) == 0 {
		return nil
	}
	for _, m := range []any{&GraphSubject{}, &GraphSubjectKey{}, &GraphLink{}, &GraphTodo{}, &GraphRevision{}} {
		if err := tx.Where("owner_id IN ?", ownerIDs).Delete(m).Error; err != nil {
			return err
		}
	}
	return nil
}
