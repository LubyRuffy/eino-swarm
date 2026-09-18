# Scheduled tasks (Codex-style)

> Product decisions are locked. This is the implementation design.

**Goal:** The manager can wait on the wall clock instead of spinning, and the
human can keep independent recurring jobs in a Scheduled inbox, without an
OS cron daemon and without asking the human to remind the agent.

The process must be running (same as Codex desktop). A quit stops the ticker;
the next start fires each overdue schedule **once**.

## Out of scope (v1)

- Gmail / Slack / GitHub event triggers
- Git worktrees (runs in the conversation or project directory)
- RFC 5545 RRULE
- `/schedule` slash command
- TUI inbox panel (notices only)
- Process-wide SSE (inbox is REST; origin-thread chips ride the existing SSE)

## Modes

| Kind | Landing | Who may create |
|---|---|---|
| **thread wake** | Same conversation, existing context | Manager, immediately. Transcript gets a dismissable notice. |
| **standalone** | **New conversation per fire** | Human REST/UI, or the manager on a **human-originated** turn. Synthetic turns (`goal_continued`, `schedule_fired`, `plan_implemented`) get an error. |

Cadence is one of `delay_s` (one-shot), `every_s` (interval), or a 5-field cron
in the host timezone. `next_run_at` is stored UTC.

Auto-inference applies to **thread wakes only**. Standalone jobs are never
minted from a heartbeat or a `/goal` continuation.

## Architecture

One Go process. `Engine` owns an injectable clock and a ticker. Rows live in
SQLite next to conversations. Restart is the same path as leftover turns:
`App.New` starts the ticker after `ResumeOrphanedTurns`.

```
manager tool / REST
    → schedules row (next_run_at)
    → ticker due
         ├─ target busy or plan_mode → skip_tick (trace only)
         ├─ thread idle → StartTurn(ContinueSchedule) on that thread
         └─ standalone → CreateThread → ContinueSchedule
                          quiet → archived
                          findings → Recents + inbox unread
```

A pending future wake on a conversation **suppresses** `continueGoal`. Waiting
is the continuation. Cancelling or finishing the wake (`keep=false` / one-shot)
restores immediate `/goal` auto-continue.

Busy policy is **skip this tick, keep wall-clock cadence, do not catch up**.
Missed beats are not queued as follow-ups and are not steered into the live
turn.

Quiet policy is **Codex archive**: nothing to report means no transcript
bubbles. The turn, events, and `zwai trace <turn>` still exist.

## Data model

### `schedules`

| column | notes |
|---|---|
| `id` | `sch_` + hex |
| `kind` | `thread` / `standalone` |
| `origin_thread_id` | conversation that created it; standalone fires do **not** reuse it |
| `thread_id` | thread wakes only: the conversation to wake |
| `project_id` | standalone workspace; empty → that fire's own workspace |
| `provider_id`, `model`, `reasoning_effort` | standalone; wakes use the target conversation's model |
| `title`, `prompt` | display name + durable per-run instruction (user text, not a baked example) |
| `delay_s`, `every_s`, `cron` | exactly one |
| `status` | `active` / `paused` / `done` / `cancelled` |
| `next_run_at`, `last_run_at` | UTC |
| `run_count`, `max_runs`, `until_at` | 0 / null = until cancelled |
| `created_by` | `human` / `manager` |

Deleting a conversation cancels wakes that target it. Standalone rows that
only originated there stay. Deleting a project cancels standalone rows pinned
to it.

### `schedule_runs`

`id`, `schedule_id`, `thread_id` (minted conversation for standalone; target
for a wake), `turn_id`, `status` (`skipped_busy` / `running` / `findings` /
`quiet` / `error`), `summary`, `unread`, timestamps.

### `turns`

`schedule_run_id`, `quiet`. `UserText` is still the durable prompt so replay
has it. The timeline records `schedule_fired`, not `user_message` (same split
as `goal_continued`).

### Event kinds (wire-stable)

`schedule` · `schedule_fired` · `schedule_skipped` · `schedule_report` ·
`schedule_cancelled`

`schedule` lands on the **origin** conversation (chip). `schedule_fired` /
`schedule_report` land on the run's conversation. `schedule_skipped` is
trace-only in the UI.

Add them to `TestNotifyKindsAreStableAcrossTheWire`.

### Config (`internal/config`, Settings-editable)

| key | default |
|---|---|
| `swarm.schedule_min_interval_seconds` | 30 |
| `swarm.schedule_tick_ms` | 1000 |
| `swarm.schedule_max_active` | 32 |

Zero/negative repaired to the default, same as other swarm caps. Below-minimum
`delay_s` / `every_s` is a tool/API error.

## Tools (manager-only)

Names travel in transcripts and Trace; renaming is a protocol change.

| tool | when | effect |
|---|---|---|
| `schedule_wake` | any turn | upsert a wake on **this** conversation (`id` optional). `prompt` + cadence, optional `title` / `max_runs` / `until` |
| `schedule_task` | human-originated turn only | create a standalone job. `ContinueGoal` / `ContinueSchedule` / `ImplementPlan` → error JSON |
| `cancel_schedule` | any turn | cancel by id (human DELETE does the same) |
| `report_schedule` | scheduled turn only | `findings` (empty = quiet), `keep` (default true if recurring), optional `next_in_s` (re-cadence) |

If `report_schedule` is omitted: a persisted manager answer is findings;
otherwise quiet.

Workers get the same deny stub pattern as `ask_user`.

No `list_schedules` tool. Open schedules for this conversation are injected
in `conversationExtra` so the model can upsert instead of doubling.

## Prompt

Generic `## Waiting` on the manager prompt, same grain as proactive spawn:

- When progress is gated on time or a condition that is not worth polling
  inside this turn, call `schedule_wake` and end the turn.
- Do not spin, do not block a tool to wait, do not ask the human to remind you.
- Do not schedule work that can finish now. Do not use a wake instead of
  `ask_user`.
- Busy ticks are skipped; choose a cadence that can miss a beat.
- A pending wake pauses `/goal` auto-continue until it fires.
- Standalone `schedule_task` only when the human asked for an independent
  recurring job, or after `ask_user` confirms the spec — and never from a
  scheduled or auto-continued turn.
- On a scheduled turn: do the check, then `report_schedule`. Empty findings
  archives the run. Cancel when the wait is over.

**No example task, filename, CI, deploy, or cron sample in the prompt, tool
descriptions, or test expectations.** Extend
`TestManagerPromptIsGenericAndGrounded` and the tool Info tests with negative
leaks.

`goalSection` must stop implying that ending a wait-turn always auto-continues
immediately: a pending wake is the next turn.

## Runtime

- Injectable `now func() time.Time`. Tests do not sleep real cadences.
- Ticker every `schedule_tick_ms`. `Shutdown` stops it (same grace idea as
  the review pool).
- Due = `status=active` AND `next_run_at <= now` AND no `running` run for
  that id AND active count under `schedule_max_active` (overflow waits for
  the next tick).
- Thread target `Running` (includes `ask_user`) or `PlanMode` →
  `skipped_busy`, record `schedule_skipped`, advance `next_run_at` from
  **now** (cron next / `now+every_s`). One-shot delay that skips stays due
  for the next idle tick (do not mark `done`).
- Overdue after restart: fire **once**, then compute next from now.
- Fire uses `UserInput{ContinueSchedule: true, Text: prompt, ScheduleID}`.
- `continueGoal` no-ops when this conversation has an active wake with
  `next_run_at` in the future (or a one-shot still due).
- Turn error → run `error`, `unread`, schedule stays `active`.
- `POST …/run` while busy → `409` with `code: skipped_busy` (map in
  `server.fail`, do not invent statuses inline).
- Standalone `CreateThread` uses the schedule's project/provider/model.
  Quiet → `archived=true`. Findings stay in Recents; auto-title may run.
- `report_schedule(next_in_s)` updates `every_s` / `next_run_at` (dynamic
  loop). Below min interval is an error.

## HTTP

All same-origin, no CORS.

| method | path | notes |
|---|---|---|
| `GET` | `/api/schedules` | `{schedules, unread}` ; query `status`, `kind` |
| `POST` | `/api/schedules` | human create (thread or standalone) |
| `GET` | `/api/schedules/:id` | row + recent runs |
| `PATCH` | `/api/schedules/:id` | pause/resume, prompt, cadence, title |
| `DELETE` | `/api/schedules/:id` | cancel |
| `POST` | `/api/schedules/:id/run` | run now |
| `POST` | `/api/schedules/runs/:rid/read` | clear unread |

Inbox is this list plus `unread` on runs with `findings` or `error`. Quiet
runs are not unread.

The shell refetches `/api/schedules` with the thread list. Live chips on the
open origin thread come from SSE.

## UI

shadcn + existing CSS variables. Copy through `messages-en.ts` /
`messages-zh.ts`.

- Sidebar **Scheduled** (unread badge). Sheet: Active/Paused, next run,
  recent runs, pause/resume/cancel/Run now, create-standalone form
  (title, prompt, cadence, project).
- Open findings → that fire's conversation. Quiet standalone threads stay
  archived and out of Recents.
- Active wake on the open conversation: short banner (next check, cancel),
  same density as `/goal`.
- `schedule` event: compact notice + cancel.
- Findings turn: chip, then the assistant reply. Do **not** render the
  durable prompt as a user bubble.
- Quiet turn: transcript reducer drops that `turn_id`'s chat bubbles.
- `schedule_skipped` is not rendered.

State in `frontend/src/store/app-schedule.ts`. Reducer rules in
`frontend/src/lib/transcript-schedule.ts`. Keep `app.ts` / `transcript.ts`
under the line budget.

TUI: one-line notices for `schedule`, `schedule_cancelled`, and findings
`schedule_report`. No inbox.

## Testing (must exist)

Fake clock. Name tests after the user-visible break.

- Wake skipped when the conversation is running; no follow-up queued
- Missed ticks do not burst after a long busy or a restart
- Pending wake suppresses `/goal` auto-continue; cancel restores it
- Quiet run has no `user_message` and the reducer emits no bubbles
- `schedule_task` rejected on `ContinueSchedule` / `ContinueGoal` /
  `ImplementPlan`
- Standalone fire mints a thread and archives it when quiet
- `delay_s` / `every_s` below min interval rejected
- Manager prompt and tool Info stay generic (negative leak assertions)
- Overdue-on-restart fires once
- Cancel stops further fires
- New kinds in `TestNotifyKindsAreStableAcrossTheWire`
- E2E (mock): REST create → Run now → inbox unread → open findings
  conversation; mock `schedule_wake` → banner + dismissable notice

## Docs in the same change

`docs/API.md`, `docs/DATA_MODEL.md`, `docs/CONFIG.md`, `docs/TESTING.md`,
`ARCHITECTURE.md`, `README.md`, `CHANGELOG.md`, TUI notice behavior in
`ARCHITECTURE.md` (TUI module paragraph). No CLI flag unless one is added.

## File split (keep every file < 1000 lines)

- `internal/store`: `Schedule` / `ScheduleRun` + `schedule.go`
- `internal/engine`: `schedule.go`, `schedule_tool.go`, `schedule_tick.go`
  (and tests beside them)
- `internal/server/schedules.go`
- `internal/config` swarm keys
- `internal/tui` notice rendering
- frontend files named above
