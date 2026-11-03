package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyTurnProjectOwnershipIsBackfilledOnce(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dropColumn bool
	}{
		{name: "database before project snapshots", dropColumn: true},
		{name: "old writer after column was added"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "turn-owners.db")
			openStore := func() *Store {
				t.Helper()
				s, err := Open(path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = s.Close() })
				return s
			}
			s := openStore()
			projectA := &Project{Name: "First"}
			projectB := &Project{Name: "Second"}
			for _, project := range []*Project{projectA, projectB} {
				if err := s.CreateProject(project); err != nil {
					t.Fatal(err)
				}
			}
			projectThread := &Thread{ProjectID: projectB.ID}
			standalone := &Thread{}
			legacyStandalone := &Thread{}
			for _, thread := range []*Thread{projectThread, standalone, legacyStandalone} {
				if err := s.CreateThread(thread); err != nil {
					t.Fatal(err)
				}
			}
			want := map[string]string{
				"legacy-project-turn":    projectA.ID,
				"legacy-standalone-turn": "",
			}
			if tc.dropColumn {
				if err := s.DB().Exec("ALTER TABLE turns DROP COLUMN project_id").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				// The Nuwa-era writer includes the column, even for an empty
				// snapshot. Moves before upgrade must preserve those snapshots.
				for _, turn := range []*Turn{
					{ThreadID: projectThread.ID, ProjectID: projectThread.ProjectID},
					{ThreadID: standalone.ID},
				} {
					if err := s.CreateTurn(turn); err != nil {
						t.Fatal(err)
					}
					want[turn.ID] = turn.ProjectID
				}
				if err := s.SetThreadProject(standalone.ID, projectA.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.SetThreadProject(projectThread.ID, projectA.ID); err != nil {
				t.Fatal(err)
			}
			// The legacy insert never mentioned project_id. Standalone
			// history also needs a concrete empty snapshot after migration.
			for id, thread := range map[string]*Thread{
				"legacy-project-turn":    projectThread,
				"legacy-standalone-turn": legacyStandalone,
			} {
				if err := s.DB().Exec("INSERT INTO turns (id, thread_id, status) VALUES (?, ?, ?)", id, thread.ID, TurnRunning).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openStore()
			assertSnapshots := func() {
				t.Helper()
				for id, project := range want {
					var got sql.NullString
					err := s.DB().Raw("SELECT project_id FROM turns WHERE id = ?", id).Row().Scan(&got)
					if err != nil || !got.Valid || got.String != project {
						t.Errorf("turn %s project snapshot = %+v, err=%v; want %q", id, got, err, project)
					}
				}
			}
			assertSnapshots()
			for _, thread := range []*Thread{projectThread, standalone, legacyStandalone} {
				if err := s.SetThreadProject(thread.ID, projectB.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openStore()
			assertSnapshots()
		})
	}
}

func TestTurnProjectOwnershipBackfillCanRetryAfterFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turn-owners.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// The second write fails after the first row was updated inside the
	// statement. Neither row can become authoritative on a failed startup.
	for _, stmt := range []string{
		`INSERT INTO turns (id, thread_id) VALUES ('legacy-first', ''), ('legacy-second', '')`,
		`CREATE TRIGGER reject_project_backfill BEFORE UPDATE OF project_id ON turns
		WHEN EXISTS (SELECT 1 FROM turns WHERE project_id IS NOT NULL)
		BEGIN SELECT RAISE(ABORT, 'owner backfill refused'); END`,
	} {
		if err := s.DB().Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if opened, err := Open(path); err == nil {
		_ = opened.Close()
		t.Fatal("opened a store with an interrupted turn owner backfill")
	} else if !strings.Contains(err.Error(), "store: backfill turn project owners") || !strings.Contains(err.Error(), "owner backfill refused") {
		t.Fatalf("unexpected migration failure: %v", err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var remaining int
	if err := raw.QueryRow("SELECT COUNT(*) FROM turns WHERE project_id IS NULL").Scan(&remaining); err != nil || remaining != 2 {
		t.Fatalf("rollback left %d legacy rows, err=%v; want 2", remaining, err)
	}
	if _, err := raw.Exec("DROP TRIGGER reject_project_backfill"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := raw.QueryRow("SELECT COUNT(*) FROM turns WHERE project_id = ''").Scan(&remaining); err != nil || remaining != 2 {
		t.Fatalf("retry preserved %d snapshots, err=%v; want 2", remaining, err)
	}
}
