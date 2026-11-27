package store

import (
	"gorm.io/gorm"
)

// Work records written before the graph live in tables the application no
// longer creates, reads or shows. Existing databases keep them on disk, but
// deleting a conversation or project must still remove what it owns, so these
// helpers address the old tables by name and tolerate their absence.
var legacyThreadTables = []struct{ table, column string }{
	{"nuwa_revisions", "thread_id"},
	{"nuwa_nodes", "thread_id"},
	{"nuwa_verify_slots", "thread_id"},
}

func deleteLegacyThreadRows(tx *gorm.DB, threadID string) error {
	return deleteLegacyRows(tx, "", []string{threadID})
}

func deleteLegacyProjectRows(tx *gorm.DB, projectID string, threadIDs []string) error {
	return deleteLegacyRows(tx, projectID, threadIDs)
}

func deleteLegacyRows(tx *gorm.DB, projectID string, threadIDs []string) error {
	for _, t := range legacyThreadTables {
		if !tx.Migrator().HasTable(t.table) {
			continue
		}
		var err error
		switch {
		case len(threadIDs) > 0 && projectID != "":
			err = tx.Exec("DELETE FROM "+t.table+" WHERE "+t.column+" IN ? OR project_id = ?", threadIDs, projectID).Error
		case len(threadIDs) > 0:
			err = tx.Exec("DELETE FROM "+t.table+" WHERE "+t.column+" IN ?", threadIDs).Error
		case projectID != "":
			err = tx.Exec("DELETE FROM "+t.table+" WHERE project_id = ?", projectID).Error
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// The wiki scan acknowledgement table is no longer written. Its triggers on
// events would keep paying for every event edit, so existing databases drop them.
func dropWikiSourceScanTriggers(db *gorm.DB) error {
	for _, name := range []string{"wiki_source_scan_deleted", "wiki_source_scan_changed"} {
		if err := db.Exec("DROP TRIGGER IF EXISTS " + name).Error; err != nil {
			return err
		}
	}
	return nil
}

// Tables written by the removed knowledge graph build, keyed by project only.
var legacyProjectTables = []string{"project_wiki_builds", "wiki_source_scans"}

func deleteLegacyProjectTables(tx *gorm.DB, projectID string) error {
	for _, table := range legacyProjectTables {
		if !tx.Migrator().HasTable(table) {
			continue
		}
		if err := tx.Exec("DELETE FROM "+table+" WHERE project_id = ?", projectID).Error; err != nil {
			return err
		}
	}
	return nil
}
