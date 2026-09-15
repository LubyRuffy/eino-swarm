# HTTP API

Everything the UI does goes through this API, and the desktop window uses exactly
the same endpoints as the browser. Base URL is the server's own origin:
`http://127.0.0.1:8787` for `zwai web` by default, a random loopback port in
desktop mode (printed on startup and used by the window).

- All bodies are JSON unless stated otherwise; timestamps are RFC 3339 with
  milliseconds.
- **Same-origin only.** No CORS headers are sent, on purpose: this server holds
  your conversations and listens on loopback.
- Errors are `{"error": "...")` with an optional `"code"`.

| status | meaning |
|---|---|
| `400` | malformed body, bad path, or a rejected value; `code: "workdir"` — a project's working directory is not an absolute path to an existing directory |
| `404` | no such conversation / turn / file / project / skill |
| `409` | `code: "busy"` — a turn is already running; `code: "idle"` — nothing to steer, interrupt or review; `code: "conflict"` — the notes changed after the editor loaded them |
| `501` | the shell cannot do this (`reveal` outside the desktop app) |

## Meta

### `GET /api/meta`

What the UI reads once at startup to decide what to render.

```json
{
  "version": "dev",
  "mode": "web",
  "mock": false,
  "configured": true,
  "default_provider": "default",
  "reasoning_levels": ["low", "medium", "high"],
  "data_dir": "/Users/me/.zwai-swarm",
  "capabilities": { "reveal": false, "memory": true },
  "swarm": { "max_concurrent": 6, "agent_timeout_seconds": 600,
             "max_turns": 24, "manager_max_iterations": 32,
             "progress_interval_seconds": 5, "delta_coalesce_ms": 50 }
}
```

`mode` is `web` or `desktop`. `configured` is false until a default provider has
a base URL and a model name — the UI shows a setup banner until then. `mock` is
true when running on the scripted offline provider. `reasoning_levels` is the
ordered set of explicit thinking levels the composer offers; the empty default
is rendered as "Default" and is not listed. `capabilities.memory` mirrors
`memory.enabled`: the UI disables a project's memory switch when the whole
install has memory off, rather than offering something that will not happen.

## Settings, models and tools

### `GET /api/settings`

Returns the editable configuration. **The API key is never returned**: each
provider carries `has_api_key` and `ready` instead.

```json
{"settings": {
  "server": {"addr": "127.0.0.1:8787", "open_browser": true},
  "models": {"default": "default", "providers": [
    {"id": "default", "label": "", "base_url": "https://endpoint/v1",
     "model": "some-model", "timeout_seconds": 300,
     "has_api_key": true, "ready": true}
  ]},
  "swarm": {"max_concurrent": 6, "agent_timeout_seconds": 600,
            "max_turns": 24, "manager_max_iterations": 32,
            "progress_interval_seconds": 5, "delta_coalesce_ms": 50},
  "tools": {"disabled": [], "enabled": [], "web_search_max_results": 8,
            "proxy": {"http": "", "https": "", "no_proxy": ""}},
  "memory": {"enabled": true, "auto_review": true, "char_limit": 2200,
             "review_max_iterations": 8, "skills_index_max": 50},
  "log": {"level": "info"}
}}
```

### `PUT /api/settings`

Every top-level section is optional; omitted sections keep their current value.
The file is rewritten atomically and the model pool is invalidated, so the next
turn uses the new endpoint without a restart.

Per provider, `api_key` is three-valued:

| `api_key` | effect |
|---|---|
| absent | keep the stored key |
| `"new-key"` | replace it |
| `""` | clear it |

`models.providers` must not be empty (`400`). Unknown or out-of-range numbers are
normalized to their defaults rather than rejected.

### `GET /api/models`

```json
{"models": [{"id": "default", "label": "some-model", "model": "some-model", "ready": true}],
 "default": "default", "mock": false}
```

### `GET /api/tools`

The catalog the Settings dialog lays out, plus which tools are currently active.

```json
{"catalog": [{"name": "read", "title": "Read file", "summary": "…",
              "group": "files", "network": false, "default_off": false}],
 "enabled": ["edit", "exec", "glob", "grep", "ls", "read", "tree", "web_fetch", "web_search", "write"]}
```

`group` is `files`, `shell` or `web`. `default_off` tools (`python_runner`,
`screenshot`) need something zwai does not ship and must be switched on
explicitly.

## Projects

A project is a working directory, an instruction and a memory shared by its
conversations. See [CONFIG.md](CONFIG.md) for the budgets and
[DATA_MODEL.md](DATA_MODEL.md) for the on-disk layout.

### `GET /api/projects`

```json
{"projects": [{"id": "pj_ab12…", "name": "Quarterly report",
               "system_prompt": "…", "workdir": "/Users/me/work/report",
               "resolved_workdir": "/Users/me/work/report",
               "memory_enabled": true,
               "memory_dir": "/Users/me/.zwai-swarm/projects/pj_ab12…/memory",
               "created_at": "…", "updated_at": "…"}]}
```

`workdir` is what the user chose and is empty when zwai manages the directory;
`resolved_workdir` is where the agents actually work, so no client has to derive
a path.

### `POST /api/projects` → `201`

Body `{"name": "…", "system_prompt": "optional", "workdir": "optional",
"memory_enabled": true}`. A name is required (`400`). `workdir` must be an
absolute path to an existing directory — anything else is `400` with
`code: "workdir"`, which the project dialog shows against that field. Left
empty, the project gets `projects/<id>/workspace` under the data directory.
Omitting `memory_enabled` takes the configured default rather than switching
memory off.

### `GET /api/projects/:id` · `PATCH /api/projects/:id`

`PATCH` accepts `name`, `system_prompt`, `workdir` and `memory_enabled`; an
omitted field is left alone. Both respond `{"project": {…}}`.

### `DELETE /api/projects/:id` → `204`

Deletes the project, **its conversations and everything it remembered**. A
`workdir` the user supplied is never touched; a directory zwai created for the
project is removed with it.

### `GET /api/projects/:id/memory`

```json
{"memory": {
  "dir": "/Users/me/.zwai-swarm/projects/pj_ab12…/memory",
  "enabled": true,
  "memory": {"text": "…", "entries": ["…"], "chars": 412, "limit": 2200, "rev": "a1b2c3d4e5f6"},
  "skills": [{"name": "weekly-rollup", "description": "…", "updated_at": "…"}]
}}
```

`enabled` is false when either the project or `memory.enabled` has it off: what
is stored stays readable, and nothing is carried into a prompt. `entries` are
the notes as the prompt sees them, split on blank lines. `skills` carries names
and one-line descriptions only — the body is fetched per skill, exactly as an
agent fetches it with `skill_view`.

### `PUT /api/projects/:id/memory`

Body `{"text": "…", "rev": "a1b2c3d4e5f6"}`, replacing the notes wholesale — the
hand edit behind the Memory panel. `rev` is the snapshot the editor loaded; a
write against a stale one is `409` with `code: "conflict"` and the current
snapshot in `memory`, so the panel can show both without a second round trip
that could itself be stale. Omit `rev` and the write is unconditional, which is
what a client that never read has to do.

Responds with the new snapshot `{"memory": {…}}`. The character limit still
applies: notes that no longer fit in a prompt are the same problem whoever
typed them (`400`).

### `GET /api/projects/:id/skills/:name`

```json
{"skill": {"name": "weekly-rollup", "description": "…",
           "body": "## Steps\n1. …", "updated_at": "…"}}
```

`404` for a skill nobody wrote, `400` for a name that could never be one
(anything outside `[a-z0-9-]`).

### `DELETE /api/projects/:id/skills/:name` → `204`

## Conversations

### `GET /api/threads?archived=1&project=<id>`

```json
{"threads": [{"id": "th_ab12…", "title": "Deadline sweep", "provider_id": "default",
              "reasoning_effort": "", "archived": false, "project_id": "pj_ab12…",
              "created_at": "2026-09-15T11:03:12.884+08:00",
              "last_active_at": "2026-09-15T11:31:02.114+08:00", "running": true}]}
```

`archived=1` returns the archived ones instead. `project=<id>` returns only that
project's conversations, which is what the sidebar filter sends. `running` is
computed from the live runtimes in one pass, so the sidebar does not poll per
row. `reasoning_effort` is the conversation's thinking level (`""`, `low`,
`medium`, `high`); empty means the model's own default. `project_id` is empty for
a conversation that belongs to no project.

### `POST /api/threads` → `201`

Body `{"title": "optional", "provider_id": "optional", "project_id": "optional"}`
(an empty body is fine). A conversation created without a title gets one from its
first message. With a `project_id` it works in that project's directory, carries
its instruction and its memory, and an unknown id is rejected (`404`).

### `GET /api/threads/:id`

Returns the conversation and its live status:

```json
{"thread": {"id": "th_ab12…", "…": "…", "running": true},
 "status": {"thread_id": "th_ab12…", "running": true, "turn_id": "tn_cd34…",
            "started_at": "2026-09-15T11:31:00.100+08:00",
            "elapsed_ms": 4120, "workers": 2}}
```

`started_at` is absent when nothing is running (never a zero timestamp, which a
client would happily turn into a two-thousand-year elapsed time).

### `PATCH /api/threads/:id`

Body may contain `title`, `provider_id`, `reasoning_effort`, `archived`,
`project_id`. An empty title is rejected; an unknown `provider_id` or
`project_id` is rejected. `reasoning_effort` is one of `""` (the model default),
`low`, `medium` or `high` — a blank clears back to the default, and any other
value is rejected the same way an unknown provider is. An empty `project_id`
takes the conversation out of its project; from then on it works in its own
workspace again. Responds like `GET`.

### `DELETE /api/threads/:id` → `204`

Deletes the conversation, its transcript and its events. Its workspace directory
goes too **only when zwai created it**: a conversation in a project shares that
project's directory, which may be the user's own repository.

## Turns

### `POST /api/threads/:id/turns` → `202`

Body `{"text": "…"}`. Returns the created turn immediately:

```json
{"turn": {"id": "tn_cd34…", "thread_id": "th_ab12…", "status": "running",
          "user_text": "…", "provider_id": "default", "model": "some-model",
          "reasoning_effort": "high",
          "started_at": "2026-09-15T11:31:00.100+08:00"}}
```

`202`, not `200`: the answer arrives on the event stream, because a turn
routinely outlives the request that started it. `409 busy` while one is running.

### `POST /api/threads/:id/steer` → `202`

Body `{"text": "…"}`. Delivers guidance to the running turn, injected at the next
turn boundary — it never interrupts an in-flight model call or tool.

```json
{"steered": true}
```

If nothing is running it **starts a turn instead** and answers
`{"steered": false, "turn": {…}}`. From the user's side both are "I pressed
Enter", and letting the browser decide would race with the turn finishing.

### `POST /api/threads/:id/interrupt` → `202`

Cancels the running turn; the transcript generated so far is kept and the turn is
recorded as `cancelled`. `409 idle` when nothing is running.

### `GET /api/threads/:id/turns`

Every turn of the conversation, oldest first. This is what renders the
"Worked for 12s" footers.

### `POST /api/threads/:id/review` → `202`

Reviews the conversation's most recent completed turn again, curating the
project's memory from it. Answers with the turn being reviewed
(`{"turn": {…}}`), not the outcome: the review is a background job and its
result arrives on the event stream as `memory_review`, the same way a turn's
answer does. `409 idle` when the conversation is in no project, has memory off,
or has no finished turn to read.

## Event stream

### `GET /api/threads/:id/events?since=<seq>`

`text/event-stream`. Replays stored events after `since`, emits a `ready` event,
then streams live. Prefer the standard `Last-Event-ID` header — a browser
`EventSource` sends it automatically on reconnect, so resuming needs no client
bookkeeping. `since` is the fallback for a first connection at a known position.

```
id: 41
event: reasoning
data: {"thread_id":"th_ab12…","turn_id":"tn_cd34…","seq":41,"kind":"reasoning",
       "agent_id":"manager","text":"…","created_at":"…"}

event: delta
data: {"thread_id":"th_ab12…","turn_id":"tn_cd34…","seq":0,"kind":"delta",
       "agent_id":"manager","text":"The three files…","created_at":"…"}

event: ready
data: {"seq":41,"status":{"running":true,"…":"…"}}

: keep-alive
```

Event names (the SSE `event:` field and the payload's `kind`):

| kind | meaning |
|---|---|
| `user_message` | the request that started this turn |
| `reasoning` | a completed thought block |
| `reasoning_delta` | streamed thinking, **full text so far**, not a fragment |
| `delta` | streamed answer, **full text so far** |
| `agent_message` | a completed assistant message |
| `tool_call` | `Text` is `name(args)`, paired by `tool_call_id` |
| `tool_result` | the result, paired by `tool_call_id`. Newlines are kept so the UI can render a file body; clipped at 64k runes |
| `spawned` | a sub-agent started; `text` is its role, `agent_id` is its id |
| `finished` | a sub-agent finished; `err` set when it failed |
| `turn` | an agent started a model turn (`turn N`) |
| `steer` | guidance was accepted |
| `cleanup` | sub-agents were stopped at the end of the turn |
| `progress` | a pulse while the turn runs (see below); `seq` is 0, not stored |
| `memory_review` | the post-turn review of a project's memory finished (see below) |
| `done` | the turn finished; `text` is the final answer |
| `error` | the turn failed; `err` explains |
| `ready` | replay is complete (no `seq`, not stored) |

Two rules the client depends on:

1. **Deltas carry the full text so far**, so a dropped one cannot corrupt the
   rendering. Replace, do not append. The engine coalesces them for
   `swarm.delta_coalesce_ms` (default 50): tokens inside the window replace the
   pending event, so a 50-token-per-second model is one event, not fifty. A
   tool call, a finished worker or the end of the turn flushes whatever is still
   held.
2. **Only stored events have `seq > 0` and an SSE `id`.** Deltas and progress
   pulses are broadcast live and never stored, which is why a reconnect resumes
   at a completed event.

### Progress pulses

While a turn runs the server emits a `progress` event every
`swarm.progress_interval_seconds` (default 5). It exists for the stretches where
every agent is inside a slow tool call and nothing streams: without it a busy
run and a stuck one look identical. `text` is JSON:

```json
{
  "elapsed_ms": 45120,
  "agents": [
    {"agent_id": "reader-1", "role": "reader", "status": "running", "elapsed_ms": 30100}
  ]
}
```

`status` is `running`, `done` or `failed`, and all durations are measured on the
server. There is no activity text and no running total: what an agent is doing
already arrives as its own events, and a total taken a pulse ago would
contradict the `spawned`/`finished` events a client has since received. A pulse
is a snapshot, not a fact about the timeline: it carries no sequence number, is
never stored, and `agents` is always a list. Treat it as advisory — a pulse
built just before a sub-agent finished can arrive just after the `finished`
event, so it must not be used to change an agent's status.

A heartbeat comment is sent every 20s. A client that falls behind is caught up
from the database rather than dropped, so no stored event is lost.

### Memory reviews

A turn of a project conversation with memory on is read back afterwards, and one
`memory_review` event is stored under **the turn's own id** — so one turn id
still reaches everything that happened, including the reviewer's model calls
(`agent_id: "memory-reviewer"` in `llm_calls`). `text` is JSON:

```json
{
  "changed": true,
  "notes": {"add": 1, "replace": 1},
  "skills": [{"target": "skill_manage", "action": "create", "name": "a-procedure", "text": "when it applies"}],
  "changes": [
    {"target": "memory", "action": "add", "text": "the durable fact that was stored"},
    {"target": "skill_manage", "action": "create", "name": "a-procedure", "text": "when it applies"}
  ],
  "note": "Kept what this project treats as done.",
  "notify": "on",
  "err": ""
}
```

The event is stored even when `changed` is false: a review that left no trace
could not be told apart from one that never ran. The turn's status is not
affected — a failed review (`err`) costs a note, not the answer.

`notify` is `off`, `on` or `verbose` — `memory.notifications` at the moment the
review finished, stamped so a later settings change does not rewrite history.
The transcript shows a line when something was kept (`on`), plus a preview of
the written text (`verbose`), or nothing (`off`). Failures always show. The
Trace tab still lists the event either way.

## Workspace files

### `GET /api/threads/:id/files`

```json
{"workspace": "/Users/me/.zwai-swarm/workspaces/th_ab12…",
 "files": [{"path": "uploads/notes.md", "name": "notes.md", "size": 812,
            "dir": false, "modified": "…", "uploaded": true}]}
```

`path` is workspace-relative and sorted so directories group with their contents;
`uploaded` marks what the human put there, as opposed to what agents produced.
The listing is capped at 2000 entries.

### `POST /api/threads/:id/files` → `201`

`multipart/form-data` with one or more parts named `files` (or `file`). Saved
into `uploads/`; a name that already exists is kept alongside the old one rather
than overwriting it. One file may not exceed 256 MiB.

```json
{"files": [{"id": 1, "thread_id": "th_ab12…", "name": "notes.md",
            "rel_path": "uploads/notes.md", "size": 812, "created_at": "…"}]}
```

### `GET /api/threads/:id/download/<path>`

Streams the file. Paths containing `..` are rejected (`400`), and every response
is `Content-Disposition: attachment` with `X-Content-Type-Options: nosniff` — an
agent-written HTML file must not run as script in the app's own origin.

### `DELETE /api/threads/:id/download/<path>` → `204`

### `POST /api/threads/:id/reveal`

Body `{"path": "optional/relative/path"}`, defaulting to the workspace root.
Opens the platform file manager. **Desktop only**: in a browser this is `501`
and `capabilities.reveal` is false, so the UI offers a download instead.

## Troubleshooting

### `GET /api/trace/:turn`

One request, one turn, everything about it — the same data `zwai trace` prints.

```json
{"turn": {"id": "tn_cd34…", "status": "done", "duration_ms": 12480, "…": "…"},
 "events": [{"seq": 1, "kind": "user_message", "…": "…"}],
 "llm_calls": [{"agent_id": "manager", "model": "some-model", "input_msgs": 6,
                "input_chars": 4210, "output_chars": 880, "duration_ms": 2210}]}
```

Model calls record sizes and durations, not prompts: enough to explain why a turn
was slow or failed, without turning the database into a transcript archive.

## Static assets

`GET /` and any unmatched path serve the embedded SPA (`frontend/dist`), with
`index.html` as the fallback so client-side routes survive a refresh. Unknown
`/api/*` paths return `404` JSON instead of HTML.
