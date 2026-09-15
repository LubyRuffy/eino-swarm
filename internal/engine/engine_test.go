package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	// Keep the scripted swarm small and quick, but still parallel.
	cfg.Swarm.MaxConcurrent = 4
	cfg.Swarm.AgentTimeoutSeconds = 30

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	e := New(cfg, st, provider.NewMock(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(e.Shutdown)
	return e
}

// waitForTurn blocks until the turn leaves the running state.
func waitForTurn(t *testing.T, e *Engine, turnID string) *store.Turn {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		turn, err := e.Store().GetTurn(turnID)
		if err != nil {
			t.Fatalf("GetTurn: %v", err)
		}
		if turn.Status != store.TurnRunning {
			return turn
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("turn %s never finished", turnID)
	return nil
}

func TestCreateThreadMakesWorkspace(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "")
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if th.ProviderID != e.Config().Models.Default {
		t.Fatalf("provider=%q", th.ProviderID)
	}
	if info, err := os.Stat(e.WorkspaceDir(th.ID)); err != nil || !info.IsDir() {
		t.Fatalf("workspace not created: %v", err)
	}
	if _, err := e.CreateThread("x", "no-such-provider"); err == nil {
		t.Fatal("want an error for an unknown provider")
	}
}

// One full turn end to end on the scripted provider: the manager fans out two
// workers, they write files into the workspace, and the answer comes back.
func TestFullTurnRunsTheWholeSwarm(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	events := sub.C

	turn, err := e.StartTurn(th.ID, "compare the two inputs and report back")
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	if turn.Status != store.TurnRunning {
		t.Fatalf("a new turn should start running: %+v", turn)
	}

	// drain the live stream while the turn runs
	live := map[string]int{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range events {
			live[ev.Kind]++
			if ev.Kind == swarm.NotifyDone.String() || ev.Kind == swarm.NotifyError.String() {
				return
			}
		}
	}()

	finished := waitForTurn(t, e, turn.ID)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the run never published a terminal event")
	}

	if finished.Status != store.TurnDone {
		t.Fatalf("turn failed: status=%s err=%s", finished.Status, finished.Error)
	}
	if !strings.Contains(finished.Final, "compare the two inputs") {
		t.Fatalf("the answer does not reflect the request:\n%s", finished.Final)
	}
	if finished.DurationMS <= 0 {
		t.Fatalf("turn duration not recorded: %+v", finished)
	}

	// the live stream must have carried streaming text, delegation and the end
	for _, kind := range []string{
		swarm.NotifyDelta.String(), swarm.NotifyReasoningDelta.String(),
		swarm.NotifySpawned.String(), swarm.NotifyToolCall.String(),
		swarm.NotifyFinished.String(), swarm.NotifyDone.String(),
	} {
		if live[kind] == 0 {
			t.Fatalf("the live stream never carried %q; got %v", kind, live)
		}
	}
	if live[swarm.NotifySpawned.String()] != 2 {
		t.Fatalf("want two spawned workers, got %d", live[swarm.NotifySpawned.String()])
	}

	// the workers really wrote into this conversation's workspace
	files, err := e.ListFiles(th.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	var notes []string
	for _, f := range files {
		if !f.Dir && strings.HasPrefix(f.Path, "notes/") {
			notes = append(notes, f.Path)
		}
	}
	if len(notes) != 2 {
		t.Fatalf("want two worker notes in the workspace, got %v (all: %+v)", notes, files)
	}

	// model calls were recorded for tracing, attributed per agent
	calls, err := e.Store().ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) < 3 {
		t.Fatalf("want model calls from the manager and both workers, got %d", len(calls))
	}
	agents := map[string]bool{}
	for _, c := range calls {
		agents[c.AgentID] = true
	}
	if !agents[swarm.DefaultManagerID] || len(agents) < 3 {
		t.Fatalf("model calls not attributed per agent: %v", agents)
	}
}

// A reload must reproduce what the user saw. Deltas are not persisted, so the
// replay has to contain the complete reasoning and answer records instead.
func TestReplayReconstructsTheTurn(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")
	turn, err := e.StartTurn(th.ID, "summarize the material")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)

	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	kinds := map[string]int{}
	var lastSeq int64
	for _, ev := range events {
		kinds[ev.Kind]++
		if ev.Seq <= lastSeq {
			t.Fatalf("replayed events are not in increasing sequence order: %d after %d", ev.Seq, lastSeq)
		}
		lastSeq = ev.Seq
		if ev.Kind == swarm.NotifyDelta.String() || ev.Kind == swarm.NotifyReasoningDelta.String() {
			t.Fatalf("streamed deltas must not be persisted; found %+v", ev)
		}
	}
	for _, kind := range []string{
		KindUser, KindReasoning, swarm.NotifyAgentMessage.String(),
		swarm.NotifyToolCall.String(), swarm.NotifyToolResult.String(),
		swarm.NotifySpawned.String(), swarm.NotifyFinished.String(), swarm.NotifyDone.String(),
	} {
		if kinds[kind] == 0 {
			t.Fatalf("replay is missing %q; got %v", kind, kinds)
		}
	}

	// replaying from a cursor returns only the tail
	mid := events[len(events)/2].Seq
	tail, err := e.Replay(th.ID, mid)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range tail {
		if ev.Seq <= mid {
			t.Fatalf("replay from %d returned %d", mid, ev.Seq)
		}
	}
	if len(tail) == 0 {
		t.Fatal("replay from the middle returned nothing")
	}
}

// A second turn has to see the first one, or the product is a one-shot box.
func TestSecondTurnSeesTheFirst(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")

	first, err := e.StartTurn(th.ID, "remember the first request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)

	second, err := e.StartTurn(th.ID, "and now the follow-up")
	if err != nil {
		t.Fatalf("second StartTurn: %v", err)
	}
	waitForTurn(t, e, second.ID)

	history, err := e.replayHistory(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	var joined strings.Builder
	for _, m := range history {
		joined.WriteString(string(m.Role))
		joined.WriteString(": ")
		joined.WriteString(m.Content)
		joined.WriteString("\n")
		// bulky tool traffic must not be replayed into later turns
		if len(m.ToolCalls) > 0 {
			t.Fatalf("history replayed a tool call: %+v", m)
		}
		if m.Role == "tool" {
			t.Fatalf("history replayed a tool result: %+v", m)
		}
	}
	for _, want := range []string{"remember the first request", "and now the follow-up"} {
		if !strings.Contains(joined.String(), want) {
			t.Fatalf("history lost %q:\n%s", want, joined.String())
		}
	}
	if !strings.Contains(joined.String(), "assistant:") {
		t.Fatalf("history has no assistant answer to continue from:\n%s", joined.String())
	}

	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 2 {
		t.Fatalf("want 2 turns, got %d", len(turns))
	}
}

func TestStartTurnRejectsConcurrentTurns(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")

	first, err := e.StartTurn(th.ID, "the long running request")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartTurn(th.ID, "a second request"); !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy while a turn is running, got %v", err)
	}
	waitForTurn(t, e, first.ID)

	// once it is idle a new turn is fine again
	third, err := e.StartTurn(th.ID, "after it finished")
	if err != nil {
		t.Fatalf("StartTurn after the turn ended: %v", err)
	}
	waitForTurn(t, e, third.ID)
}

// The UI sends the next message as soon as it sees the terminal event, so the
// conversation has to be idle by then — not a moment later.
func TestNextTurnAcceptedImmediatelyAfterTheDoneEvent(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	events := sub.C

	if _, err := e.StartTurn(th.ID, "the first request"); err != nil {
		t.Fatal(err)
	}
	for ev := range events {
		if ev.Kind != swarm.NotifyDone.String() && ev.Kind != swarm.NotifyError.String() {
			continue
		}
		if _, err := e.StartTurn(th.ID, "the very next request"); err != nil {
			t.Fatalf("the conversation was still busy when it reported being done: %v", err)
		}
		return
	}
	t.Fatal("the run never published a terminal event")
}

func TestStartTurnValidatesInput(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")
	if _, err := e.StartTurn(th.ID, "   "); err == nil {
		t.Fatal("want an error for an empty message")
	}
	if _, err := e.StartTurn("no-such-thread", "hi"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// Steering must reach the manager mid-turn and be recorded as part of the
// conversation, not silently swallowed.
func TestSteerReachesARunningTurn(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")

	if err := e.Steer(th.ID, "too early"); !errors.Is(err, ErrIdle) {
		t.Fatalf("steering an idle conversation should report ErrIdle, got %v", err)
	}

	turn, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	var steered bool
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := e.Steer(th.ID, "focus on the second part"); err == nil {
			steered = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !steered {
		t.Fatal("could not steer the turn while it was running")
	}
	waitForTurn(t, e, turn.ID)

	events, _ := e.Replay(th.ID, 0)
	var sawSteer bool
	for _, ev := range events {
		if ev.Kind == KindSteer && strings.Contains(ev.Text, "focus on the second part") {
			sawSteer = true
		}
	}
	if !sawSteer {
		t.Fatal("the steer never appeared in the timeline")
	}
	msgs, _ := e.Store().ListMessages(th.ID)
	var steerMsgs int
	for _, m := range msgs {
		if strings.HasPrefix(m.Content, "[steer] ") {
			steerMsgs++
		}
	}
	if steerMsgs != 1 {
		t.Fatalf("want the steer stored exactly once, got %d", steerMsgs)
	}
	if err := e.Steer(th.ID, ""); err == nil {
		t.Fatal("want an error for an empty steer")
	}
	if err := e.Steer("nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// An interrupted turn keeps what it produced: the user gets a short answer,
// not a blank one.
func TestInterruptCancelsButKeepsWork(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")

	if err := e.Interrupt(th.ID); !errors.Is(err, ErrIdle) {
		t.Fatalf("interrupting an idle conversation should report ErrIdle, got %v", err)
	}

	turn, err := e.StartTurn(th.ID, "a request that will be stopped")
	if err != nil {
		t.Fatal(err)
	}
	// let the manager get going, then stop it
	time.Sleep(120 * time.Millisecond)
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnCancelled {
		t.Fatalf("want a cancelled turn, got %s (%s)", finished.Status, finished.Error)
	}

	events, _ := e.Replay(th.ID, 0)
	var sawSomething bool
	for _, ev := range events {
		switch ev.Kind {
		case KindReasoning, swarm.NotifyAgentMessage.String(), swarm.NotifyToolCall.String():
			sawSomething = true
		}
	}
	if !sawSomething {
		t.Fatal("an interrupted turn kept nothing; partial work was lost")
	}
	if !e.statusIdle(th.ID) {
		t.Fatal("the conversation is still marked running after the interrupt")
	}

	// and the conversation is usable afterwards
	next, err := e.StartTurn(th.ID, "try again")
	if err != nil {
		t.Fatalf("the conversation is unusable after an interrupt: %v", err)
	}
	waitForTurn(t, e, next.ID)
}

func (e *Engine) statusIdle(threadID string) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Status(threadID).Running {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestStatusReportsProgress(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")
	if s := e.Status(th.ID); s.Running {
		t.Fatalf("a fresh conversation is not running: %+v", s)
	}
	if s := e.Status("unknown"); s.Running {
		t.Fatalf("unknown conversation reported running: %+v", s)
	}

	turn, err := e.StartTurn(th.ID, "work on this")
	if err != nil {
		t.Fatal(err)
	}
	var sawRunning bool
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s := e.Status(th.ID)
		if s.Running {
			sawRunning = true
			if s.TurnID != turn.ID {
				t.Fatalf("status reports the wrong turn: %+v", s)
			}
			if len(e.Running()) == 0 {
				t.Fatal("Running() does not list the working conversation")
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !sawRunning {
		t.Fatal("status never reported the turn as running")
	}
	waitForTurn(t, e, turn.ID)
}

// The first message names the conversation, so the sidebar is readable.
func TestAutoTitleFromFirstMessage(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")
	turn, err := e.StartTurn(th.ID, "  look into the  reporting pipeline  ")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Title != "look into the reporting pipeline" {
		t.Fatalf("title=%q", got.Title)
	}
	waitForTurn(t, e, turn.ID)

	// a later message must not rename it
	next, err := e.StartTurn(th.ID, "a different follow-up entirely")
	if err != nil {
		t.Fatalf("second StartTurn: %v", err)
	}
	waitForTurn(t, e, next.ID)
	got, _ = e.Store().GetThread(th.ID)
	if got.Title != "look into the reporting pipeline" {
		t.Fatalf("the title was overwritten by a later turn: %q", got.Title)
	}

	// an explicit title is never overwritten
	named, _ := e.CreateThread("My Name", "")
	t2, err := e.StartTurn(named.ID, "something else")
	if err != nil {
		t.Fatalf("StartTurn on a named conversation: %v", err)
	}
	waitForTurn(t, e, t2.ID)
	got, _ = e.Store().GetThread(named.ID)
	if got.Title != "My Name" {
		t.Fatalf("an explicit title was replaced: %q", got.Title)
	}
}

func TestTitleFrom(t *testing.T) {
	if got := titleFrom("   "); got != "" {
		t.Fatalf("titleFrom=%q", got)
	}
	long := strings.Repeat("字", 100)
	got := titleFrom(long)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("long title not truncated: %q", got)
	}
	if len([]rune(got)) != titleMaxRunes+1 {
		t.Fatalf("truncated on bytes instead of runes: %d runes", len([]rune(got)))
	}
}

func TestRenameArchiveAndProvider(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")

	if err := e.RenameThread(th.ID, " Renamed "); err != nil {
		t.Fatalf("RenameThread: %v", err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Title != "Renamed" {
		t.Fatalf("title=%q", got.Title)
	}
	if err := e.SetThreadArchived(th.ID, true); err != nil {
		t.Fatalf("SetThreadArchived: %v", err)
	}
	list, _ := e.Store().ListThreads(false)
	for _, l := range list {
		if l.ID == th.ID {
			t.Fatal("an archived conversation still shows in the sidebar")
		}
	}
	if err := e.SetThreadProvider(th.ID, e.Config().Models.Default); err != nil {
		t.Fatalf("SetThreadProvider: %v", err)
	}
	if err := e.SetThreadProvider(th.ID, "nope"); err == nil {
		t.Fatal("want an error for an unknown provider")
	}
	if err := e.RenameThread("missing", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// The thinking level is a per-conversation choice, applied from the next turn
// on and recorded on the turn so a trace shows what actually ran. A blank level
// is the default; any other unknown value is rejected, the same as an unknown
// provider, so a typo does not silently run at the wrong level.
func TestThreadReasoningLevelSticksAndIsRecorded(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "")
	if err != nil {
		t.Fatal(err)
	}
	if th.ReasoningEffort != config.ReasoningDefault {
		t.Fatalf("a new conversation must start on the default level, got %q", th.ReasoningEffort)
	}
	// case-insensitive, so the UI can send whatever casing it likes
	if err := e.SetThreadReasoning(th.ID, "HIGH"); err != nil {
		t.Fatalf("SetThreadReasoning: %v", err)
	}
	if err := e.SetThreadReasoning(th.ID, "nonsense"); err == nil {
		t.Fatal("an unknown thinking level must be rejected, not silently ignored")
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.ReasoningEffort != config.ReasoningHigh {
		t.Fatalf("a rejected level must leave the previous one intact, got %q", got.ReasoningEffort)
	}
	// a blank level clears back to the model's own default
	if err := e.SetThreadReasoning(th.ID, ""); err != nil {
		t.Fatalf("clearing the level must be allowed: %v", err)
	}

	if err := e.SetThreadReasoning(th.ID, config.ReasoningHigh); err != nil {
		t.Fatalf("SetThreadReasoning: %v", err)
	}
	turn, err := e.StartTurn(th.ID, "do the work")
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.ReasoningEffort != config.ReasoningHigh {
		t.Fatalf("the turn must record the level it ran with, got %q", finished.ReasoningEffort)
	}
}

// Deleting a conversation must take its files with it: leaving workspaces
// behind silently fills the user's disk.
func TestDeleteThreadRemovesWorkspace(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")
	turn, err := e.StartTurn(th.ID, "write something down")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	ws := e.WorkspaceDir(th.ID)
	if _, err := os.Stat(ws); err != nil {
		t.Fatalf("workspace missing before delete: %v", err)
	}

	if err := e.DeleteThread(th.ID); err != nil {
		t.Fatalf("DeleteThread: %v", err)
	}
	if _, err := os.Stat(ws); !os.IsNotExist(err) {
		t.Fatalf("workspace survived the delete: %v", err)
	}
	if _, err := e.Store().GetThread(th.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("thread row survived: %v", err)
	}
	if err := e.DeleteThread("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// Deleting a conversation mid-turn has to stop it first, or the run keeps
// writing events for a conversation that no longer exists.
func TestDeleteThreadStopsARunningTurn(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")
	if _, err := e.StartTurn(th.ID, "a request in flight"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if err := e.DeleteThread(th.ID); err != nil {
		t.Fatalf("DeleteThread while running: %v", err)
	}
	if _, err := e.Store().GetThread(th.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("thread survived: %v", err)
	}
}

// ---------- workspace paths ----------

// The download endpoint is unauthenticated on loopback; a path that escapes
// the workspace would turn the app into a file server for the whole machine.
func TestResolveWorkspacePathRejectsEscapes(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")
	root := e.WorkspaceDir(th.ID)

	ok, err := e.ResolveWorkspacePath(th.ID, "notes/report.md")
	if err != nil {
		t.Fatalf("a normal relative path was rejected: %v", err)
	}
	if ok != filepath.Join(root, "notes", "report.md") {
		t.Fatalf("resolved to %q", ok)
	}

	for _, bad := range []string{
		"../../etc/passwd",
		"../" + filepath.Base(root) + "-other/x",
		"notes/../../../../../../etc/passwd",
		"",
		"   ",
		".",
	} {
		if got, err := e.ResolveWorkspacePath(th.ID, bad); err == nil {
			t.Fatalf("path %q escaped the workspace to %q", bad, got)
		}
	}

	// An absolute path is read as workspace-relative rather than rejected, so
	// a client that sends a leading slash gets its own file, never the host's.
	abs, err := e.ResolveWorkspacePath(th.ID, "/etc/passwd")
	if err != nil {
		t.Fatalf("an absolute path should be confined, not rejected: %v", err)
	}
	if abs != filepath.Join(root, "etc", "passwd") {
		t.Fatalf("absolute path resolved outside the workspace: %q", abs)
	}
}

func TestUploadAndDeleteFiles(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")

	write := func(content string) func(string) error {
		return func(dst string) error { return os.WriteFile(dst, []byte(content), 0o600) }
	}
	att, err := e.SaveUpload(th.ID, "input.csv", write("a,b\n1,2\n"))
	if err != nil {
		t.Fatalf("SaveUpload: %v", err)
	}
	if att.RelPath != "uploads/input.csv" {
		t.Fatalf("uploads should be grouped: %q", att.RelPath)
	}
	if att.Size == 0 {
		t.Fatalf("size not recorded: %+v", att)
	}

	// a second upload with the same name must not overwrite the first
	second, err := e.SaveUpload(th.ID, "input.csv", write("different"))
	if err != nil {
		t.Fatal(err)
	}
	if second.RelPath == att.RelPath {
		t.Fatalf("the second upload overwrote the first: %q", second.RelPath)
	}
	first, err := os.ReadFile(filepath.Join(e.WorkspaceDir(th.ID), filepath.FromSlash(att.RelPath)))
	if err != nil || string(first) != "a,b\n1,2\n" {
		t.Fatalf("the first upload was clobbered: %q %v", first, err)
	}

	// a path in the name must not place the file outside uploads/
	escaped, err := e.SaveUpload(th.ID, "../../evil.sh", write("x"))
	if err != nil {
		t.Fatalf("SaveUpload: %v", err)
	}
	if !strings.HasPrefix(escaped.RelPath, "uploads/") || strings.Contains(escaped.RelPath, "..") {
		t.Fatalf("an upload escaped the uploads directory: %q", escaped.RelPath)
	}

	if _, err := e.SaveUpload(th.ID, "  ", write("x")); err == nil {
		t.Fatal("want an error for an upload with no name")
	}
	if _, err := e.SaveUpload("missing", "a.txt", write("x")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	list, err := e.Store().ListAttachments(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("want 3 recorded uploads, got %d", len(list))
	}

	if err := e.DeleteFile(th.ID, att.RelPath); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.WorkspaceDir(th.ID), filepath.FromSlash(att.RelPath))); !os.IsNotExist(err) {
		t.Fatalf("the file survived the delete: %v", err)
	}
	list, _ = e.Store().ListAttachments(th.ID)
	if len(list) != 2 {
		t.Fatalf("the attachment record survived: %+v", list)
	}
	if err := e.DeleteFile(th.ID, "../outside"); err == nil {
		t.Fatal("DeleteFile must refuse a path outside the workspace")
	}
}

func TestListFilesReportsDirsAndSizes(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")
	root := e.WorkspaceDir(th.ID)
	if err := os.MkdirAll(filepath.Join(root, "notes", "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "deep", "a.md"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := e.ListFiles(th.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	byPath := map[string]FileEntry{}
	for _, f := range files {
		byPath[f.Path] = f
		if strings.Contains(f.Path, string(os.PathSeparator)) && os.PathSeparator != '/' {
			t.Fatalf("paths must be slash-separated for the UI: %q", f.Path)
		}
	}
	if !byPath["notes"].Dir || !byPath["notes/deep"].Dir {
		t.Fatalf("directories not reported: %+v", files)
	}
	f := byPath["notes/deep/a.md"]
	if f.Dir || f.Size != 5 || f.Modified.IsZero() {
		t.Fatalf("file entry wrong: %+v", f)
	}

	// listing a conversation with no workspace yet must not fail
	if _, err := e.ListFiles("never-existed"); err != nil {
		t.Fatalf("ListFiles on a fresh conversation: %v", err)
	}
}

// ---------- event bus ----------

// A stalled subscriber must not stall the run for everybody else.
func TestSlowSubscriberDoesNotBlockTheRun(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")

	slow := e.Subscribe(th.ID) // never read
	defer slow.Close()
	fastSub := e.Subscribe(th.ID)
	defer fastSub.Close()
	fast := fastSub.C

	turn, err := e.StartTurn(th.ID, "keep going despite a stalled client")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan struct{})
	go func() {
		defer close(got)
		for ev := range fast {
			if ev.Kind == swarm.NotifyDone.String() {
				return
			}
		}
	}()
	select {
	case <-got:
	case <-time.After(30 * time.Second):
		t.Fatal("a stalled subscriber blocked the run")
	}
	waitForTurn(t, e, turn.ID)
	_ = slow
}

// Dropping events for a stalled client is fine, but it has to be able to find
// out: otherwise it shows an incomplete conversation until the next reload.
func TestOverflowingASubscriberIsReported(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")

	stalled := e.Subscribe(th.ID) // never read
	defer stalled.Close()
	reading := e.Subscribe(th.ID)
	defer reading.Close()
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range reading.C {
		}
	}()

	total := subscriberBuffer + 50
	for i := 0; i < total; i++ {
		e.record(store.Event{ThreadID: th.ID, Kind: "tool_call", Text: fmt.Sprint(i)})
	}
	if !stalled.Lagged() {
		t.Fatal("a subscriber that overflowed was not told it missed stored events")
	}
	if stalled.Lagged() {
		t.Fatal("reading the flag should clear it")
	}

	// Everything it missed is in the database, in order, so catching up
	// rebuilds exactly what was dropped.
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != total {
		t.Fatalf("stored %d events, want %d", len(events), total)
	}
	for i, ev := range events {
		if ev.Seq != int64(i+1) || ev.Text != fmt.Sprint(i) {
			t.Fatalf("event %d is out of order: seq=%d text=%q", i, ev.Seq, ev.Text)
		}
	}

	reading.Close()
	<-drained
}

// Sequence numbers must reach subscribers in order, even though workers and
// the manager record events from different goroutines: a client that resumes
// from the highest sequence it saw drops anything older as a duplicate.
func TestConcurrentRecordsArriveInSequenceOrder(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	const writers, each = 8, 20
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				e.record(store.Event{ThreadID: th.ID, Kind: "tool_call",
					AgentID: fmt.Sprintf("worker-%d", w)})
			}
		}(w)
	}

	seen := make([]int64, 0, writers*each)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range sub.C {
			seen = append(seen, ev.Seq)
			if len(seen) == writers*each {
				return
			}
		}
	}()
	wg.Wait()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("only %d of %d events arrived", len(seen), writers*each)
	}
	for i, seq := range seen {
		if seq != int64(i+1) {
			t.Fatalf("event %d arrived with sequence %d; a resuming client would drop it", i, seq)
		}
	}
}

func TestUnsubscribeIsIdempotent(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")
	sub := e.Subscribe(th.ID)
	sub.Close()
	sub.Close()
	if _, open := <-sub.C; open {
		t.Fatal("the channel should be closed after unsubscribing")
	}
}

// Shutdown must not leave conversations looking like they are still working.
func TestShutdownClosesRunningTurns(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")
	turn, err := e.StartTurn(th.ID, "in flight at shutdown")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	e.Shutdown()

	got, err := e.Store().GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == store.TurnRunning {
		t.Fatalf("a turn survived shutdown as running: %+v", got)
	}
}

func TestManagerPromptIsGenericAndGrounded(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "")
	set, err := buildTestToolset(t, e, th.ID)
	if err != nil {
		t.Fatal(err)
	}
	prompt := managerPrompt(set, e.Config())

	// it must tell the agent the things only the runtime knows
	if !strings.Contains(prompt, e.WorkspaceDir(th.ID)) {
		t.Fatal("the prompt does not tell the agent where its workspace is")
	}
	for _, name := range set.Names {
		if !strings.Contains(prompt, name) {
			t.Fatalf("the prompt does not mention the %q tool the agent actually has", name)
		}
	}
	for _, tool := range []string{"spawn_agent", "send_message", "wait_agents", "close_agent"} {
		if !strings.Contains(prompt, tool) {
			t.Fatalf("the prompt does not explain %s", tool)
		}
	}
	// and it must not smuggle in a particular task
	for _, leak := range []string{"summarize", "researcher", "reviewer", "notes/"} {
		if strings.Contains(strings.ToLower(prompt), strings.ToLower(leak)) {
			t.Fatalf("the prompt hardcodes example-specific text %q", leak)
		}
	}
}

func buildTestToolset(t *testing.T, e *Engine, threadID string) (*tools.Set, error) {
	t.Helper()
	return tools.Build(context.Background(), e.Config(), e.WorkspaceDir(threadID))
}
