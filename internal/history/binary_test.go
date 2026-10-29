package history

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

// Real-model candidate acceptance found this in a normal executable while
// race-instrumented tests stayed green. Keep the ordinary build boundary and
// the observed SQL shapes, with private synthetic source rows and identities.
func TestNormallyBuiltHistoryReaderPreservesEmptyTypedValues(t *testing.T) {
	path, db := historyFixture(t)
	for _, statement := range []string{
		`ALTER TABLE threads ADD COLUMN archived BOOLEAN DEFAULT 0`,
		`ALTER TABLE threads ADD COLUMN pinned BOOLEAN DEFAULT 0`,
		`ALTER TABLE threads ADD COLUMN created_at TEXT DEFAULT ''`,
		`ALTER TABLE threads ADD COLUMN last_active_at TEXT DEFAULT ''`,
		`ALTER TABLE threads ADD COLUMN goal TEXT DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN source TEXT DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN agent_id TEXT DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN created_at TEXT DEFAULT ''`,
		`INSERT INTO messages(id,thread_id,seq,role,content) VALUES(3,'first',3,'user','before'||char(0)||'after')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(t.TempDir(), "history-query-probe")
	build := exec.CommandContext(t.Context(), "go", "build", "-race=false", "-o", binary, "./testdata/queryprobe")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("normal history build: %v\n%s", err, out)
	}
	cases := []struct {
		name, sql string
		params    []any
		check     func(*testing.T, Result)
	}{
		{"empty-text-blob-null", `SELECT typeof(''),'',typeof(x''),x'',typeof(NULL),NULL,'before'||char(0)||'after'`, nil, func(t *testing.T, got Result) {
			if len(got.Rows) != 1 {
				t.Fatalf("typed rows=%#v", got)
			}
			row := got.Rows[0]
			if row[0] != "text" || row[1] != "" || row[2] != "blob" || row[3] != "" || row[4] != "null" || row[5] != nil || row[6] != "before\x00after" {
				t.Fatalf("typed source changed: %#v", row)
			}
		}},
		{"candidate-thread-query", `SELECT id, title, archived, pinned, created_at, last_active_at, goal FROM threads WHERE project_id = ? ORDER BY last_active_at DESC`, []any{"project"}, func(t *testing.T, got Result) {
			if len(got.Rows) != 1 || got.Rows[0][6] != "" {
				t.Fatalf("empty goal changed: %#v", got)
			}
		}},
		{"candidate-transcript-query", `SELECT m.thread_id, t.title, m.id, m.seq, m.role, m.source, m.agent_id, m.content, m.created_at FROM messages m JOIN threads t ON t.id = m.thread_id WHERE t.project_id = ? AND t.id <> ? ORDER BY m.thread_id, m.seq`, []any{"project", "excluded"}, func(t *testing.T, got Result) {
			if len(got.Rows) != 3 || got.Rows[2][5] != "" || got.Rows[2][6] != "" || got.Rows[2][7] != "before\x00after" {
				t.Fatalf("transcript bytes changed: %#v", got)
			}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request, err := json.Marshal(map[string]any{"sql": test.sql, "params": test.params})
			if err != nil {
				t.Fatal(err)
			}
			out, err := exec.CommandContext(t.Context(), binary, path, string(request)).CombinedOutput()
			if err != nil {
				t.Fatalf("normal history execution: %v\n%s", err, out)
			}
			var got Result
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("result JSON: %v\n%s", err, out)
			}
			test.check(t, got)
		})
	}
}
