package store

import (
	"path/filepath"
	"testing"
)

func TestRetryLineageMigrationReadsLegacyHistoryAndKeepsNewTurnsUnmarked(t *testing.T) {
	for _, tc := range []struct {
		name          string
		legacyLineage bool
	}{
		{name: "existing lineage column", legacyLineage: true},
		{name: "database before lineage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "turns.db")
			open := func() *Store {
				t.Helper()
				s, err := Open(path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = s.Close() })
				return s
			}
			s := open()
			withThreads(t, s, "thread")
			original := &Turn{ThreadID: "thread", Status: TurnError}
			legacy := &Turn{ThreadID: "thread", Status: TurnError}
			for _, turn := range []*Turn{original, legacy} {
				if err := s.CreateTurn(turn); err != nil {
					t.Fatal(err)
				}
			}
			want := ""
			if tc.legacyLineage {
				want = original.ID
				if err := s.DB().Exec("UPDATE turns SET retry_turn_id = ? WHERE id = ?", want, legacy.ID).Error; err != nil {
					t.Fatal(err)
				}
			} else if err := s.DB().Exec("ALTER TABLE turns DROP COLUMN retry_turn_id").Error; err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}

			s = open()
			if !s.DB().Migrator().HasColumn("turns", "retry_turn_id") {
				t.Fatal("migration did not make the nullable lineage column readable")
			}
			// New callers must not be able to assign lineage to a fresh turn,
			// or replace the source of one already recorded by the old client.
			fresh := &Turn{ThreadID: "thread", RetryTurnID: legacy.ID}
			if err := s.CreateTurn(fresh); err != nil {
				t.Fatal(err)
			}
			if err := s.UpdateTurn(legacy.ID, map[string]any{"retry_turn_id": fresh.ID, "final": "updated"}); err != nil {
				t.Fatal(err)
			}
			turns, err := s.ListTurns("thread")
			if err != nil || len(turns) != 3 {
				t.Fatalf("list turns: %+v, %v", turns, err)
			}
			if turns[0].RetryTurnID != "" || turns[1].RetryTurnID != want || turns[2].RetryTurnID != "" {
				t.Fatalf("retry lineage: got [%q %q %q], want [empty %q empty]",
					turns[0].RetryTurnID, turns[1].RetryTurnID, turns[2].RetryTurnID, want)
			}
			if turns[1].Final != "updated" {
				t.Fatal("normal turn update did not persist")
			}
		})
	}
}
