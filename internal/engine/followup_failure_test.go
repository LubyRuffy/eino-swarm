package engine

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type followupWarningLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *followupWarningLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *followupWarningLog) snapshot() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func followupFailureTrigger(t *testing.T, e *Engine, name, sql string) {
	t.Helper()
	if err := e.Store().DB().Exec(sql).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Store().DB().Exec("DROP TRIGGER IF EXISTS " + name).Error; err != nil {
			t.Error(err)
		}
	})
}

func assertFollowupRows(t *testing.T, e *Engine, threadID string, want ...*store.Followup) {
	t.Helper()
	rows, err := e.ListFollowups(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(want) {
		t.Fatalf("queue has %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i, f := range want {
		if rows[i].ID != f.ID || rows[i].Text != f.Text {
			t.Fatalf("queue row %d lost identity, text or order: %+v, want %+v", i, rows[i], f)
		}
	}
}

// Removing the row is the first irreversible step. A failed delete must
// neither start the text nor report success to the caller.
func TestSteerFollowupDoesNotConsumeOrStartAfterDeleteFails(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.Store().EnqueueFollowup(th.ID, "keep the first request")
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.Store().EnqueueFollowup(th.ID, "keep the next request")
	if err != nil {
		t.Fatal(err)
	}
	followupFailureTrigger(t, e, "refuse_followup_delete", `CREATE TRIGGER refuse_followup_delete BEFORE DELETE ON followups BEGIN SELECT RAISE(ABORT, 'followup delete unavailable'); END`)
	turn, err := e.SteerFollowup(th.ID, first.ID)
	if turn != nil || err == nil || !strings.Contains(err.Error(), "followup delete unavailable") {
		t.Fatalf("failed deletion became a successful send: turn=%+v err=%v", turn, err)
	}
	assertFollowupRows(t, e, th.ID, first, second)
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil || len(turns) != 0 || e.Status(th.ID).Running {
		t.Fatalf("failed deletion started work: turns=%+v err=%v", turns, err)
	}
}

// A real running swarm cannot accept steering when its event write fails.
// The clicked row returns to the front and the original turn remains live.
func TestSteerFollowupRestoresTheRequestAfterLiveSteerStorageFails(t *testing.T) {
	t.Setenv("ZWAI_MOCK_WORKER_DELAY_MS", "1200")
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "keep the current task running")
	if err != nil {
		t.Fatal(err)
	}
	first := queueWhileRunning(t, e, th.ID, "the selected request")
	second := queueWhileRunning(t, e, th.ID, "the next request")
	followupFailureTrigger(t, e, "refuse_followup_steer", `CREATE TRIGGER refuse_followup_steer BEFORE INSERT ON events WHEN NEW.kind = 'steer' BEGIN SELECT RAISE(ABORT, 'steer event unavailable'); END`)
	started, err := e.SteerFollowup(th.ID, first.ID)
	if started != nil || err == nil || !strings.Contains(err.Error(), "steer event unavailable") {
		t.Fatalf("failed live steering started another turn: turn=%+v err=%v", started, err)
	}
	assertFollowupRows(t, e, th.ID, first, second)
	st := e.Status(th.ID)
	if !st.Running || st.TurnID != turn.ID {
		t.Fatalf("failed steering changed the live turn: %+v", st)
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindSteer && ev.Text == first.Text {
			t.Fatalf("rejected steering was reported as persisted: %+v", ev)
		}
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnCancelled {
		t.Fatalf("stop did not cancel the original turn: %+v", got)
	}
	assertFollowupRows(t, e, th.ID, first, second)
}

func TestSteerFollowupRestoresTheRequestWhenIdleStartCannotResolveItsProvider(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.Store().EnqueueFollowup(th.ID, "retry this request")
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.Store().EnqueueFollowup(th.ID, "the next request")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{"provider_id": "provider-that-was-removed"}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.SteerFollowup(th.ID, first.ID)
	if turn != nil || err == nil || !strings.Contains(err.Error(), "provider-that-was-removed") {
		t.Fatalf("unusable provider was accepted: turn=%+v err=%v", turn, err)
	}
	assertFollowupRows(t, e, th.ID, first, second)
	if e.Status(th.ID).Running {
		t.Fatal("a failed provider resolution occupied a live slot")
	}
}

// Failure to restore must be observable without masking the failure that
// caused it, or logging the user's queued message content.
func TestSteerFollowupWarnsWhenFailedLiveSteeringCannotRestoreTheRow(t *testing.T) {
	t.Setenv("ZWAI_MOCK_WORKER_DELAY_MS", "1200")
	e := newTestEngine(t)
	logs := &followupWarningLog{}
	e.log = slog.New(slog.NewJSONHandler(logs, nil))
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "keep the current task running")
	if err != nil {
		t.Fatal(err)
	}
	first := queueWhileRunning(t, e, th.ID, "private queued request content")
	second := queueWhileRunning(t, e, th.ID, "leave this next request")
	followupFailureTrigger(t, e, "refuse_live_steer", `CREATE TRIGGER refuse_live_steer BEFORE INSERT ON events WHEN NEW.kind = 'steer' BEGIN SELECT RAISE(ABORT, 'original steer failure'); END`)
	followupFailureTrigger(t, e, "refuse_followup_restore", `CREATE TRIGGER refuse_followup_restore BEFORE INSERT ON followups BEGIN SELECT RAISE(ABORT, 'followup restoration failure'); END`)
	started, err := e.SteerFollowup(th.ID, first.ID)
	if started != nil || err == nil || !strings.Contains(err.Error(), "original steer failure") || strings.Contains(err.Error(), "restoration failure") {
		t.Fatalf("restore masked the original error: turn=%+v err=%v", started, err)
	}
	assertFollowupRows(t, e, th.ID, second)
	got := logs.snapshot()
	for _, want := range []string{"WARN", "could not restore a follow-up after a failed steer", th.ID, first.ID, "followup restoration failure"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing recovery warning %q in %s", want, got)
		}
	}
	if strings.Contains(got, first.Text) {
		t.Fatal("recovery warning leaked the queued message content")
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnCancelled {
		t.Fatalf("failed steering did not retain the original turn: %+v", got)
	}
}
