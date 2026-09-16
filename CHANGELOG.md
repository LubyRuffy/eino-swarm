# Changelog

All notable changes to this project are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project has not
tagged a release yet.

## [Unreleased]

The repository grew from a swarm orchestration library into a desktop
co-working app built on it. The library API is unchanged except where noted.

### Added

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
  for after this turn finishes (`followups` table, `GET/POST/DELETE
  /api/threads/:id/followups`). **Steer** on that row, or ⌘Enter on a new draft,
  injects into the current turn at the next model boundary without killing an
  in-flight tool (`POST …/followups/:fid/steer`). Stop and errors leave the
  queue in place; a late unread steer still takes the next turn ahead of it.
  Refresh does not lose waiting text.

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
  start — same turn id, a `resumed` event on the timeline. A user **Stop** stays
  `cancelled`. `ResumeOrphanedTurns` at startup; shutdown no longer bulk-cancels
  leftovers.

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
  `internal/server`. `frontend/dist` is committed so a fresh clone runs without a
  Node toolchain.
- A 25 MB `swarm-real` binary was removed from the repository and a `.gitignore`
  added.

### Fixed

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
