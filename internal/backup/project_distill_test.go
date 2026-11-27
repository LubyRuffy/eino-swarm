package backup

import (
	"path/filepath"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestDistillationReceiptsFollowBackupPrivacyAndProjectScope(t *testing.T) {
	src := seed(t)
	ctx := t.Context()
	for _, p := range []*store.Project{src.project, src.other} {
		th := src.thread
		if p.ID != src.project.ID {
			th = &store.Thread{ProjectID: p.ID}
			if err := src.st.CreateThread(th); err != nil {
				t.Fatal(err)
			}
		}
		turn := store.Turn{ThreadID: th.ID, Status: store.TurnDone}
		if err := src.st.CreateTurn(&turn); err != nil {
			t.Fatal(err)
		}
		if err := src.st.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "user_message", Text: "Source for a pending review."}); err != nil {
			t.Fatal(err)
		}
		row := store.ProjectDistillation{ProjectID: p.ID, ID: "review-" + p.ID, State: "running"}
		total, err := src.st.StartDistillationQueue(ctx, row, "", func(int) ([]byte, error) { return []byte(`{"state":"running"}`), nil })
		if err != nil || total != 1 {
			t.Fatalf("could not seed the pending review: records=%d %v", total, err)
		}
	}
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		options    Options
		wantSource bool
		wantOther  bool
	}{
		{name: "full", options: Options{Kind: KindFull}, wantSource: true, wantOther: true},
		{name: "full-with-omission", options: Options{Kind: KindFull, OmitThreads: []OmittedThread{{ID: src.thread.ID}}}, wantOther: true},
		{name: "project", options: Options{Kind: KindProject, ProjectID: src.project.ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opt := tc.options
			opt.DataDir, opt.DB, opt.Output = src.dir, sqlDB, filepath.Join(t.TempDir(), "source.zip")
			if _, err := Create(ctx, opt); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if _, err := Restore(ctx, RestoreOptions{DataDir: dir, Path: opt.Output, Confirm: true, Replace: true}); err != nil {
				t.Fatal(err)
			}
			dst, err := store.Open(filepath.Join(dir, filepath.Base(src.cfg.DBPath())))
			if err != nil {
				t.Fatal(err)
			}
			defer dst.Close()
			for projectID, want := range map[string]bool{src.project.ID: tc.wantSource, src.other.ID: tc.wantOther} {
				row, err := dst.ProjectDistillationReceipt(ctx, projectID)
				if err != nil || (row != nil) != want {
					t.Fatalf("receipt for %s present=%v want %v, %v", projectID, row != nil, want, err)
				}
				window, err := dst.DistillationWindow(ctx, "review-"+projectID, 0, 20)
				if err != nil || (len(window) == 1 && window[0].Text == "Source for a pending review.") != want ||
					(!want && len(window) != 0) {
					t.Fatalf("archive separated the receipt from its source queue: %+v, %v", window, err)
				}
			}
		})
	}
}
