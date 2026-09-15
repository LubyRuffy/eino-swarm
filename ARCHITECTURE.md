# Architecture

zwai is one Go process. It assembles a config, a SQLite database, a model pool, a
per-conversation agent runtime and an HTTP server, then puts either a native
window or a browser in front of it. Both shells load the **same** `http://127.0.0.1`
origin, so there is exactly one implementation of uploads, downloads and the live
event stream.

```mermaid
flowchart LR
  subgraph shells
    Desktop[internal/desktop<br/>wails3 window]
    Browser[zwai web<br/>browser]
  end
  subgraph frontend [frontend/ React + shadcn]
    UI[App shell / Sidebar / Transcript / Composer / RightPanel]
  end
  subgraph server [internal/server gin]
    REST[REST + upload + download]
    SSE[SSE per conversation, seq replay]
    Static[embed frontend/dist + SPA fallback]
  end
  subgraph engine [internal/engine]
    RT[runtime per conversation<br/>turn / steer / interrupt]
    Bus[event bus: seq, persist, broadcast]
    Acc[accumulator: notifications to events]
  end
  Swarm[swarm library, repo root<br/>manager + sub-agents]
  Tools[internal/tools<br/>eino-tools per workspace]
  Pool[internal/provider<br/>OpenAI-compatible / scripted mock]
  Store[(internal/store<br/>gorm + sqlite)]
  Cfg[internal/config<br/>config.yaml]

  Desktop --> server
  Browser --> server
  UI -->|fetch| REST
  UI -->|EventSource| SSE
  REST --> RT
  RT --> Swarm --> Tools
  Swarm --> Pool
  RT --> Acc --> Bus --> SSE
  Bus --> Store
  RT --> Store
  Cfg --> Pool
  Cfg --> Tools
  Cfg --> RT
  Mem[internal/memory<br/>MEMORY.md + skills/]
  RT -->|prompt sections + tools| Mem
  RT -->|after a done turn| Review[review: one reviewer agent]
  Review --> Mem
  Review --> Bus
```

## Modules

| package | responsibility |
|---|---|
| `.` (root) | the swarm library: `Registry`, `spawn_agent`/`send_message`/`wait_agents`/`close_agent`/`resume_agent`, `RunWith` → `RunResult{Final, Transcript}`, `Notification` stream. Usable on its own — see [docs/LIBRARY.md](docs/LIBRARY.md). |
| `internal/config` | `config.yaml` under the data directory: load, normalize, atomic save, `OPENAI_*` seeding on first run. Nothing else in the tree hardcodes an endpoint or model. |
| `internal/store` | gorm + pure-Go SQLite. Conversations, transcript messages, turns, the event timeline, model-call records, attachments. See [docs/DATA_MODEL.md](docs/DATA_MODEL.md). |
| `internal/provider` | builds eino chat models from config, records per-call telemetry, and provides the scripted offline provider used by `--mock` and the tests. |
| `internal/tools` | assembles the eino-tools toolset anchored at one conversation's workspace; catalog + enable/disable rules feed the Settings UI. |
| `internal/memory` | a project's memory as files: `MEMORY.md` notes under a character budget, `skills/<name>/SKILL.md` procedures, the three agent tools (`memory`, `skill_view`, `skill_manage`), the prompt sections they are rendered into, and the reviewer's instruction. Owns the files; knows nothing about conversations. |
| `internal/engine` | one runtime per conversation: starts turns, steers running ones, interrupts, converts `swarm.Notification`s into persisted events, manages workspaces, projects and titles, and runs the post-turn memory review. |
| `internal/server` | gin: REST, SSE, upload/download, trace, embedded assets. See [docs/API.md](docs/API.md). |
| `internal/app` | wiring shared by both shells, plus listen/serve/shutdown, `openURL` and `revealPath`. |
| `internal/desktop` | wails3 single window pointed at the local server URL. |
| `internal/tui` | terminal renderer for `zwai tui`, on the same swarm and config. |
| `cmd/zwai` | subcommand table: `desktop`, `web`, `tui`, `trace`, `config`. |
| `frontend/` | React + TypeScript + Tailwind + shadcn/ui, embedded via `frontend/embed.go`. The project list is `GET /api/projects` including each project's skill index (names and one-line descriptions), so the sidebar can list them without a round-trip per row; bodies stay behind `GET /api/projects/:id/skills/:name`. |

## A turn, end to end

1. **`POST /api/threads/:id/turns`** → `engine.StartTurn`. One turn per
   conversation at a time; a second request while one runs returns `409` and the
   UI sends it as steering instead.
2. The runtime loads the conversation's transcript from the database (compacted
   to user/assistant messages, so context stays bounded), builds a
   `swarm.Registry` with the configured limits, and builds the toolset anchored
   at the conversation's working directory — `workspaces/<thread-id>/`, or the
   project's directory when it belongs to one, so every conversation in a
   project works on the same files.
3. The manager's system prompt is generated per turn from the live toolset, the
   workspace path and the concurrency limits. It is task-agnostic: no example
   task, filename or domain word is baked into it. On a project conversation it
   also carries the project's instruction, its notes with how full they are, and
   an index of its skills — names and one-line descriptions only — and the
   manager gets the three memory tools on top of the toolset. Sub-agents do not:
   a worker that wrote to the project's memory would be writing about work the
   manager had not yet accepted.
4. `Registry.RunWith` drives the manager. It spawns sub-agents, which run
   concurrently under `MaxConcurrent`, each with a watchdog timeout.
5. Every `swarm.Notification` reaches the engine's accumulator, which decides
   what is worth storing:
   - `delta` / `reasoning_delta` are **broadcast only** (`seq = 0`) — they are
     the same text growing, and storing each would be storing the answer N times.
     They are also **coalesced** for `swarm.delta_coalesce_ms` (default 50):
     tokens that arrive inside the window replace the pending event, so a
     50-token-per-second model costs the UI one redraw, not fifty. A tool call,
     a finished worker or the end of the turn flushes whatever is still held
     so the last tokens are not sitting in the coalescer when the row that
     follows them appears;
   - completed messages, tool calls, tool results, spawn/finish, steer and
     cleanup notices are **persisted with a gap-free per-conversation `seq`**,
     then broadcast.
   Alongside them a ticker emits a `progress` pulse every
   `swarm.progress_interval_seconds`, built from `Registry.Progress()`. It is
   broadcast only, for the same reason a delta is: it restates events that are
   already stored. Without it a turn whose agents are all inside a slow tool
   call streams nothing at all and cannot be told apart from a stuck one.
6. Turn end: `Registry.Cleanup()` kills sub-agents still running (recorded as a
   `cleanup` event), the transcript is persisted, a terminal `done`/`error`
   event is recorded, then the turn row is closed. Steering that arrived after the
   manager's last model call is not dropped — it starts a follow-up turn.
7. **Review** (project conversations with memory on, `memory.auto_review`): a
   goroutine hands the finished conversation to a single reviewer agent — one
   `adk.ChatModelAgent` with the memory tools, not a swarm — which stores what
   is worth carrying forward. Its model calls are recorded under the same turn
   id as `memory-reviewer`, and one `memory_review` event says what changed. The
   transcript it reads is clipped per message and in total, so a turn that read
   a large file cannot make the review cost more than the work. Reviews of one
   project are serialized: two turns finishing together would each read the same
   bounded notes, both decide there is room, and one would lose its entry. Only
   a turn that finished cleanly is reviewed, and `Shutdown` waits for the ones
   in flight before exiting.

## Why the event stream is built this way

The UI must survive a refresh, a reconnect and a slow client without losing an
event or showing a duplicate:

- **Sequence numbers are assigned under one lock, together with the broadcast**,
  so stored order and streamed order cannot diverge.
- **`GET /api/threads/:id/events` replays first, then goes live**, from a
  subscription taken *before* the replay. Subscribing afterwards would drop
  whatever happened in between — for a streaming turn, the interesting part.
  Duplicates from the overlap are filtered by `seq`.
- **A slow subscriber is marked lagged, not dropped.** When its channel is full
  a persisted event sets a flag instead of vanishing; the connection notices on
  a short tick and catches up from the database. The event that says the turn
  finished can be the one that would have been dropped, and nothing later would
  come to trigger a re-read.
- **Only persisted events carry an SSE `id`.** `Last-Event-ID` (or `?since=`)
  resumes exactly where the client stopped; deltas were never stored, so they
  are never resumed.

The front end folds this stream into blocks per agent in
`frontend/src/lib/transcript.ts`, pairing a tool call with its result by
`tool_call_id` (agents issue several in one message, and they finish out of
order). That reducer is pure and unit-tested; the store around it only moves data.
Tool rows are not a JSON dump: `frontend/src/lib/tool-view.ts` picks the primary
argument (command, query, path, …) for the summary, and parses `exec` /
`web_search` / `python_runner` results into stdout, hits, or a failed flag.
`ToolInvokableDefer` can turn a Go error into `error: …` text with `err=nil`;
`exec` can also succeed as JSON with `failed: true` / a non-zero `exit_code`.
The UI treats both as a red error, not a grey success.
`resume_agent` continues a finished worker under the same `agent_id`; a second
`spawned` event for that id is a continuation, not a second roster row.
Live status text (heartbeat, pending tools, wait roll-up, sub-agent activity) is
rendered by `MarqueeText`: a sweep while the work is in progress, a scroll if
the line overflows, truncation once it is idle.

## Data layout

```
$ZWAI_HOME (default ~/.zwai-swarm)
├── config.yaml            0600, holds the API key
├── zwai.db                everything else
├── workspaces/<thread>/   one per standalone conversation; uploads/ inside
└── projects/<project>/
    ├── workspace/         only when the project has no workdir of its own
    └── memory/            MEMORY.md + skills/<name>/SKILL.md
```

The workspace is where relative tool paths resolve and what the Files panel
shows. It is **not** a sandbox: `exec` runs as the user, and absolute paths work.
That is the "Full access" model, stated plainly in the UI rather than implied.

A project's memory is kept here rather than in the working directory: a project
pointed at a repository must not leave files in it. For the same reason,
deleting a conversation removes its workspace **only when zwai created it** —
`DeleteThread` on a project conversation would otherwise delete the user's
repository — and deleting a project removes its memory and its managed
workspace, never a `workdir` the user supplied.

## Shell differences

Everything is shared except two things, both advertised by `GET /api/meta` so the
UI never renders a control it cannot deliver:

| | `zwai web` | `zwai desktop` |
|---|---|---|
| listen address | configured (default `127.0.0.1:8787`) | random loopback port |
| `mode` in `/api/meta` | `web` | `desktop` |
| `capabilities.reveal` | `false` — download instead | `true` — opens the file manager |

`capabilities.memory` is not a shell difference: it reports `memory.enabled`, so
the project dialog does not offer a switch the install will ignore.

## Design rules that shaped the code

- **One HTTP surface.** A `wails://` asset protocol would have meant two upload
  paths and two download paths. Loading a local URL keeps one.
- **No hardcoded model or endpoint anywhere.** They come from config; `OPENAI_*`
  only seeds blank fields on first run.
- **Same-origin only.** No CORS headers: a permissive policy on a loopback port
  is how another tab reads your conversations.
- **The turn id is the debug handle.** Every event and model call carries it, the
  UI shows it, and `zwai trace <id>` reconstructs the run from it.
