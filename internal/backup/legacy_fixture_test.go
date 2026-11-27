package backup

import (
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// The work-record tables belong to features the application no longer has, so
// no model creates them. These statements recreate only the columns the backup
// code touches, which is what an upgraded database still carries.
var legacyWorkDDL = []string{
	`CREATE TABLE IF NOT EXISTS nuwa_nodes (id TEXT PRIMARY KEY, project_id TEXT, scope TEXT NOT NULL DEFAULT 'project', kind TEXT, title TEXT, thread_id TEXT, turn_id TEXT)`,
	`CREATE TABLE IF NOT EXISTS nuwa_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, project_id TEXT NOT NULL DEFAULT '', thread_id TEXT NOT NULL DEFAULT '', revision INTEGER, node_id TEXT, request_key TEXT, payload TEXT)`,
	`CREATE TABLE IF NOT EXISTS nuwa_verify_slots (id INTEGER PRIMARY KEY AUTOINCREMENT, project_id TEXT, thread_id TEXT, turn_id TEXT, target_id TEXT, request_key TEXT)`,
}

type legacyWork struct {
	ID, Title string
}

func createLegacyWorkTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, statement := range legacyWorkDDL {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
}

// seedLegacyWork writes one record for a conversation the way the removed
// ledger did: a node, its audit revision and a verification slot.
func seedLegacyWork(t *testing.T, st *store.Store, projectID string, thread *store.Thread, scope, title string) legacyWork {
	t.Helper()
	db := st.DB()
	createLegacyWorkTables(t, db)
	id := "legacy-" + thread.ID + "-" + title
	for _, err := range []error{
		db.Exec(`INSERT INTO nuwa_nodes (id, project_id, scope, kind, title, thread_id, turn_id) VALUES (?, ?, ?, 'fact', ?, ?, '')`, id, projectID, scope, title, thread.ID).Error,
		db.Exec(`INSERT INTO nuwa_revisions (project_id, thread_id, revision, node_id, request_key, payload) VALUES (?, ?, 1, ?, ?, ?)`, projectIDIf(scope, projectID), thread.ID, id, "key-"+id, title).Error,
		db.Exec(`INSERT INTO nuwa_verify_slots (project_id, thread_id, turn_id, target_id, request_key) VALUES (?, ?, 'turn', ?, ?)`, projectID, thread.ID, id, "slot-"+id).Error,
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return legacyWork{ID: id, Title: title}
}

func projectIDIf(scope, projectID string) string {
	if scope == "thread" {
		return ""
	}
	return projectID
}

func legacyRows(t *testing.T, st *store.Store, table, column, value string) int64 {
	t.Helper()
	var n int64
	if err := st.DB().Raw(`SELECT count(*) FROM `+table+` WHERE `+column+` = ?`, value).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}
