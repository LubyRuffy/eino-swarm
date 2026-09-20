# HTTP API

Everything the UI does goes through this API, and the desktop window uses exactly
the same endpoints as the browser. Base URL is the server's own origin:
`http://127.0.0.1:8787` for `zwai web` by default, a random loopback port in
desktop mode (printed on startup and used by the window).

- All bodies are JSON unless stated otherwise; timestamps are RFC 3339 with
  milliseconds.
- **Same-origin only.** No CORS headers are sent, on purpose: this server holds
  your conversations and listens on loopback. Mutating `/api` requests with a
  non-loopback `Origin` are `403`. The PTY upgrade also requires a loopback
  `Origin` (or a loopback peer when `Origin` is empty).
- Errors are `{"error": "...")` with an optional `"code"`.

| status | meaning |
|---|---|
| `400` | malformed body, bad path, or a rejected value; `code: "workdir"` — a project's working directory is not an absolute path to an existing directory |
| `404` | no such conversation / turn / file / project / skill / unread steer / schedule / schedule run |
| `409` | `code: "busy"` a turn is already running; `code: "idle"` nothing is waiting; `code: "no_steer"` Interrupt was asked with no unread steering; `code: "ask_mismatch"` that `ask_user` call is not the open questionnaire; `code: "nothing_to_compact"` compact had nothing to fold; `code: "conflict"` a stale memory write; `code: "skipped_busy"` Run now skipped because the target conversation is already running or a fire is already claimed; `code: "remote_offline"` phone pairing is off or the hub is unreachable |
| `429` | too many terminals are already open |
| `501` | the shell cannot do this (`reveal` / `open` outside the desktop app) |

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
  "capabilities": { "reveal": false, "open_url": false, "memory": true },
  "swarm": { "max_concurrent": 6, "agent_timeout_seconds": 600,
             "max_turns": 200, "manager_max_iterations": 200,
             "progress_interval_seconds": 5, "delta_coalesce_ms": 50,
             "auto_title": true, "title_provider": "", "title_model": "",
             "compact_provider": "", "compact_model": "",
             "context_char_budget": 80000, "compact_keep_messages": 6,
             "auto_compact_tokens": 80000,
             "goal_max_auto_turns": 12,
             "goal_session_max_iterations": 40, "goal_auto_compact_percent": 80,
             "schedule_min_interval_seconds": 30, "schedule_tick_ms": 1000,
             "schedule_max_active": 32},
  "locale": "system",
  "ui": {"locale": "system", "font": "system", "font_size": "medium",
         "content_width": "comfortable"}
}
```

`mode` is `web` or `desktop`. `configured` is false until a default provider has
a base URL and a model name — the UI shows a setup banner until then. `mock` is
true when running on the scripted offline provider. `reasoning_levels` is the
ordered set of explicit thinking levels the composer offers; the empty default
is rendered as "Default" and is not listed. `capabilities.memory` mirrors
`memory.enabled`: the UI disables a project's memory switch when the whole
install has memory off, rather than offering something that will not happen.
`capabilities.open_url` is true only in the desktop app, where an http(s) link
is opened with the system browser instead of inside the webview. `locale` is
`system`, `en` or `zh` — the chrome language from `ui.locale`. The UI applies it
on boot without writing it back. `ui` is the rest of the chrome: `font`
(`system` / `serif` / `mono`), `font_size` (`small` / `medium` / `large`), and
`content_width` (`comfortable` / `full`). `locale` is also at the top level so
an older client that only reads that field still pins the dictionary.

## Settings, models and tools

### `GET /api/settings`

Returns the editable configuration. **The API key is never returned**: each
provider carries `has_api_key` and `ready` instead.

```json
{"settings": {
  "server": {"addr": "127.0.0.1:8787", "open_browser": true},
  "models": {"default": "default", "providers": [
    {"id": "default", "label": "", "base_url": "https://endpoint/v1",
     "model": "some-model", "catalog": ["some-model", "other-model"],
     "timeout_seconds": 300, "context_window": 0, "model_context": {},
     "has_api_key": true, "ready": true}
  ]},
  "swarm": {"max_concurrent": 6, "agent_timeout_seconds": 600,
            "max_turns": 200, "manager_max_iterations": 200,
            "progress_interval_seconds": 5, "delta_coalesce_ms": 50,
            "auto_title": true, "title_provider": "", "title_model": "",
            "compact_provider": "", "compact_model": "",
            "context_char_budget": 80000, "compact_keep_messages": 6,
            "auto_compact_tokens": 80000,
            "goal_max_auto_turns": 12,
            "goal_session_max_iterations": 40, "goal_auto_compact_percent": 80,
            "schedule_min_interval_seconds": 30, "schedule_tick_ms": 1000,
            "schedule_max_active": 32},
  "tools": {"disabled": [], "enabled": [], "web_search_max_results": 8,
            "proxy": {"http": "", "https": "", "no_proxy": ""}},
  "memory": {"enabled": true, "auto_review": true, "char_limit": 2200,
             "entry_max": 360, "review_max_iterations": 8,
             "skills_index_max": 50, "notifications": "on"},
  "personality": {"instructions": ""},
  "log": {"level": "info"},
  "ui": {"locale": "system", "font": "system", "font_size": "medium",
           "content_width": "comfortable"},
  "remote": {"enabled": false, "hub_url": "", "thread_limit": 5,
             "summary_chars": 280, "open_turns": 6, "event_chars": 4000}
}}
```

### `PUT /api/settings`

Every top-level section is optional; omitted sections keep their current value.
The file is rewritten atomically and the model pool is invalidated, so the next
turn uses the new endpoint without a restart. `swarm.max_concurrent` is also
pushed onto every live or parked registry in this process, so workers already
queued under the old cap start as soon as a slot opens; lowering it does not
kill in-flight workers. A language-only write is
`{"ui":{"locale":"zh"}}` and must not wipe swarm, models, the typeface, or the
conversation column. Unknown locale values become `system`; unknown `font` /
`font_size` / `content_width` become `system` / `medium` / `comfortable`.
Omitted ui fields keep what is stored, so a language PUT cannot reset the
typeface.

Per provider, `api_key` is three-valued:

| `api_key` | effect |
|---|---|
| absent | keep the stored key |
| `"new-key"` | replace it |
| `""` | clear it |

`models.providers` must not be empty (`400`). Unknown or out-of-range numbers are
normalized to their defaults rather than rejected. Per provider, `catalog` is
the last discovered model list: omit it to keep what is stored, send `[]` to
clear it. `context_window` and `model_context` are the same: omit them to keep
the stored windows, send `0` / `{}` to clear. A window is never invented from
the model name. The Settings UI edits `model_context` per catalog name; `context_window`
is only the fallback for a name that still has none.

### `GET /api/models`

Every selectable model across every endpoint. One provider that listed three
names is three rows; the composer groups them by `provider_id` (`provider_label`
is the provider's name, never its default model).

```json
{"models": [{
  "id": "default\talpha", "provider_id": "default", "provider_label": "Main",
  "label": "alpha", "model": "alpha", "ready": true, "default": true,
  "context_window": 128000
}],
 "default": "default", "mock": false}
```

`id` is the composer's opaque selection key (`provider_id`, tab, `model`).
`default` on a row is that provider's configured default name. `default` at the
top level is still the default **provider** id. `context_window` is that name's
token limit: a discovered per-name value, else the provider fallback, else `0`
(unknown — the meter then shows a count without a percentage). Offline (`--mock`)
reports a simulated window so the ring still moves.

### `POST /api/models/discover`

Lists the names an OpenAI-compatible endpoint serves (`GET {base_url}/models`).
The listing is applied to the Settings document and written the same way as
any other edit (debounced, flushed when leaving the sheet).

```json
{"provider_id": "default", "base_url": "https://endpoint/v1", "api_key": "optional"}
```

`provider_id` fills in a stored URL and key when those fields are omitted.
`api_key` is three-valued like settings: absent uses the stored key, `""` sends
none. Responds `{"models": ["alpha", "beta"], "context_windows": {"alpha": 128000}}`.
`context_windows` only includes names the listing actually reported a limit for
(`context_length`, `max_model_len`, `context_window`, `max_context_length`,
`max_input_tokens`, `n_ctx`, `max_seq_len`, including one nesting under
`top_provider` / `meta` / `limits` / `parameters`). Output caps
(`max_tokens`, `max_completion_tokens`) are ignored. Most OpenAI-compatible
`/models` bodies have none; those leave the map empty rather
than inventing a number from the name. Offline (`--mock`) fills the scripted
window so Settings still has something to save. A blank URL is `400`. An unknown
`provider_id` is `400` only when no `base_url` is sent — a URL still lists,
so Settings can Discover on a row that has not been saved yet.

A provider is ready for a turn once it has a base URL **and** a default model;
discover only needs the URL.

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

## Phone pairing

These endpoints drive the desktop QR. They are still same-origin loopback HTTP.
The phone does **not** call them; it talks pairlink to the hub, and the hub
forwards sealed frames to this process.

`remote` in `GET/PUT /api/settings` is `{enabled, hub_url, thread_limit,
summary_chars, open_turns, event_chars}`. `hub_url` is whatever you typed — never compiled
in. A PUT of `remote` reloads the pairlink host. The Host Token is **not** in
settings JSON; it lives under `$ZWAI_HOME/remote/` and is written with
`PUT /api/remote/token`.

### `GET /api/remote/status`

```json
{"enabled": false, "hub_url": "", "has_token": false, "online": false,
 "fingerprint": "", "error": ""}
```

`has_token` is a boolean. The Host Token is never returned.

### `PUT /api/remote/token`

Body `{"token": "…"}`. Empty string deletes the stored token. Reloads the
pairlink host. Returns the same JSON as `GET /api/remote/status`.

### `POST /api/remote/offer`

Mints a short-lived pairing code and a PNG. `409` `remote_offline` when the
hub URL or Host Token is missing.

```json
{"uri": "pairlink:v1:<hub_url>:<code>:<host_spk>",
 "pairing_id": "…",
 "expires_at": "2026-09-19T12:00:00Z",
 "png": "data:image/png;base64,…"}
```

`uri` is what a camera must decode. The same string may be pasted. `png` is
a high-contrast QR of that URI.

### `GET /api/remote/bindings`

```json
{"bindings": [{"id": "…", "device_fp": "abcd1234efgh5678",
               "created_at": "…", "session_id": "…"}]}
```

Fingerprints only. No Host Token, no session keys.

### `POST /api/remote/bindings/:id/revoke`

Drops that phone. Further tickets fail at the hub.

The slim RPC the phone sends over pairlink is not an HTTP API. Request ops:
`list` / `more` / `open` / `start` / `send` / `steer` / `stop` / `answer` /
`watch` / `unwatch`. Default list size is 5 threads; `more` pages.
Responses carry `path` (`relay` or `direct`) and `session_id` so `zwai trace`
can join the hop.

`watch` `{thread_id, since}` subscribes to the same event kinds as desktop
SSE (`frontend/src/lib/stream.ts` `KINDS`). The PC pushes `event` (one
clipped `EventView`, same `seq` as the store), then `ready` `{seq, status}`,
and `lagged` when a slow phone missed a live frame — catch-up is replayed
from the database, not dropped. `spawned` bodies are stripped; `tool_delta`
is one line; other text is capped by `event_chars` (default 4000). A frame
over 64KiB is dropped rather than tearing the link. Settings, Files, PTY
and Trace stay on the PC.

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
               "skills": [{"name": "weekly-rollup", "description": "…", "updated_at": "…"}],
               "sort_rank": 0, "created_at": "…", "updated_at": "…"}]}
```

`workdir` is what the user chose and is empty when zwai manages the directory;
`resolved_workdir` is where the agents actually work, so no client has to derive
a path. `skills` is the same index the Memory panel and the prompt use — names
and one-line descriptions, never bodies. The sidebar no longer lists them
under the project; **View skills** on the row menu opens the Memory tab.
An unreadable memory store is an
empty list on this endpoint rather than a failed listing: hiding every project
because one directory is broken would be the worse failure. Bodies stay behind
`GET /api/projects/:id/skills/:name`. Default order is `sort_rank` then last
updated (`updated_at`, which a conversation in the project bumps). Unranked
(`sort_rank` `0`) rows interleave by last update with ranked ones, so an idle
unranked project does not sit above one that was just used. A drag writes a
positive rank and pins relative order.

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

### `PUT /api/projects/reorder` → `200`

Body `{"ids": ["pj_a", "pj_b"]}`. Pins that order in the sidebar. Unknown ids are
`404`; a repeated id is `400`. An empty list is a no-op. Responds with the same
`{"projects": […]}` as `GET /api/projects`.

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
agent fetches it with `skill_view`. That index is the project's memory, not a
`SKILL.md` already in the workspace.

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
              "model": "alpha",
              "reasoning_effort": "", "goal": "", "compacted": false,
              "archived": false, "project_id": "pj_ab12…", "pinned": false,
              "created_at": "2026-09-15T11:03:12.884+08:00",
              "last_active_at": "2026-09-15T11:31:02.114+08:00",
              "sort_rank": 0, "running": true}]}
```

`archived=1` returns the archived ones instead. `project=<id>` returns only that
project's conversations. The sidebar no longer uses that as a filter: it lists
every conversation and groups them. Default order is
`sort_rank` then last activity, then unranked (`0`) rows interleave by last
activity with ranked ones. `sort_rank` `0` means never dragged — a stale
unranked row does not sit above a ranked one that just ran. A drag writes a
positive rank and pins relative order inside its group (Recents or one project). `pinned` is a separate flag: a
project topic the user is tracking at the top of the sidebar, ordered by
`pinned_at`. `running` is computed from the live runtimes in one pass, so the sidebar does
not poll per row. `reasoning_effort` is the conversation's thinking level (`""`,
`low`, `medium`, `high`); empty means the model's own default. `project_id` is empty for
a conversation that belongs to no project. `goal` is the standing objective from
`/goal` (empty when none). `goal_complete` is true after the manager called
`complete_goal`; `reopen_goal` in that turn, or **Start** on the Done banner,
clears it and pursuit continues. `goal_blocked` is true after `block_goal` or after a pursuing
turn fails for a reason that is not a recoverable model error (progress needs the
human or an external change). A truncated tool-call JSON, a `429`, or a dropped
stream retries inside the same turn twice, then auto-continues; `goal_capped` is true after consecutive
auto-continues hit `swarm.goal_max_auto_turns`, or after the human
interrupts a pursuing turn (the banner shows Paused, a line that this is
not an error, and labelled Start); `goal_idle`
is true after an auto-continue finished with no counted tool activity
(Start or a human message resumes). The objective text stays in
every case so the banner can show it. `goal_block_reason` is the optional
one-line reason from `block_goal`, or the public turn error when a pursuing
turn dies before the manager can call it. `goal_started_at` is when the current
objective was set (not edited). `plan_mode` is true while `/plan` is open.
`plan_markdown` is the current plan body (also written to
`$ZWAI_HOME/plans/<thread_id>/PLAN.md`). `compacted` is true after `/compact` or auto-compact has folded
earlier replay into a briefing; the event log is unchanged.

### `POST /api/threads` → `201`

Body `{"title": "optional", "provider_id": "optional", "project_id": "optional"}`
(an empty body is fine). A conversation created without a title shows its first
message as a placeholder, then gets a short generated name after the first
finished turn (`swarm.auto_title`, on by default). `title_auto` on the thread is
true while the engine still owns that name; a title supplied here, or typed
later via rename, clears it and is never overwritten. It follows the provider's default
model until the composer picks one (`model` on the thread). With a `project_id`
it works in that project's directory, carries its instruction and its memory, and
an unknown id is rejected (`404`).

### `GET /api/threads/:id`

Returns the conversation and its live status:

```json
{"thread": {"id": "th_ab12…", "…": "…", "running": true},
 "status": {"thread_id": "th_ab12…", "running": true, "turn_id": "tn_cd34…",
            "started_at": "2026-09-15T11:31:00.100+08:00",
            "elapsed_ms": 4120, "workers": 2,
            "awaiting_continue": false, "awaiting_answer": false},
 "usage": {
   "context_tokens": 71300, "context_window": 256000,
   "turn": {"prompt_tokens": 12000, "completion_tokens": 3100,
            "cached_tokens": 4000, "reasoning_tokens": 800,
            "total_tokens": 15100, "calls": 4},
   "thread": {"prompt_tokens": 71300, "completion_tokens": 3100,
              "cached_tokens": 4000, "reasoning_tokens": 800,
              "total_tokens": 74400, "calls": 4}
 }}
```

`started_at` is absent when nothing is running (never a zero timestamp, which a
client would happily turn into a two-thousand-year elapsed time).
`awaiting_continue` is true while the manager is paused at its tool-round cap.
`awaiting_answer` is true while `ask_user` is blocked waiting for the human.
The turn is still `running` in both cases.

`usage` is the composer meter. `context_tokens` is the **last manager prompt**
(workers, the namer and the reviewer have their own prompts and would lie).
`context_window` is the selected model's limit, or `0` when nobody has said.
When it is `0` the ring still draws a growing arc (scaled by the compact
character budget) but the label stays a count, not a fake percentage.
`turn` / `thread` are billed totals from `llm_calls` (in, out, cached, thinking,
call count). A new conversation is all zeros. `context_chars` /
`context_budget` on the thread object, when present, are the rune compact hint
and are not these token counts.

### `PATCH /api/threads/:id`

Body may contain `title`, `provider_id`, `model`, `reasoning_effort`, `archived`,
`pinned`, `project_id`, `goal`, `goal_edit`, `goal_resume`, `plan_mode`,
`plan_markdown`. An empty title is rejected; an unknown `provider_id` or
`project_id` is rejected. `model` is the name this conversation sends from the
next turn; empty follows the provider's configured default. `reasoning_effort` is
one of `""` (the model default), `low`, `medium` or `high` — a blank clears back
to the default, and any other value is rejected the same way an unknown provider
is. An empty `project_id` takes the conversation out of its project; from then on
it works in its own workspace again. `pinned` tracks a conversation at the top
of the sidebar (`pinned_at` is set on pin and cleared on unpin). `goal` is the standing objective (empty
clears it); it is recorded as a `goal` event, resets completion/block/cap/auto-continue
counts, and is pursued from the next model round until `complete_goal`,
`block_goal`, or a clear. The composer starts that round immediately when
idle. Clearing also interrupts a running turn. Setting while a turn is
running steers the new text into this turn's context, not only the next.
`goal_edit: true`
with `goal` changes the text without reopening pursuit (a completed goal is
still reopened). A running turn is steered so the new text is in this turn's
context, not only the next. `goal_resume: true` starts the next turn for an
open objective (blocked, capped, idle, or completed in error). A missing goal is
rejected. Responds like `GET`.

`plan_mode: true` enters planning while idle (`409 busy` if a turn is running).
Write/edit/exec and similar are unmounted; `propose_plan` and `ask_user` stay.
An open `/goal` is paused (`goal_capped` / interrupted-class). `plan_mode: false`
leaves planning without starting a turn. `plan_markdown` is a human edit of the
plan body while `plan_mode` is true (file + column + `plan_updated` event).

### `POST /api/threads/:id/compact` → `200`

Folds older replay into a briefing for later turns. The transcript the human
sees does not change: events stay. Recent messages
(`swarm.compact_keep_messages`, default 6) stay verbatim. When the conversation
already has a rolling session briefing (`session_memory` on the thread), that
text is copied as the compact view and the compact summarizer is not called
(including a `/goal` wrap-up when there is not enough replay to fold).
A briefing that is a `Tool:`/`Human:`/`Assistant:` transcript or pasted exec
JSON is refused: the thread is unchanged and `compacted` carries `err`.
Otherwise the summarizer follows the conversation's model unless
`swarm.compact_provider` / `swarm.compact_model` pin a different one (same
empty-means-follow rule as the conversation namer). The summarizer is streamed.
Silence between chunks uses that endpoint's `timeout_seconds` (idle, next
byte), same as any other model call. A timeout is recorded on the `compacted`
event and the thread is unchanged.
Responds like `GET`.

The same fold also runs **during a turn**, before a manager model call, when
billed or estimated prompt tokens exceed `swarm.auto_compact_tokens` (default
80000). Older replayable tool results are cleared first; if that is not
enough, the session briefing is caught up and copied in, and only then is
eino's summarizer used. That path is not this endpoint: the UI hears a live
`compacted` event (`seq` 0, `phase: "start"`) and then a stored one with
`auto: true` and the token counts.

`409` with `code: "busy"` while a turn is running. `409` with
`code: "nothing_to_compact"` when there is not enough replay to fold and no
accepted session briefing to copy (already short, or already compacted through
the same tail).

### `DELETE /api/threads/:id` → `204`

Deletes the conversation, its transcript and its events. Its workspace directory
goes too **only when zwai created it**: a conversation in a project shares that
project's directory, which may be the user's own repository.

### `PUT /api/threads/reorder` → `200`

Body `{"ids": ["th_a", "th_b"]}`. Pins that order in the sidebar group those
ids belong to. Unknown ids are
`404`; a repeated id is `400`. An empty list is a no-op. `?project=<id>` on the
same URL filters the response the way `GET /api/threads` does. The sidebar
sends the ids of one group (Recents or one project) and lists everything
afterwards. Responds `{"threads": […]}`.

### `POST /api/threads/:id/turns`

## Turns

### `POST /api/threads/:id/turns` → `202`

Body `{"text": "…", "images": [{"name": "clip.png", "mime": "image/png", "data": "<base64>"}], "files": ["uploads/notes.md"], "from_event_seq": 12}`.
`text` may be empty when `images` or `files` is not, or when `from_event_seq`
points at a `user_message` that still has its original images. Each image is sniffed (png/jpeg/gif/webp);
a renamed HTML file is rejected. At most 8 images, 8 MiB decoded each. They are
**visual input**, stored under `$ZWAI_HOME/inputs/<thread>/`, not workspace
uploads. `files` are workspace-relative paths of uploads attached to **this**
send (always under `uploads/`). They are named on the user message so the
model reads those first instead of scavenging older leftovers in the same
folder. Unknown, escaped, or non-upload paths are `400`. At most 32 files.

A `text` that is `/goal <objective>` is the slash command, not a chat line.
The name is the ASCII identifier after `/` (or the fullwidth solidus `／`,
or the CJK punctuation comma `、` a Slash key emits under Chinese
punctuation);
the rest is the objective, space optional so a glued CJK IME string still
matches. That pins the standing objective and starts pursuit when idle, or
steers the live turn. The stored `user_text` is the objective. Bare `/goal`
is `400`. `/goals …` is still an ordinary message.

`from_event_seq`, when set, names a stored `user_message` to replace. That
event and everything after it is deleted (later turns, their messages and
model-call rows, and any queued follow-up). A compact briefing whose
`compact_through_seq` landed in the deleted range is cleared, and so is a
session briefing whose `session_memory_through_seq` did. Sequence
counters are **not** wound back, so a live `Last-Event-ID` still lands on new
events. A running turn is interrupted first, so this is not `409 busy`. A
seq that is not a `user_message` is `400`; a missing seq is `404`. Original
pasted images are reused when the body has none. `POST …/steer` ignores this
field. Live clients get a `rewound` event (`seq` 0, not stored; `text` is the
cut seq) so the UI can drop everything below that bubble before the replacement
`user_message` arrives.
Returns the created turn immediately:

```json
{"turn": {"id": "tn_cd34…", "thread_id": "th_ab12…", "status": "running",
          "user_text": "…", "provider_id": "default", "model": "some-model",
          "reasoning_effort": "high",
          "started_at": "2026-09-15T11:31:00.100+08:00"}}
```

`202`, not `200`: the answer arrives on the event stream, because a turn
routinely outlives the request that started it. `409 busy` while one is
running, unless the body carries `from_event_seq`.

### `POST /api/threads/:id/steer` → `202`

Body same as a turn (`text` + optional `images` + optional `files`). Delivers guidance to the running
turn, injected at the next model boundary — by itself it never interrupts an
in-flight model call or tool. The `steer` event is recorded when the send is
accepted (queued), with `images` as `{id,name,mime}` handles. Attached `files`
are named on that steer the same way as on a new turn. The UI pins that bubble
under the live working line until the manager starts another model round; it is
not mixed into the turn body while it is still sitting in the inbox.

This is **Steer** / ⌘Enter, not ordinary Enter. Enter while a turn is running
hits the follow-up queue instead. **Interrupt** on that pin
(`POST …/preempt`) aborts the current manager tool or generate so every unread
steer lands on the next model call of **this** turn. **Delete** on one bubble
(`DELETE …/steers/:seq`) retracts that inbox item; the manager never sees it.

```json
{"steered": true}
```

If nothing is running it **starts a turn instead** and answers
`{"steered": false, "turn": {…}}`. A Steer click that lost the race to the turn
finishing still lands.

### `POST /api/threads/:id/preempt` → `202`

Aborts the current **manager** tool call or in-flight generate so unread
steering is drained on the next model call of this turn. Sub-agents stay up.
Stop (`POST …/interrupt`) is still the turn cancel. `409 idle` when nothing is
running; `409 no_steer` when the inbox is empty.

```json
{"preempted": true}
```

The timeline records `steer_preempted`. Interrupted tools get a synthetic
`tool_result` (`the user interrupted this tool before it finished`) rather than
killing the ReAct graph.

### `DELETE /api/threads/:id/steers/:seq` → `204`

Retracts one unread `steer` whose timeline seq is `:seq`. The `steer` event
stays on the log; a `steer_retracted` row with `text` `{"seq":N}` follows, and
the `[steer]` **message** for that seq is dropped so replay does not feed it
back. `400` when `seq` is not a positive integer. `409 idle` when nothing is
running. `404` when that seq is not an unread steer on this turn (already
consumed, already retracted, or never existed). Duplicate captions are
disambiguated by seq, not by text.

### Follow-ups

A follow-up is a message typed while a turn was already running. It waits for
that turn to finish cleanly, then starts as the next turn. The live turn's own
user text is not queued: a leftover Enter or an IME restoring the committed
string must not schedule the same request again. ⌘Enter (and Steer on a waiting
row) injects into this turn and drops a queued copy of that text. Cancelled and failed
turns leave the remaining queue in place. Refresh, a crash, or quitting the app does not
lose it: the rows live on the conversation and run after a leftover turn
finishes. Editing a waiting row and submitting it moves that message to the
back of the FIFO. Pasted images are not queued (the row is text-only) — they
steer.

### `GET /api/threads/:id/followups`

```json
{"followups": [{"id": "fu_ab12…", "thread_id": "th_ab12…", "seq": 1,
                "text": "…", "created_at": "2026-09-16T12:00:00.000+08:00"}]}
```

Oldest first. Empty is `[]`, not `null`.

### `POST /api/threads/:id/followups` → `202`

Body `{"text": "…"}`. Appends one waiting message. `409 idle` when nothing is
running — the composer then starts a turn instead. `400` for an empty body.

```json
{"followup": {"id": "fu_ab12…", "thread_id": "th_ab12…", "seq": 1, "text": "…",
              "created_at": "…"}}
```

### `DELETE /api/threads/:id/followups/:fid` → `204`

Drops one waiting message. `404` if it was already flushed or never existed.

### `PATCH /api/threads/:id/followups/:fid` → `200`

Body `{"text": "…"}`. Saves an edited waiting message and moves it to the back
of the FIFO (new `seq`, same id). Empty text is `400`. `404` if it was already
flushed. The conversation does not have to be running: leftover rows after Stop
are still editable.

```json
{"followup": {"id": "fu_ab12…", "thread_id": "th_ab12…", "seq": 3, "text": "…",
              "created_at": "…"}}
```

### `POST /api/threads/:id/followups/:fid/steer` → `202`

Pulls that waiting message into the running turn (same injection as
`POST …/steer`). The row leaves the queue. If the turn has already ended the
row is put back and the response is `409 idle`.

```json
{"steered": true}
```

### `POST /api/threads/:id/interrupt` → `202`

Cancels the running turn; the transcript generated so far is kept and the turn is
recorded as `cancelled`. `409 idle` when nothing is running.

### `POST /api/threads/:id/continue` → `202`

Body `{"continue": true|false}`. Answers a pause at the manager's tool-round cap
(`max_iterations` on the event stream). `true` extends the current turn by
another `swarm.manager_max_iterations` rounds; `false` ends it as `cancelled`
with a short "stopped after N tool rounds" rather than eino's graph error.
Steering while paused also continues. `409 idle` when the turn is not waiting.

```json
{"continued": true}
```

### `POST /api/threads/:id/answers` → `202`

Body `{call_id, answers}` or `{text}`. Answers an in-flight `ask_user` on this
turn. `answers` is `{ "<question_id>": { "answers": ["label or free text"] } }`.
`text` is Other for every unanswered question (composer Enter while waiting).
`409 idle` when nothing is waiting; `409 ask_mismatch` when `call_id` is not
the open questionnaire. The same ReAct turn continues after the tool result.

```json
{"answered": true}
```

### `POST /api/threads/:id/plan/implement` → `202`

Leaves planning, remounts implementation tools, and starts an execute turn
with a generic cue. The current `PLAN.md` is injected in the manager extra.
Does not resume a paused `/goal`. `400` when there is no plan body.

```json
{"turn": {"id": "tn_cd34…", "status": "running", "…": "…"}}
```

### `GET /api/threads/:id/turns`

Every turn of the conversation, oldest first. This is what renders the
"Worked for 12s" footers.

### `POST /api/threads/:id/review` → `202`

Reviews the conversation's most recent completed turn again, curating the
project's memory from it. The reviewer reads the stored event log (and the
rolling session briefing), not a compacted ADK transcript. Auto-review after
a turn is skipped when the manager already wrote with `memory` or
`skill_manage`; this endpoint still runs. Answers with the turn being reviewed
(`{"turn": {…}}`), not the outcome: the review is a background job and its
result arrives on the event stream as `memory_review`, the same way a turn's
answer does. `409 idle` when the conversation is in no project, has memory off,
or has no finished turn to read.

## Event stream

### `GET /api/threads/:id/log?before=<seq>&limit=<n>`

JSON page of stored events, **newest page, oldest-first inside it**. `before`
omitted or `0` means the live edge. Each later call with `before` set to the
first seq of the previous page walks upward. `limit` defaults to 80 and is
clamped to 200. Response:

```json
{"events":[{ "seq": 12, "kind": "user_message", "…" : "…" }],
 "has_more": true,
 "roster":[{ "seq": 4, "kind": "spawned", "agent_id": "worker-1", "…" : "…" }]}
```

The front end opens a conversation with one viewport of this tail, then
`GET /api/threads/:id/events?since=<last seq>` for live. Scrolling up fetches
the next older page. An empty conversation is `"events":[]` with
`has_more: false`. The live-edge page (`before` omitted or `0`) also sends
`roster`: every `spawned`, `finished` and `cleanup` row whose seq is **not**
already in `events`. A long turn's viewport is often only manager tools;
without those rows the Agents tab looks empty. Older pages omit `roster` —
the client already has it, and mixing those seqs into `before` would skip
the tools in between. The client hydrates the Agents tab from `roster`
without inserting "Started" rows into the manager transcript — those rows
belong to the contiguous page, or they turn a long `/goal` into a wall of
agent names with no work in between. Clicking a worker whose tools are
outside the viewport fetches `GET /api/threads/:id/agents/:agent/log`.

### `GET /api/threads/:id/agents/:agent/log`

JSON list of that worker's stored events, oldest first. Empty array when
the id is unknown. `404` when the conversation is missing. The live-edge
conversation page is a viewport of recent tools; this is how the Agents
tab shows a worker whose `spawned` row has fallen out of that window
without walking the rest of the log.

```json
{"events":[{ "seq": 6, "kind": "tool_call", "agent_id": "worker-1", "…" : "…" }]}
```

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
| `user_message` | the request that started this turn. `images` is `{id,name,mime}[]` when the send carried pasted vision input; never the pixels |
| `reasoning` | a completed thought block |
| `reasoning_delta` | streamed thinking, **full text so far**, not a fragment |
| `delta` | streamed answer, **full text so far** |
| `agent_message` | a completed assistant message |
| `tool_call` | `Text` is `name(args)`, paired by `tool_call_id` |
| `tool_result` | the result, paired by `tool_call_id`. Newlines are kept so the UI can render a file body; clipped at 64k runes |
| `tool_delta` | live tool output while a call still runs; `text` is the accumulated payload so far (for `exec`, `{stdout,stderr}` JSON), paired by `tool_call_id`. `seq` is 0, not stored. The model still receives one `tool_result` when the call finishes |
| `spawned` | a sub-agent started; `text` is its system prompt, `role` is its role, `agent_id` is its id. Older rows stored the role in `text` too. A later `spawn_agent` for that role reuses the same id: steering while running, a second `spawned` after it finished |
| `finished` | a sub-agent finished; `err` set when it failed |
| `turn` | an agent started a model turn (`turn N`) |
| `steer` | human guidance was accepted. `images` as on `user_message` when the steer carried a paste. The `/goal` session wrap-up cue is **not** this event — it is delivered to the in-flight manager only. Retracting unread steering does not delete this row |
| `steer_retracted` | the human dropped one unread `steer` before the manager read it. `text` is JSON `{seq}` naming that steer. The chat hides the bubble; the Trace log still has both rows |
| `steer_preempted` | Interrupt aborted the current manager tool or generate so unread steering can land on this turn. No payload. Workers were not cancelled |
| `cleanup` | sub-agents were stopped at the end of the turn |
| `resumed` | this turn was left running by a crash or quit and is continuing; `text` is a short notice. The original `user_message` is not repeated. In-flight `tool_call` rows that never got a result are closed first (`tool_result` with `err`: `the previous process stopped`) so a killed `exec` does not keep spinning — except an unfinished `ask_user`, which is re-armed so the human can still answer. Leftover sub-agents are started again under the same `agent_id` (a second `spawned` for that id is the roster coming back, not a twin). Unread `steer` rows stay on the turn. A leftover `max_iterations` confirm is no longer pending |
| `progress` | a pulse while the turn runs (see below); `seq` is 0, not stored |
| `usage` | a live token snapshot after each model call (see below); `seq` is 0, not stored |
| `memory_review` | the post-turn review of a project's memory finished (see below) |
| `max_iterations` | the manager hit `swarm.manager_max_iterations`; `text` is `{"limit":N,"extend_by":N}` and the turn is still running |
| `max_iterations_continued` | the human extended the turn; `text` names how many extra rounds |
| `model_retry` | the manager hit a recoverable ChatModel failure (truncated tool JSON, `429`, a dropped stream) and re-entered this turn; `text` is JSON `{attempt,cap}`. The transcript notice is generic. Invalid tool arguments are dropped from the next model request so a `400 Unterminated string` cannot repeat |
| `title` | the conversation was named; `text` is the new title, `agent_id` is `title-namer`. Not rendered in the transcript |
| `session_memory` | the rolling session briefing was refreshed from the event log; `text` is JSON `{summary,through_seq}`. `agent_id` is `session-memory`. Not rendered in the transcript; compact and the reviewer read the thread fields. Post-turn refresh is background work: a queued follow-up starts as soon as the turn is `done`, and does not wait for this event |
| `goal` | the human set or cleared a standing objective; `text` is the objective (empty when cleared). Resets complete/blocked/capped |
| `goal_complete` | the manager called `complete_goal`; auto-continue stops. `text` is JSON `{summary}`. `reopen_goal` in the same turn records `goal_resumed`, resets the auto-continue budget, and pursuit continues |
| `goal_blocked` | the manager called `block_goal`, or a pursuing turn failed for a reason that is not a recoverable model error. Auto-continue stops until the human resumes. `text` is JSON `{reason}` |
| `goal_resumed` | the human started pursuit again after a block, a cap, an idle open goal, or a completed objective that finished too early — or the manager called `reopen_goal` after a mistaken `complete_goal`. Same Working-clock rule as `goal_continued` |
| `goal_continued` | the runtime started the next turn to keep pursuing an open objective. Not a `user_message`. Clients start the Working clock from this event's `created_at` (a `done` has just cleared `status.started_at`) |
| `goal_capped` | consecutive auto-continues hit `swarm.goal_max_auto_turns` (`text` is JSON `{auto_turns,cap}`), or the human interrupted a pursuing turn (`text` is JSON `{reason:"interrupted"}`). A later human message or resume resets the budget |
| `goal_idle` | an engine-started continuation finished with no counted tool activity (`text` is the notice). Auto-continue stops until a human message or resume; the objective stays open |
| `goal_edited` | the human changed the objective text in place; `text` is the new objective. Status stays put. A running turn is also steered |
| `plan` | the conversation entered `/plan`; write/edit/exec and similar are unmounted. `text` is `planning` |
| `plan_updated` | `propose_plan` or a human edit wrote the plan body. `text` is the markdown (also on disk at `$ZWAI_HOME/plans/<thread_id>/PLAN.md`) |
| `plan_implemented` | the human accepted the plan. Planning ended and an execute turn started. `text` is the generic cue |
| `plan_cancelled` | the human left planning without implementing. `text` is `left planning` |
| `goal_session` | historical: older builds forced a `/goal` turn to end so the next session could start. New runs do not emit it. `text` is JSON `{reason,elapsed_ms,rounds}` where `reason` is `time` (the removed wall-clock cut) or `iterations` (older builds that treated eino's ReAct slice as a session boundary). The turn is `done`, not cancelled. In-flight sub-agents were parked for the next session |
| `compacted` | earlier replay was folded into a briefing, either by `/compact` or automatically at `swarm.auto_compact_tokens`. `text` is JSON `{summary,through_seq,chars_before?,chars_after?,auto?,tokens_before?,tokens_after?,phase?}`. `phase: "start"` is live only (`seq` 0) and means compression is in flight — clients keep the Working clock and show that in chrome (title bar **Compressing**, composer banner), not only as a transcript notice. A stored auto event has `auto: true` and the token counts. `err` is set when the summarizer failed and the thread is unchanged. The transcript notice is generic (the briefing is for later prompts); an icon on that row opens `summary` in a dialog |
| `rewound` | a live client should drop rows from `text` (the cut seq) onward. `seq` is 0, not stored — a reload already has the truncated log |
| `schedule` | a wait was armed. `text` is JSON `{id,kind,title,next_run_at}`. The transcript shows a short chip; the id lives in `detail` so cancel can target it |
| `schedule_fired` | the runtime started this turn because a wait fired. Not a `user_message`. `text` is the short chip (`Scheduled check.`). Same Working-clock rule as `goal_continued` |
| `schedule_skipped` | a due tick could not start (busy). Trace-only; the transcript adds no row |
| `schedule_report` | the scheduled check reported. `text` is JSON `{findings,quiet}`. Empty findings / `quiet:true` hide that turn's chat bubbles; the events stay in the log. When this event is omitted after `schedule_fired` and `done` is empty, the client treats the turn the same way. A findings report plus empty `done` keeps the fired chip |
| `schedule_cancelled` | a wait was cancelled. `text` is the schedule id. The transcript shows a short chip, not the raw id |
| `done` | the turn finished; `text` is the final answer. Empty / whitespace `text` hides chat bubbles only after `schedule_fired` when that turn did not already report findings. An armed `schedule`, `goal_continued`, compact, or other notice plus empty `done` stays visible. Non-empty `text` after a fire is findings and keeps the fired chip |
| `error` | the turn failed; `err` explains |
| `ready` | replay is complete (no `seq`, not stored) |

Two rules the client depends on:

1. **Deltas carry the full text so far**, so a dropped one cannot corrupt the
   rendering. Replace, do not append. The engine coalesces them for
   `swarm.delta_coalesce_ms` (default 50): tokens inside the window replace the
   pending event, so a 50-token-per-second model is one event, not fifty. A
   tool call, a finished worker or the end of the turn flushes whatever is still
   held. `tool_delta` uses the same coalesce window and is keyed by
   `tool_call_id`, so two parallel `exec` calls do not overwrite each other.
2. **Only stored events have `seq > 0` and an SSE `id`.** Deltas, tool deltas,
   progress pulses and usage pulses are broadcast live and never stored, which
   is why a reconnect resumes at a completed event.

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

### Token usage pulses

Each model call records billed tokens on `llm_calls` and then broadcasts a
`usage` event with `seq = 0`. `text` is the same JSON as `GET /api/threads/:id`'s
`usage` object. The composer meter replaces its snapshot; the transcript ignores
the kind. Reloading rebuilds the numbers from `llm_calls` — persisting a copy
per call would double the table `zwai trace` already reads.

`context_tokens` is the last **manager** prompt. Sub-agents, the namer and the
reviewer are billed in `turn` / `thread` but do not move the ring. Hover labels
this-turn totals as billed so a swarm's summed worker prompts are not read as
the context used.

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
could not be told apart from one that never ran. A `skill_manage` create that
collides, or a `memory` write that exceeds `entry_max` or restates a skill
(summary or steps), is a tool refusal — it does not appear in `changes`. The
turn's status is not affected — a failed review (`err`) costs a note, not the
answer.

`notify` is `off`, `on` or `verbose` — `memory.notifications` at the moment the
review finished, stamped so a later settings change does not rewrite history.
The transcript shows a line when something was kept (`on`), plus a preview of
the written text (`verbose`), or nothing (`off`). Failures always show. The
Trace tab's Full log still lists the event either way.

### Conversation titles

A conversation created without a title shows its first message as a placeholder
so the sidebar is readable while the turn runs. After that turn finishes, one
`title` event is stored under **the turn's own id** (`agent_id: "title-namer"`),
and the sidebar / header pick up `text` as the new name. The transcript ignores
the event — a title is metadata, not a chat row — but the Trace tab's Full log
lists it.
Interrupted and failed turns keep the placeholder. A title the user typed
(`POST /api/threads` or rename) is never overwritten. `swarm.auto_title: false`
leaves the placeholder. Empty `title_provider` / `title_model` follow the
conversation's model; pin another name in Settings → Models when an endpoint
lists more than one.

Compact uses the same pin: empty `compact_provider` / `compact_model` follow
the conversation; Settings → Models has a Compact summary picker.

## Workspace files

### `GET /api/threads/:id/files`

```json
{"workspace": "/Users/me/.zwai-swarm/workspaces/th_ab12…",
 "files": [{"path": "uploads/notes.md", "name": "notes.md", "size": 812,
            "dir": false, "modified": "…", "uploaded": true}]}
```

`path` is workspace-relative. The walk is breadth-first: every directory at a
depth is recorded before files, and files are taken round-robin across parents,
so a fat folder that sorts early cannot spend the cap before later siblings the
file manager still shows. `uploaded` marks what the human put there, as opposed
to what agents produced. The listing is capped at 2000 entries. Dot-directories,
`node_modules` and `vendor` are skipped so a repository does not spend that cap
on a VCS or dependency tree (agents can still read those paths). The Files panel
nests this flat walk into a collapsible tree; the payload does not.

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

### `GET /api/threads/:id/input-images/:image_id`

The pixels of one pasted image, `Content-Type` sniffed, `Content-Disposition:
inline` so the transcript can render a thumbnail. `image_id` is the handle from
the event (`img_` + hex). Unknown, traversal, or non-image ids are `404`. This
is not a workspace file and does not appear in the Files listing.

### `DELETE /api/threads/:id/download/<path>` → `204`

### `POST /api/threads/:id/reveal`

Body `{"path": "optional/relative/path"}`, defaulting to the workspace root.
Opens the platform file manager. **Desktop only**: in a browser this is `501`
and `capabilities.reveal` is false, so the UI offers a download instead.

### `GET /api/threads/:id/terminal`

WebSocket. One connection is one PTY, started in that conversation's
workspace — the project's directory when the conversation belongs to one,
otherwise `workspaces/<thread-id>/`. Query `cols` and `rows` set the initial
size (defaults 80×24). The client does **not** send a path: a `cwd` query is
ignored.

First text frame:

```json
{"type":"ready","cwd":"/Users/me/repo","shell":"/bin/zsh"}
```

`ready` is sent **before** the shell is forked, so the tab can name the
directory even when the PTY cannot start (then a `{"type":"error","error":"…"}`
frame follows). Then binary frames are raw PTY output. The client sends binary frames as
keystrokes and text frames `{"type":"resize","cols":120,"rows":32}` to
reflow. Closing the socket kills the process group.

A missing conversation is `404` before the upgrade. Eight sessions per
process is the cap (`429`). Same-origin only: a foreign `Origin` is `403`,
including a DNS-rebind host that matches `Host` but is not loopback. A
missing `Origin` is allowed only from a loopback peer.

### `GET /api/projects/:id/terminal`

The same PTY when there is no open conversation. `cwd` is the project's
resolved working directory. Missing project: `404`.

### `POST /api/open`

Body `{"url": "https://example.invalid/docs"}`. Opens the URL in the platform
browser. **Desktop only**: in a browser this is `501` and `capabilities.open_url`
is false, so the UI uses a new tab (`window.open`) instead. Only absolute
`http`/`https` URLs are accepted (`400` otherwise). The webview must never
navigate to the destination — that would replace the app with a third-party
page.

## Scheduled tasks

Same-origin only: mutating `/api/schedules` with a non-loopback `Origin` is
`403`. No CORS headers.

A wait is a `schedule` row. Each fire (or skipped tick) is a `run`. Inbox
`unread` is a **count** of runs with `unread=true`. Quiet runs are not unread.

`POST /api/schedules/runs/:rid/read` is registered before
`/api/schedules/:id` so `runs` is not captured as an id.

### `GET /api/schedules?status=&kind=`

Returns every wait, optionally filtered. `unread` is the global unread count,
not the filtered subset.

```json
{"schedules": [
  {"id": "sch_ab12…", "kind": "thread", "thread_id": "th_ab12…",
   "origin_thread_id": "th_ab12…", "title": "wake", "prompt": "…",
   "every_s": 60, "status": "active", "next_run_at": "2026-09-19T02:00:00.000Z",
   "created_by": "human", "…": "…"}
], "unread": 2}
```

`kind` is `thread` (wake an existing conversation) or `standalone` (mint a
conversation per fire). `status` is `active`, `paused`, `done`, or
`cancelled`.

### `POST /api/schedules` → `201`

Body: `kind`, `thread_id`, `origin_thread_id`, `project_id`, `provider_id`,
`model`, `title`, `prompt`, exactly one of `delay_s` / `every_s` / `cron`,
optional `max_runs`, optional `until` (RFC 3339). `created_by` is always
`human`; the body cannot set it. Thread wakes need `thread_id`. Responds
`{"schedule": {…}}`.

### `GET /api/schedules/:id`

The row plus its runs, oldest first. Missing ids are `404`.

```json
{"schedule": {"id": "sch_ab12…", "…": "…"},
 "runs": [{"id": "srun_cd34…", "status": "findings", "unread": true,
           "summary": "…", "thread_id": "th_ab12…", "turn_id": "tn_cd34…"}]}
```

### `PATCH /api/schedules/:id`

Pause/resume with `{"status": "paused"}` or `"active"`. Also `title`,
`prompt`, and cadence. When cadence changes, send exactly one of `delay_s`,
`every_s`, or `cron` (the others are cleared and `next_run_at` is recomputed
from now). Cancel is `DELETE`, not a status patch. Responds
`{"schedule": {…}}`.

### `DELETE /api/schedules/:id` → `204`

Cancels an active or paused wait. A second delete of an already-cancelled
row is `204`. Unknown ids are `404`. Cancelling a `kind=thread` wake while
its conversation is idle starts the next standing-objective turn when one
is still open (`goal_continued`). A live turn is left alone; auto-continue
resumes when that turn finishes. A wait with no standing objective stays
idle. Standalone jobs do not start a turn on the origin conversation.

### `POST /api/schedules/:id/run` → `202`

Fires now, even when `next_run_at` is still in the future. Body none.
Responds `{"turn": {…}}` like `POST /api/threads/:id/turns`. The turn is
`schedule_continue`. A paused, done, or cancelled wait is `400`. While the
target conversation is running (or planning), or a fire is already claimed:
`409` with `code: "skipped_busy"`. An early cron fire consumes that due
slot (`next_run_at` becomes the occurrence after the one that was pending);
an interval resets from now. A delay one-shot is marked `done`. The
composer wait banner hides while that conversation is working.

### `POST /api/schedules/runs/:rid/read` → `204`

Clears `unread` on that run. Missing ids are `404`. Already-read is `204`.

## Troubleshooting

### `GET /api/trace/:turn`

One request, one turn, everything about it — the same data `zwai trace` prints.

```json
{"turn": {"id": "tn_cd34…", "status": "done", "duration_ms": 12480, "…": "…"},
 "events": [{"seq": 1, "kind": "user_message", "…": "…"}],
 "llm_calls": [{"agent_id": "manager", "model": "some-model", "input_msgs": 6,
                "input_chars": 4210, "output_chars": 880, "duration_ms": 2210,
                "prompt_tokens": 90, "completion_tokens": 12, "total_tokens": 102,
                "cached_tokens": 20, "reasoning_tokens": 0}]}
```

Model calls record sizes, billed tokens and durations, not prompts: enough to
explain why a turn was slow, expensive or failed, without turning the database
into a transcript archive. Token fields are `0` when the endpoint did not say.

## Static assets

`GET /` and any unmatched path serve the SPA (`frontend/dist`), with
`index.html` as the fallback so client-side routes survive a refresh. Unknown
`/api/*` paths return `404` JSON instead of HTML. From a checkout, `desktop`
and `web` rebuild that bundle when the sources changed and serve the
directory on disk (`frontend.Load`); a shipped binary serves the `go:embed`
snapshot. Without `index.html` the API still works and `GET /` is a 404.
