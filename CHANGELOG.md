# Changelog

All notable changes to this project are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project has not
tagged a release yet.

## [Unreleased]

The repository grew from a swarm orchestration library into a desktop
co-working app built on it. The library API is unchanged except where noted
(`Restore`, `PlantFinished`, `RunConfig.RestoreWorkers` / `FinishedWorkers`,
`SetMaxConcurrent`).

### Added

- **Scheduled inbox, wake banner, and Swarm caps.** The sidebar **Scheduled**
  control (`data-testid="schedule-inbox"`, `aria-haspopup="dialog"`) opens a
  dialog: list waits (pause/resume/cancel/Run now), create a standalone job
  (title, prompt, exactly one of delay / interval / cron, optional project),
  and open unread findings in that fire's conversation. Unread counts belong
  in the trigger's accessible name, not only the badge. An active thread wake
  on the open conversation gets a composer banner (next check + cancel). An
  armed `schedule` notice Cancel is `DELETE /api/schedules/:id`. Settings →
  Swarm edits `schedule_min_interval_seconds` / `schedule_tick_ms` /
  `schedule_max_active` (defaults 30 / 1000 / 32) as part of the whole swarm
  object so a PUT cannot zero the Go struct. Chrome strings are in `en`/`zh`.
  Run-now while busy sets a localized error (`skipped_busy`) inside the inbox
  dialog.

- **Scheduled-task inbox HTTP API.** Same-origin `GET/POST /api/schedules`,
  get/patch/delete one wait, `POST /api/schedules/:id/run` (202, even when
  `next_run_at` is still future), and `POST /api/schedules/runs/:rid/read`.
  List returns `{schedules, unread}` where `unread` counts runs with
  `unread=true`. Run-now while the target conversation is busy is `409`
  `code: skipped_busy`. The web client folds `schedule` / `schedule_fired` /
  `schedule_skipped` / `schedule_report` / `schedule_cancelled` into chips
  (quiet reports drop that turn's chat bubbles and keep Trace), and
  subscribes to those kinds live so a kind missing from `KINDS` cannot sit
  stored-and-invisible until reload.

- **Quiet scheduled checks archive like Codex.** Empty `report_schedule`
  findings, or a finished scheduled turn with no answer, close the run as
  `quiet` (unread cleared, `turn.quiet`). A manager answer without a report
  is `findings` and unread. Standalone quiet fires hide the minted
  conversation from Recents; findings stay in the sidebar. A crashed or
  cancelled scheduled turn marks the run `error` and unread so it cannot
  stick `running`.

- **Pending thread wakes pause `/goal` auto-continue.** An active
  `kind=thread` wake targeting this conversation, or a claimed fire still
  `running`, is the next turn: `continueGoal` reaps parked workers and
  returns, including a one-shot delay that is still due. Cancel restores
  auto-continue on the next clean pursuing finish. Paused, cancelled,
  done-with-no-run, and standalone origin-only rows do not suppress.

- **Manager schedule tools.** The manager can arm a wait on this conversation
  (`schedule_wake`; optional id upserts instead of minting a second), arm an
  independent job on a human-originated turn (`schedule_task`), cancel by id
  (`cancel_schedule`), and report a scheduled check (`report_schedule`; empty
  findings return `{ok,quiet}`). Workers get a JSON deny stub. The ticker
  that fires due waits is not in this change.

- **Waiting on the manager prompt.** When progress is gated on time or a
  condition that is not worth polling in this turn, the manager is told to
  call `schedule_wake` and end the turn — not to spin, block a tool, or wait
  for the human to remind it. Open wakes for this conversation land in extra
  (`## Scheduled`: id, next, cadence type, prompt head) so it can upsert.
  `schedule_task` only when the human asked, or after `ask_user`. A scheduled
  turn reports through `report_schedule`.

- **Live `exec` output.** While a shell command still runs, Web and TUI stream
  stdout/stderr into the pending tool row (`tool_delta`, broadcast only, keyed
  by `tool_call_id`). The model still gets one JSON `tool_result`. A `\r` in
  the output overwrites the current line. The pending row starts open.

- **Show more on long sidebar groups.** A project folder and Recents
  show the five conversations active in the last seven days. The rest
  sit behind **Show more** (and **Show less** folds them again). The
  open or running conversation stays visible so More cannot hide the
  row on screen. Pinned is unchanged.

- **Same-turn interrupt injection.** Unread steering under the working line
  can **Interrupt** the current manager tool or generate (workers stay up)
  so those nudges land on this turn, or **Delete** one bubble so the model
  never sees it. `POST /api/threads/:id/preempt`, `DELETE …/steers/:seq`,
  events `steer_preempted` / `steer_retracted`. Stop is still the turn
  cancel.

- **In-turn retry of a recoverable model error.** A truncated tool-call JSON
  (`400 Unterminated string`), a `429`, or a dropped stream used to fail the
  turn and `goal_blocked` on the first hit. The manager now re-enters the
  same turn (drops the invalid arguments so they are not sent back, records
  `model_retry`) twice before the turn fails. A real refusal still blocks
  immediately.

- **Interactive questions (`ask_user`).** The manager pauses this ReAct turn
  with a multiple-choice card (1–3 questions, host-injected Other). Web,
  desktop, TUI and `--task` can answer; workers cannot. Status is
  `awaiting_answer`. Composer Enter is Other, not a follow-up. A crash
  resume re-arms the questionnaire. Piped TUI stdin fails the tool instead
  of hanging.

- **`/plan` before changing anything.** A conversation-level planning mode
  unmounts write/edit/exec (and similar). The manager explores, asks, and
  writes `$ZWAI_HOME/plans/<thread>/PLAN.md`. Edit the banner, then
  **Implement** to remount those tools and start the work. Entering plan
  pauses an open `/goal`; Implement does not resume it. TUI: `/plan`,
  `/implement`, `zwai tui --plan`.

- **Compact briefing on demand.** The compressed-context notice stays a
  one-liner (token counts, transcript unchanged). An icon opens the
  briefing later turns will see, instead of pasting it into the chat.

- **Integrated terminal.** Title-bar icon (and ⌘J) opens a bottom PTY in
  the current conversation's workspace — the project's directory when the
  conversation is in one. Each click starts a new session; the server
  resolves the path, the client cannot pick one. Same-origin WebSocket,
  eight sessions per process.

- **Charts in answers.** When two or more comparable quantities would be
  easier to see as a plot, the manager emits a fenced `chart` block
  (bar, line, area, pie JSON), keeps the prose to the takeaway, and is
  told not to restate the same series as a list, a markdown table, or
  emoji. The transcript paints the plot and keeps the rows as a table
  behind a tab. No new event kind — the fence lives in the assistant
  markdown.

- **`Registry.SetMaxConcurrent`.** Resizes a live swarm's worker gate so
  waiters see a new cap without a restart. Assigning `MaxConcurrent`
  after the first spawn still does not wake them.

- **Tail-first conversation history.** Opening a conversation fetches
  `GET /api/threads/:id/log` for one viewport of the live edge, then
  the event stream resumes after that seq. Scrolling up pages older
  events. The left jump rail still lists every human turn.

- **Rolling session memory.** A conversation-local briefing is refreshed
  from the event log (token/tool breakpoints, or when compact is about
  to run) and recorded as `session_memory`. `/compact`, auto-compact,
  and a `/goal` session cut copy that view instead of re-summarizing a
  folded ADK transcript. The chat ignores the event the way it ignores
  a generated title.

- **Wide layout from the title bar.** The conversation still defaults to
  the reading column. A title-bar control (and ⌘K) switches to a wide
  layout that fills the space between the sidebars. Same
  `ui.content_width` as Settings → General.

- **Global personality.** **Settings → Personality** stores install-wide
  preferences (tone, language habits) in `config.yaml` and adds them to
  every manager system prompt, including `zwai tui`. Empty omits the
  section. A project's instruction is the business context and wins on a
  conflict. Sub-agents do not see it.

- **Interactive `zwai tui`.** No `--task` opens a composer and keeps the
  session, like the other terminal CLIs. Enter sends the next turn on the
  same transcript; `ctrl+c` leaves. `--task` is still the one-shot path
  that starts immediately and exits when that run finishes. The idle
  composer parks the real terminal cursor at the insert point so CJK
  IME preedit follows the committed text. Typing `/` opens a Codex-style
  command popup (rounded surface, name + description, prefix filter,
  `/model` and `/reason` pickers, `/help` / `/clear` / `/exit`).
  `shift+tab` still cycles thinking level; `--model` / `--reasoning` set
  the same choice for a one-shot run.

- **Sub-agents can read a project's memory.** On a project with memory on,
  workers receive the notes snapshot, the skills index, and `skill_view`.
  They still cannot call `memory` or `skill_manage`. Their final message
  is the task result; they may append a short durable convention or
  procedure for the manager to store. Most tasks have nothing to add.

- **Memory quality is enforced in the tools, not only in the reviewer prompt.**
  `skill_manage` create that collides with an existing skill (edition suffix,
  shared summary, copied body) is refused and names the skill to patch.
  `memory` add/replace refuses a note longer than `memory.entry_max` (default
  360) and a note that restates a recorded skill — the summary or the
  steps. The Memory panel's editor still uses the total notes budget only.

- **`/goal` work sessions.** A standing objective no longer welds itself
  into one turn until 200 manager rounds. Each pursuit turn ends when
  the manager stops calling tools. `swarm.goal_session_max_iterations`
  (default 40) is eino's ReAct slice: hitting it extends the same turn
  without a confirm, a session cut, or spending `goal_max_auto_turns`.
  Context at or above
  `swarm.goal_auto_compact_percent` (default 80) is compacted first.
  In-flight sub-agents are parked across sessions instead of killed.
  Finished session turns fold in the transcript the way Codex's
  "Worked for …" rows do. Goal pursuit no longer pops the
  `max_iterations` confirm. Sub-agents spawned during a session
  survive manager-context cancel (`Handle.Cancel` / `Cleanup` /
    `Close` still stop them).

### Fixed

- **Scheduled is a dialog trigger; busy run-now errors show in the inbox.**
  The sidebar control used a collapsed `SidebarSection`, so a screen reader
  heard a fold and the chevron lied. It is now `aria-haspopup="dialog"` with
  `aria-expanded` tied to the inbox, no chevron, and unread in the accessible
  name (`Scheduled, 2 unread`). Run-now `skipped_busy` used to set `store.error`
  behind the dialog overlay; the dialog now shows that string as a labelled
  `role="alert"`, and closing the inbox clears it.

- **Empty `done` only quiets fired scheduled turns.** The reducer used to
  treat any notice on the turn as `schedule_fired`, so arming a wait,
  `goal_continued` / compact, or a findings `schedule_report` plus empty
  `done` hid the chip. Quiet now requires `schedule_fired`, empty
  `done`, and no findings report on that turn.

- **A scheduled check with no `schedule_report` and an empty `done` hides
  the fired chip.** The engine already archives that turn as quiet; the
  transcript reducer only quieted empty `schedule_report` payloads, so
  the "Scheduled check." notice stayed. Empty or whitespace `done` after
  a fired chip now marks the turn quiet and drops its chat bubbles.
  Non-empty `done` is findings. Ordinary empty `done` is unchanged.

- **Inbox create and the schedule ticker share one frozen cap.**
  `StartScheduler` already snapshotted `schedule_max_active` /
  `schedule_min_interval_seconds` (and now the default provider) so a
  `PUT /settings` `Replace` cannot race the tick. Create, resume, field
  patch, and schedule tools used to read live `cfg.Swarm`, so a cfg write
  without `ApplyLiveSwarmLimits` let the inbox arm more waits than the
  ticker would run. Those paths now use the same helpers; Settings still
  refreshes the snapshot.

- **A leftover scheduled turn that cannot restart closes its run.** Resume used
  to `FinishTurn` a superseded leftover or an unresumable `ScheduleContinue`
  row and leave the bound fire `running`, so `HasRunningRun` never cleared
  and `CountRunningRuns` held a cap slot. Those paths now call
  `finishScheduledRun` (`error` / unread). The live `run()` hook also closes
  the fire when `FinishTurn` itself misses (deleted thread).

- **A claimed one-shot still pauses `/goal` auto-continue.** Claim marks
  the delay `done` and inserts a `running` run before `StartTurn`; looking
  only at `status=active` let `continueGoal` steal the turn (`ErrBusy`,
  no resurrect).

- **`schedule_task` stays blocked after resume of an implement-plan turn.**
  `occupy()` wipes the in-memory plan flag; the gate now also reads
  `plan_implemented` on the turn, so a leftover execute turn cannot mint
  a standalone job.

- **A generated conversation name survives the `done` list refresh.** The
  title-bar used to snap back to the truncated prompt when `GET /threads`
  still had `title_auto` after the `title` event had already named the row.

- **Slash menu follows the `/` token, not column 0.** Typing `/` after
  existing text (or CJK with no ASCII space) opens goal/plan/compact the
  way Cursor does. `foo/bar` and `https://` stay ordinary text. Escape
  drops only the `/query`.

- **CJK IME Slash key now opens the command menu.** With Chinese punctuation
  on, that key inserts `、` (or `／`) instead of `/`, so the palette never
  appeared. The composer rewrites those runes to `/`; TUI and `StartTurn`
  parse them the same way.

- **Unread steering survives a manager re-entry.** `RunWith` used to wipe the
  inbox at the start of every slice, so a steer queued before the next
  generate vanished from Interrupt and Delete while the pin stayed on
  screen. The inbox is registry-lifetime; only the transcript snapshot
  resets.

- **Interrupt after the last generate no longer cancels the next turn.**
  `Preempt` with no live epoch armed the next `bindEpoch`. `TakePreempt`
  now clears that pending cancel, and a successful run with unread
  steering still re-enters this turn.

- **⌘Enter during `ask_user` is a steer, not Other.** Goal edits and
  follow-up promotions were also being stamped onto every question.

- **A late answer after Interrupt cannot auto-fill the next card.** The
  stash only applies to a call that has not finished waiting.

- **A cancelled generate is not recorded as a finished answer.**

- **Interrupt during generate no longer deadlocks the stream.** Closing the
  inner reader while `Recv` was blocked does not unblock eino's pipe, and
  on a copied stream it nests `sync.Once`. The wrapper waits for the
  provider to EOF (it already sees epoch cancel) and then maps that to
  `context.Canceled`.

- **Lowering `max_concurrent` tests keyed waiters by role.** `ModelBuilder`
  runs after the gate, so two live workers racing an increment used to
  close the wrong release channel and hang `Done`.

- **PTY `ready` is on the copy loop as well as the HTTP upgrade.** Tests
  that drive the WebSocket without `Start` still get the cwd frame; the
  live upgrade still sends it before fork so a refused PTY can name the
  tab, then `error`.

- **PTY origin is loopback, not `Host`.** A DNS-rebind page whose Origin
  host matched `Host` used to upgrade. Blank Origin is allowed only from
  a loopback peer. Mutating `/api` with a non-loopback Origin is `403`.

- **Escape in `/plan`, ask Other, and banner editors no longer Stops the
  turn.** Capture-phase Escape ignored those fields.

- **`ask_user` sets Waiting immediately**, so Enter answers instead of
  queuing a follow-up that can never run. A resumed question stays
  pending. `plan_implemented` starts the working clock.

- **Scrolling up a long conversation now loads the older page.** The top
  sentinel's IntersectionObserver fires ~80px early; the hook used to
  ignore that and never listen for `scroll`, so dragging to the first
  loaded row showed empty space. Sentinel intersection pages now, a
  scroll to the top pages even after that first shot, and a prepend
  does not yank the reader back down if they already reached 0.

- **Integrated terminal would not start a shell.** `Setpgid` was set on
  the PTY command; `creack/pty` also sets `Setsid`, and Darwin refuses
  both (`fork/exec … operation not permitted`). The session is the
  process group; Close still kills `-pid`.

- **/plan and ask_user reach the server.** The composer and ask card already
  called store actions that were missing, so the bundle would not typecheck.

- **Empty textareas look like fields.** The shared `Textarea` had no
  border, so New project's instruction (and Memory notes) looked like
  leftover whitespace. It now uses the same `border-input` chrome as
  `Input`. Nested uses (composer, message edit) keep the outer chrome
  only.

- **Composer card kept its top-left border.** Giving `Textarea` a default
  fill left the composer input opaque. That square background overflowed
  the rounded card and painted over the top corners. The nested field is
  transparent again, same as message edit.

- **Raising sub-agents-at-once takes effect on workers already queued.**
  The first spawn used to mint a buffered semaphore under `sync.Once`,
  and a parked `/goal` registry kept that cap until a restart. Settings
  now resizes the live gate, so extra slots open immediately; lowering
  it does not kill in-flight workers.

- **`wait_agents` no longer reports leftover workers as unknown after a
  restart between `/goal` sessions.** The next turn used to create an empty
  registry and drop previous spawn results from replay, so ids from the last
  session vanished. Finished workers are planted from the conversation event
  log, still-running ones are restored under the same ids, and a continuation
  re-pins those spawn ids. A `cleanup` kill plants as stopped rather than
  restarting the worker.

- **Add to chat no longer vanishes while a turn is streaming.** Selecting
  transcript text used to flash the pill then hide it: auto-follow (and a
  live thought's inner scroll) fired `scroll`, and token replacements
  collapsed the native selection. The snippet is snapshotted until you
  add it, click away, wheel, or press Escape, and selecting unpins follow.

- **Parked `/goal` workers no longer look finished while `wait_agents`
  is still blocked.** A session yield used to paint leftover sub-agents
  Done (and freeze their in-flight `exec`) because manager `done` was
  treated as cleanup. They stay running until a real `finished` or a
  `cleanup` kill; the wait card no longer counts a finished row as
  someone still being waited on.

- **Parked `/goal` workers keep recording after the manager returns.**
  `RunWith` used to restore a nil notification sink (and nil spawn/finish
  hooks) the instant the manager stopped calling tools. In-flight
  sub-agents kept running, but tool rows and `finished` fell on the floor
  — Agents stayed on starting with an empty pane, and the next session
  spawned replacements. `SetHostNotify` plus default lifecycle emitters
  keep that gap audible. Opening a worker whose tools sat above the live
  edge still fetches `GET /agents/:agent/log` even after paging claimed
  the log was complete.

- **Opening a long conversation no longer empties the Agents tab.** The
  live-edge log page is often only recent manager tools; `spawned` /
  `finished` had fallen out of that window, so the panel said there were
  no sub-agents. That page now carries those roster rows as a sidecar
  (without moving the paging cursor).

- **Opening a long `/goal` no longer replaces the transcript with a wall
  of "Started" rows.** The roster sidecar used to fold every historical
  `spawned` into the manager chat, so paging stopped at those names and
  the actual work (and each worker's tool log) looked gone. Sidecar
  rows now hydrate the Agents tab only. Opening a worker whose tools
  fell out of the live-edge viewport fetches
  `GET /api/threads/:id/agents/:agent/log`.

- **Switching into a running conversation no longer shows the empty-state
  idea cards.** The live-edge log page is often only worker tool rows;
  that used to look like a brand-new chat (while the roster still listed
  the workers). Opening now pages until a manager row exists, and the
  welcome pane stays off while a turn is running, workers are on the
  roster, or older history is still above the tail.

- **Opening a sub-agent lands on its latest line.** The Agents tab log
  used to paint from the first tool call. It now follows the live edge
  the same way the conversation does (and offers Jump to latest after a
  wheel-up).

- **A failed `/goal` turn no longer hides why it stopped.** The banner
  stored a generic `the last turn failed` and the session row still said
  工作了, with the error inside a collapsed fold. The banner now shows the
  public turn error (the sentinel is localized for older conversations),
  a failed session stays open as 停止于, and a collapsed preview prefers
  that error over the last answer.

- **`/goal` Start is resume, not a gap between sessions.** The banner Play
  control only appears when the objective is Paused or Blocked. A pursuing
  turn that just ended still shows 进行中 and auto-continues; Stop pauses
  the goal (`goal_capped`, `{reason:"interrupted"}`) so Start can resume it.

- **Switching conversations lights the latest jump-rail tick.** The
  opener follows the live edge; measuring at scrollTop 0 before that
  jump used to leave the first tick current.

- **`/goal` session wrap-up stays off the transcript.** The cue 30s
  before the time cap is still delivered to the in-flight manager, but
  it is no longer recorded as a `steer` (or a stored user message), so
  it cannot show up as a 引导 bubble. Replay of older sessions hides
  the same text. An unread wrap still cannot become a human turn.

- **A compact briefing that is a transcript dump is refused.** The
  summarizer used to persist a `Tool:`/`Human:`/`Assistant:` replay or
  pasted exec JSON as `compact_summary` / `session_memory`. That briefing
  is now rejected: the thread fields stay, the event carries `err`, ADK
  state is not folded.

- **Session memory on a long `/goal` no longer tries to swallow the whole
  event log in 15s.** Refresh is incremental and newest-first under a rune
  cap (tool results clipped harder than answers), on the provider idle
  timeout. A failed refresh does not stamp `through_seq`. A failed or
  refused refresh does stamp `session_memory_tokens`, so auto-compact
  cannot resend the same payload on every Generate. `/compact` and
  auto-compact summarizer input is capped the same way. A stored
  transcript dump is omitted from the manager prompt.

- **A `/goal` session cut hands off usable state.** Wrap-up manager
  answers are copied into session memory; heat is `goal_auto_compact_percent`
  of `min(model window, auto_compact_tokens)` rather than 80% of a
  million-token window; a moved session briefing still folds when the meter
  is cold. The wrap cue names `block_goal` when the same obstacle was
  retried; it does not call the tool itself.

### Changed

- **A finished scheduled turn now closes its run.** The ticker used to leave
  `schedule_runs.status=running` after `FinishTurn`, so `HasRunningRun`
  could stick until the next crash. Quiet, findings, and error are written
  on the way out.

- **An open `/goal` no longer promises an immediate auto-continue after every
  wait-turn.** A pending wake is the next turn until it fires.

- **`ask_user` is a question dialog, not a chip row.** Numbered choices,
  a radio list, Other only after that row is picked, then Submit. Same
  API; the empty textarea under every card is gone.

- **A recoverable ChatModel failure no longer dumps `NodeRunError` on the
  banner.** Truncated tool JSON that still fails after in-turn retries
  becomes a one-line public error; the graph path stays in Trace.

- **`/goal` turns end when the manager stops calling tools, like Codex.**
  There is no wall-clock session timer. `swarm.goal_session_max_seconds`
  is gone; a leftover YAML key is ignored, and the next Settings save
  drops it. Historical `goal_session` events still replay. A continuation
  that finishes with no counted tool activity records `goal_idle` and
  stops auto-continue until a human message or Start. `zwai tui --goal`
  holds the same way instead of looping until `goal_max_auto_turns`.
  `block_goal` now requires
  the same genuine blocker on at least three consecutive turns.

- **`/goal` is a slash command at every layer, not a user task.** Codex
  `parse_slash_name` cuts the name at whitespace, so a CJK objective glued
  to `/goal` (IME never inserts that space) became an unknown name and a
  chat line. The parser now splits on the ASCII identifier, accepts the
  fullwidth solidus `／`, and the engine intercepts `POST /turns` the same
  way Codex dispatches `SlashCommand::Goal`. Idle starts the turn; a live
  turn is steered. `zwai tui` `/goal` is in the catalog and sends the
  objective, not the slash line.

- **`/goal` tool-round slices stay on the same turn.** Hitting
  `swarm.goal_session_max_iterations` no longer records `goal_session`
  (`reason=iterations`) or spends `goal_max_auto_turns`. The same turn
  extends in place (no confirm card). Historical `reason=iterations`
  events still replay as a notice.

- **Memory extract stays on the append-only event log.** Post-turn
  auto-review reads this turn's events (and a clipped session briefing),
  never the compacted ADK transcript. If the manager already wrote with
  `memory` or `skill_manage`, auto-review is skipped; **Review now** still
  runs. Rewind clears a session briefing whose through-seq landed in the
  deleted range.

- **Auto-compact clears old tool results first.** Replayable catalog
  results (file bodies, listings) older than the last three are replaced
  with a placeholder before a full fold. Spawn/memory/lifecycle results
  stay. There is no idle Phase-2 distillation.

- **Compact streams the briefing, like Codex, and has no second timeout.**
  `/compact` and auto-compact used a one-shot Generate behind a swarm
  deadline. That second clock is gone: the briefing drains Stream, and
  silence uses the provider idle timeout (`timeout_seconds`). eino's
  summarizer still calls Generate — a wrapper streams underneath.

- **Manager prompt prefers proactive swarming.** Spawn when it would save
  time or improve quality, without waiting for the human to ask — Codex
  Ultra, not explicit-request-only. A one-worker wait is not a win (an
  extra hop, not a swarm). Solo remains the path for a greeting or a
  one-step lookup. Parallel workers still need distinct roles (one worker
  per role). An open `/goal` repeats that prior.

- **Auto-compact uses eino's summarizer.** The coding-agent default prompt
  is replaced via `UserInstruction`. Worker ids are re-injected from
  `spawned`/`finished` events after the fold, not kept inside compactable
  messages. The in-flight ReAct tail still stays in ADK state.
  `autocompact_invariants_test.go` fails the build if those come back.

- **`zwai tui --goal` starts immediately.** The objective is the first
  user message when `--task` is omitted. Requiring a second flag just
  to kick the first turn was a footgun. `--task` (or leftover words)
  still wins as the first line, and is still the one-shot exit path.
  After `complete_goal` / `block_goal`, an interactive `--goal` session
  returns the composer.

- **`frontend/dist` is a build artefact.** It is gitignored. A clone
  needs Node once (`make frontend`, or `make run` / `make e2e` which
  build it). `go:embed` still compiles because `dist/.gitkeep` stays in
  the tree; without `index.html` the binary serves the API only.

- **Project folders open and close as a directory.** An expanded
  project shows an open-folder glyph; a collapsed one shows a closed
  folder. The same icon column holds a running conversation's
  progress, so the dot lines up with the directory rather than
  trailing the title. Drag-to-reorder still works from the title
  (past 8px); the grip glyph is gone so it does not crowd the row.

- **Sidebar rows are shorter.** Conversation and folder rows are a
  fixed `h-7` with compact menus, instead of padding around a 28px
  button. Nested topics stack closer, the way the list is meant to be
  scanned.

- **Open conversation is the row, not the folder.** A project folder
  no longer paints as selected when one of its topics is open. The
  current topic keeps the accent, so it does not look like the
  directory is the thing you selected.

- **Pinned, Projects and Recents fold from the section header.** Same
  disclosure as a project folder (chevron after the name). The fold is
  remembered in `localStorage` so a reload is not a reset. **New project**
  stays on the Projects row and does not toggle the section.

- **`spawn_agent` is one worker per role.** A later `spawn_agent` with the
  same role does not mint a twin — including when the manager keeps going
  and nobody hit Stop. If that worker is still running, the new task is
  queued for its next turn; if it already finished, it continues in place
  under the same `agent_id`. `fork_context` only applies on the first worker
  for that role. `resume_agent(agent_id, …)` still targets a specific leftover
  sibling. The roster shows the id next to the role.

- **Composer typing no longer stalls during a live `/goal`.** Folded
  session turns stayed in the React tree, so every token re-created
  every past answer. A `usage` pulse (one per model call) re-rendered
  the controlled textarea and `height: auto` on IME preedit jumped the
  candidate window. Folded work stays unmounted; the ring reads usage
  in a child; the box does not measure itself while composing.

- **Closing a `/goal` mid-slice no longer asks to extend.** `complete_goal`
  or `block_goal` during a ReAct slice used to fall through to the non-goal
  confirm card (`max_iterations`) and stall the turn. The slice now ends
  cleanly; auto-continue stays off.

- **A `/goal` ReAct slice keeps tool results.** eino's iteration cap is a
  ChatModel preprocessor failure, so the history snapshot used to drop the
  last round's tool results and the next slice re-issued the same calls
  (workers never finished; the turn sat on Working). The transcript now
  records each result as it lands, `SetHistory` keeps wrap-appended
  results that the capped next step would wipe, and the engine stitches
  manager `tool_result` events into the next slice — inserting a missing
  result or refreshing a stale one when a later wait reused the same
  call id.

- **A short session-memory extract no longer refuses its briefing.** The
  rolling briefing compared length against an already-short extract, so
  a restatement of the last request (including `--mock`) stored nothing
  and auto-compact had no briefing to fold. Transcript-dump rejection
  still applies; length/tail checks stay on `/compact` of a long
  transcript.

- **A session-memory force refresh no longer re-summarizes its own trace event.**
  The `session_memory` row is recorded after the through-seq stamp. Treating
  it as new work made every `/goal` cut and auto-compact spend a second
  summarizer call on the briefing it just wrote.

- **`zwai tui` no longer swallows the live answer.** Finished text
  stayed folded to the first line, and a full pane cropped the bottom
  (where tokens arrive). Answers stay readable; the pane follows the
  live edge; a folded thought previews the last line they were watching.

- **Manager no longer treats a one-worker wait as a swarm.** Spawning a
  single sub-agent and then idling is an extra hop, not a time or quality
  win. The prompt now says so; a one-step lookup stays on the manager.

- **`zwai tui` uses the app manager prompt and workspace tools.** The
  typed task is the user message. The manager used to get only the five
  swarm tools and a system prompt that was the task itself, so it spawned
  a worker for a one-step lookup it could have run.

- **Opening Settings no longer stalls on a long conversation.** The sheet is
  not a modal Dialog (`hideOthers` walked every transcript node) and the
  conversation stays mounted without re-rendering on the click. `GET /api/settings`
  was never the wait.

- **A crashed `/goal` turn is blocked, not still Pursuing.** A `NodeRunError`
  (or any other failed turn) used to leave the banner on 进行中 and, when
  the crash was mis-read as a session yield, kick another auto-continue
  into the same failure. The objective is now `goal_blocked`, auto-continue
  stops, and the composer goes idle so the human can resume.

- **Pasting an image with a caption no longer crashes ChatModel.** OpenAI
  refuses to marshal `Content` and `MultiContent` on the same message.
  A vision send keeps the caption only in `UserInputMultiContent`, and the
  OpenAI client drops a leftover dual-set `Content` so a later concat cannot
  revive `[NodeRunError] can't use both Content and MultiContent`.

- **Collapsed `/goal` session rows stay one line.** Duration does not wrap
  (CJK used to split 「工作了」 around the chevron). The preview truncates
  with an ellipsis. The user bubble stays outside the fold; a session that
  just finished collapses itself.

- **TUI CJK IME no longer types at the front of the prompt.** bubbletea v1
  homes the hardware cursor to column 0 of the last line after each frame;
  IME preedit follows that cursor. The idle composer now re-parks the real
  cursor at the insert point and does not blink-repaint, so composition
  stays after the committed text.

- **A killed in-flight tool no longer keeps spinning after resume.** Force-quit
  still continues the leftover turn, but a `tool_call` that never got a
  result is closed first (`tool_result` with `err`) so the previous `exec`
  does not look live. Leftover sub-agents stay running; they are restarted
  under the same ids.

- **`send_message` hands a missed sibling to the manager.** An unknown or
  finished target no longer kills the worker (`NodeRunError`), and a worker
  that misses does not get a roster to retry against. The host is notified
  with the text (`notified: manager`); `send_message(manager, …)` is the
  same path on purpose. The worker finishes `done`; `wait_agents` reports it.

- **Working clock no longer freezes at 1s on a standing-objective auto-continue.**
  `done` cleared `started_at`; `goal_continued` / `goal_resumed` only set
  `running`, so the title-bar badge clamped a missing clock to 1s for the
  whole next turn. The banner's Pursuing timer was already right; this one
  now starts from the continue event.

- **Rewind does not hit a locked database.** SQLite is one connection
  for the process, so truncating a conversation while a turn is still
  flushing events no longer returns `SQLITE_BUSY`.

- **An unread /goal session wrap-up does not start a human turn.** The
  wrap steer is for the in-flight manager. If the time cap lands first,
  leftover wrap text used to become a new `user_message` and reset the
  auto-continue cap, so a short session never settled.

- **Compression no longer dies at 60 seconds.** Auto-compact and `/compact`
  wrapped the briefing Generate in a hardcoded minute, shorter than the
  provider idle timeout (default 5 minutes). A slow endpoint failed with
  `context deadline exceeded` while the UI still said compressing. Compact
  now streams and uses the provider idle timeout instead.

### Added

- **Auto-compact at a token budget.** When a manager call would exceed
  `swarm.auto_compact_tokens` (Settings → Swarm, default 80 000), older
  replay is folded into a briefing before the next Generate. The transcript
  is unchanged; the UI shows compressing, then the token counts. `/compact`
  still works by hand. The summarizer is eino's middleware with the
  task-agnostic compact prompt; Finalize keeps the in-flight ReAct tail
  and rehydrates worker ids from `spawned`/`finished` events. `TestCompactEffectComparedWithEinoDefault`
  is the keep-or-delete scorecard (structure, not briefing prose).

- **Font, size and conversation width.** Settings → General: system / serif /
  mono, small / medium / large, and whether the transcript fills the space
  between the sidebars or stays the current reading column. Preference is
  `ui.font` / `ui.font_size` / `ui.content_width` in `config.yaml` so a
  desktop window on a random port still remembers it.

- **Sub-agent system prompt.** Opening a worker in the Agents tab shows the
  instruction it was started with (host snapshot plus the task). The
  `spawned` event stores that prompt in `text`, so a reload, the Trace
  Full log, and `zwai trace` still have it. `Role` is still the role;
  hosts that printed `Text` as the name should switch. Conversations
  recorded before this only stored the role name and hide the control.

- **Read results follow the file suffix.** Opening a `read` paints the
  body with the same syntax tokens as `exec` — keywords, strings, comments,
  names — chosen from the path (`*.go`, `*.ts`, `*.py`, …). Markdown still
  renders as prose. An unknown suffix stays plain numbered lines; the
  highlighter never guesses a language from the contents.

- **Chinese / English chrome.** Settings → General, the title-bar **中 / EN**
  control, or ⌘K. Preference is `ui.locale` (`system` / `en` / `zh`) so a
  desktop window on a random port still remembers it. Agents keep answering
  in the language you are using.

- **Deletes ask first.** Conversations, projects, providers, workspace
  files, skills, and queued messages open a confirm. Cancel leaves them.
  Unsent composer chips (a file or quote not yet sent) still drop on
  the first click — those are not stored yet.

- **Copy and edit under a sent message.** Each user bubble grows a Codex-style
  row: the send time, copy, and a pencil that turns that bubble into an
  in-place editor. Send keeps that bubble, clears everything below it
  (`from_event_seq`), and starts again at that position — not a composer
  prefill, not a new turn on top. An image-only send has nothing to copy, so
  copy is hidden.

- **Conversation list is resizable.** Drag the right border, or focus the
  separator and use the arrow keys. The title bar's leading cluster tracks
  the same width so the conversation title still starts with the transcript.
  The width is remembered in the browser (or webview) the same way hide/show
  is.

- **Drag to reorder the sidebar.** Conversations and projects can be dragged
  by the title or the grip; a click still opens. The drop is persisted
  (`PUT /api/threads/reorder`, `PUT /api/projects/reorder`). A new conversation
  still lands on top.

- **Expanded exec shows the full command.** Collapsed tool rows stay one
  truncated line. Opening an `exec` wraps the whole invocation (heredocs
  included) and paints it with shell highlighting — keywords, strings, flags,
  variables — the way Codex does. Stdout stays below it.

- **Desktop Dock icon.** `zwai desktop` embeds the app mark, rounds it to a
  macOS squircle on Apple's 824/1024 grid, and hands it to Wails as
  `application.Options.Icon`. A `go run` binary has no `.app` bundle, so
  without that mask and inset the Dock would show a square, a full-bleed
  tile larger than neighbouring apps, or the generic Unix-exec glyph.

- **Token usage meter.** The composer grows a Cursor-style ring: last manager
  prompt versus the selected model's window. Hover shows the percentage, a
  compact `used / limit` count, and this-turn billed in/out/cached/thinking. The
  Trace tab repeats the snapshot under the turn summary; `zwai trace` prints `90/12 tok cache 20` on
  each model call. Numbers come from the endpoint's usage block and live on
  `llm_calls`. A live `usage` SSE pulse (`seq = 0`) is not stored — reload
  rebuilds from the table. The window is `model_context[name]`, else
  `context_window`, else unknown. Unknown no longer leaves an empty ring: the
  arc still grows on a saturating curve scaled by the compact character budget
  (a visual half-life, not a fake token %), and the label stays a count until
  Settings or Discover fills a real window. Discover copies nested vendor keys
  (`top_provider.context_length`, `meta.n_ctx`, …) when present. `--mock`
  simulates a 128k window and rune-count usage so the ring still moves offline.

- **Slash commands `/goal` and `/compact`.** Typing `/` at the start of the
  composer lists built-in commands the same way Cursor and Codex do. **goal**
  stores a standing objective (`PATCH` `goal`, `goal` event) and the runtime
  keeps starting turns until the manager calls `complete_goal`, calls
  `block_goal` (stuck: the same obstacle has already been retried), the human
  clears it, or `swarm.goal_max_auto_turns` consecutive auto-turns elapse
  (`goal_complete` / `goal_blocked` / `goal_continued` / `goal_capped`). The
  banner shows the state, elapsed time, a **Start** control to resume, inline
  edit (`PATCH` `goal_edit`, `goal_edited` — a live turn is steered so this
  turn sees the new text), and clear. Follow-ups and unread
  steers still take the next turn first. **compact** folds older replay into a
  briefing (`POST /api/threads/:id/compact`, `compacted` event) without
  rewriting the transcript, on an optional pinned summarizer
  (`swarm.compact_provider` / `swarm.compact_model`). The `/compact` hint stays
  hidden on an empty chat. Settings expose the compact budget, keep-tail, and
  the goal auto-continue cap. `zwai tui --goal` is the same pursuit on a
  one-shot terminal run (`complete_goal` or `block_goal` both stop it).

- **Files panel is a collapsible tree.** Directories expand and collapse,
  a **Filter files** box sits at the top, and glyphs tint by kind instead of
  dumping every path as a flat indented list. A workspace that is a repository
  starts collapsed at the root; a conversation whose listing is a single nested
  folder still opens that folder so the files are visible. Arrow keys move,
  expand and collapse. The HTTP listing is still a flat walk — the tree is
  client-side. The walk is breadth-first so a fat directory cannot hide later
  siblings, and it skips `node_modules` and `vendor`, the same way it already
  skipped dot-directories, so a JavaScript or Go repository does not spend the
  2000-entry cap on a dependency tree.

- **Find in the conversation (`⌘F`).** The webview's own find bar searches the
  chrome. Ours searches the open transcript: a Codex-style overlay in the
  top-right, live match count, previous/next, and a highlight on the current
  hit. Nearby matches are highlighted; a live count of thousands does not
  allocate a Range per hit. Collapsed thoughts and tool payloads that hold a
  two-or-more-character query open the first matching row so the highlight has
  a node to paint. `Esc` closes the bar before it would stop a
  running turn. `⌘G` / `F3` step while it is open.

- **Follow-up queue vs Steer.** Enter while a turn is running queues a message
  for after this turn finishes (`followups` table, `GET/POST/DELETE/PATCH
  /api/threads/:id/followups`). Click a waiting row to edit it; submitting
  that edit keeps the id and puts it at the back of the FIFO. **Steer** on
  that row, or ⌘Enter on a new draft, injects into the current turn at the
  next model boundary without killing an in-flight tool
  (`POST …/followups/:fid/steer`). Stop and errors leave the queue in place;
  a late unread steer still takes the next turn ahead of it. Refresh does
  not lose waiting text.

- **Host environment in the system prompt.** Every turn injects the live OS,
  architecture, kernel, shell, date, timezone, user and home into the manager
  prompt, and the same snapshot is prepended to every sub-agent's instruction
  (`Registry.WorkerPreamble`). Without it, models emit GNU-only flags on BSD
  userland and dates from their training cutoff.

- **Paste a screenshot into the composer.** Cmd/Ctrl+V lands a thumbnail you can
  drop before sending. The pixels go to the model as visual input
  (`UserInputMultiContent`), stored under `$ZWAI_HOME/inputs/<thread>/`, not in
  the workspace. `POST /turns` and `POST /steer` accept `images: [{name,mime,data}]`;
  the transcript fetches thumbs from `GET /api/threads/:id/input-images/:image_id`.
  Paperclip uploads stay workspace files.

- **Drop files onto the composer.** Hovering the box with files arms a dashed
  overlay; dropping splits the payload the same way as paste vs paperclip —
  images become vision thumbs, other files become workspace chips you can
  remove before send. Folders are ignored. Chips spin while the workspace
  upload runs.

- **Unfinished turns resume after a crash or quit.** A turn left `running` by a
  killed process, a power loss, or closing the app is continued on the next
  start — same turn id, a `resumed` event on the timeline. Sub-agents that were
  still running are restarted under the same ids so the manager can wait for
  them instead of spawning replacements. Follow-ups waiting in the queue stay
  there and run after that leftover turn finishes. Unread steering is kept even
  if a tool call was in flight. A leftover tool-round confirm is dismissed
  because the turn is running again. A user **Stop** stays `cancelled`.
  `ResumeOrphanedTurns` at startup; shutdown no longer bulk-cancels leftovers.

- **Quote selected text into the next message.** Select a passage in the
  transcript (or a sub-agent's log) and **Add to chat**. The snippet lands as an
  annotation on the composer — hover to read, edit or drop it — and is prefixed
  onto the send as `Selected text:` so the model sees it without the box filling
  up. Switching conversation clears the annotations.

- **Jump to a previous message.** Two or more of your own turns grow a compact
  tick cluster in the middle of the transcript's left edge — not a full-height
  scrollbar. Hover it for a list of those messages; click (or the arrow keys) to
  scroll there. Auto-follow unpins in the same click so a live stream cannot
  yank you back to the bottom. New tokens while you are reading light a
  jump-to-latest control; click it to return to the live edge and follow again.
  Steering is not a jump target — it is a nudge inside a turn.

- **Provider catalogs, composer model switch.** Settings is provider config:
  one URL, one default, a discovered catalog. The composer picker groups names
  by provider, searches, **Refresh models** rediscovers every catalog without
  rebooting the open conversation, and **Edit providers** jumps to Settings.
  No provider row per model. `POST /api/models/discover`, `catalog` on each
  provider, `model` on each conversation. `GET /api/models` is one row per
  selectable name.

- **Automatic conversation titles.** A new conversation still shows the first
  message as a placeholder so the sidebar is readable at once. After the first
  finished turn, a short name replaces that quote — same language as the
  request, not the request itself. A title the user typed is never overwritten,
  and an interrupted turn keeps the placeholder. Off in Settings → Swarm
  (`swarm.auto_title`). The namer is recorded under the turn as a `title`
  event (`agent_id: title-namer`),   so `zwai trace` still shows the call; the
  transcript does not. Empty `title_provider` / `title_model` follow the
  conversation's model; pin a cheaper name in Settings → Models when an
  endpoint lists more than one.

- **New conversation from a project row.** Hover a project in the sidebar and a
  new-conversation control appears on the row, next to the existing menu. It
  selects that project and starts the conversation in it, so you do not have to
  click the project first and then the top of the list.

- **Projects, with a memory.** Conversations can be grouped into a project that
  gives them one working directory, one instruction added to every manager
  prompt, and a memory they share. With memory on, each finished turn is read
  back by a reviewer agent that keeps what is worth carrying forward: short
  notes in `MEMORY.md` under a character budget, and reusable procedures as
  skills in `skills/<name>/SKILL.md` (the
  [agentskills.io](https://agentskills.io) layout). Later conversations in the
  project start with the notes in their prompt and an index of the skills, and
  open one with `skill_view` when they need it; the manager can also write to
  memory itself with `memory` and `skill_manage`. Sub-agents cannot — a worker
  would be recording work the manager had not accepted yet.

  Everything about it is visible and correctable: a **Memory** tab shows the
  notes (editable), the skills (readable and deletable) and a **Review now**
  button; the review lands under the turn's own id as a `memory_review` event,
  so `zwai trace <id>` still tells the whole story, reviewer model calls
  included. The files live in the data directory under
  `projects/<id>/memory/`, never in the working directory: a project pointed at
  a repository leaves nothing in it. New endpoints under `/api/projects`,
  `project_id` on conversations, `?project=` on the list, and
  `POST /api/threads/:id/review`; new config section `memory` (`enabled`,
  `auto_review`, `char_limit`, `review_max_iterations`, `skills_index_max`,
  `notifications`), editable in Settings → Memory.

  A write that landed is named in the transcript (`Memory updated: 1 note
  stored`), not only in a panel the user may not have open; `verbose` adds a
  preview of the text, `off` keeps the review and hides the line. The Memory
  tab badges when something was stored while it was not the one on screen. A
  save against notes a review changed in between is refused (`409 conflict`)
  with what is stored now, so the editor can keep yours or take the new ones
  rather than the last writer silently winning. The sidebar lists each
  project's skills under its name (`GET /api/projects` carries the index);
  clicking one opens that skill in the Memory tab. Past eight names the row
  points at the tab for the rest.

- **Tool-round cap asks before stopping.** `swarm.manager_max_iterations`
  (default 200, same as `max_turns`) is the manager's ReAct budget. Hitting it
  no longer fails the turn with eino's `exceeds max iterations`; the transcript
  asks whether to add another slice of that size (`POST /api/threads/:id/continue`,
  events `max_iterations` / `max_iterations_continued`). Declining keeps the
  partial answer and records `cancelled`. Existing config files keep their saved
  value; raise it in Settings if an older install still has 32.

- **Live status lines move.** While a turn is running, the heartbeat, a pending
  tool's arguments, a `wait_agents` roll-up and a sub-agent's activity sweep
  (and scroll if they do not fit) instead of sitting truncated and looking
  stuck. Hover pauses a scrolling line so it can be read. `prefers-reduced-motion`
  turns the animation off.

- **`resume_agent`.** A finished worker's conversation is archived (independent of
  `Stats()` pruning the live handle) so the manager can continue that **same**
  worker instead of spawning a twin with the same role. `fork_context` still
  copies the **manager** conversation; continuing a worker is `resume_agent` and
  keeps the original `agent_id`. `send_message` remains running-only; leftover
  steering that never reached a model call shows up on `wait_agents` as
  `undelivered`. The inbox seed for fork/resume is applied before the worker
  goroutine starts, so the first model call cannot miss it.
- **Stream coalescing.** Streamed tokens are held for
  `swarm.delta_coalesce_ms` (default 50) and sent as the latest snapshot, so a
  50-token-per-second model costs the UI one redraw, not fifty. The front end
  also folds a burst of deltas into one animation frame, memoises completed
  markdown, and keeps the composer, clock and panel width out of the
  conversation's render path — typing and a ticking timer no longer re-parse
  the whole transcript.
- **A pulse while the turn runs.** A swarm whose agents are all inside a slow
  tool call streams nothing, and a busy run then looks exactly like a stuck one.
  Every `swarm.progress_interval_seconds` (default 5, editable in Settings → Swarm)
  a running turn now emits a `progress` event carrying the turn's age and each
  sub-agent's status and age. The transcript shows a "Working for 1m 12s ·
  2 sub-agents running" line that ticks between pulses, and the roll-up under a
  pending `wait_agents` ages each row. Pulses are broadcast and never stored:
  they restate events the turn already recorded, so a trace loses nothing and a
  replay does not wade through hundreds of rows.
- **Per-conversation thinking level.** The composer now carries a thinking-level
  menu (Default / Low / Medium / High) next to the model picker, so a
  conversation can be told to reason harder or lighter without touching
  settings. The choice is stored on the conversation and applied from the next
  turn as the model's `reasoning_effort`; the empty default sends nothing, so a
  non-reasoning endpoint is never handed a field it rejects. The level rides the
  one-id troubleshooting path — recorded on the turn, shown in `zwai trace` and
  the Trace tab — and the levels the UI offers arrive in `GET /api/meta` as
  `reasoning_levels` rather than being hardcoded in the front end.
- **The zwai app.** A single Go binary that serves a React + shadcn/ui front end
  and runs a swarm of agents per conversation, with a three-pane Codex-style
  layout: conversation list, transcript, and an Agents / Files / Trace panel.
  Launch it with `go run ./cmd/zwai desktop` or `zwai web`.
- **`cmd/zwai` subcommands**: `desktop` (Wails 3 native window, the default),
  `web`, `tui`, `trace`, `config`, `version`, `help`. Flags may follow positional
  arguments; misuse exits `2` and failure exits `1`.
- **One-id troubleshooting.** `zwai trace <turn-id|conversation-id>` prints a
  turn's event timeline and every model call with its size and duration;
  `--full` keeps text untruncated. The same data is at `GET /api/trace/:turn`.
  The UI Trace tab shows status, error and billed tokens; **Full log** is the
  on-screen timeline, folded by default.
- **`internal/config`** — one YAML file under the data directory covering server,
  models, swarm limits, tools and logging, saved atomically, editable from
  Settings, seeded from `OPENAI_*` on first run only.
- **`internal/store`** — gorm + pure-Go SQLite for conversations, transcripts,
  turns, the event timeline, model-call telemetry and attachments, with gap-free
  per-conversation sequence numbers.
- **`internal/provider`** — a model pool over any OpenAI-compatible endpoint,
  per-call telemetry, and a scripted offline provider (`--mock`) that runs a
  complete swarm turn without a network or an API key.
- **`internal/tools`** — the [eino-tools](https://github.com/LubyRuffy/eino-tools)
  toolset (`read`, `write`, `edit`, `ls`, `tree`, `glob`, `grep`, `exec`,
  `web_search`, `web_fetch`, plus opt-in `python_runner` and `screenshot`),
  anchored per conversation workspace, with a proxy setting for the network tools.
- **`internal/engine`** — one runtime per conversation: turns, steering,
  interrupt, workspace management, automatic titles, end-of-turn sub-agent
  cleanup, and a sequenced event bus that persists completed events and
  broadcasts streamed deltas.
- **`internal/server`** — gin REST + SSE, multipart upload, download with
  traversal rejection, workspace listing, desktop-only reveal, and the embedded
  SPA with fallback. Documented in [docs/API.md](docs/API.md).
- **Event stream that survives a refresh.** `GET /api/threads/:id/events` replays
  from `Last-Event-ID` (or `?since=`) then goes live; a slow client is caught up
  from the database instead of losing events.
- **Steering.** Typing while a turn runs injects guidance at the next turn
  boundary. Steering that arrives after the manager's last model call becomes a
  follow-up turn instead of disappearing.
- **Per-conversation workspaces** at `workspaces/<thread-id>/`, with uploads in
  `uploads/`, provenance in the Files panel, download, delete, and "Show in
  Finder" in the desktop shell.
- **Swarm library**: `RunWith`/`RunConfig`/`RunResult` for multi-turn use with a
  transcript to persist, `Registry.SteerManager`, `Registry.TakePendingSteers`,
  and `Notification.ToolCallID` for pairing parallel tool calls with their
  results.
- **Escape stops the running turn**, when no dialog or palette is open. Existing
  shortcuts: `⌘K` palette, `⌘N` new conversation, `⌘B` conversation list, `⌘\` panel, `⌘,` settings.
- **Tests**: Go unit and HTTP tests with `-race` across every package, vitest for
  the stream-to-blocks reducer, and Playwright end-to-end specs driving a real
  server on the offline provider. See [docs/TESTING.md](docs/TESTING.md).
- **Documentation**: rewritten [README](README.md), plus
  [ARCHITECTURE](ARCHITECTURE.md), [AGENTS](AGENTS.md), and `docs/` for API, CLI,
  CONFIG, DATA_MODEL, TESTING and the swarm LIBRARY.
- **Makefile** with `run`, `web`, `mock`, `build`, `frontend`, `dev`, `test`,
  `e2e`, `check`.

### Changed

- **Settings lists a context window per model, not per endpoint.** One number
  for a provider that serves eleven names would pin them all to the same
  limit. Each catalog name has its own field (`model_context`); the old box
  is only a fallback for names that still have none.

- **Sidebar is Pinned / Projects / Recents.** Project topics nest under
  collapsible folders. Conversations outside a project sit in Recents.
  **New conversation** lands there; a project's own control starts one in
  that folder. Pin a project topic to the top (`PATCH pinned`). Skills
  moved into the project ⋯ menu (**View skills**) so the folder stays a
  directory.

- **Trace tab is a summary, not a syslog.** Status, error, duration, and billed
  tokens sit up top with the turn id. The event dump is behind **Full log** —
  an 800-call turn no longer paints `spawn_agent` JSON in the sidebar.
  `zwai trace <id>` is still the full replay.

- **Settings writes as you go.** Toggles, fields, Discover, and provider
  rows persist after a short debounce — the same as Codex/Cursor. **Back to
  app** flushes the last keystroke. There is no Save/Cancel pair, and a
  failed write keeps the sheet open with the error.

- **Provider request timeout is idle, not total.** `timeout_seconds` used to
  be `http.Client.Timeout`, which includes reading the streamed body. A
  thinking model still emitting tokens died mid-thought with `failed to
  receive stream chunk: context deadline exceeded (Client.Timeout …)`. The
  setting is now how long to wait for the next byte (headers or a chunk). A
  live stream can outlast it; a silent endpoint still dies. The transcript
  names Settings and thinking instead of dumping eino's `NodeRunError`
  graph path.

- **Settings → Models lists providers collapsed.** Each endpoint is a row
  (name, default model, default badge). Open one to edit URL, key, and
  timeout. **Add a provider** sits under the list and opens the new row.
  A settings search that matches a field opens the matching rows.

- **Settings is a full-page sheet.** Left rail with search and sections,
  each control on the right of its row — the same layout as a desktop
  settings window, not a stacked form in a modal.

- **Sidebar sorts by last update, not creation.** Projects float when a
  conversation in them is used. Conversations already used `last_active_at`;
  a drag now pins `sort_rank` so that order survives a later turn.

- **Memory notes: Save is gone until you edit.** The Memory tab no longer
  parks a disabled **Save notes** under the box. The control (and Revert)
  appears when the draft differs from what is stored, and leaves after a
  successful save or a revert back to the saved text.

- **Title bar is one line.** A conversation in a project shows `project · title`
  on the window chrome instead of stacking the project name under the title.
  The model name is only on the composer — repeating it on the right of the
  bar ate the same row.

- **Quit no longer records leftover turns as a user Stop.** Closing the window
  or `Ctrl-C` abandons in-memory runs and leaves them `running` so the next
  start continues them.

- **Thinking stays a 10-line window.** A live thought no longer grows without
  bound and push the answer off the screen: the box caps at ten lines, follows
  new tokens, and fades at the top once earlier lines have scrolled away. The
  **Thinking** label sweeps like the other live status lines. After it finishes
  it still collapses to **Thought**.

- **Answers render as markdown while they stream.** A heading no longer sits as
  `## …` until the turn finishes; incomplete `**` / `` ` `` / `~~` are closed
  for the live view so a half-typed marker does not flash as punctuation.
  Finished answers are still memoised and parsed once.

- **macOS desktop title bar is one full-width strip.** The sidebar toggle
  sits next to the traffic lights (Codex/Cursor), not in a padded chrome row
  inside the conversation list. Native lights are centred in the 48px header
  and the front end pads to the zoom button's measured right edge, so a Tahoe
  geometry shift cannot leave the toggle sitting under the yellow blob.

- **The composer sits on the transcript.** The input is a floating card at the
  bottom of the conversation, with a fade instead of a full-width hairline, so
  the last lines go under the box the way they do in Codex. The transcript pads
  by the card's height (`--composer-pad`) so follow and a jump still leave the
  last message readable.

- **Default swarm tool-round caps are 200**, not 32 (manager) / 24 (sub-agent).
  Existing `config.yaml` files keep whatever they already saved; new installs
  and repaired zeros get 200. Hitting the manager cap now asks before stopping
  (see Added).

- **Deleting a conversation no longer deletes the directory it was working in,
  unless zwai made that directory.** With projects, a conversation's workspace
  can be the user's own repository, and `DeleteThread` removed it
  unconditionally. Deleting a project removes its conversations, its memory and
  the workspace zwai created for it — never a `workdir` the user chose.
- **The Files panel skips dot-directories.** A project pointed at a repository
  would otherwise spend its whole 2000-entry listing on `.git`.
- **`engine.CreateThread` takes a project id** (`CreateThread(title,
  providerID, projectID)`), and `store.ListThreads` takes one to filter by
  (`ListThreads(includeArchived, projectID)`). Empty behaves exactly as before.
- **Tool rows show what happened, not the JSON.** `exec` is the command and its
  stdout (or a red error when `exit_code` is not 0); `web_search` is the query
  and the hit list. Other built-in tools follow the same rule: the summary is
  the path / pattern / URL, the body is the parsed result. The TUI uses the
  same summaries instead of dumping `name({json})`.
- **`resume_agent` keeps the same `agent_id`.** It used to mint a twin worker
  with the same role, so a failed sub-agent showed up twice in the roster. It
  now continues that worker in place; a second `spawned` event for the same id
  is a continuation, not another "Started" row.
- **`NotifyToolResult` keeps newlines.** It used to collapse `\n` to spaces and
  cut to 400 runes, which made a file body unreadable in any UI. The text is now
  the tool's stdout, clipped at 64k runes so a huge `exec` cannot fill the event
  log. The model-facing transcript was already verbatim.
- **`wait_agents` returns on the first finish, not the whole batch.** It used to
  block until every listed sub-agent was done (or the timeout ran out), so a
  running swarm looked frozen with no progress until the very end. It now returns
  the moment the next sub-agent reaches a final status and reports every agent's
  state (`running`/`done`/`failed`), the finished ones' results, and the running
  ones' latest activity, plus `timed_out`. The manager is told to narrate what
  came back before waiting again — the periodic feedback Codex gives. This
  changes the tool's result shape from a flat array to `{agents, timed_out}`.
- **The data directory is `~/.zwai-swarm`** (`ZWAI_HOME` overrides, `--data-dir`
  overrides that). `~/.zwai` belongs to a different project and is left alone.
- **The desktop window loads a local HTTP URL** rather than a `wails://` asset
  protocol, so uploads, downloads and SSE have exactly one implementation shared
  with the browser.
- **No model endpoint or model name is compiled in.** `cmd/zwai` used to hardcode
  both; they now come from the config file.
- **README** is now the app's documentation; the library reference moved to
  [docs/LIBRARY.md](docs/LIBRARY.md).
- The demo `internal/webui` (Chrome `--app`, one global registry, hardcoded
  colours, no persistence) was removed in favour of `frontend/` +
  `internal/server`. The UI is embedded from `frontend/dist` at build time.
- A 25 MB `swarm-real` binary was removed from the repository and a `.gitignore`
  added.

### Fixed

- **Force-quit no longer drops in-flight sub-agents.** Restarting the app
  while a turn is still working used to continue only the manager. Workers
  that had `spawned` without `finished` start again under the same
  `agent_id`; already-finished ones stay resolvable for `wait_agents` /
  `resume_agent`. A quit does not record end-of-turn `cleanup` or a
  cancelled `finished` for those workers.

- **Desktop Dock icon was larger than a real `.app`.** `setApplicationIconImage`
  does not apply Apple's 824/1024 content grid, so a full-bleed PNG filled
  the tile. The mark is now inset to that grid before rounding.

- **Thought fade no longer covers the jump rail, and the active tick
  follows the turn you are actually on.** A live thought's top wash used
  `z-10` in the same stacking context as the rail, so a pinned thought
  painted over the ticks. The fade is isolated now. Sitting at the bottom
  of a long transcript (composer padding) also kept the first tick lit
  because the latest user row never crossed the top probe; the latest turn
  wins when the scroller is at the bottom.

- **Memory writes that do not fit said “replace”, so the model replaced
  again.** A full store returned *this write of N would not fit. Consolidate
  with replace…* without saying the replacement had to be *shorter*, and a
  replace/remove miss did not list the notes. The manager then retried
  `memory replace` with a longer status note until it guessed it should
  merge. The result now carries `over_by` (and the matched note, for
  replace), says not to retry the same text, a miss lists `current_entries`,
  and a store already at 80% is told in the prompt that a growing write will
  be refused. The collapsed tool row shows the refusal, not just the note
  that did not land.

- **Idle unranked conversations no longer sit above ones that just ran.**
  `sort_rank` `0` meant "never dragged", and `ORDER BY sort_rank ASC` treated
  that as "always first". After a drag inside a project, every global
  conversation floated above the work that was still going. Unranked rows
  now interleave by last activity; a new conversation still lands on top.

- **Dragging the conversation list stuttered.** Every pointer frame
  re-rendered the whole sidebar and wrote `localStorage`. Live moves now
  only paint `--zwai-sidebar-width`; storage waits for pointer up.

- **Sidebar needed two clicks to switch conversations.** A `draggable` row
  made Chrome/WKWebView start an HTML5 drag on a trackpad twitch and swallow
  the click. Reorder now waits 8px of pointer movement (the Codex pattern);
  a click still opens, and you can drag from the title.

- **Composer files were not bound to the message.** Uploads landed in
  `uploads/` and the turn was just the caption, so a leftover archive in the
  same folder could be treated as "the" file. A send now names this request's
  paths on the user message (`files` on `POST /turns` / `POST /steer`), binds
  `attachments.turn_id`, and the manager prompt says to read those first.
  File-only send starts a turn; a failed upload keeps the chip.

- **Interrupted tools stopped spinning.** A Stop kills in-flight `exec` (and
  any other tool) without a `tool_result`. The transcript still treated those
  rows as pending, so the spinner kept turning next to the interrupted banner.
  The reducer now closes leftover pending tools when the turn ends or the
  agent finishes.

- **UI**: full-page Settings sat under the macOS traffic lights, so **Back to
  app** was unreadable and unclickable. Desktop macOS now keeps an empty
  `h-12` drag strip (the same height as the hidden title bar) and puts the
  back control on the row below, the way Codex does. A browser tab still
  starts at the top of the sheet.

- **Context meter ring stayed empty on a real endpoint.** No discovered
  `context_window` meant fill was hardcoded to 0, so 54K tokens still looked
  like an unused circle. The arc now grows on a saturating curve until Settings
  or Discover fills a real window; hover still refuses a fake percentage.
  Discover also reads nested vendor keys (`top_provider.context_length`,
  `meta.n_ctx`) instead of only top-level ones.

- **Find no longer freezes a live turn.** A one-character query used to expand
  every matching thought and tool dump, then build a CSS Highlight from every
  Range (`new Highlight(...thousands)`), then do that again on each streamed
  token. Count still covers the whole transcript; painting is a window around
  the current hit, collapsed rows stay closed for a one-character query, and
  stream re-paints wait until the tokens pause.

- **UI**: expanding a skill in the Memory tab covered the Files tree. Inactive
  tab panes stayed `display:flex` (Tailwind `.flex` beating `[hidden]`), so
  workspace names painted through the skill cards, and opening a body then
  sat on top of them. Inactive `[role=tabpanel]` is now `display:none
  !important`, and an opened skill's markdown stays inside its card.

- **Files listing matches the workspace root.** Depth-first walk spent the
  2000-entry cap inside a fat directory that sorts early (`build/`), so the
  panel could miss later siblings Finder still shows. The walk is now
  breadth-first: directories at a depth first, then files round-robin across
  parents.

- **External links no longer take over the window.** A markdown URL used to
  navigate the webview, so the desktop app became the destination site. Clicks
  now leave the app: the desktop shell opens the system browser
  (`POST /api/open`, `capabilities.open_url`), and `zwai web` opens a new tab.
  Same-origin downloads and fragment links are unchanged.

- **Force-quit no longer wipes the last turn from the model's context.** Manager
  answers were stored as events (so the transcript still showed them) but the
  `messages` table used for the next model call was only written when the turn
  ended. Killing the app mid-turn, then reopening, resumed with the original
  request and none of the work already on screen. Answers are now stored with
  each `agent_message`, and resume also reads any that only exist on the event
  log and writes them into `messages` so a second crash still has them.

- **Sidebar**: Projects sat 8px further in than Today. The project list wrapped
  itself in a second `px-2` on top of the scrollport's, so All conversations and
  every project row started further right than the day groups. Both sections
  now share that one gutter.

- **Switching conversations lands at the latest turn.** Opening another item in
  the list used to paint the top of the history: a freshly mounted overflow box
  fires `scroll` at 0, which unpinned follow with no unread, so Jump to latest
  never appeared either. The pin resets on the new id, that layout scroll is
  ignored, and the scroller sticks to the live edge before paint.

- **Scrolling up during a stream no longer yanks you back.** Auto-follow used to
  re-pin inside an 80px slack on every token, and the scroll listener never
  attached if the transcript first painted as a skeleton. Wheel-up now unpins
  immediately; new tokens stay put and light the jump-to-latest control.

- **A live thought can be collapsed.** Streaming used to force the block open
  (`open || streaming`), so clicking the chevron did nothing until the model
  finished. The click now wins; it still starts open and still collapses to
  **Thought** when it finishes if you never opened it.

- **UI**: Settings → Models clipped the bottom border of **Add an endpoint**
  (and any other outline at the end of Tools / Memory). The tab's
  `overflow-y-auto` did not replace the shared `overflow-hidden`, so the last
  pixel of the scrollport ate the 1px outline. Those tabs now scroll with
  `overflow-auto` and a sliver of padding under the last control.

- **Unread steering sat in the middle of the turn.** Guidance is queued for the
  next model call, but the bubble was appended where the user typed — above
  "Working for…" and in the middle of the tool list — so it read as already
  applied. Unread steers now pin under the working line until the manager
  starts another round.

- **Jump rail no longer stretches the pane.** Two user turns used to paint ticks
  at the top and bottom of the transcript, like a fake scrollbar. They now sit
  in a short cluster in the middle.

- **Memory**: `skill_view` treated a `SKILL.md` found in the workspace as a
  recorded project skill. Those are two stores — memory lives under
  `projects/<id>/memory/`, the workspace is just files — and the usual miss was
  a listing of the repository followed by `no skill named …`. The prompt and
  the refusal now say so, so the next move is `read`, not another guess. The
  transcript no longer prints that error twice.

- **Desktop**: launching aborted with `NSWindow geometry should only be
  modified on the main thread`. Wails delivers window events on a worker
  goroutine; aligning the traffic lights now hops back to the main thread
  before touching AppKit.
- **Desktop**: ⌘Q froze the window for five seconds, then Wails logged
  `Window #1 not found`. The live event stream is a request that never ends, so
  graceful HTTP shutdown waited out the whole grace period on the UI thread.
  Quitting now cancels that stream immediately; the window goes away at once.
  The leftover `Window #1 not found` lines (Wails draining native events after
  it already dropped the window) are swallowed, not printed as warnings.
- **UI**: the Memory tab clipped skills and the memory path at the bottom of
  the window. Notes ate the column and skill blurbs were CSS-truncated, so a
  long description looked cut off even after the pane could scroll. Skills now
  keep the leftover height and wrap their descriptions.
- **UI**: the Memory tab's sparkles control ("Review now") looked like it did
  nothing. The review runs in the background, and an outcome that kept nothing
  is silent in the transcript on purpose — a line after every turn would train
  people to ignore it. A click now shows progress on the panel, and the result,
  including when there was nothing to keep or no finished turn to read.
- **UI**: the Memory tab opened with a blank half-panel above the notes. The
  Agents pane is a flex column so a worker transcript can scroll; `display:flex`
  and the HTML `hidden` attribute have equal specificity in this Tailwind, so
  the empty Agents shell stayed in the layout. Inactive tab panels now force
  `display: none`.
- **UI**: dragging the side panel's border selected text in the transcript
  and the agent log. The drag now holds selection off until the pointer is up.
- **UI**: dragging a sub-agent's transcript used to paint through the back
  button. That chrome is no longer a sticky overlay on the scroller, and the
  panel resize strip stays above it.
- **Desktop**: double-clicking the title bar on macOS now zooms and restores
  the window, the way Finder and Safari do (and honouring System Settings if
  that action is set to minimise or to do nothing). Dragging already worked;
  Wails leaves the second click to the page, and the page was not sending it.
- **UI**: Enter while a CJK IME was confirming leftover Latin (keeping English
  as typed) sent the draft. WebKit fires `compositionend` before that Enter, so
  `isComposing` was already false. The composer now leaves that key to the IME;
  the next settled Enter sends.
- **Engine**: closing a turn used to mark it finished *before* recording `done`,
  so a reload that raced `FinishTurn` could replay a complete swarm missing its
  last row. The terminal event is now stored first.
- **UI**: an event kind the app renders but never subscribed to on the event
  stream was stored, visible in `zwai trace`, and invisible on screen until the
  page was reloaded — which is how the first memory reviews arrived. The
  subscribed kinds are now covered by a test.
- **UI**: expanding a `read` showed a mashed one-liner (`encoding=utf-8 path=… 1|# …`)
  because `NotifyToolResult` collapsed newlines and cut the body to 400 runes.
  Tool results now keep newlines (clipped at 64k runes so `exec` cannot fill the
  event log), and a `read` row renders the file — markdown as markdown, anything
  else with line numbers.
- **UI**: on the macOS desktop window, hidden-inset traffic lights sat on top of
  **New conversation**. The sidebar now has a Codex/Cursor-style chrome row
  (lights + hide toggle); New conversation sits under it. The list can be hidden
  and shown (`⌘B`); when it is hidden the toggle moves to the main title bar,
  which then pads for the lights.
- **Swarm**: `fork_context` could lose the race against the worker's first model
  call, so the inherited manager conversation never arrived. The inbox is now
  seeded before the goroutine starts.
- **Swarm**: `send_message` reported `delivered: true` for text that was only
  queued. If the worker finished without another model call, that steering
  vanished. `wait_agents` now reports leftovers as `undelivered`.
- **Swarm**: asking a run how it was doing could destroy its result. `Registry.Stats`
  prunes the finished sub-agents it counts, and the engine called it to answer
  `GET /api/threads/:id`, so a status poll landing between a worker finishing and
  the manager's `wait_agents` collecting it turned a completed worker into
  "unknown agent" and lost the result. Reporting now goes through a new read-only
  `Registry.Progress()`, which never forgets an agent.
- **Swarm**: the manager's `tool_result` notifications were never emitted, because
  only streaming events were handled. Non-streaming tool messages now surface.
- **Swarm**: a worker's streamed output was accumulated twice per turn, and the
  manager's reasoning was not reset between turns, so text compounded.
- **Swarm**: merging streamed tool calls double-counted the first chunk's
  arguments, producing invalid JSON for a tool call.
- **Engine**: a conversation could reject its own next turn because the runtime
  was still marked busy when the terminal event was delivered.
- **Engine**: an interrupted turn was recorded as `cancelled` correctly, but a
  completed one could be too, depending on when the context was released.
- **Engine**: absolute or `..` workspace paths are rejected outright at the HTTP
  boundary rather than silently remapped.
- **Server**: a slow SSE subscriber could lose persisted events, including the one
  that says the turn finished; it is now marked lagged and caught up.
- **Engine**: `broadcast` snapshotted its subscribers, released the lock, then
  sent on their channels — so a subscriber could be closed (its channel closed
  under the lock) in the gap, and the next send would panic on a closed channel.
  The non-blocking send now stays under the same lock the subscriber lifecycle
  uses, closing the window. Surfaced under `-race` once turns produced more
  events per run.
- **App**: `Serve` could bind lazily while another goroutine read the URL or shut
  the server down — a real data race, now behind a lock.
- **Startup**: turns left `running` by a crash or a `kill` are closed as
  `cancelled`, so a conversation no longer looks like it is working forever.
- **CLI**: `zwai trace <id> --data-dir X` ignored the flag, because Go's flag
  package stops at the first positional argument.
- **CLI**: `zwai config show` reported `api_key: <set, hidden>` for a key that was
  genuinely empty, and truncated multi-byte characters into broken glyphs.
- **CLI**: `zwai --data-dir DIR` and `zwai --mock` reported `unknown command`,
  although both the README and the program's own usage line offer them. A flag
  with no subcommand now opens the app, which is what `zwai` on its own does.
- **CLI**: `zwai config` with an unknown subcommand called `os.Exit` from inside a
  library-style function; it now returns a usage error that exits `2`.
- **TUI**: the transcript printed after exit lost every sub-agent, because
  bubbletea works on a copy of the model. The tool marker was also printed twice,
  and the roster's activity line went blank when streamed text ended on a
  newline.
- **UI**: sending or uploading while a new conversation was still being created
  landed in the previous conversation, so the turn ran where nobody could see it.
- **UI**: a theme preference could not be read in a context where storage throws
  (private browsing, some embedded webviews), which stopped the app from starting.
- **UI**: a completed thought arriving after its stream produced a duplicate
  "Thought" block.
- **UI**: turn footers ("Worked for …") were all rendered at the end of the
  transcript instead of after each turn.
- **UI**: `spawn_agent` appeared twice — as a tool row and as "Started
  sub-agent".
- **UI**: the header's elapsed time showed a nonsensical duration for a turn with
  no recorded start.
- **UI**: newly created conversations were grouped under "Earlier" in the sidebar.
- **UI**: the Files panel showed directories with a file icon and a size.
- **UI**: settings inputs were not associated with their labels.
- **UI**: the Settings dialog's Tools tab crashed on a config whose tool
  exception lists were empty, because a nil Go slice reaches JSON as `null`. The
  config now always serializes them as lists, and the client tolerates `null`.
- **UI**: two Swarm settings described the wrong field — the timeout had no
  explanation and its text sat under "tool rounds".
- **UI**: Settings could not edit the tools' no-proxy list, which
  `internal/tools` already honoured from the config file.
- **Docs**: `zwai help` omitted `tui --workspace`, the `-v`/`-h` aliases and the
  fact that `zwai config` defaults to `show`. A test now reads the flags out of
  the source and fails when `usage()` stops mentioning one.
- **Docs**: `docs/LIBRARY.md` listed a `swarm-tui` example that was an empty
  directory — the terminal renderer is `internal/tui` — and named a test that
  does not exist.
- **Tests**: the end-to-end suite kept its data directory between runs, so the
  sidebar specs depended on how often the suite had been run and the settings
  specs never saw a first start. It now wipes the directory before the server
  starts, and does not reuse a server that is already listening.

### Deliberately not in this change

Project memory covers notes and skills per project. Three neighbouring ideas
were left out on purpose, so that what shipped is complete rather than broad:

- **A profile that follows the user across projects.** Notes are per project; a
  preference stated in one is not known in the next.
- **Searching past conversations.** The reviewer reads the turn it just
  followed, not the archive. Recalling an older conversation is still a matter
  of finding it in the sidebar or `zwai trace`.
- **A native directory picker** for a project's working directory. The field
  takes an absolute path and the server says so when it is not one; choosing a
  folder needs the desktop shell, not the browser.
