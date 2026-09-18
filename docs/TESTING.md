# Testing

```bash
make test        # go test -race -cover ./...  +  front-end unit tests
make e2e         # Playwright, on the scripted offline provider
make check       # go vet + both of the above
```

No test needs a model endpoint, an API key or a network. Anything that would
otherwise call a model runs on the **scripted offline provider**, so the suite is
deterministic and fast enough to run on every change.

## Layers

| layer | what it covers | command |
|---|---|---|
| Go unit tests | config, store, memory, provider, tools, engine, server, CLI, TUI, and the swarm library | `go test -race -cover ./...` |
| HTTP tests | every endpoint, SSE replay and resume, the tail log page (`GET /log`, including the live-edge roster sidecar), one worker's log (`GET /agents/:agent/log`), upload path traversal, restart recovery (leftover turns, in-flight sub-agents, and the follow-up queue continue; in-flight tools are closed), PTY terminals (`GET /terminal`, same-origin / loopback Origin, DNS-rebind Host refused, project cwd) | `go test ./internal/server/` |
| Front-end unit tests | the event reducer that turns the stream into blocks, the store's conversation targeting, quoting selected transcript text into the composer (the Add to chat snapshot surviving a live stream), clipboard image paste, file drop onto the composer, find-in-conversation matching (count vs a paint window so a live turn does not freeze), http(s) links leaving the window, sidebar drag order (title drag after 8px, first click still opens), chrome i18n (`en`/`zh` key parity, locale persist through settings), appearance tokens (`font` / `font_size` / `content_width`), tail-first history pages (`thread-log` / `thread-history` / `use-history-window`) | `cd frontend && npm test` |
| End-to-end | a real browser against a real server: conversation, streaming, sub-agents, files, settings (including the per-note memory cap), theme, chrome language, font and conversation width | `cd frontend && npm run e2e` |

Current Go coverage, from `go test -race -cover ./...`:

| package | coverage |
|---|---|
| `internal/memory` | 96.8% |
| `internal/provider` | 92.0% |
| `.` (swarm library) | 95.9% |
| `internal/tools` | 97.0% |
| `internal/store` | 92.2% |
| `internal/engine` | 92.7% |
| `internal/config` | 90.6% |
| `internal/terminal` | 95.7% |
| `internal/server` | 90.0% |
| `internal/slash` | 92.9% |
| `internal/tui` | 94.7% |
| `internal/app` | 88.0% |
| `cmd/zwai` | 84.0% |

What is deliberately not unit-tested: `main`, `runDesktop`/`runWeb`/`runTUI` (thin
wrappers around functions that *are* tested), `desktop.Run` (opens a native
window) and `tui.Run` (drives a real terminal; `runConfig` is the testable
payload). The rest of `internal/desktop`
is tested: traffic-light geometry, hopping AppKit geometry onto the main
thread (Wails delivers window events off-thread), the quit-time
`Window #1 not found` filter, and that the Dock icon is a real PNG with
transparent rounded corners on Apple's 824/1024 icon grid, wired into
`application.Options` (the window itself still cannot be opened in a unit
test). Each untestable shell was
split so that everything which can fail is testable: `startDesktopServer`
returns the window options and the live server without opening a window,
`serveWeb` takes an injected stop signal, and `assembleTUI` / `buildTUISwarm`
assemble the swarm without a terminal (same manager prompt and workspace
tools as the app). The window itself is verified by hand — see below.

## The scripted offline provider

`--mock` (or `provider.NewMock`) replaces the model with a script that behaves
like a real swarm run: it thinks, spawns two sub-agents (one with
`fork_context`), has a worker call `write` to produce a file in the workspace,
waits for both, then streams a markdown answer that includes a `chart`
fence whose values are the report lengths (so the UI's plot renderer is
on the same path, without a sample domain baked in).

It answers as a **memory reviewer** too, when the agent asking is the review
after a turn: it stores one note and records one skill, both derived from the
event log it was handed (not a compacted ADK transcript), then reports in a
line. Auto-review is skipped when the manager already wrote; Review now still
runs. Without that, a `--mock` run
and the E2E suite would exercise projects but quietly skip the whole memory
path — the tools, the files, the event and the panel.

It also answers as a **conversation namer** (`title-namer`): a short label
derived from the request, shorter than the placeholder, so `--mock` and the
tests can tell a generated name from a quoted first message.

It answers as a **compact summarizer** (`compact-summarizer`) and as a
**session-memory** writer (`session-memory`) with a short briefing derived
from the messages or events it was handed. A manager Generate whose
billed or estimated prompt tokens exceed `swarm.auto_compact_tokens` (default
80 000) is rewritten in `BeforeModelRewriteState` before the call: older
replayable tool results are cleared first, then older messages become that
briefing (preferring the rolling session memory), the recent tail stays, and
the human transcript is untouched. The briefing is streamed; silence uses the
provider idle timeout. Tests live in `internal/engine/compact_test.go`
(streamed briefing, no compact deadline, session-memory preference, a
transcript dump not persisted),
`internal/engine/compact_briefing_test.go` (refuse `Tool:`/`Human:`/`Assistant:`
and exec JSON, refuse a source-tail clip, keep dense prose, `/compact` and
auto-compact summarizer input newest-first under a rune cap, no task words in
the prompt),
`internal/engine/session_memory_test.go` (token/tool gate, event-log extract,
a force refresh that is already caught up, a short extract still storing a
briefing, cancel while another refresh holds
the gate),
`internal/engine/session_memory_bound_test.go` (newest-first rune cap, a
multi-thousand-event log staying under the cap, tool results clipped harder than
answers, dump not stamped, a failed refresh stamping the token watermark
so force does not resend, refresh wait is the provider idle timeout),
`internal/engine/microcompact_test.go` (replayable results only),
`internal/engine/goal_session_test.go` (historical wrap leftover stays
generic and is dropped from leftover steers, heat vs
`auto_compact_tokens` not a million-token window, wrap-up copied into
session memory when the meter is still cold), and
`internal/engine/autocompact_test.go` (under-budget
skip, in-turn fire, summarizer errors swallowed, a canceled turn stopping compact, a rejected dump leaving ADK state and `compact_summary` alone, roster rehydrated from
`spawned` events, default budget leaving a mock turn alone). `internal/engine/autocompact_vs_eino_test.go` is the keep-or-delete
gate against eino's `adk/middlewares/summarization`: same fixture, same stub
briefing, score spawn roster / in-flight `wait_agents` / last human as a user
message / swallowed summarizer errors / a task-agnostic summarizer prompt. The
Generate is eino's; Finalize keeps the tail and pins ids from events. It
does not score briefing prose. `internal/engine/autocompact_invariants_test.go`
locks the wiring and the regressions that would silently come back: default
Finalize, fishing spawn pairs from folded text, dropping `wait_agents`,
losing the current user message, stale billed usage, missing finished-worker
pins (including leftovers from an earlier `/goal` session). `TestLiveCompactBriefingQualityVsEino` does
that against the configured endpoint when `ZWAI_LIVE_COMPACT=1`; default
`go test` / `make test` skip it. If the custom Finalize does not beat eino's
`DefaultFinalize` on the structural checks, it should be deleted.
And when the manager prompt
still has an open standing objective, it calls `complete_goal` so a `--mock`
run does not auto-continue until the cap. Tests that need to observe
auto-continue call `provider.SetCompleteOpenGoal(false)`.
`internal/slash` and `internal/engine/slash_test.go` are why a `/goal`
send (glued CJK, fullwidth `／`, IME punctuation `、`) is the command at `StartTurn`, not a
user task; `/goals` still is. `/plan` is the same parse; a live turn is
`ErrBusy`. `internal/engine/ask_test.go` is why `ask_user` blocks this
turn until an answer, idle answers are `ErrIdle`, a wrong `call_id` is
`ErrAskMismatch`, interrupt cancels the wait, resume re-arms an orphaned
questionnaire instead of swallowing it, and a worker call fails in JSON.
`internal/engine/schedule_tool_test.go` is why `schedule_wake` upserts on
this conversation, `schedule_task` refuses a `GoalContinue` /
`ScheduleContinue` / plan-implement turn, empty `report_schedule` findings
are quiet, garbage arguments come back as JSON `ok:false`, Info text stays
generic, and workers get a deny stub.
`internal/engine/schedule_goal_test.go` is why a pending thread wake
pauses `/goal` auto-continue, why cancelling it restores the next
pursuing turn's `goal_continued`, and why paused, cancelled, and
standalone origin-only rows do not suppress. A still-due delay that
has already been claimed (`status=done` plus a `running` run, matching
`advanceAfterFire` before `StartTurn`) is `TestClaimedOneShotStillSuppressesGoalAutoContinue`
— the armed-row tests do not cover that race.
`internal/engine/schedule_report_test.go` is why a scheduled check with
nothing to surface archives (`quiet`, unread cleared, `turn.quiet`; a
standalone fire leaves Recents) while an omitted report with an answer
is `findings` and stays in the sidebar, and why a crashed or cancelled
scheduled turn marks the run `error` so `HasRunningRun` cannot stick.
`TestCreateScheduleUsesFrozenCapWhenTickerIsLive` is why inbox create
(and resume/patch, and a standalone claim's default provider) reads the
same snapshot the ticker froze, not a later `e.cfg.Swarm` write;
`ApplyLiveSwarmLimits` is what Settings uses to refresh it.
`TestResumeCannotRestartClosesTheScheduleRun` and
`TestResumeDropsASupersededScheduledRun` are why a leftover
`ScheduleContinue` that resume cannot continue — empty user text, or
superseded by a later unfinished row — closes that bound fire as
`error` instead of leaving `running`.
Tests opt into `provider.SetMockScheduleQuiet` / `SetMockScheduleSilent`;
the default mock still spawns and does not sniff a scheduled turn.
`internal/server/schedules_test.go` is why the inbox HTTP API creates
human waits, lists `{schedules, unread}`, pauses/cancels, run-now on
idle starts a `ScheduleContinue` turn, run-now while the conversation is
busy is `409 skipped_busy`, and reading a findings run clears unread.
`internal/store/schedule_test.go` is why `HasPendingThreadWake` treats
an active `kind=thread` row and a running thread fire as pending, and
why paused/cancelled/done-with-no-run, standalone origin-only, an empty
thread id, and a missing table do not (the last returns the error;
the engine fail-opens).
`internal/engine/prompt_test.go` is why the manager prompt has `## Waiting`
(`schedule_wake`, do not wait for the human to remind, `report_schedule`)
without CI / deploy / pull-request / cron-job samples, why an open `/goal`
names a pending wake as the next turn, and why extra lists this
conversation's active wakes from a real `CreateSchedule` row.
`internal/engine/plan_test.go` is why planning unmounts write/exec,
`propose_plan` writes the file, Implement remounts those tools,
deleting a conversation takes `PLAN.md` with it, and
`/goal` auto-continue does not fire while planning. The mock asks first
during planning (`explore → ask → propose`); default non-plan runs do
not, unless a test calls `provider.SetMockAskUser`.
`internal/tui/ask_test.go` is why a missing TTY fails `ask_user` instead of
hanging, and why digits pick an option. `internal/tui/plan_test.go` is why
planning drops write/exec and a paused `/goal` stays held after Implement.
`internal/engine/project_test.go` and `internal/memory/prompt_test.go`
are why a project worker gets `skill_view` and the notes/skills snapshot,
not `memory` / `skill_manage`, and why that worker prompt stays generic.

```bash
go run ./cmd/zwai web --mock --no-open --data-dir /tmp/zwai-demo
```

Its output is derived from the input, so tests can assert on what they sent
without hardcoding a script. **It must stay generic**: no example task, filename
or domain word from a user request belongs in the script, the manager prompt, or
any test expectation. The host snapshot in the prompt is live (`runtime.GOOS`,
today's date); tests assert those values, never a sample OS or a sample command.

## Go tests

```bash
go test -race -cover ./...                    # everything
go test -race ./internal/engine/              # one package
go test -race -run TestSteer ./internal/engine/
go test ./internal/server/ -coverprofile=/tmp/c.out && go tool cover -html=/tmp/c.out
```

`-race` is not optional here. The engine runs a swarm, an event bus and an HTTP
server concurrently, and the interesting bugs in it have all been ordering bugs:
a subscriber that misses the "done" event, a runtime that is still marked busy
when the next turn starts, a listener read while another goroutine binds it.
`TestShutdownDoesNotWaitForTheEventStream` is why ⌘Q does not freeze the
window: the EventSource is cancelled instead of waited out for five seconds.
`TestTruncateDoesNotBusyAgainstAWriter` is why editing a sent message does
not fail with `SQLITE_BUSY` while the previous turn is still flushing events
(the database is one connection).
`TestListWorkspaceDoesNotHideSiblingsBehindAFatDirectory` is why the Files
panel still shows later siblings when an early directory would otherwise spend
the 2000-entry cap.
`TestMissedHandoffReachesTheWaitingManager` is why a worker that
`send_message`s an invented id still finishes as done and the manager's next
model call sees the handoff text: the miss notifies the host
(`notified: manager`) instead of a `NodeRunError` or a retry roster.
`TestKeepHumanSteersDropsTheSessionWrap` is why an unread `/goal` wrap-up
leftover cannot become a human turn that resets the auto-continue cap.
The front-end reducer also hides the same historical text on older sessions.
`TestRetractUnreadSteerDropsItFromReplay` /
`TestPreemptKeepsTheTurnRunning` are why Delete drops one unread `[steer]`
by event seq (the `steer` row stays; replay does not) and Interrupt does not
cancel the turn. The library side is
`TestPreemptAbortsInFlightToolAndDeliversSteer` /
`TestRetractPendingSteerDropsItFromTheInbox` /
`TestPreemptDoesNotCancelWorkers`.
`TestGoalSessionExtendsTheSameTurnAtTheIterationCap` is why a `/goal`
ReAct slice keeps the same turn instead of asking to extend or cutting a
session; `TestAGoalTurnFinishesAcrossOneRoundSlices` is why `complete_goal` still
lands when each ReAct slice is one round;
`TestStitchManagerToolResultsFillsDroppedWaitResults` /
`TestStitchManagerToolResultsRefreshesAStaleWait` /
`TestManagerToolResultsIgnoresWorkersAndEmptyTurns` are why a slice that
lost its ADK tool results still reads the event-log `wait_agents` report
(including a later wait that reused the same call id);
`TestMockWaitCallIDsStayUniqueAfterAContinuedRun` is why the scripted
manager does not reuse `mock-wait-1` on the next ReAct slice;
`TestSetHistoryKeepsDroppedToolResults` /
`TestRunWithMaxIterationsKeepsToolResultsInTranscript` are why that slice's
tool results are in the transcript the next slice reads;
`TestClosingAGoalAtTheReActSliceDoesNotAskToExtend` /
`TestBlockingAGoalAtTheReActSliceDoesNotAskToExtend` are why
`complete_goal` / `block_goal` mid-slice must not pop the non-goal
confirm card;
`TestGoalContinuesWhenTheManagerStopsCallingTools` is why the manager
stopping tools starts the next turn with no `goal_session`;
`TestAContinuationWithoutToolsStopsAutoContinue` /
`TestAContinuationWithToolsKeepsAutoContinue` are why an empty
continuation records `goal_idle` instead of looping, and a live
`wait_agents` does not;
`TestHoldGoalIdleIsIdempotentAndSkipsMissingThreads` /
`TestAHumanMessageClearsAHeldGoal` / `TestResumeClearsAHeldGoal` are why
the hold is sticky until a human message or Start;
`TestParkedWorkersStayVisibleBetweenGoalSessions` is why Agents still
sees in-flight sub-agents after the manager yields.
`TestHostNotifyKeepsWorkerEventsAfterRunReturns` /
`TestAttachHostNotifyIgnoresNil` are why a parked worker still records
`finished` after the manager `RunWith` returns, instead of freezing
Agents on starting with an empty pane.
`TestRaisingMaxConcurrentUnblocksQueuedWorkers` /
`TestLoweringMaxConcurrentKeepsWaitersQueuedUntilASlotFrees` /
`TestApplyLiveSwarmLimitsResizesParkedAndRunningRegistries` are why
Settings → Swarm → sub-agents at once takes effect on the live and
parked registry: a higher cap starts waiters immediately, a lower cap
does not kill in-flight workers.
`TestNextGoalSessionAfterRestartResolvesLeftoverWorkers` /
`TestNextGoalSessionAfterRestartRestoresRunningWorkers` are why a process
death between `/goal` sessions does not turn `wait_agents` into
`unknown agent`: leftovers are planted or restored from the conversation
event log, and spawn ids are re-pinned because later turns drop previous
tool results. `TestOrphanedWorkersTreatsCleanupAsFinished` is why a
killed leftover is planted as stopped instead of restarted.
`TestAutoCompactRehydratesFinishedWorkersFromEarlierTurn` is why a fold
on the next session still pins those ids.
`TestANormalMockTurnDoesNotAutoCompact` is why the default token budget
leaves a scripted turn alone; `TestAutoCompactFiresDuringATurnWhenOverBudget`
is the in-turn fold.
`TestBuildUserMessagePutsImagesOnUserInputMultiContent` is why a pasted
screenshot with a caption does not send `Content` and `UserInputMultiContent`
together (OpenAI's marshaler rejects that pair);
`TestExclusiveContentDropsCaptionAlreadyInParts` is the client-side strip
so a later concat cannot revive the same error.

`TestAFailedTurnBlocksAnOpenGoalAndDoesNotAutoContinue` is why a crashed
`/goal` turn shows Blocked with the public turn error on the banner (not a
generic sentinel) and an idle composer instead of auto-continuing into the
same failure; `TestARetryableModelErrorRetriesInsteadOfBlockingTheGoal` is
why truncated tool JSON / a `429` / a dropped stream re-enters the same
turn (`model_retry`) instead; `TestARetryableModelErrorBlocksAfterRetriesAreExhausted`
is the cap so a poison request cannot loop;
`TestFailedTurnBlockReasonPrefersThePublicError` is the
fallback when the turn row has no error text;
`TestTurnOutcomePrefersAModelFailureOverInterruptNoise`
is why a `NodeRunError` is still an error;
`TestTurnOutcomeTreatsABareCancelAsAnError` is why a cancelled model
call without Interrupt is not a clean `done`.
`TestAnInterruptedTurnDoesNotBlockTheGoal` is why Stop pauses (`goal_capped`)
instead of blocking; `TestPauseOpenGoalOnInterruptMarksAPursuingGoal` is the
direct pause path.

The TUI package also covers the same tool-display rule as the desktop UI: an
`exec` notification whose args are JSON is shown as the command, a non-zero
exit becomes a failed block (not a JSON dump), and `web_search` lists hits.
A missing `--task` opens the composer instead of failing; typed turns continue
the transcript, and a `--task` run still exits when that swarm finishes.
`--goal` without `--task` is the first user message and starts immediately
(`TestTUIGoalWithoutATaskStartsTheObjective`); leftover words still win as the
task. An empty auto-continue returns the composer
(`TestAGoalContinuationWithoutToolsReturnsTheComposer`) instead of looping
until `goal_max_auto_turns`. Typing `/` opens a command popup (`TestSlashMenuAppearsAboveTheComposer`) with
prefix filter, a `/model` / `/reason` picker, and `/help` / `/clear` / `/exit`.
`shift+tab` still cycles thinking level for the next turn, same choice the
desktop composer makes. The idle composer parks
the real terminal cursor after committed text (`TestComposerIMECursorSitsAfterCommittedText`)
and idle ticks must not rewrite the view (`TestIdleTicksDoNotRepaintTheComposer`),
or CJK IME preedit jumps to the start of the line.

Conventions in these tests:

- Every test gets its own data directory (`t.TempDir()`) and clears the
  `OPENAI_*` variables, so a developer's environment cannot change the outcome.
- Test names say what would break for the user, not which function they call:
  `TestStartTurnIsAcceptedNotAwaited`, `TestSlowSubscriberDoesNotBlockTheRun`.
- Comments in tests explain *why the behaviour matters*, because that is what a
  later reader needs to decide whether a failing assertion is a regression or an
  obsolete expectation.

`cmd/zwai/cli_trace_test.go` is split from `cli_test.go` so neither file
crosses 1000 lines: a memory review on the same turn id shows in `zwai trace`.
`internal/provider/provider_wait_test.go` is wait_agents progress parsing.

## Front-end unit tests

```bash
cd frontend
npm test          # vitest, once
npm run test:watch
```

`src/test/setup.ts` replaces `localStorage` with a Map. Node 25's global
`localStorage` is not Web Storage and it shadows jsdom's; chrome preferences
(theme, conversation-list width) would otherwise silently no-op in unit
tests. The Map is cleared after every test.

Several things are tested here, some as pure logic and some in jsdom:

- **`src/lib/transcript.ts`**, where the stream becomes UI: streamed text
  replaces rather than appends, a completed block folds into the streamed one
  instead of duplicating it, a tool call pairs with its result by
  `tool_call_id`, a `tool_delta` fills the pending row without closing it,
  `collapseLiveEvents` keeps the latest snapshot per live tool call (two
  parallel `exec`s do not mix), a progress pulse updates the live summary without leaving
  a row in the timeline or reviving an agent that has already finished, a
  `usage` pulse is ignored by the reducer (the composer meter owns it) and
  `collapseLiveEvents` keeps only the latest usage snapshot in a burst, a
  `max_iterations` event becomes a confirm card (and `max_iterations_continued`
  resolves it) rather than an unknown notice. A `model_retry` is a generic
  notice (the `{attempt,cap}` JSON stays in Trace). An interrupt, a `cleanup`, a
  worker `finished`, or a manager `error` closes tools left pending so they
  stop spinning. A clean manager `done` does not: leftover workers are parked
  for the next `/goal` session, and `goal_continued` must not paint them done
  or `wait_agents` sits next to a Done roster while the worker is still in
  `exec`. `cleanup` is the kill signal.
  `collapseLiveEvents` keeps only the latest snapshot per agent and kind from
  a burst of deltas — lossless, because a delta carries the accumulated string.
  Live tool output uses the same rule keyed by `tool_call_id`. Carriage return
  in an expanded tool body is overwrite (`src/lib/carriage.ts`). A pending `exec`
  row starts open.	`splitQueuedSteers` pulls unread steering out of the turn body while a turn
  is running (a later model round on the same turn consumes it; a previous
  turn's steer stays put). `steer_retracted` hides that bubble by seq (a
  duplicate caption stays); `steer_preempted` is silent. A retract that
  arrives before its `steer` (history paging) still hides the later row. A `resumed` event keeps leftover sub-agents running
  — a second `spawned` for the same id is the roster coming back, not a twin —
  closes tools left pending so a killed `exec` does not keep spinning, and
  dismisses a leftover tool-round confirm so a crash does not look like
  the turn is still waiting for a click. Engine tests assert the matching
  `tool_result` (`err`: the previous process stopped) is stored before `resumed`.
  A `rewound` event (and `rewindTranscript`) drops
  that `user_message` seq and everything after it without lowering `lastSeq`.
  `placePendingEdit` puts the edited text back at the cut so the bubble stays
  while the replacement `user_message` is in flight. Both are pure functions,
  Standing-objective notices (`goal`, `goal_continued`,
  `goal_idle`, `goal_session`, `goal_resumed`, compact) live in `transcript-goal.test.ts`: auto-continue
  starts the next turn's clock, a parked worker stays running across manager
  `done` and `goal_continued` so Agents cannot say Done while `wait_agents`
  is still blocked, a forced session end does not paste its JSON,
  a session wrap-up `steer` is omitted so it cannot look like human guidance,
  a `goal_capped` interrupt payload notices as paused without pasting `interrupted`,
  the briefing JSON never becomes a chat row (`detail` holds `summary` so an
  icon can open it), and auto-compact shows compressing then the token counts
  instead of the payload. Scheduled-task kinds live in `transcript-schedule.test.ts`:
  `schedule` / `schedule_fired` / `schedule_cancelled` are manager notices (not
  `user`, not the JSON payload or raw id), `schedule_skipped` is a no-op,
  a quiet `schedule_report` drops that turn's chat bubbles and later events
  cannot grow them back, and a findings report keeps the fired chip then the
  answer. `compact-notice.test.tsx` clicks that icon. A `session_memory` event is
  quiet on the manager like a generated title — no chat row, no extra worker.
  Finished `goal_session` /
  `goal_continued` turns fold behind a one-line Worked-for row in
  `transcript-session.test.tsx` (CJK preview truncates; duration does not wrap).
  A failed session stays expanded as Stopped-after with the error visible,
  and a collapsed preview prefers that error over the last answer.
  Folded successful work is not mounted until the row is opened, so a long `/goal`
  cannot re-parse every past answer on each streamed token.
- **`src/lib/turn-nav.ts`** and **`src/components/app/turn-nav.tsx`**: user
  turns become jump targets (steering does not), ticks pack into a compact
  cluster in the middle of the pane rather than stretching it,   the active tick
  is the last message whose top has crossed a probe near the viewport, or the
  latest turn when the scroller is at the bottom or still following the live
  edge (opening a conversation used to measure at scrollTop 0 and keep the
  first tick current), turns not yet mounted are skipped rather than treated
  as offset 0, and a
  missing id after a conversation switch is a no-op. The rail stays hidden until
  there are two user turns; hover opens the list; a click (or arrow keys) jumps.
- **`src/lib/thread-log.ts`**, **`src/lib/use-history-window.ts`**, **`src/store/thread-history.ts`**, **`src/lib/welcome.ts`**: opening a
  conversation paints one viewport of the live edge (`GET /log`) and resumes
  SSE after that seq; scrolling up prepends older pages without jumping the
  viewport. The top sentinel pages even when IntersectionObserver fires in
  its 80px lead (`scrollTop` still 80), and a later scroll to 0 pages even
  if that first intersection already happened — IO does not re-fire while
  the sentinel stays on screen. Prepend keeps the same row on screen unless
  the reader already reached the top, in which case they stay on the newly
  loaded rows. A jump to an unloaded turn keeps paging until that user row exists.
  A worker-only tail keeps paging until a non-quiet, non-spawn manager row exists so the
  empty-state idea cards cannot cover a running conversation. The live-edge
  page also folds a `roster` sidecar (`spawned` / `finished` / `cleanup`
  outside the viewport) so a manager-only tail does not empty the Agents tab.
  Roster sidecar rows do not become manager "Started" lines. Opening a
  worker off the live edge fetches `GET /api/threads/:id/agents/:agent/log`.
  `isWelcomePane`
  stays false while a turn is running, workers are on the roster, or older
  history is still above the tail.
- **`src/store/app.ts`**, against a fake API: which conversation an action lands
  in when New conversation is clicked twice, when a send races a slow create,
  and when a file is dropped on the empty state; sending while a conversation is
  still being created must wait for it, or the turn runs in the conversation the
  user just left; Enter while a turn is running queues a follow-up instead of
  steering, ⌘Enter / `{steer:true}` injects now, `{fromEventSeq}` starts a turn
  (never a follow-up) after clearing everything below that bubble and leaving
  the edited text in place, an edited follow-up is moved to the back of the
  queue, and an idle enqueue falls
  through to starting a turn; a title event renaming the open conversation (and a failed
  namer leaving the placeholder, and a `done` `refreshThreads` whose GET
  is still `title_auto` not stomping that name — `src/lib/thread-title.ts`); `refreshCatalogs` rediscovering every
  endpoint with a URL without rebooting the open conversation; a live
  `usage` event filling the composer snapshot (reload reads it from GET);
  and opening a conversation whose live-edge page is only worker tools pages
  older events until a manager row exists before painting as loaded.
  Live SSE folding lives in `src/store/app-stream.ts` so `app.ts` stays under
  1000 lines. `title` flushes immediately like `done`.
  `app-steer.test.ts` is why Interrupt-inject and per-bubble retract hit the
  open conversation and swallow `no_steer` / `404` races.
  A roster sidecar of `spawned` rows must not stop that paging or paint a
  wall of "Started" lines. Opening a worker off the live edge fetches
  `GET /agents/:agent/log` even when paging already claimed the log was
  complete but that worker's pane is still empty
  (`src/store/app-history.test.ts`). Opening paints the tail and resumes
  the stream after that seq (also here, split from `app.test.ts`).
- **`src/store/app-goal.test.ts`**: `/goal` pursuing until `complete_goal` or
  `block_goal` (edit and resume from the banner); setting a standing
  objective while idle starts that turn; setting one during a run does not
  queue a second turn; `/compact` landing on the
  open conversation, with the hint hidden at 0% (and a compact with nothing
  open being a no-op); a live compressing pulse (`seq` 0) not marking the
  conversation compacted. A `done` then `goal_continued` (or `goal_resumed`)
  keeps `status.started_at` so the title-bar Working clock is the current
  turn, not a frozen 1s. An `error` then `goal_blocked` idles the composer
  and marks the objective blocked, so a crashed `/goal` turn is not still
  Working — after in-turn `model_retry` of truncated tool JSON / `429` / a
  dropped stream is exhausted. A `goal_idle` event holds the banner until a human message or Start.
  Split from `app.test.ts` so neither file crosses 1000 lines.
- **`src/store/app-plan.ts`**: `/plan` enters planning and starts a turn with
  an argument (not during a run, not when the argument is empty); a human
  edit PATCHes the body; Leave does not start a turn; Implement starts one;
  ask_user answers POST the open conversation; nothing open is a no-op. A
  live `plan_updated` folds into the banner thread. Split from `app.test.ts`
  so neither file crosses 1000 lines.
- **`src/store/plan-events.ts`**: thread flags for `/plan` events live here so
  `app.ts` is not the owner of that protocol.
- **`src/lib/transcript-review.test.ts`**: memory review notices, title and
  session-memory rows, rewind, `ask_user` cards, and plan notices. Split
  from `transcript.test.ts` so neither file crosses 1000 lines.
- **`src/lib/transcript-ask.ts`** and **`src/components/app/ask-card.tsx`**: an
  `ask_user` tool_call becomes a question block; the host injects Other; a
  result settles the card; a numbered choice plus Submit POSTs answers.
- **`src/components/app/plan-banner.tsx`**: Planning, editable markdown,
  Implement, Leave. Split from the goal banner so neither file grows past
  1000 lines.
- **`src/store/goal-events.ts`**: thread flags for `/goal` events live here so
  `app.ts` stays under 1000 lines. `goal-events.test.ts` covers the idle hold.
- **`src/lib/usage.ts`** and **`src/components/app/context-meter.tsx`**: compact
  Cursor counts (`71.3K`), a percentage that is undefined without a window
  rather than a fake 0%, a saturating arc so an unknown window is not a dead
  empty circle, Discover merging per-name windows, selecting a listed name
  without stealing a sibling's window, and the ring's accessible name.
- **`src/lib/api.ts`**, against a stubbed `fetch`: the server's error message and
  code reach the UI instead of a bare status line, a tool list that arrives
  as `null` is read as an empty list rather than crashing the settings dialog,
  and `POST /compact` / `PATCH goal` hit the right paths.
- **`src/lib/marquee.ts`** and **`src/components/app/marquee.tsx`**: overflow is
  a width comparison (jsdom cannot layout a real line), a longer line gets a
  longer loop, and a live `MarqueeText` is marked `data-marquee="shimmer"` while
  idle text stays `"off"`.
- **`src/lib/chart-spec.ts`**: a `chart` fence body is JSON for bar / line /
  area / pie. Parallel `labels`+`values`, a values object, and missing x/y
  still parse; a single row, an unknown type, and junk do not; a truncated
  object is incomplete so a live fence can wait; rows and series are capped.
- **`src/components/app/transcript-chart.tsx`**: a valid spec is a figure with
  Chart / Table tabs (plot first; the table is the same rows, not a
  screen-reader-only dump); a live incomplete fence is a
  placeholder; fills and axes use `var(--chart-*)` / muted / border tokens
  so a theme switch restyles the same plot.
- **`src/lib/stream-markdown.ts`**: trailing `**` / `` ` `` / `~~` in the
  current prose region are closed so a streaming answer can render; an open
  fence is left alone (it is already a code block), and a marker with no
  content yet is not turned into `****`.
- **`src/components/app/markdown.tsx`**, rendered in jsdom: a streaming
  `## heading` is a heading, trailing bold renders as `<strong>`, a
  finished answer is not rewritten, a `chart` fence paints, a `json` fence
  does not, a live truncated chart fence is a placeholder, and a finished
  invalid chart fence stays code.
- **`src/components/app/transcript.tsx`**, rendered in jsdom: while `wait_agents`
  is pending the transcript shows the sub-agents it is waiting on, each one's
  live activity and the age the latest pulse gave it — the roll-up that keeps a
  running swarm from looking frozen — and clicking a row opens that agent. The
  heartbeat line is tested separately: it reports the turn's age and the number
  of sub-agents still working, keeps ticking on fake timers between pulses, and
  renders nothing before the first pulse or after the turn ends. The id parser
  behind the roll-up is also unit tested for half-streamed and malformed
  arguments. A streaming answer (manager transcript and a worker's panel) is a
  heading, not raw hashes. A live thought is a 10-line scrolling box that
  follows new tokens and fades at the top once earlier lines have left; the
  **Thinking** label sweeps (`MarqueeText`) until it collapses to **Thought**.
  Clicking the row hides it while tokens still arrive; later deltas do not
  force it back open.   Opening an `exec` row wraps the full command with shell
  highlighting instead of leaving it truncated (`transcript-exec.test.tsx`).
  A refused `memory` write shows the refusal on the collapsed row, not only
  inside the disclosure. An exec still open when the turn is interrupted loses its spinner rather
  than running forever. Copy and a pencil sit under each user bubble (copy
  is hidden when there is no text); edit opens the bubble in place and Send
  restarts from that `user_message` seq, clearing everything below
  (`transcript-user.test.tsx`).
  A compact notice stays a one-liner; `compact-notice.test.tsx` opens the
  briefing from the icon without pasting it into the row.
  Each turn group carries `data-turn-nav` so the rail can jump; one turn
  keeps the rail hidden, two turns show it. A live thought's top fade is
  trapped in its own stacking context so it cannot cover the rail.   Unread steering is pinned below
  the heartbeat (`queued-steers`), not above the working line; a steer the
  manager has already read stays in the turn body. That pin has Interrupt
  (all unread) and per-bubble Delete (`queued-steers.test.tsx`). Auto-follow unpins on
  wheel-up even inside the old 80px slack; new tokens then show a jump-to-latest
  control, and a skeleton→loaded remount still attaches the listener. Switching
  conversations re-pins even if the previous one was scrolled up; a layout
  scroll at 0 while the scroller is opening does not unpin.
- **`src/lib/follow-scroll.ts`**: whether the reader is at the live edge, whether
  a jump control should show (only after content arrived while unpinned), and
  whether a wheel started in a nested scroller (a thought, a tool payload)
  should leave the transcript pinned. `reset` restores the initial pin when the
  open conversation (or the open sub-agent) changes.
- **`src/lib/thought-scroll.ts`**: whether the top fade should show, whether
  new tokens should pin the box to the bottom — the same "do not yank a reader
  who scrolled up" contract as the transcript scroller — and whether a live
  thought stays open (`thoughtExpanded`: the reader's click wins over streaming).
- **`src/lib/ime.ts`**: Enter sends only when composition is settled. An IME
  Process key (`keyCode` 229), a live `isComposing` flag, or composition that
  ended in this frame (WebKit fires compositionend *before* the confirming
  Enter) must not send; leftover Latin staying as typed is the point.
- **`src/components/app/composer.tsx`**, rendered in jsdom: the thinking-level
  menu labels the empty default as "Default" and a set level by name, and it is
  absent when the server offers no levels, so an older server never draws a
  control that would send a meaningless choice.   The input is owned by the
  composer, not the app shell, so typing cannot re-render the transcript.
  A live `usage` pulse is read by the ring child (`ComposerContextMeter`),
  not by the textarea's parent, so an in-flight turn cannot rewrite the
  field while a CJK IME is composing. Auto-resize skips `height: auto`
  during composition (`resizeComposerArea`); the confirming
  `compositionend` measures once.
  Enter while composition is live, or on the key that just confirmed it, leaves
  the draft in the box; the next settled Enter sends. The chrome is a fade over
  the transcript, not a top border, so a docked toolbar cannot regress in.
  The model control is always a switcher, even with one ready name.
  ⌘Enter marks the send as steer; Enter while running queues. The Queued tray
  (`queue-tray.tsx`) names the count, edits a row in place (Enter saves and
  that message goes to the back of the FIFO; Escape cancels), steers or drops
  a row after a confirm, and Clear queue asks first.
  File drop onto the box (`composer-drop.ts`) arms a dashed overlay, then
  splits images into vision thumbs and other files into workspace chips
  (`composer-attachments.tsx`); a disabled composer ignores the drop.
  Send names the uploaded paths on the turn (`files` on `POST /turns`); a
  failed upload keeps the chip and does not start a turn.
  `/plan` waits for the work after picking the command; `/plan <task>` submits
  in one go. While `awaiting_answer` the placeholder is the Other prompt.
- **`src/components/app/goal-banner.tsx`**: Pursuing / Blocked / Paused / Done,
  Start only when Paused or Blocked (not while Pursuing between sessions),
  inline edit that saves on blur and cancels on Escape, and
  elapsed time from `goal_started_at`. A failed-turn sentinel is localized
  and yields to the turn's public error when that is on the transcript.
- **`src/lib/models.ts`**: choice ids round-trip through a tab separator,
  `groupModels` keeps server order, and auxiliary choices skip blanks.
- **`src/components/app/model-settings.tsx`**, rendered in jsdom: providers
  start as a collapsed list (name, model, default badge) so a second
  endpoint is not a wall of fields; opening a row reveals Provider / URL /
  key / default; Add a provider appends a row and opens it; a settings
  search that matches a field opens the matching rows. Discover fills the
  default-model dropdown from the catalog. A catalog of two names gets two
  window fields — typing one must not write the provider fallback.
  Removing a provider asks first.
- **`src/lib/composer-chrome.ts`**: the composer writes `--composer-pad` onto the
  conversation stage from its own height; a zero height (jsdom) leaves the CSS
  fallback so a unit test cannot collapse the transcript into the box.
  `resizeComposerArea` skips the `height: auto` measure while `composing` is
  set, so an IME candidate window is not laid out on every preedit key.
- **`src/lib/tool-view.ts`**: a built-in tool's JSON args collapse to the
  command / query / path the user needs to see, `execCommand` keeps newlines
  so an expanded row can show a heredoc, `exec` payloads become stdout
  plus a failed flag when the exit code is not 0, and `web_search` payloads
  become a list of hits. A refused memory write puts the refusal on the
  collapsed row (`toolRowSummary`), not only inside the disclosure. Assertions
  check structure, not any particular query.
- **`src/lib/shell-highlight.ts`**: an `exec` command tokenizes into
  keywords / strings / flags / variables without dropping characters; a
  too-long line falls back to plain text.
- **`src/lib/source-highlight.ts`** / **`src/lib/source-lang.ts`**: a `read`
  body tokenizes from the path suffix into the same token kinds; concatenating
  tokens reproduces the file; markdown and unknown suffixes are left alone.
- **`src/lib/read-result.ts`**, a `read` tool payload becomes a file listing:
  newline-separated bodies, and the older flattened one-liners, both recover
  the path and the lines; other tool output is left alone.
- **`src/components/app/tool-result.tsx`**, rendered in jsdom: a markdown `read`
  renders headings, a `*.go` `read` paints keywords onto tokens and keeps line
  numbers, an unknown suffix stays uncoloured, `exec` shows
  the full wrapped command plus stdout (and a role=alert error when it failed),
  and `web_search` lists hits instead of dumping JSON.
- **`src/components/app/source-code.tsx`**, rendered in jsdom: a numbered
  listing maps token kinds onto the syntax CSS variables.
- **`src/components/app/shell-command.tsx`**, rendered in jsdom: the expanded
  command wraps instead of truncating, and the collapsed preview stays one
  line.
- **`src/components/app/sidebar.tsx`**,
  **`src/components/app/sidebar-section.tsx`**,
  **`src/components/app/sidebar-slots.tsx`**,
  **`src/components/app/sidebar-thread-row.tsx`**, and
  **`src/components/app/sidebar-thread-group.tsx`**, rendered in jsdom: the list
  starts with New conversation; there is no title-bar chrome row and no hide
  control — those live on the window title bar. Projects and Recents share
  the scrollport gutter: wrapping the project section in a second `px-2` is
  rejected so it cannot sit 8px further in than Recents. The list starts at
  256px, the arrow keys change that width (CSS variable, not a React `width`
  style), a remembered width is restored, and the resize strip sits on the
  right edge (`z-20`) with the aside stacked above the transcript (`z-10`) so
  the 4px overlap is not painted over by the main column. Dropping one Recents
  row on another reports the new id order; a drag that starts on the row menu
  is ignored. A title click still opens when a dragstart races it; dragging
  the title past 8px reorders. There is no grip glyph. A pinned project
  topic sits in Pinned and still under its folder. The folder itself is
  not pressed; the open topic is `aria-current`. The folder icon is the
  fold control: open directory vs closed directory. A running
  conversation's progress sits in that same icon column. Recents,
  Pinned and nested titles keep a `size-4` spacer so they share a
  column with the project name. A section chevron is hover-only
  while that section is open. Folder and topic rows are `h-7`
  so the row menus do not pad the list out. Clicking Pinned, Projects,
  or Recents folds that section (`aria-expanded`);
  a reload keeps the fold. A project folder and Recents show at most
  five conversations from the last seven days; **Show more** reveals
  the rest and **Show less** folds them. The open or running
  conversation stays in the preview. Deleting a conversation asks first.
- **`src/lib/sortable.ts`**: the row is never HTML5-`draggable`. A title
  click still opens; a pointer move of 8px from the title reports the move;
  a twitch under that threshold is still a click. Synthetic row `dragstart`
  (jsdom / Playwright) still reorders. A dragstart with no `dataTransfer`
  is ignored. A disabled bind does not arm.
- **`src/lib/reorder.ts`**, **`src/lib/sidebar-groups.ts`**,
  **`src/lib/sidebar-preview.ts`**,
  **`src/lib/sidebar-collapse.ts`**: moving a row is a splice; Recents and a
  project sort their own ranks, then unranked rows interleave by last
  activity so a stale global cannot sit above a ranked topic that just ran.
  Pinned order is `pinned_at`. A folder without an override follows
  the open conversation (or a selected empty project); garbage storage is
  an empty map. Pinned / Projects / Recents default open; a remembered
  fold is the three booleans in `zwai.sidebar.section-expanded`. A
  sidebar group preview keeps the first five conversations active in
  the last seven days; older or extra rows sit behind Show more unless
  they are the open or running conversation.
- **`src/lib/sidebar-width.ts`**: missing or garbage storage is the default
  column, out-of-range values are clamped, a live drag paints
  `--zwai-sidebar-width` without writing storage, and a commit (pointer up
  or arrow key) is what remembers the width.
- **`src/components/app/resize-handle.tsx`**, rendered in jsdom: a right-edge
  strip widens on ArrowRight; a left-edge strip widens when the separator
  moves left (the right panel). A pointer drag does not start a text
  selection. Width stops at the min and max. Keyboard (and pointer up) fire
  an optional commit so the parent can skip React state until the gesture
  ends.
- **`src/components/app/header.tsx`**, rendered in jsdom: the title bar always
  has a Hide/Show conversations control, pads for traffic lights on desktop,
  sizes the leading cluster from `--zwai-sidebar-width` while the list is
  open so the title starts with the transcript, is marked as window chrome so
  a double-click can zoom, and a conversation in a project prefixes the title
  with the project name on the same line — which directory the tools are
  pointed at is otherwise invisible. The model name is not repeated here; it
  lives on the composer. A running turn counts from `status.started_at`
  (hours included); a missing start time is `Working` with no invented 1s.
  A width control flips `comfortable` / `full` the same way the theme
  control flips light / dark; `aria-pressed` is on while the column is wide.
- **`src/lib/settings-persist.ts`**: edits coalesce into one `PUT` after
  400ms; `flush` writes immediately; a failed write does not block the next.
  The `ui` object always carries locale, font, size and column width so a
  swarm edit cannot reset General.
- **`src/lib/appearance.ts`**: chrome tokens (`system`/`serif`/`mono`,
  `small`/`medium`/`large`, `comfortable`/`full`) map to CSS variables and
  `data-*` attributes. Junk becomes the current defaults. A `localStorage`
  cache paints the first frame; `GET /api/meta` is the source of truth.
  `toggleContentWidth` is the title-bar / ⌘K flip. `--content-gutter`
  shrinks in `full` so the column sits against the sidebars.
- **`src/components/app/settings-dialog.tsx`**, rendered in jsdom: Settings
  is a full-page sheet (`h-dvh`) with **Back to app**, a labelled search
  box, and a left rail of tabs. There is no Save/Cancel: edits debounce into
  `PUT /api/settings` (locale rides along) and **Back to app** flushes.
  Searching a Swarm-only word jumps to that
  section and hides Models. The Personality tab writes personal preferences
  into `personality.instructions`. The Swarm tab exposes compact keep-messages,
  the auto-compact token budget. Every page scrolls with `overflow-auto` and
  bottom padding, so an outline at the
  end of the tab (Add a provider) is not clipped. `overflow-y-auto` is
  rejected because it would leave the shared `overflow-hidden` in place.
  With `trafficInset`, an `h-12` `data-drag-region` strip sits above the
  rail so **Back to app** is not a descendant of that chrome (macOS
  traffic lights live there); a browser sheet has no strip. The sheet is
  `modal={false}` so Radix does not `aria-hide` the conversation underneath.
- **`src/components/app/settings-field.tsx`**, rendered in jsdom: a row
  puts the label left of the control; a query hides non-matching rows;
  a section with no remaining rows is omitted.
- **`src/lib/file-tree.ts`**: a flat workspace listing becomes a nested tree
  (missing parents are synthesised), filtering keeps ancestors, collapse hides
  children unless a query is forcing matches open, and a unique directory
  chain starts expanded while a repository-shaped root stays collapsed.
  Keyboard actions are pure: arrows move, Right expands, Left collapses or
  walks to the parent, Enter opens a file.
- **`src/components/app/files-tab.tsx`**, rendered in jsdom: the filter is
  labelled, a repository listing starts collapsed, clicking a folder toggles
  it, a query surfaces a nested file without expanding first, download/delete
  stay off directory rows, deleting a file asks first, and the tree takes
  arrow keys.
- **`src/components/app/panel.tsx`**, rendered in jsdom: opening a sub-agent
  puts the back row *beside* the scroller, not sticky on top of it — dragging
  the transcript must not paint through the back button. The worker log
  lands at the latest line, re-pins when switching workers, follows tokens
  at the live edge, and a wheel-up leaves the viewport put until Jump to
  latest. A prompt control opens the worker's recorded instruction and is
  absent when the event only stored the role name. The roster lists
  `agent_id` next to the role so two workers with the same name are
  distinct. The resize strip sits above that chrome (`z-20`) and the arrow
  keys still change the width. A pointer drag on the strip must not start a
  text selection. The Files pane is a flex column (`overflow-hidden`) so the
  filter stays put while the tree scrolls. Inactive Files/Agents panes are
  `hidden` when Memory is selected, and the active pane is an opaque
  `bg-card` so a leaked flex sibling cannot paint through the skill list.
- **`src/lib/selection.ts`**: while a gesture holds the pointer, existing
  ranges are cleared and `selectstart` is cancelled, then the previous
  `user-select` is restored. Dragging a panel over a transcript is a selection
  as far as the browser is concerned.
- **`src/lib/titlebar.ts`**: a double-click on the desktop title bar (not on a
  button in it) sends `wails:drag:doubleclick` so the native window zooms; a
  browser has no bridge and the click is left alone. Dragging is native and is
  not this module.
- **`src/lib/slash.ts`** and **`src/components/app/slash-menu.tsx`**: `/` as a
  token (start of the box, after whitespace, or after CJK — no space yet)
  is a menu; a space after the name makes it a submit. `foo/bar` and
  `https://` are not a menu. A known command name followed by a non-ASCII
  token is a submit too, so a CJK objective glued to `/goal` is not a user
  message, and a fullwidth `／` or IME punctuation `、` is still a slash
  (the composer rewrites those to `/`). `/plan` is in the same catalog.
  Filtering is
  prefix-or-contains, compact's hint is a percentage (hidden when used is 0),
  and unknown names are not commands. The menu is a listbox; the textarea
  owns the keys.
- **`src/lib/stream.ts`**, against a fake `EventSource`: every kind the server
  sends is subscribed to (including `goal_session`), a review that lands after
  `done` is delivered, the event name wins over a disagreeing payload, and a
  connection that dropped on its own is not reported as a failure. The first of
  those is a regression test: a kind missing from the list is stored, traceable,
  and invisible until the page is reloaded. Live session fold depends on this —
  a `goal_session` that only exists in the database looks like a TurnFooter
  until refresh.
- **`src/store/projects.ts`**, against a fake API: a project deleted elsewhere
  stops being selected, a slow memory response for a project that is
  no longer open is ignored (one project's notes under another's name is worse
  than none), a refused create or edit reaches the dialog so it can show the
  message against the field that caused it, and a memory reload copies the skill
  index onto the project so the Memory tab updates without a second listing.
- **`src/components/app/project-dialog.tsx`**, rendered in jsdom: a rejected
  working directory is shown under that field and the dialog stays open, a
  nameless project cannot be created, the instruction textarea uses the same
  `border-input` chrome as a one-line field, and the memory switch is disabled
  when the install has memory off.
- **`src/components/app/project-list.tsx`** and
  **`src/components/app/delete-project-dialog.tsx`**: the folder is not
  pressed (the open topic is `aria-current`), each row's menu is named after
  its project, a hover control on the row starts a conversation in that
  project (named after it, so it cannot collide with the list's New
  conversation), conversations nest under an expanded folder whose glyph
  is an open directory (closed when collapsed), a running topic's progress
  sits in that icon column, skills are opened from the row menu rather than
  listed under the name, the section does not add a second horizontal gutter
  on top of the sidebar scrollport, a sixth topic (or one idle longer than
  a week) sits behind **Show more**, dropping a project row on another reports
  the new id order (title drag, no grip), a name click still selects when a
  dragstart races it, and the delete dialog says that the conversations and
  the memory go too while the user's own directory does not.
- **`src/components/app/confirm-delete-dialog.tsx`**, rendered in jsdom: Cancel
  leaves the thing in place; the destructive button is the only path that
  fires onConfirm; a closed dialog is not in the document.
- **`src/components/app/memory-panel.tsx`**, rendered in jsdom: notes and their
  budget are shown, **Save notes** is absent until the draft differs from what
  is stored and leaves again after a save or a revert, a failed save stays on
  screen, a skill's body is fetched only when it is opened, a skill named by
  the sidebar is opened and fetched on arrival, an opened body stays inside
  its card (overflow clipped, the next skill still follows it in the list)
  rather than overlaying Files, notes that arrive from a
  review are adopted unless the user is mid-edit — in which case a conflict
  banner keeps what they typed and offers Reload — and a write that landed
  while the tab was closed is a badge, not a silent panel. Deleting a skill
  asks first.

## End-to-end tests

```bash
cd frontend
npx playwright install chromium   # once
npm run e2e
npx playwright test --headed      # watch it happen
npx playwright test e2e/shell.spec.ts -g "settings"
```

Playwright starts the real server itself (`go run ./cmd/zwai web --mock --no-open`),
so a run exercises the whole stack: gin, the engine, SQLite, SSE and the built
front end.

**The data directory is deleted before the server starts**, so every run is a
first install. This matters: a config file written by an earlier run has already
been normalized, which once hid a crash that only happened on a fresh one. The
wipe is part of the `webServer.command` rather than a `globalSetup` hook, because
Playwright starts the web server first and a hook would delete the directory the
server had already opened. For the same reason the suite does not reuse a server
that is already listening on the port. `ZWAI_MOCK_WORKER_DELAY_MS=1500` holds
each mock worker before its first tool call so `wait_agents` stays pending
long enough for Steer; unit tests leave it unset.

| spec | covers |
|---|---|
| `e2e/conversation.spec.ts` | a full swarm turn, a live thought in a 10-line scrolling box whose **Thinking** label sweeps, clicking that row hiding the thought while it still streams, a live status line marked as sweeping while the turn runs, opening a sub-agent (back control beside the scroller, not sticky on it; system prompt from the chrome; log at the live edge), a generated sidebar title after the first turn (not the raw request, not a transcript row), a heading rendered as a heading while the turn is still Working, a chart in the scripted answer with Chart/Table tabs (and after reload), scrolling up mid-stream leaving the viewport put and a jump-to-latest control returning to the live edge, switching conversations landing at the latest turn rather than the top of the history (latest jump-rail tick current), context carried across turns, jumping to an earlier user message from the left rail (latest tick current while idle at the live edge), Enter while `wait_agents` is pending queuing a follow-up until the turn finishes, editing a queued row and submitting it so that message goes to the back of the FIFO, **Steer** (⌘Enter while `wait_agents` is pending) pinning unread steering under the working line with Interrupt and Delete, retracting an unread steer so the turn stays Working, Interrupt aborting the current tool without cancelling the turn, **Stop** while a tool is in flight leaving no spinner next to the interrupted banner, quoting selected transcript text into the next send as an editable composer annotation, copying or editing a sent message in place so Send restarts from that bubble and clears everything below, file upload appearing in the Files panel with the user bubble naming `uploads/brief.txt`, collapsing a workspace directory in Files and filtering to a nested file, dropping a file and an image onto the composer (overlay, then a workspace chip vs a vision thumb), the turn id on the Trace summary with the event log folded until Full log, an IME-confirming Enter leaving the draft in the box, the manager tool-round cap pausing for Continue/Stop instead of dumping eino's iteration error, and switching the catalog model from a grouped searchable picker (Refresh models / Edit providers) so a reload still sends that name, and the composer context ring plus Trace usage after a turn (reload keeps the ring; the snapshot never lands as a transcript row), `/` listing goal, plan and compact without a 0% hint on an empty chat, pinning a standing objective, starting it from the banner without a human message, editing it in place, compacting without rewriting user bubbles (an icon opens the briefing), auto-compacting at a low token budget with a visible compressed notice and the same briefing icon, a scripted run with a goal finishing as Done, and a one-round ReAct slice leaving a standing objective running until Done instead of pausing it as two Worked-for sessions, `/plan` showing a Planning banner and an `ask_user` dialog (a numbered choice then Submit continues the same turn), then Implement remounting work and leaving planning |
| `e2e/projects.spec.ts` | a project created from the sidebar, a conversation started from the project row that says so with the project name prefixing the title on one line, the review named in the transcript without opening a tab, **View skills** on the project menu opening the Memory tab with that skill expanded and in view (body inside its card, not over Files), the notes in the panel without a reload, the review in the same Full log as the turn, a second conversation starting with the first one's memory, a hand-edited note surviving a reload (Save notes absent until the draft changes), a deleted project taking its conversations with it after a confirm, the Memory tab not leaving a blank Agents pane above the notes or clipping Skills off the window or painting inactive Files beside Memory, Review now saying when there is nothing to review, hovering a project row revealing a new-conversation control that starts one in that project rather than Recents (the folder is not pressed; the open topic is `aria-current`; the folder glyph is open when expanded and closed when collapsed; a running conversation's progress sits in that same icon column; topic names sit under the project name; there is no drag-grip glyph), pinning a project topic to the top across reload, dragging a project pinning that order across reload, and a sixth topic in the folder sitting behind **Show more** until it is opened |
| `e2e/shell.spec.ts` | keyboard shortcuts (including hiding the conversation list, `⌘F` find in the conversation, and `⌘J` / the title-bar terminal opening a PTY in the conversation workspace — and in a project's working directory when the conversation belongs to one), dragging the conversation list and the side panel without selecting transcript text (the list width is remembered across reload and the title-bar leading cluster tracks it), the composer sitting on the transcript with a fade instead of a dock hairline, Projects and Recents sharing one left gutter (conversation titles in the icon column), collapsing Recents so its conversations stay hidden across reload, an external link opening a new window instead of replacing the app, the tool catalogue on a never-saved config, settings written to the config file and read back, personality round-tripping through Settings → Personality, pinning a title-generation model when more than one name is listed, opening a collapsed provider row then discovering models into the default-model dropdown, **Back to app** remaining on screen on a short window when the Swarm page is long, Back to app sitting in the first 48px of a browser sheet (the desktop title-bar strip is not shipped to the tab), the Add-a-provider outline staying inside the Models scrollport, theme switching persisted, chrome language switching (restored to English because locale is in the shared yaml), the title-bar width control filling the pane in wide mode and restoring the reading column (also persisted), font / size / conversation width round-tripping through Settings → General, renaming a conversation and deleting it after a confirm, and dragging a Recents conversation pinning that order across reload |

E2E tests run against `frontend/dist`. `make e2e` rebuilds that bundle first;
if you invoke `npm run e2e` directly, run `make frontend` after changing
anything under `frontend/src` or you will be testing the previous bundle.

The progress pulse has no positive E2E assertion on purpose: even with the
worker pause, a scripted turn usually finishes near the 5-second default
interval, so a test that waited for a pulse would pass or fail on timing. The
suite asserts instead that a finished turn leaves no pulse behind — the
regression that actually bites is an unhandled event kind landing in the
timeline as a raw row, or the heartbeat outliving the answer. That pulses reach
a client at all is verified in the engine and reducer tests.

## Manual checks that no automated test replaces

- **The desktop window**: `go run ./cmd/zwai desktop`. Check that the window
  opens, the UI loads, a turn runs, an upload opens a native file dialog, and
  "Show in Finder" works. A link in the transcript must open the system
  browser, not replace the window. The Dock / taskbar should show the rounded
  app mark at the same size as a bundled `.app`, not a square canvas, not a
  full-bleed tile, and not the generic Unix-exec glyph.
  Double-click the title bar: the window should zoom,
  then restore on the next double-click. On macOS the traffic lights and the
  sidebar toggle must share one baseline in that title bar; New conversation
  starts on the row below. Opening Settings, **Back to app** must sit on the
  row under the lights, not in the drag strip. ⌘Q should return the shell promptly without
  `Window #1 not found` warnings. Wails cannot be driven by these tests.
- **CJK IME**: leftover Latin confirmed with Enter must stay in the composer;
  the next Enter sends. The desktop window is WebKit, which fires
  `compositionend` before that key — Chromium unit tests cannot replace a real
  input method.
- **A real model**: the offline provider proves the plumbing, not that a prompt
  works. Run one real conversation before shipping a change to the manager
  prompt or the toolset.
- **A UI change**: look at it, in both light and dark, before and while a turn
  runs. The panel, the transcript and the composer all change shape mid-turn.

## When a turn misbehaves

Reproduce it, then read it:

```bash
zwai trace <turn-id>          # the timeline plus every model call
zwai trace <turn-id> --full    # untruncated text
zwai tui                       # interactive; type a task, then enter
zwai tui --goal "..."          # standing objective; starts immediately
zwai tui --task "..."          # the same swarm, one-shot, no UI in the way
```

Raise `log.level` to `debug` in the config for one line per HTTP request and per
event-stream decision. If a turn is slow, the model-call table at the end of
`zwai trace` usually shows why: `input_msgs` growing, billed tokens climbing,
or one call eating the wall clock.

## Adding tests

- New Go code needs unit tests with it, in the same package, above 90% of the new
  statements. If a function cannot be tested, split it until the untestable part
  is a two-line wrapper.
- A new endpoint needs a test for its success path **and** its refusals — a `404`
  for an unknown conversation, a `409` when busy, a rejected path traversal.
- A new event kind needs a reducer test, and usually an assertion in
  `TestNotifyKindsAreStableAcrossTheWire`: the names travel over the network and
  are stored in the database, so renaming one breaks replay of old conversations.
- A new user-visible flow needs an E2E test. If it cannot be driven in the
  browser, it probably cannot be driven by a user either.
