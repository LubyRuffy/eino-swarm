# Changelog

All notable changes to this project are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project has not
tagged a release yet.

## [Unreleased]

The repository grew from a swarm orchestration library into a desktop
co-working app built on it. The library API is unchanged except where noted.

### Added

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
  `--full` keeps text untruncated. The same data is at `GET /api/trace/:turn` and
  in the UI's Trace tab.
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
