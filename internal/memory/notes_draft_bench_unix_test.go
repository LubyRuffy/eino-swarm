//go:build darwin || linux

package memory

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Both paths produce exactly the same notes. File identities count actual
// atomic replacements rather than assuming that a tool call wrote anything.
func TestNotesDraftReducesThreeFileReplacementsToOne(t *testing.T) {
	var final string
	for _, useDraft := range []bool{false, true} {
		s := NewLimited(t.TempDir(), 1000, 100)
		if _, err := s.Overwrite("old alpha\n\nold beta"); err != nil {
			t.Fatal(err)
		}
		observe, writes := observeNotesWrites(t, s)
		var snap Snapshot
		if useDraft {
			draft, err := s.BeginNotesDraft()
			if err != nil {
				t.Fatal(err)
			}
			runNotesReviewEdits(t, draft.Store, observe)
			if *writes != 0 {
				t.Fatalf("draft published %d intermediate writes", *writes)
			}
			var changed bool
			snap, changed, err = draft.Commit()
			if err != nil || !changed {
				t.Fatalf("Commit changed=%v err=%v", changed, err)
			}
		} else {
			snap = runNotesReviewEdits(t, s, observe)
			final = snap.Text
		}
		observe()
		want := 3
		if useDraft {
			want = 1
		}
		if *writes != want || snap.Text != final {
			t.Fatalf("draft=%v writes=%d want=%d final=%q want=%q", useDraft, *writes, want, snap.Text, final)
		}
	}
}

func BenchmarkNotesReviewWrites(b *testing.B) {
	for _, mode := range []string{"Immediate", "Draft"} {
		b.Run(mode, func(b *testing.B) {
			s := NewLimited(b.TempDir(), 6000, 500)
			b.ReportAllocs()
			var cpuTotal, writesTotal int64
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				if _, err := s.Overwrite("old alpha\n\nold beta"); err != nil {
					b.Fatal(err)
				}
				observe, writes := observeNotesWrites(b, s)
				cpuBefore := processCPUTime(b)
				b.StartTimer()
				var snap Snapshot
				if mode == "Draft" {
					draft, err := s.BeginNotesDraft()
					if err != nil {
						b.Fatal(err)
					}
					runNotesReviewEdits(b, draft.Store, observe)
					snap, _, err = draft.Commit()
					if err != nil {
						b.Fatal(err)
					}
				} else {
					snap = runNotesReviewEdits(b, s, observe)
				}
				observe()
				b.StopTimer()
				cpuTotal += processCPUTime(b) - cpuBefore
				writesTotal += int64(*writes)
				if snap.Text != "retained alpha\n\nnew policy" {
					b.Fatalf("unexpected net notes: %q", snap.Text)
				}
			}
			b.ReportMetric(float64(cpuTotal)/float64(b.N), "cpu-ns/op")
			b.ReportMetric(float64(writesTotal)/float64(b.N), "writes/op")
		})
	}
}

func runNotesReviewEdits(t testing.TB, s *Store, observe func()) Snapshot {
	t.Helper()
	if _, _, err := s.Add("new policy"); err != nil {
		t.Fatal(err)
	}
	observe()
	if _, err := s.Replace("old alpha", "retained alpha"); err != nil {
		t.Fatal(err)
	}
	observe()
	snap, _, err := s.Remove("old beta")
	if err != nil {
		t.Fatal(err)
	}
	observe()
	return snap
}

func observeNotesWrites(t testing.TB, s *Store) (func(), *int) {
	t.Helper()
	path := filepath.Join(s.Dir(), MemoryFile)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	return func() {
		after, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before, after) {
			writes++
		}
		before = after
	}, &writes
}

func processCPUTime(t testing.TB) int64 {
	t.Helper()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatal(err)
	}
	return int64(usage.Utime.Sec+usage.Stime.Sec)*1e9 + int64(usage.Utime.Usec+usage.Stime.Usec)*1e3
}
