# Changelog

All notable changes to this project are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project has not
tagged a release yet.

## [Unreleased]

The repository grew from a swarm orchestration library into a desktop
co-working app built on it. The library API is unchanged except where noted.

### Added

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
  shortcuts: `⌘K` palette, `⌘N` new conversation, `⌘\` panel, `⌘,` settings.
- **Tests**: Go unit and HTTP tests with `-race` across every package, vitest for
  the stream-to-blocks reducer, and Playwright end-to-end specs driving a real
  server on the offline provider. See [docs/TESTING.md](docs/TESTING.md).
- **Documentation**: rewritten [README](README.md), plus
  [ARCHITECTURE](ARCHITECTURE.md), [AGENTS](AGENTS.md), and `docs/` for API, CLI,
  CONFIG, DATA_MODEL, TESTING and the swarm LIBRARY.
- **Makefile** with `run`, `web`, `mock`, `build`, `frontend`, `dev`, `test`,
  `e2e`, `check`.

### Changed

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
