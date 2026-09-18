# Scheduled Tasks Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Codex-style scheduled tasks: manager-armed thread wakes (no human prompt required), human-gated standalone jobs that mint a conversation per fire, a Scheduled inbox, quiet runs hidden from the transcript, and `/goal` auto-continue yielding to a pending wake.

**Architecture:** SQLite `schedules` / `schedule_runs`, an injectable clock + ticker on `Engine`, manager-only tools, REST inbox. App must be running. Spec: `docs/plans/2026-09-18-scheduled-tasks-design.md`.

**Tech Stack:** Go + gorm/sqlite + gin, React/TS + shadcn, scripted mock provider, Playwright.

TDD. Fake clock. No example-task words in prompts, tool Info, defaults, or assertions. Files stay under 1000 lines. Docs in the same change as the code they describe.

---

### Task 1: Config keys

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `docs/CONFIG.md`

**Step 1: Write the failing test**

Add to `config_test.go`:

```go
func TestSwarmScheduleDefaultsRepairZero(t *testing.T) {
	cfg := Defaults("")
	if cfg.Swarm.ScheduleMinInterval() != DefaultScheduleMinIntervalSeconds*time.Second {
		t.Fatalf("min interval=%s", cfg.Swarm.ScheduleMinInterval())
	}
	if cfg.Swarm.ScheduleTick() != time.Duration(DefaultScheduleTickMS)*time.Millisecond {
		t.Fatalf("tick=%s", cfg.Swarm.ScheduleTick())
	}
	if cfg.Swarm.ScheduleMaxActive() != DefaultScheduleMaxActive {
		t.Fatalf("max active=%d", cfg.Swarm.ScheduleMaxActive())
	}
	zero := SwarmConfig{}
	if zero.ScheduleMinInterval() <= 0 || zero.ScheduleTick() <= 0 || zero.ScheduleMaxActive() <= 0 {
		t.Fatal("zero values must repair")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -race ./internal/config/ -run TestSwarmScheduleDefaultsRepairZero -count=1`

Expected: FAIL (undefined names).

**Step 3: Write minimal implementation**

On `SwarmConfig`:

```go
ScheduleMinIntervalSeconds int `yaml:"schedule_min_interval_seconds" json:"schedule_min_interval_seconds"`
ScheduleTickMS             int `yaml:"schedule_tick_ms" json:"schedule_tick_ms"`
ScheduleMaxActive          int `yaml:"schedule_max_active" json:"schedule_max_active"`
```

Defaults: `30`, `1000`, `32`. Accessors repair `<=0`. `Load`/`normalize` fill blanks the same way as `GoalMaxAutoTurns`.

**Step 4: Run tests**

Run: `go test -race ./internal/config/ -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go docs/CONFIG.md
git commit -m "feat(config): add swarm schedule ticker caps"
```

---

### Task 2: Store models and CRUD

**Files:**
- Modify: `internal/store/models.go` (`Turn.ScheduleRunID`, `Turn.Quiet`, `Turn.ScheduleContinue`)
- Create: `internal/store/schedule.go`
- Create: `internal/store/schedule_test.go`
- Modify: `internal/store/store.go` (`AutoMigrate`)
- Modify: `docs/DATA_MODEL.md`

**Step 1: Write the failing test**

```go
func TestCreateScheduleRoundTripsAThreadWake(t *testing.T) {
	s := openTestStore(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.fatal(err)
	}
	row := &Schedule{
		Kind: ScheduleThread, ThreadID: th.ID, OriginThreadID: th.ID,
		Title: "wake", Prompt: "Inspect current state.",
		EveryS: 60, Status: ScheduleActive,
		NextRunAt: time.Now().UTC().Add(time.Minute),
		CreatedBy: ScheduleCreatedManager,
	}
	if err := s.CreateSchedule(row); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(row.ID, "sch_") {
		t.Fatalf("id=%q", row.ID)
	}
	got, err := s.GetSchedule(row.ID)
	if err != nil || got.EveryS != 60 || got.ThreadID != th.ID {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestListDueSchedulesSkipsPausedAndFuture(t *testing.T) {
	// one due, one future, one paused → only the due row
}

func TestDeleteThreadCancelsTargetedWakes(t *testing.T) {
	// wake on that thread becomes cancelled; standalone with only origin_thread_id stays
}
```

Use a generic prompt string. Do not put a sample task in `Prompt` assertions beyond "Inspect current state." if even that — prefer `"Check current state."` as structure, or just `"do the scheduled check"` as the durable instruction the human/manager stored. Safer: `Prompt: "Continue the wait."` and assert equality, not a domain word.

**Step 2: Run to verify fail**

`go test -race ./internal/store/ -run TestCreateScheduleRoundTripsAThreadWake -count=1`

**Step 3: Implement**

Constants: `ScheduleThread`, `ScheduleStandalone`, statuses `active|paused|done|cancelled`, run statuses `skipped_busy|running|findings|quiet|error`, `CreatedBy` `human|manager`.

`CreateSchedule` assigns `sch_` id. `ListDue(now)` = active AND next_run_at <= now. `CountActive()`. `CreateRun` / `FinishRun`. `CancelSchedulesForThread(threadID)` sets wakes with `thread_id=threadID` to cancelled.

Hook `DeleteThread` to cancel targeted wakes (test it).

**Step 4:** `go test -race ./internal/store/ -count=1` PASS

**Step 5: Commit** `feat(store): persist schedules and runs`

---

### Task 3: Next-run helper (cron / interval / delay)

**Files:**
- Create: `internal/engine/schedule_spec.go`
- Create: `internal/engine/schedule_spec_test.go`

Do **not** add a new dependency unless a 5-field parser in this file would blow the line budget. Prefer a focused parser: `*`, `n`, `n-m`, `n/s`, comma lists, fields minute hour dom month dow. Host timezone in, UTC `time.Time` out.

**Step 1: Failing tests**

```go
func TestNextRunAfterIntervalDoesNotCatchUp(t *testing.T) {
	spec := scheduleSpec{every: 60 * time.Second}
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	next := spec.nextAfter(now)
	if !next.Equal(now.Add(60 * time.Second)) {
		t.Fatalf("next=%s", next)
	}
}

func TestNextRunCronWeekdayMorning(t *testing.T) {
	// use a local location; assert the next instant is after `now`
	// do not hardcode a city name in the product, only in the test location
}

func TestParseSpecRejectsBelowMinInterval(t *testing.T) {
	_, err := parseScheduleSpec(0, 1, "", 30*time.Second)
	if err == nil {
		t.Fatal("1s every must fail a 30s floor")
	}
}

func TestParseSpecRequiresExactlyOneCadence(t *testing.T) {
	_, err := parseScheduleSpec(10, 10, "", time.Second)
	if err == nil {
		t.Fatal("delay and every together")
	}
}
```

Cron tests assert structure (next > now, skipping to the following slot) not a sample job title.

**Step 2–4:** implement `scheduleSpec`, `parseScheduleSpec(delayS, everyS, cron string, min time.Duration)`, `nextAfter(now)`. One-shot delay: `nextAfter` after a successful fire returns zero time (caller marks `done`).

**Step 5: Commit** `feat(engine): parse schedule cadence without catching up`

---

### Task 4: Engine create/cancel/list + kinds

**Files:**
- Create: `internal/engine/schedule.go`
- Create: `internal/engine/schedule_test.go`
- Modify: `internal/engine/turn.go` (kind constants live near the others **or** in `schedule.go` — keep wire strings in one place)

**Step 1: Failing tests**

```go
func TestCreateThreadWakeRecordsScheduleEvent(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: "Inspect current state.", EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hasKind(t, e, th.ID, KindSchedule) != 1 {
		t.Fatal("origin thread needs a schedule chip event")
	}
	_ = sch
}

func TestCreateScheduleRejectsIntervalBelowFloor(t *testing.T) { /* 1s every */ }

func TestCancelScheduleWritesCancelledKind(t *testing.T) {}
```

Kinds:

```go
const (
	KindSchedule          = "schedule"
	KindScheduleFired     = "schedule_fired"
	KindScheduleSkipped   = "schedule_skipped"
	KindScheduleReport    = "schedule_report"
	KindScheduleCancelled = "schedule_cancelled"
)
```

`KindSchedule` payload JSON `{id,kind,title,next_run_at}` — Trace, not a sample task.

Wire `TestNotifyKindsAreStableAcrossTheWire` in this task or Task 14; do not forget it.

**Step 2–4:** `CreateSchedule`, `CancelSchedule`, `ListSchedules`, `PatchSchedule` (pause/resume). No ticker yet.

**Step 5: Commit** `feat(engine): create and cancel schedules`

---

### Task 5: Manager tools

**Files:**
- Create: `internal/engine/schedule_tool.go`
- Create: `internal/engine/schedule_tool_test.go`
- Modify: `internal/engine/iterations.go` (append tools; `report_schedule` only useful when the turn is scheduled — still mount it always, handler returns error if not)
- Modify: `internal/engine/input.go` (`ContinueSchedule bool`, `ScheduleID string`)

**Step 1: Failing tests** (same grain as `goal_tool_test.go`)

- Info names stay `schedule_wake` / `schedule_task` / `cancel_schedule` / `report_schedule`
- Info text is generic: assert it does **not** contain `CI`, `deploy`, `cron example`, `GitHub`
- `schedule_wake` upserts on this thread
- `schedule_task` on a handler that sees `ContinueSchedule` returns `ok:false`
- `report_schedule` with empty findings returns quiet

Tool names:

```go
const (
	ToolScheduleWake   = "schedule_wake"
	ToolScheduleTask   = "schedule_task"
	ToolCancelSchedule = "cancel_schedule"
	ToolReportSchedule = "report_schedule"
)
```

**Step 2–4:** Implement tools. `InvokableRun` returns JSON errors, never a Go error, like `complete_goal`.

Engine handlers:

```go
func (e *Engine) scheduleWakeJSON(threadID, turnID, args string) (string, error)
func (e *Engine) scheduleTaskJSON(threadID, turnID, args string) (string, error)
func (e *Engine) reportScheduleJSON(threadID, turnID, args string) (string, error)
```

`scheduleTaskJSON` fails unless the current turn is human-originated: `!turn.GoalContinue && !turn.ScheduleContinue && !implementing`. Detect implement via existing plan flag on the turn path (`recordingPlanImplement`).

Mount next to `AskUserTool` in `runManager` so they exist even without a `/goal`. Workers: deny stub `"workers cannot schedule"`.

**Step 5: Commit** `feat(engine): manager tools to arm and report schedules`

---

### Task 6: Prompt — Waiting + extra + goal yield copy

**Files:**
- Modify: `internal/engine/prompt.go`
- Modify: `internal/engine/prompt_test.go`
- Modify: `internal/engine/engine_test.go` (`TestManagerPromptIsGenericAndGrounded`)

**Step 1: Failing assertions**

`TestManagerPromptPrefersProactiveDelegation` / grounded test must require:

- `"schedule_wake"`
- `"do not wait for the human to remind"`
- `"report_schedule"`

Must **forbid**: `CI`, `deploy`, `pull request`, `cron job` (case-insensitive) in `ManagerPrompt`.

`conversationExtra` includes `## Scheduled` when the thread has active wakes (id, next, cadence type, prompt head). Test with a real row, not a sample domain.

Update `goalSection`: a pending wake is the next turn; do not imply every ended wait-turn auto-continues immediately.

**Step 2–4:** Add `## Waiting` after `## Asking the human` (or after Delegating — pick one place and keep it). `scheduleSection(threadID)` needs the store; `conversationExtra` today has no engine. Pass open schedules in:

Change `conversationExtra(th, pc)` to `conversationExtra(th, pc, schedules []store.Schedule)` **or** fold the section in `managerExtra` by giving it `e *Engine`. Prefer `managerExtra(cfg, th, pc, scheduleLines string)` to avoid a circular grab. `runManager` already has `e` — load schedules there and pass the section string.

**Step 5: Commit** `feat(engine): tell the manager to wake instead of spinning`

---

### Task 7: Ticker, skip-busy, no catch-up

**Files:**
- Create: `internal/engine/schedule_tick.go`
- Create: `internal/engine/schedule_tick_test.go`
- Modify: `internal/engine/engine.go` (`now func() time.Time`, start/stop ticker)
- Modify: `internal/engine/input.go` / `turn.go` (`ContinueSchedule` records `schedule_fired` instead of `user_message`)
- Modify: `internal/app/app.go` (ticker already started from `Engine.New` or `StartScheduler`; `Shutdown` stops it)

**Step 1: Failing tests using a fake clock**

```go
func TestWakeSkippedWhenTheConversationIsRunning(t *testing.T) {
	e := newTestEngine(t)
	clk := &fakeClock{t: time.Now().UTC()}
	e.now = clk.Now
	// start a live turn, arm a due wake, tick once
	// assert KindScheduleSkipped, no second running turn, next_run_at in the future
}

func TestMissedTicksDoNotBurstAfterRestart(t *testing.T) {
	// next_run_at two hours ago, every_s=60
	// one fire only, then next from now
}

func TestScheduleFiredIsNotAUserMessage(t *testing.T) {
	// fire a due idle wake (use a scripted model that finishes immediately)
	// event kinds: schedule_fired present, user_message absent for that turn
}
```

`ScheduleContinueText()` — generic protocol, like `GoalContinueText()`:

```go
func ScheduleContinueText(prompt string) string {
	return "This turn is a scheduled check. Do the check in the instruction below, then call report_schedule. Empty findings archive the run. Cancel the schedule when the wait is over.\n\n" + strings.TrimSpace(prompt)
}
```

User message stored for replay = that wrapper. Timeline = `schedule_fired` with a short notice `"Scheduled check."` (i18n is UI-side).

Skip: `Status.Running` OR `PlanMode` → skipped. Advance `next_run_at` with `spec.nextAfter(now)`. One-shot delay that skips stays `active` and `next_run_at=now` (retry next tick when idle) — document in a comment.

Cap concurrent fires with `ScheduleMaxActive`.

**Step 2–4:** `StartScheduler` from `New` (tests that do not want it: `e.StopScheduler()` in `newTestEngine` **or** only start from `app.New`). Prefer **start in `app.New` after resume**, not `Engine.New`, so unit tests stay deterministic unless they call `e.StartScheduler()`.

`Shutdown` calls `StopScheduler`.

**Step 5: Commit** `feat(engine): fire due schedules and skip busy ticks`

---

### Task 8: `/goal` yields to a pending wake

**Files:**
- Modify: `internal/engine/goal_continue.go`
- Modify: `internal/engine/goal_continue.go` tests (or `schedule_tick_test.go`)

**Step 1: Failing test**

```go
func TestPendingWakeSuppressesGoalAutoContinue(t *testing.T) {
	// open goal, arm wake next_run_at in the future, finish a pursuing turn
	// assert no KindGoalContinued
}

func TestCancelWakeRestoresGoalAutoContinue(t *testing.T) {
	// after cancel, the next clean finish continues the goal
}
```

**Step 2–4:** At the top of `continueGoal`, if `e.hasFutureWake(threadID)` { reapParked; return }.

**Step 5: Commit** `feat(engine): pending wake pauses goal auto-continue`

---

### Task 9: Quiet vs findings

**Files:**
- Modify: `internal/engine/schedule.go` / turn finish path
- Create or extend: `internal/engine/schedule_report_test.go`

**Step 1: Failing tests**

```go
func TestEmptyReportArchivesAQuietTurn(t *testing.T) {
	// fire, report_schedule findings=""
	// turn.Quiet == true, run status quiet, KindScheduleReport
}

func TestOmittedReportWithAnswerIsFindings(t *testing.T) {
	// mock finishes with assistant text, no report_schedule
	// run status findings, unread, turn.Quiet == false
}

func TestOmittedReportWithNoAnswerIsQuiet(t *testing.T) {}
```

Finish hook (after `FinishTurn` success for `ScheduleContinue`): if no `schedule_report` yet, derive quiet from empty final.

Standalone: `CreateThread` then `StartTurnInput`. Quiet → `SetThreadArchived(id, true)`. Findings leave it visible.

```go
func TestStandaloneQuietFireIsArchived(t *testing.T) {}
func TestStandaloneFindingsStayInTheSidebar(t *testing.T) {}
func TestScheduleTaskRejectedOnScheduledTurn(t *testing.T) {}
```

**Step 5: Commit** `feat(engine): archive quiet scheduled runs`

---

### Task 10: HTTP API

**Files:**
- Create: `internal/server/schedules.go`
- Create: `internal/server/schedules_test.go`
- Modify: `internal/server/server.go` (routes + `fail` map)
- Modify: `docs/API.md`

New sentinel `engine.ErrSkippedBusy` → `409` `code: skipped_busy`.

Routes (no CORS):

```
GET    /api/schedules
POST   /api/schedules
GET    /api/schedules/:id
PATCH  /api/schedules/:id
DELETE /api/schedules/:id
POST   /api/schedules/:id/run
POST   /api/schedules/runs/:rid/read
```

**Step 1:** Handler tests like `steers_test.go`: create, list unread, run now on idle, run now while busy → 409 skipped_busy, delete, pause.

**Step 4:** `go test -race ./internal/server/ -count=1`

**Step 5: Commit** `feat(server): scheduled-task inbox API`

---

### Task 11: Transcript reducer + API client

**Files:**
- Create: `frontend/src/lib/transcript-schedule.ts`
- Create: `frontend/src/lib/transcript-schedule.test.ts`
- Modify: `frontend/src/lib/transcript.ts`
- Modify: `frontend/src/lib/transcript.test.ts`
- Modify: `frontend/src/lib/api.ts` + `api.test.ts`
- Modify: `frontend/src/lib/types.ts`

**Step 1: Failing tests**

- `schedule` → notice (not a user bubble)
- `schedule_fired` → running turn, notice chip, **not** kind user
- `schedule_report` with empty findings → mark that `turn_id` quiet; drop user/answer/tool bubbles for it (keep events out of the chat list)
- empty / whitespace `done` after `schedule_fired` with no `schedule_report` → same quiet drop (`sealQuietTurns(state, ev)`). Non-empty `done` is findings (keep the chip). Ordinary empty `done` is not quiet.
- `schedule_skipped` → no block
- `schedule_cancelled` → notice

Quiet: maintain `Set<turnId>` on `TranscriptState` or stamp `quiet` on blocks and filter at render. Prefer reducer drops chat bubbles and sets `turn.quiet`.

**Step 5: Commit** `feat(ui): reduce scheduled events without leaking quiet turns`

---

### Task 12: Inbox UI, banner, settings, i18n

**Files:**
- Create: `frontend/src/store/app-schedule.ts` + test
- Create: `frontend/src/components/app/schedule-inbox.tsx`
- Create: `frontend/src/components/app/schedule-banner.tsx` + tests
- Modify: `frontend/src/components/app/sidebar.tsx`
- Modify: `frontend/src/components/app/transcript.tsx` / session (notice + cancel)
- Modify: `frontend/src/components/app/settings-swarm.tsx`
- Modify: `frontend/src/lib/messages-en.ts`, `messages-zh.ts`, `i18n.test.ts` key parity
- Modify: `frontend/src/store/app.ts` — refetch schedules next to `refreshThreads`
- shadcn only; no hex

**Behavior:**

- Sidebar section **Scheduled** with unread badge (`data-testid="schedule-inbox"`)
- Sheet: list, pause/resume/cancel/Run now, create form (title, prompt, cadence, project)
- Banner on the open thread if it has an active wake
- Notice cancel calls `DELETE /api/schedules/:id`

**Step 4:** `cd frontend && npm test && npm run lint`

**Step 5: Commit** `feat(ui): Scheduled inbox and wake banner`

---

### Task 13: TUI notices + mock provider

**Files:**
- Modify: `internal/tui/session.go` / render path that maps event kinds to lines
- Modify: `internal/tui/session_test.go` or a focused test
- Modify: `internal/provider/mock.go`
- Create: `internal/provider/mock_schedule_test.go`

Mock:

- If last user text contains `This turn is a scheduled check.` and `report_schedule` not yet called → tool call `report_schedule` with a short generic `findings` string derived from the instruction (or empty when `ZWAI_MOCK_SCHEDULE_QUIET=1`).
- Optional `ZWAI_MOCK_SCHEDULE_WAKE=1`: first manager step calls `schedule_wake` with `every_s` = min interval and prompt = last user text (generic). Default off so the existing suite does not arm timers.

TUI: `schedule` / `schedule_cancelled` / findings `schedule_report` as one-line notices. Skip `schedule_skipped` and quiet reports.

**Step 5: Commit** `feat(tui): show schedule notices; mock scheduled turns`

---

### Task 14: E2E + remaining docs + kind stability

**Files:**
- Create: `frontend/e2e/schedules.spec.ts`
- Modify: `internal/server/server_test.go` (`TestNotifyKindsAreStableAcrossTheWire`)
- Modify: `ARCHITECTURE.md`, `README.md`, `CHANGELOG.md`, `docs/TESTING.md`, `docs/LIBRARY.md` only if the library API changed (it should not)

**E2E (mock, no real model):**

1. REST create standalone (from the page form) → Run now → inbox unread → open the findings conversation.
2. REST create a **thread** wake on the current conversation → banner visible → cancel removes it.
3. Optional: `ZWAI_MOCK_SCHEDULE_WAKE=1` send a generic message → dismissable `schedule` notice.

**Step 4:**

```
go test -race ./...
cd frontend && npm test && npm run lint
make frontend
make e2e
```

Fix coverage regressions (`docs/TESTING.md` table).

**Step 5: Commit** `feat: document and e2e scheduled tasks`

---

## Execution notes

- `newTestEngine` must **not** start the ticker. Tests that fire time call `e.StartScheduler()` after installing `e.now`.
- `e.now` defaults to `time.Now`.
- Same-origin only; `server.fail` for new sentinels.
- If `models.go` or `transcript.ts` would cross 1000 lines, split before adding more.

After each task: compile, tests for that package, then commit.
