package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestEmptyReportArchivesAQuietTurn(t *testing.T) {
	provider.SetMockScheduleQuiet(true)
	t.Cleanup(func() { provider.SetMockScheduleQuiet(false) })

	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	th, _ := e.CreateThread("", "", "")
	sch := armDueWake(t, e, clk, th.ID, 60)
	e.fireDueSchedules()

	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("turns=%d", len(turns))
	}
	waitForTurn(t, e, turns[0].ID)
	run := waitForScheduleRun(t, e, turns[0].ScheduleRunID)

	findings, quiet := reportPayload(t, e, th.ID)
	if findings != "" || !quiet {
		t.Fatalf("report findings=%q quiet=%v", findings, quiet)
	}
	gotTurn, err := e.Store().GetTurn(turns[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !gotTurn.Quiet {
		t.Fatal("empty report_schedule must stamp turn.quiet")
	}
	if run.Status != store.ScheduleRunQuiet || run.Unread || run.Summary != "" {
		t.Fatalf("run=%+v, empty findings archive quiet/unread=false", run)
	}
	gotTh, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotTh.Archived {
		t.Fatal("a thread wake must not archive the conversation")
	}
	running, err := e.Store().HasRunningRun(sch.ID)
	if err != nil || running {
		t.Fatalf("HasRunningRun=%v err=%v", running, err)
	}
}

func TestOmittedReportWithAnswerIsFindings(t *testing.T) {
	provider.SetMockScheduleSpawn(true)
	t.Cleanup(func() { provider.SetMockScheduleSpawn(false) })

	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	th, _ := e.CreateThread("", "", "")
	sch := armDueWake(t, e, clk, th.ID, 60)
	e.fireDueSchedules()

	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("turns=%d", len(turns))
	}
	waitForTurn(t, e, turns[0].ID)
	run := waitForScheduleRun(t, e, turns[0].ScheduleRunID)

	if hasKind(t, e, th.ID, KindScheduleReport) {
		t.Fatal("spawn mock must not call report_schedule")
	}
	gotTurn, err := e.Store().GetTurn(turns[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotTurn.Quiet {
		t.Fatal("an answer without a report is findings, not quiet")
	}
	if strings.TrimSpace(gotTurn.Final) == "" {
		t.Fatal("spawn mock must persist an answer")
	}
	if run.Status != store.ScheduleRunFindings || !run.Unread {
		t.Fatalf("run=%+v, omitted report with an answer is findings/unread", run)
	}
	if run.Summary != strings.TrimSpace(gotTurn.Final) {
		t.Fatalf("summary=%q want the persisted answer", run.Summary)
	}
	for _, w := range []string{"CI", "deploy", "GitHub"} {
		if strings.Contains(run.Summary, w) {
			t.Fatalf("leaked %q", w)
		}
	}
	running, err := e.Store().HasRunningRun(sch.ID)
	if err != nil || running {
		t.Fatalf("HasRunningRun=%v err=%v", running, err)
	}

	e.finishScheduledRun(gotTurn, store.TurnDone, gotTurn.Final, "")
	again, err := e.Store().GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != store.ScheduleRunFindings || again.Summary != run.Summary {
		t.Fatalf("a second finish must not overwrite: %+v", again)
	}
	e.finishScheduledRun(nil, store.TurnDone, "x", "")
	e.finishScheduledRun(&store.Turn{ScheduleContinue: true}, store.TurnDone, "x", "")
}

func TestOmittedReportWithNoAnswerIsQuiet(t *testing.T) {
	provider.SetMockScheduleSilent(true)
	t.Cleanup(func() { provider.SetMockScheduleSilent(false) })

	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	th, _ := e.CreateThread("", "", "")
	_ = armDueWake(t, e, clk, th.ID, 60)
	e.fireDueSchedules()

	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("turns=%d", len(turns))
	}
	waitForTurn(t, e, turns[0].ID)
	run := waitForScheduleRun(t, e, turns[0].ScheduleRunID)

	if hasKind(t, e, th.ID, KindScheduleReport) {
		t.Fatal("silent mock must omit report_schedule")
	}
	gotTurn, err := e.Store().GetTurn(turns[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !gotTurn.Quiet {
		t.Fatal("no answer and no report is quiet")
	}
	if strings.TrimSpace(gotTurn.Final) != "" {
		t.Fatalf("silent final=%q", gotTurn.Final)
	}
	if run.Status != store.ScheduleRunQuiet || run.Unread || run.Summary != "" {
		t.Fatalf("run=%+v", run)
	}
}

func TestStandaloneQuietFireIsArchived(t *testing.T) {
	provider.SetMockScheduleQuiet(true)
	t.Cleanup(func() { provider.SetMockScheduleQuiet(false) })

	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	origin, _ := e.CreateThread("origin", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: origin.ID,
		Title: "nightly check", Prompt: scheduleWaitPrompt, EveryS: 60,
		ProviderID: origin.ProviderID, CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": clk.Now()}); err != nil {
		t.Fatal(err)
	}
	e.fireDueSchedules()

	minted := mintedThreadExcept(t, e, origin.ID)
	turns, err := e.Store().ListTurns(minted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("minted turns=%d", len(turns))
	}
	waitForTurn(t, e, turns[0].ID)
	run := waitForScheduleRun(t, e, turns[0].ScheduleRunID)
	if run.Status != store.ScheduleRunQuiet {
		t.Fatalf("run=%+v", run)
	}
	if run.ScheduleID != sch.ID {
		t.Fatalf("run schedule=%q want %q", run.ScheduleID, sch.ID)
	}
	waitStandaloneArchived(t, e, minted.ID)

	visible, err := e.Store().ListThreads(false, "")
	if err != nil {
		t.Fatal(err)
	}
	if threadListed(visible, minted.ID) {
		t.Fatal("quiet standalone must leave Recents")
	}
	if !threadListed(visible, origin.ID) {
		t.Fatal("origin conversation must stay in Recents")
	}
	all, err := e.Store().ListThreads(true, "")
	if err != nil {
		t.Fatal(err)
	}
	if !threadListed(all, minted.ID) {
		t.Fatal("the minted row still exists for Trace")
	}
	_ = sch
}

func TestStandaloneFindingsStayInTheSidebar(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	origin, _ := e.CreateThread("origin", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: origin.ID,
		Title: "nightly check", Prompt: scheduleWaitPrompt, EveryS: 60,
		ProviderID: origin.ProviderID, CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": clk.Now()}); err != nil {
		t.Fatal(err)
	}
	e.fireDueSchedules()

	minted := mintedThreadExcept(t, e, origin.ID)
	if minted.Archived {
		t.Fatal("a just-fired standalone stays visible until the check finishes")
	}
	turns, err := e.Store().ListTurns(minted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("minted turns=%d", len(turns))
	}
	waitForTurn(t, e, turns[0].ID)
	run := waitForScheduleRun(t, e, turns[0].ScheduleRunID)
	if run.Status != store.ScheduleRunFindings {
		t.Fatalf("run=%+v", run)
	}

	got, err := e.Store().GetThread(minted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Archived {
		t.Fatal("findings must stay in Recents")
	}
	visible, err := e.Store().ListThreads(false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !threadListed(visible, minted.ID) {
		t.Fatal("findings standalone missing from the sidebar")
	}
}

func TestScheduleTaskRejectedOnScheduledTurn(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	th, _ := e.CreateThread("", "", "")
	_ = armDueWake(t, e, clk, th.ID, 60)
	e.fireDueSchedules()

	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || !turns[0].ScheduleContinue {
		t.Fatalf("turns=%+v", turns)
	}
	out, err := e.scheduleTaskJSON(th.ID, turns[0].ID,
		`{"prompt":"`+scheduleWaitPrompt+`","every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("schedule_task on a scheduled check: %s %v", out, err)
	}
	listed, err := e.ListSchedules()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range listed {
		if row.Kind == store.ScheduleStandalone {
			t.Fatalf("rejected schedule_task still persisted: %+v", row)
		}
	}
	waitForTurn(t, e, turns[0].ID)
	_ = waitForScheduleRun(t, e, turns[0].ScheduleRunID)
}

func TestScheduledTurnErrorMarksTheRun(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		provider.SetMockFailure(errors.New("chat model refused the request"))
		t.Cleanup(func() { provider.SetMockFailure(nil) })

		e := newTestEngine(t)
		clk := newScheduleClock()
		e.now = clk.Now
		th, _ := e.CreateThread("", "", "")
		sch := armDueWake(t, e, clk, th.ID, 60)
		e.fireDueSchedules()
		turns, err := e.Store().ListTurns(th.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(turns) != 1 {
			t.Fatalf("turns=%d", len(turns))
		}
		got := waitForTurn(t, e, turns[0].ID)
		if got.Status != store.TurnError {
			t.Fatalf("status=%s err=%s", got.Status, got.Error)
		}
		assertScheduledRunClosedError(t, e, sch.ID, turns[0].ScheduleRunID)
	})

	t.Run("cancelled", func(t *testing.T) {
		provider.SetMockAskUser(true)
		t.Cleanup(func() { provider.SetMockAskUser(false) })

		e := newTestEngine(t)
		clk := newScheduleClock()
		e.now = clk.Now
		th, _ := e.CreateThread("", "", "")
		sch := armDueWake(t, e, clk, th.ID, 60)
		e.fireDueSchedules()
		waitUntilRunning(t, e, th.ID)
		if err := e.Interrupt(th.ID); err != nil {
			t.Fatal(err)
		}
		turns, err := e.Store().ListTurns(th.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(turns) != 1 {
			t.Fatalf("turns=%d", len(turns))
		}
		got := waitForTurn(t, e, turns[0].ID)
		if got.Status != store.TurnCancelled {
			t.Fatalf("status=%s", got.Status)
		}
		assertScheduledRunClosedError(t, e, sch.ID, turns[0].ScheduleRunID)
	})
}

func TestResumeCannotRestartClosesTheScheduleRun(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := &store.ScheduleRun{
		ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning,
	}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	// No request and no transcript: resume cannot continue, same as
	// TestResumeOrphanedTurnsClosesATurnItCannotRestart. The bound fire
	// must not stay running or HasRunningRun never clears.
	turn := plantScheduledLeftover(t, e, th, run.ID, "")

	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatalf("ResumeOrphanedTurns: %v", err)
	}
	if n != 0 {
		t.Fatalf("claimed to resume an unresumable scheduled turn: %d", n)
	}
	gotTurn, err := e.Store().GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotTurn.Status == store.TurnRunning {
		t.Fatalf("an unresumable leftover is still marked running: %+v", gotTurn)
	}
	got, err := e.Store().GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.ScheduleRunError || !got.Unread {
		t.Fatalf("run=%+v, a leftover that cannot restart must close error/unread", got)
	}
	running, err := e.Store().HasRunningRun(sch.ID)
	if err != nil || running {
		t.Fatalf("HasRunningRun=%v err=%v, the claim must not stick", running, err)
	}
}

func TestResumeDropsASupersededScheduledRun(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	olderRun := &store.ScheduleRun{
		ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning,
	}
	if err := e.Store().CreateRun(olderRun); err != nil {
		t.Fatal(err)
	}
	newerRun := &store.ScheduleRun{
		ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning,
	}
	if err := e.Store().CreateRun(newerRun); err != nil {
		t.Fatal(err)
	}
	text := ScheduleContinueText(scheduleWaitPrompt)
	older := plantScheduledLeftover(t, e, th, olderRun.ID, text)
	newer := plantScheduledLeftover(t, e, th, newerRun.ID, text)
	if older.Seq >= newer.Seq {
		t.Fatalf("seq older=%d newer=%d", older.Seq, newer.Seq)
	}

	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatalf("ResumeOrphanedTurns: %v", err)
	}
	if n != 1 {
		t.Fatalf("resumed %d, want 1", n)
	}

	dropped, err := e.Store().GetTurn(older.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dropped.Status == store.TurnRunning {
		t.Fatalf("superseded leftover still running: %+v", dropped)
	}
	gotOlder, err := e.Store().GetRun(olderRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotOlder.Status != store.ScheduleRunError {
		t.Fatalf("dropped run=%+v, superseded leftover must close as error", gotOlder)
	}

	waitForTurn(t, e, newer.ID)
	gotNewer, err := e.Store().GetRun(newerRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotNewer.Status == store.ScheduleRunRunning {
		t.Fatalf("surviving run still running after the turn closed: %+v", gotNewer)
	}
}

func plantScheduledLeftover(t *testing.T, e *Engine, th *store.Thread, runID, text string) *store.Turn {
	t.Helper()
	turn := &store.Turn{
		ThreadID:         th.ID,
		UserText:         text,
		ProviderID:       th.ProviderID,
		Model:            th.Model,
		ScheduleContinue: true,
		ScheduleRunID:    runID,
	}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(text) != "" {
		if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
			{Role: "user", Content: text},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return turn
}

func assertScheduledRunClosedError(t *testing.T, e *Engine, scheduleID, runID string) {
	t.Helper()
	run := waitForScheduleRun(t, e, runID)
	if run.Status != store.ScheduleRunError || !run.Unread {
		t.Fatalf("run=%+v, a crashed or cancelled check must be error/unread", run)
	}
	running, err := e.Store().HasRunningRun(scheduleID)
	if err != nil || running {
		t.Fatalf("HasRunningRun=%v err=%v, the claim must not stick", running, err)
	}
	got, err := e.Store().GetSchedule(scheduleID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.ScheduleActive {
		t.Fatalf("status=%q, a failed check must not cancel the wait", got.Status)
	}
}

func waitForScheduleRun(t *testing.T, e *Engine, runID string) *store.ScheduleRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last store.ScheduleRun
	for time.Now().Before(deadline) {
		run, err := e.Store().GetRun(runID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		last = *run
		if run.Status != store.ScheduleRunRunning {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s stuck status=%q", runID, last.Status)
	return nil
}

func waitStandaloneArchived(t *testing.T, e *Engine, threadID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		th, err := e.Store().GetThread(threadID)
		if err != nil {
			t.Fatal(err)
		}
		if th.Archived {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("standalone quiet fire was not archived")
}

func mintedThreadExcept(t *testing.T, e *Engine, originID string) *store.Thread {
	t.Helper()
	threads, err := e.Store().ListThreads(true, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := range threads {
		if threads[i].ID != originID {
			return &threads[i]
		}
	}
	t.Fatal("standalone fire must mint a conversation")
	return nil
}

func threadListed(threads []store.Thread, id string) bool {
	for i := range threads {
		if threads[i].ID == id {
			return true
		}
	}
	return false
}

func reportPayload(t *testing.T, e *Engine, threadID string) (findings string, quiet bool) {
	t.Helper()
	ev := eventOfKind(t, e, threadID, KindScheduleReport)
	var payload struct {
		Findings string `json:"findings"`
		Quiet    bool   `json:"quiet"`
	}
	if err := json.Unmarshal([]byte(ev.Text), &payload); err != nil {
		t.Fatalf("schedule_report payload=%q err=%v", ev.Text, err)
	}
	return payload.Findings, payload.Quiet
}
