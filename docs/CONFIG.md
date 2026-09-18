# Configuration

One YAML file, `config.yaml`, in the data directory:

```
$ZWAI_HOME (default ~/.zwai-swarm)/config.yaml     mode 0600
```

`zwai config path` prints it, `zwai config show` prints its effective contents
with the API key redacted. Everything in it is editable from **Settings** in the
app: each control writes itself (debounced; **Back to app** flushes). A
`PUT /api/settings` rewrites the file atomically — a crash mid-write
cannot leave a truncated file that fails to parse on the next start.

Nothing in the binary hardcodes an endpoint or a model name. If it is not in this
file (or seeded from the environment on first run), it does not exist.

## A complete file

```yaml
server:
    addr: 127.0.0.1:8787
    open_browser: true
models:
    default: default
    providers:
        - id: default
          label: ""
          base_url: https://your-endpoint/v1
          api_key: sk-…
          model: your-model
          catalog:
            - your-model
            - your-other-model
          timeout_seconds: 300
          context_window: 0
          model_context: {}
swarm:
    max_concurrent: 6
    agent_timeout_seconds: 600
    max_turns: 200
    manager_max_iterations: 200
    progress_interval_seconds: 5
    delta_coalesce_ms: 50
    auto_title: true
    title_provider: ""
    title_model: ""
    compact_provider: ""
    compact_model: ""
    context_char_budget: 80000
    compact_keep_messages: 6
    auto_compact_tokens: 80000
    goal_max_auto_turns: 12
    goal_session_max_iterations: 40
    goal_auto_compact_percent: 80
    schedule_min_interval_seconds: 30
    schedule_tick_ms: 1000
    schedule_max_active: 32
tools:
    disabled: []
    enabled: []
    proxy:
        http: ""
        https: ""
        no_proxy: ""
    web_search_max_results: 8
memory:
    enabled: true
    auto_review: true
    char_limit: 2200
    entry_max: 360
    review_max_iterations: 8
    skills_index_max: 50
    notifications: on
personality:
    instructions: ""
log:
    level: info
ui:
    locale: system
    font: system
    font_size: medium
    content_width: comfortable
```

Any key you leave out, set to zero or set to an empty string is repaired with its
default on load, so a partially hand-edited file always starts.

## `server`

| key | default | meaning |
|---|---|---|
| `addr` | `127.0.0.1:8787` | listen address for `zwai web`. `--addr` overrides it. Desktop mode ignores it and binds a random loopback port. |
| `open_browser` | `true` | open a browser when `zwai web` starts. `--no-open` overrides it. |

Binding to anything but loopback exposes an unauthenticated API to agents with
full machine access. Do that only on a network you control.

## `models`

`providers` is a list of OpenAI-compatible endpoints; `default` names the one new
conversations use. Each conversation remembers its own provider **and** model, so
you can switch names in the composer without adding an endpoint per model.
Settings → Models shows that list collapsed: open a row to edit URL, key,
default model, and timeout. **Add a provider** sits under the list and opens
the new row.

| key | meaning |
|---|---|
| `id` | stable identifier, referenced by `models.default` and by each conversation. Blank or duplicate ids are renamed to `provider-N` on load. |
| `label` | name of this provider in Settings and as the composer group heading. Empty groups by `id`. Never the default model — that made one endpoint look like a pile of unrelated names. |
| `base_url` | the endpoint, including any `/v1`. |
| `api_key` | may be empty: local endpoints frequently need none. Never returned by the API — the settings dialog sees only a "set / not set" flag. |
| `model` | the default model name new conversations start on. Switch per conversation in the composer. |
| `catalog` | names this endpoint listed the last time you clicked **Discover models**. The composer offers every name here; you do not add a provider row per model. Empty until you discover (or type a default). |
| `timeout_seconds` | how long we wait for the next byte (headers or a stream chunk). Default `300`. A thinking model that is still producing tokens is not killed; a silent endpoint is. Compact (`/compact` and auto-compact) uses this same idle clock — there is no separate swarm compact timeout. |
| `context_window` | fallback token limit for **names that have no row of their own**. `0` means unknown: the composer ring then shows a count (not a fake 0%) and a saturating arc scaled by `swarm.context_char_budget`. Never invented from a model name. Do not put one model's limit here — that would pin every other name on the endpoint to the same number. |
| `model_context` | per-name windows. Settings lists one field per catalog name; **Discover models** fills a value when the listing included `context_length` / `max_model_len` / `context_window` / `max_context_length` / `max_input_tokens` / `n_ctx` / `max_seq_len`, including one nesting under `top_provider` / `meta` / `limits` / `parameters`. Empty for endpoints that only return names. Output caps are not copied. |

A provider is **ready** when it has both `base_url` and `model`. Until the default
provider is ready, `GET /api/meta` reports `configured: false` and the UI shows a
setup banner instead of pretending a turn can run. Listing models only needs the
base URL (`POST /api/models/discover`); the request is capped at 15 seconds so a
hung endpoint cannot freeze Settings.

### First-run seeding

On a first run only, blank fields of the default provider are filled from the
environment:

| variable | fills |
|---|---|
| `ZWAI_MODEL_BASE_URL`, else `OPENAI_BASE_URL` | `base_url` |
| `ZWAI_MODEL_API_KEY`, else `OPENAI_API_KEY` | `api_key` |
| `ZWAI_MODEL_NAME`, else `OPENAI_MODEL` | `model` |

Only blanks are filled, so an exported variable can never override what you
configured in the UI.

## `swarm`

The limits that keep a swarm from running away. All of them apply per turn.

| key | default | meaning |
|---|---|---|
| `max_concurrent` | `6` | sub-agents running at the same time. The manager prompt tells the model to use this budget when the work has that many independent parts. Saving Settings applies it to the live and parked swarm immediately: queued workers start as soon as a slot opens under the new cap. Lowering it does not kill in-flight workers. Raise it for wide fan-out; every one of them is a model call in flight. |
| `agent_timeout_seconds` | `600` | watchdog per sub-agent. A hung endpoint is force-terminated and the agent's result records the timeout. |
| `max_turns` | `200` | ReAct iterations per sub-agent. A model stuck in a loop ends here instead of spinning. |
| `manager_max_iterations` | `200` | iterations for the manager. Lower it and complex plans get truncated mid-way; the manager also spends turns waiting for workers. Reaching the cap **pauses** the turn and asks whether to add another slice of this size, rather than failing with eino's iteration error. |
| `progress_interval_seconds` | `5` | how often a running turn emits a progress pulse. It is the only thing that moves while every agent sits in a slow tool call, so a higher value makes a busy run look stuck for longer. Zero or negative falls back to the default; pulses cannot be switched off. |
| `delta_coalesce_ms` | `50` | how long streamed tokens wait to be sent as one event. A token every few milliseconds would redraw the whole UI; one pulse per interval keeps the screen moving without a frame per token. Zero or negative falls back to the default. |
| `auto_title` | `true` | after the first finished turn, ask the model for a short sidebar name instead of leaving the truncated first message. Off keeps the placeholder. A title the user typed is never overwritten. An older config file without the key stays on. |
| `title_provider` | empty | endpoint the namer calls. Empty follows the conversation's provider. An id that is no longer in `providers` is cleared on load. |
| `title_model` | empty | model name the namer calls. Empty (with an empty provider) follows the conversation's model. A name with no provider stays on the conversation's endpoint. Pin one in Settings → Models when an endpoint lists more than one name. |
| `compact_provider` | empty | endpoint `/compact` calls. Same empty-means-follow rule as `title_provider`. A deleted id is cleared on load. |
| `compact_model` | empty | model name `/compact` calls. Same pin rules as `title_model`. Pin one in Settings → Models. |
| `context_char_budget` | `80000` | rune count treated as 100% full on the `/compact` hint when the model has not reported a token window. Zero or negative is repaired to the default. |
| `compact_keep_messages` | `6` | recent user/assistant replay messages that stay verbatim after `/compact` or auto-compact. The rest become the briefing. Zero or negative is repaired to the default. |
| `auto_compact_tokens` | `80000` | prompt tokens on a manager call that trigger in-turn compression. Older replayable tool results are cleared first; if that is not enough, older messages become a briefing and the recent tail stays. The briefing prefers the rolling session memory (refreshed from the event log, newest events that fit a hard rune cap, tool results clipped harder than answers); the same pin as `/compact` (`compact_provider` / `compact_model`) does the summary only when that is empty, and that summarizer call is itself newest-first under the same rune cap. A briefing that is a transcript dump or pasted tool JSON is refused: the thread fields stay, the `compacted` / `session_memory` event carries `err`, and a stored dump is not copied into the next manager prompt. A failed session-memory refresh stamps the token watermark so the next Generate does not resend the same payload. Zero or negative is repaired to the default so a long ReAct loop cannot silently skip compression. The briefing is streamed; silence uses that endpoint's `timeout_seconds` (idle), same as any other model call. Session-memory refresh uses that same idle timeout, not a 15s cap. |
| `goal_max_auto_turns` | `12` | consecutive engine-started turns that may pursue an open `/goal` without another human message. Zero or negative is repaired to the default. A human message or resume resets the count. `block_goal` and a failed turn stop auto-continue without waiting for the cap. A truncated tool-call JSON, a `429`, or a dropped stream retries inside the same turn twice before that failure counts. |
| `goal_session_max_iterations` | `40` | manager ReAct slice while a `/goal` is open. Hitting it extends the same turn (no confirm, no `goal_session`, no auto-continue spent). |
| `goal_auto_compact_percent` | `80` | when context is at least this full (tokens vs `min(model window, auto_compact_tokens)`, else chars vs `context_char_budget`), compact before the next auto-continue. A million-token window is not the denominator. The rolling session briefing is caught up first (this session's last manager answers, then a bounded incremental refresh) so the fold copies that view; a session briefing that has moved since the last compact also folds even when the meter is still cold. Zero or negative is repaired to the default; above 100 is clamped. |
| `schedule_min_interval_seconds` | `30` | shortest cadence a schedule may use, in seconds. Zero or negative is repaired to the default so a hand-edit cannot arm a sub-second loop. Editable in Settings → Swarm. |
| `schedule_tick_ms` | `1000` | how often the process looks for due schedules. Zero or negative is repaired to the default so the ticker cannot silently stop. Editable in Settings → Swarm. |
| `schedule_max_active` | `32` | schedule runs that may execute at once. Overflow waits for the next tick. Zero or negative is repaired to the default so a hand-edit cannot refuse every fire or unbounded fan-out. Editable in Settings → Swarm. PUT `/api/settings` sends the whole `swarm` object; omitting these keys would zero the Go struct before repair. |

A leftover `goal_session_max_seconds` from older builds is ignored on load; the next save omits it.

The current values are reported in `GET /api/meta` and are part of the manager's
system prompt, so it knows how wide it may fan out.

## `tools`

Both lists record **exceptions** rather than the whole toolset, so a tool added in
a later release behaves sensibly without anyone editing their config:

- `disabled` — switch off a tool that is on by default.
- `enabled` — switch on a tool that is off by default (those need something zwai
  does not ship, so they are opt-in).

| tool | group | default | notes |
|---|---|---|---|
| `read` | files | on | read a text file, with paging |
| `write` | files | on | create or overwrite |
| `edit` | files | on | search/replace or patch edits |
| `ls` | files | on | list a directory |
| `tree` | files | on | directory tree with depth control |
| `glob` | files | on | match files by pattern |
| `grep` | files | on | search file contents |
| `exec` | shell | on | run a shell command |
| `web_search` | web | on | search the web (network) |
| `web_fetch` | web | on | fetch a page and extract text (network) |
| `python_runner` | shell | **off** | needs Python on `PATH` |
| `screenshot` | shell | **off** | needs platform capture support |

```yaml
tools:
    disabled: [exec]            # no shell for the agents
    enabled: [python_runner]    # yes to Python
```

Turning a tool off removes it from the agents' toolset **and** from the manager's
system prompt, so the model does not plan around a tool it cannot call.

| key | default | meaning |
|---|---|---|
| `web_search_max_results` | `8` | results per search. More context per call, more tokens. |
| `proxy.http` / `proxy.https` | empty | outbound proxy for the network tools (`web_search`, `web_fetch`). |
| `proxy.no_proxy` | empty | comma-separated hosts that bypass the proxy. |

Tool paths resolve against the conversation's workspace, but the workspace is not
a boundary: an absolute path or a `..` reaches outside it and `exec` runs as you.
See the "Full access" note in the [README](../README.md).

## `memory`

What a project remembers between conversations, and what that costs. Memory is
per project — these keys are the budget every project with it switched on
shares. Nothing here applies to a conversation outside a project.

| key | default | meaning |
|---|---|---|
| `enabled` | `true` | the master switch. Off means no project carries notes or skills and no review runs, whatever a project's own switch says. What is already stored stays readable in the Memory panel. |
| `auto_review` | `true` | read a turn's event log when it finishes and keep what is worth carrying forward. Off leaves memory to the agents' own tools and the "Review now" button. Only a turn that finished cleanly is reviewed: nobody who pressed stop asked for a half-finished approach to become a skill. Auto-review is skipped when the manager already wrote with `memory` or `skill_manage` this turn; "Review now" still runs. |
| `char_limit` | `2200` | how long the notes may get. They ride in the system prompt of **every** turn in the project, so this is a per-turn cost, not a disk one. A write that would grow past this is refused — including replacing a note with a longer one. The tool result says by how many characters (`over_by`) and lists what is stored, so the agent shortens or drops a note rather than retrying the same text. Small on purpose. |
| `entry_max` | `360` | how long **one** note may get on an agent write (`memory` add/replace). A runbook that would eat a quarter of the budget belongs in a skill, where only the summary rides in the prompt. A note that restates a recorded skill — the summary or the steps — is refused the same way. The Memory panel's editor still uses `char_limit` only: a person who pastes a longer note is spending that budget on purpose. Zero or negative is repaired to the default; a value above `char_limit` is clamped. |
| `review_max_iterations` | `8` | how many times the review may think and write before it is stopped. It reads one conversation and makes a handful of tool calls; a large number here buys a slow, expensive review rather than a better one. |
| `skills_index_max` | `50` | how many skills are listed in the prompt (manager and sub-agents). Only names and one-line descriptions are listed; an agent opens the one it needs with `skill_view`. Beyond this cap the prompt says how many were not listed. `skill_view` opens only this index — a procedure sitting in the workspace is a file. Sub-agents get `skill_view` and this index; they cannot call `memory` or `skill_manage`. |
| `notifications` | `on` | how a completed review appears in the transcript. `on` is one line naming what changed (`Memory updated: 1 note stored`). `verbose` adds a preview of the written text. `off` writes nothing in the transcript — the review still runs, and the Trace tab's Full log still lists it. An unknown value is repaired to `on`, not to silence. |

Numbers that are zero or negative fall back to their defaults, so a hand-edited
file cannot leave a project with no room to remember anything.

The files live in the data directory, never in your repository — see
[DATA_MODEL.md](DATA_MODEL.md) for the layout, and edit them from the Memory
panel rather than by hand while the app is running.

## `personality`

Install-wide personal preferences for how the manager works with you: tone,
language habits, standing likes. It is **not** a task and **not** a project's
business context. Edit it in **Settings → Personality**.

| key | default | meaning |
|---|---|---|
| `instructions` | empty | added to the manager's system prompt of **every** conversation. Empty omits the section entirely, so a blank setting costs no tokens. The text sits before a project's instruction; when the two conflict, the project wins. Sub-agents do not see it — they get a task from the manager, the same way they do not see the project instruction. `zwai tui` prepends the same section to its Extra. |

## `log`

| key | default | meaning |
|---|---|---|
| `level` | `info` | `debug`, `info`, `warn` or `error`. `debug` adds one line per HTTP request and per event-stream decision — that plus `zwai trace` is usually enough to explain a bad turn. |

Logs go to stderr. Anything unrecognized falls back to `info`.

## `ui`

Chrome only. Agents still answer in the language you are using.

| key | default | meaning |
|---|---|---|
| `locale` | `system` | `system`, `en` or `zh`. `system` follows the browser (`zh*` → Chinese, everything else English). The title-bar control pins `en` or `zh`. Desktop binds a random loopback, so this lives in the file rather than in `localStorage` alone. Junk becomes `system`. |
| `font` | `system` | `system`, `serif` or `mono`. `system` is the UI sans stack. The whole window uses it. Junk becomes `system`. |
| `font_size` | `medium` | `small`, `medium` or `large`. Scales the window from the CSS root (14 / 16 / 18px). Junk becomes `medium`. |
| `content_width` | `comfortable` | `comfortable` keeps the reading column (`max-w-3xl`). `full` fills the space between the sidebars. The title-bar control and ⌘K toggle the same preference. Junk becomes `comfortable`. |

Theme stays in the browser; language, typeface and column width are first-class
config so a new window keeps them.

## Multiple instances

Everything is scoped to the data directory, so a second instance is one flag away:

```bash
zwai web --data-dir /tmp/zwai-demo --mock --no-open
```

That gets its own `config.yaml`, its own database and its own workspaces, and
touches nothing in `~/.zwai-swarm`. It is how the end-to-end tests run.

Plan files are derived from the data directory, not a config key:
`$ZWAI_HOME/plans/<thread_id>/PLAN.md`. They are app artefacts, not workspace
files, and they are not editable in Settings.
