# Configuration

One YAML file, `config.yaml`, in the data directory:

```
$ZWAI_HOME (default ~/.zwai-swarm)/config.yaml     mode 0600
```

`zwai config path` prints it, `zwai config show` prints its effective contents
with the API key redacted. Desktop **Settings** controls save themselves
(debounced; **Back to app** flushes). A paired phone's **Menu → PC timeouts**
edits that PC's worker, first-byte and stream-idle timeouts independently of
phone-direct model settings. A `PUT /api/settings` rewrites the file atomically
— a crash mid-write cannot leave a truncated file. Saves merge into a detached
candidate under the shared configuration lock and publish runtime settings only
after persistence succeeds; a failed save leaves the current configuration intact.

Desktop autosave sends changed sections and omits unchanged timeout fields.
Changing a font, another swarm limit or a provider label therefore preserves a
timeout saved by the phone while Settings was open. Explicit timeout edits
replace the current value; reopening Settings loads the latest saved values.

Nothing in the binary hardcodes an endpoint or a model name. If it is not in this
file (or seeded from the environment on first run), it does not exist.

A full backup includes this file. By default the provider `api_key` values and
MCP `env` / `headers` are in the archive, and so is `remote/`. Turning
credentials off writes those secrets empty, strips proxy and MCP userinfo, and
omits `remote/`. Restoring that archive copies this machine's existing non-empty
API keys and MCP secrets back over the empty values and leaves `remote/` where
it is, so a backup made without credentials does not wipe the keys already
saved here. A machine that has never saved a config has nothing to copy back.
An empty key that was only filled from the environment is not saved over by an
empty value from the archive.

## Voice settings

The PC's **Settings → Models** controls input/output switches, recording and
playback parameters and limits. Gateway mode defaults each direction on when
the signed-in normal or third-party Gateway resolves a model for that role.
Each direction stores its opt-out in `gateway_disabled`. Legacy configurations
without this field follow model availability in Gateway mode. LOCAL and personal
services continue to use `enabled`, which defaults to false. **Models → Functional
models** selects recognition and spoken-answer models independently of conversation.
LOCAL and an editable gateway allow personal providers. A managed third-party
gateway supplies the models and hides model and endpoint controls. Voice
parameters follow the model's declared permissions. In LOCAL, `source: account` uses the signed-in
account's enabled recognition or speech role when no functional binding
overrides that direction. A personal binding, provider, or custom endpoint
remains the request target. Without that login or role, the direction stays
not ready.

`speech.input.device_id` is the preferred PC or Web microphone's browser audio input ID. Empty follows the operating-system default. When capture cannot find the saved device, it tries the system default once and keeps the preference for when that device returns. Selecting **System default** clears the saved ID. Phones use their own microphone.

Under **Advanced settings**, `speech.input.recording_format` accepts `auto`
and `wav`. Automatic recording
normalizes MP4/AAC, WebM/Opus and Ogg recordings to 16 kHz mono PCM WAV for ASR
interoperability, including newer iPhones that record WebM. Explicit WAV also
converts other containers. The input byte limit applies after conversion too.
Before draft insertion, a complete leading ASR `language …<asr_text>` envelope
is separated from the transcript. Plain text and literal tags elsewhere are kept.

`speech.output.format` accepts `mp3` (the existing default), `wav` and `pcm`.
Select **PCM (streaming playback)** to play a response while it is generated.
The service must return mono 24 kHz signed 16-bit little-endian PCM, either as
audio chunks or OpenAI speech SSE events. MP3/WAV remain complete-segment
playback. Phone PC-local RPC remains buffered.
Gateway PCM streaming follows the declared stream format, with SSE as the legacy
account behavior. Personal services use the audio streaming request. Paired phones route that output independently
of recognition, with their own same-origin gateway login.

Paired phones use the selected PC's safe projection only while that PC is online
and its engine is ready. Gateway audio uses the phone's own same-origin account
credential. A cached projection is status information and cannot authorize an
offline request. LOCAL direct Chat can configure voice independently under its
personal model services; credentials stay in the phone's existing provider store.
Refreshing this phone's unrelated model catalog does not interrupt paired-PC
dictation or playback; changes to its speech authority still cancel them.
API reads omit voice credentials; `config show` redacts them. Credential-free
backups remove voice keys and restore preserves a custom key only for the same endpoint.

## A complete file

```yaml
server:
    addr: 127.0.0.1:8787
    open_browser: true
models:
    default: default
    default_source: provider
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
          first_byte_timeout_seconds: 30
          context_window: 0
          model_context: {}
          # enabled omitted means on. enabled: false hides it from the composer.
swarm:
    max_concurrent: 6
    agent_timeout_seconds: 3600
    max_turns: 200
    manager_max_iterations: 200
    progress_interval_seconds: 5
    delta_coalesce_ms: 50
    auto_title: true
    composer_predictions: true
    graph: true
    incremental_execution: false
    default_reasoning_effort: ""
    title_provider: ""
    title_model: ""
    compact_provider: ""
    compact_model: ""
    context_char_budget: 80000
    compact_keep_messages: 6
    auto_compact_tokens: 80000
    compact_output_reserve: 8192
    max_completion_tokens: 16384
    goal_max_auto_turns: 12
    goal_session_max_iterations: 40
    goal_auto_compact_percent: 80
    schedule_min_interval_seconds: 30
    schedule_tick_ms: 1000
    schedule_max_active: 32
    policy_reasoning_effort: ""
    schedule_answer_check: true
    schedule_answer_model: chat
tools:
    disabled: []
    enabled: []
    web_execution: auto
    proxy:
        http: ""
        https: ""
        no_proxy: ""
    web_search_max_results: 8
    result_max_chars: 40000
mcp:
    servers: []
memory:
    enabled: true
    auto_review: true
    auto_dream: true
    char_limit: 2200
    entry_max: 360
    review_max_iterations: 8
    review_reasoning_effort: ""
    skills_index_max: 50
    notifications: on
personality:
    instructions: ""
log:
    level: info
ui:
    locale: system
    font: system
    ui_font_size: medium
    content_font: ui
    font_size: ui
    code_font: mono
    code_font_size: content
    content_width: comfortable
    transcript_mode: user
    palette: zwai
    welcome_dismissed: false
remote:
    enabled: false
    hub_url: https://ai.fofa.info:2440
    thread_limit: 5
    summary_chars: 280
    open_turns: 6
    event_chars: 4000
    watch_events: 80
    keep_awake: true
search:
    embedding: false
    embedding_provider: ""
    embedding_model: ""
clients:
    enabled: false
    claude_dir: ""
    codex_dir: ""
    cursor_dir: ""
    recent_days: 3
    running_stale_seconds: 90
```

Any key you leave out, set to zero or set to an empty string is repaired with its
default on load, so a partially hand-edited file always starts.

## `product`

| key | default | meaning |
|---|---|---|
| `mode` | absent, which means `fofa` | `fofa` or `local`. Absent and `fofa` are the same and are not written. `local` is written. Any other value is rejected on load and on save. |

`fofa` is the legacy name for gateway mode, the default. Work requires a ready
signed-in account (`--mock` skips login). The gateway decides whether its model
preset is editable. An ordinary account can select personal providers and override
functional defaults; a managed third-party account cannot open model settings or
select other models. `local` does not require a gateway login to use personal
providers. When an account is already signed in, LOCAL keeps that login and
lists the server catalogs it authorizes: the third-party preset and the original
gateway catalog. The same model name can appear in both. An unauthorized or
empty catalog is omitted and does not ask for another sign-in. Functional roles
do not inherit the gateway preset. Switching back to gateway restores the
managed preset.

No normal Settings row switches modes. The account portrait's existing triple-click
debug entry switches LOCAL/gateway without waiting for a running turn or worker.
That turn keeps the model it already opened; the next model lookup uses the new
source. A model request that has started, and backup, restore or restart, refuse
the switch with the existing busy error. The portrait says to wait. A config-file
edit is read on the next load.

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
default model, first-byte wait, and stream idle timeout. **Add a provider** sits under the list and opens
the new row.

Chat Completions reads both `message.reasoning_content` / `delta.reasoning_content`
and the newer vLLM `message.reasoning` / `delta.reasoning` automatically. A nonempty
`reasoning_content` takes precedence; an empty or null value falls back to
`reasoning`, without concatenating duplicate fields. This also applies to direct
phone chat. There is no field-name switch or configuration migration. The
Responses API uses its separate reasoning events and output items. See
[vLLM reasoning outputs](https://docs.vllm.ai/en/latest/features/reasoning_outputs/).

| key | meaning |
|---|---|
| `id` | stable identifier, referenced by `models.default` and by each conversation. Blank or duplicate ids are renamed to `provider-N` on load. |
| `label` | name of this provider in Settings and as the composer group heading. Empty groups by `id`. Never the default model — that made one endpoint look like a pile of unrelated names. |
| `base_url` | the endpoint, including any `/v1`. |
| `api_mode` | empty or `chat_completions` calls `/chat/completions` (the existing default); `responses` calls `/responses`. Set this explicitly from the endpoint documentation; provider labels and model names never select a protocol. |
| `builtin_tools` | optional list of complete cloud tool objects. Empty disables them. Requires `api_mode: responses`; accepts `web_search`, `web_search_preview`, `file_search`, and `code_interpreter`, with one entry per type. Nested provider parameters are preserved. Unknown, local-execution and approval-dependent tool types are rejected. |
| `api_key` | may be empty: local endpoints frequently need none. Never returned by the API — the settings dialog sees only a "set / not set" flag. |
| `model` | the default model name new conversations start on. Switch per conversation in the composer. |
| `enabled` | whether this endpoint appears in the composer model list (and the paired phone's catalog, and a phone's own direct-chat list). Omitted or `true` means on, so an older file stays visible; a save of `true` omits the key. `false` hides it. A conversation already on that endpoint keeps using it. If the hidden one was `models.default`, default moves to the first endpoint still listed. Turning every provider off is repaired on load: the default stays on, because a new conversation needs somewhere to start. |
| `catalog` | names this endpoint listed the last time you clicked **Discover models**. The composer offers every name here; you do not add a provider row per model. Empty until you discover (or type a default). |
| `first_byte_timeout_seconds` | how long this provider may take to return response headers, shown as **First-byte timeout (seconds)** in Settings → Models. Default `30`; missing, zero, and negative values use that default. Raise this field when a model needs more time before its response starts. It is independent of `timeout_seconds` and may be longer. A first-byte failure is retried once on a new connection, then reports the actual wait; desktop/Web directs the user to this setting. Changes save automatically and apply from the next turn without restarting the engine. |
| `timeout_seconds` | how long a stream may stay silent after it has started. Default `300`. A thinking model that is still producing tokens is not killed. This does not change the first-byte wait. Compact (`/compact` and auto-compact) and the memory review (post-turn and catalog tidy) use this same idle clock — there is no separate swarm compact timeout. The review streams, so its first-byte wait concerns the start of that stream, not the finished JSON body. An identified provider stream-idle timeout in a PC sub-agent failure opens Settings → Models; the error does not identify a provider, so this entry does not select one or focus an endpoint field. |
| `context_window` | fallback token limit for **names that have no row of their own**. `0` means unknown: the composer ring then shows a count (not a fake 0%) and a saturating arc scaled by `swarm.context_char_budget`. Never invented from a model name. Do not put one model's limit here — that would pin every other name on the endpoint to the same number. |
| `model_context` | per-name windows. Settings lists one field per catalog name; **Discover models** fills a value when the listing included `context_length` / `max_model_len` / `context_window` / `max_context_length` / `max_input_tokens` / `n_ctx` / `max_seq_len`, including one nesting under `top_provider` / `meta` / `limits` / `parameters`. Empty for endpoints that only return names. Output caps are not copied. |

Existing configuration files need no migration: an omitted first-byte wait uses
30 seconds. Previously, a `timeout_seconds` below 30 also shortened the
first-byte wait; these budgets are now independent. Set
`first_byte_timeout_seconds` explicitly to retain that shorter wait.

The desktop/Web error's **Configure first-byte timeout** button opens the failed
turn's provider, scrolls to this field and focuses it. If that provider is
unknown or was deleted, the button opens Models without selecting a different
provider. Raise the wait, let the change save, close Settings and resend the
request when ready.
Chat recovery uses a shared origin-level cooldown: 5 → 10 → 20 → 30 seconds,
then 30 seconds until the recovery probe completes successfully. These policy defaults live
in `internal/config/backoff.go`; they are not new YAML fields or retry counts.
Provider aliases and models on the same scheme/host/port share one probe;
settings saves replace clients without clearing failures. Stop cancels waiting.
After cooldown, only that probe starts until it yields a decoded nonempty body,
reasoning or tool-call delta. This restores concurrent admission while the stream
stays open, without resetting failures. Headers, role-only/empty/usage-only deltas
and error frames do not release waiting calls. Only the probe's final result can
clear or advance the failure streak; parallel results during it cannot change the
streak or a later generation. A failed probe renews cooldown without cancelling
requests already admitted. Non-streaming Generate waits for its complete result.
The one fresh-connection header retry also waits. The response-header timeout
starts when each HTTP attempt is sent, after admission; an explicit caller
deadline includes the cooldown. A Chat Completions streamed probe needs `[DONE]` or an explicit
finish reason; Responses requires a valid `response.completed` event before the
adapter emits its finish reason. A connection ending after a partial answer cannot
reset failures. Catalog discovery and embeddings
keep their existing request policies. See `C-029` in `docs/CONTRACTS.md`.

An immediate gateway route refusal has a separate recovery policy: HTTP 503
`provider_unavailable` with the exact message `no healthy provider can serve this
model`, fully returned within this provider's first-byte budget, keeps the same
logical model call waiting through the shared cooldown. These probes do not
consume the turn's two model-error retries. Each actual HTTP rejection is still
recorded, and a successful probe's response continues the original call without
another send. The same logical call stops after three consecutive confirmed
refusals (`DefaultRouteRecoveryAttempts`); the turn then fails instead of
probing forever. That cap is not a YAML field. Success, a different error, or
caller cancellation still ends the wait earlier. Stop interrupts both the
wait and active request; an explicit caller deadline and the worker watchdog
remain effective. The header, stream-idle and gateway body timeouts are separate
per-request budgets. Other 503s, request hangs and broken streams retain the
ordinary bounded retry policy. While this origin remains in route failure,
post-turn session briefing and automatic memory review are skipped rather than
started as additional probes; this does not report notes as saved. Local
deterministic skill folding remains available. Post-turn briefing, all memory
reviews (including Review now), and conversation/schedule titles keep their
original single-request failure policy while still participating in shared
cooldown. If one first discovers the routing outage, it records that rejection
without repeatedly probing until its existing timeout. Manual compact keeps its
original single-request failure policy because it has no active turn
cancellation owner.

Scheduling-answer classification uses the conversation's provider and these
same response-header/body-idle budgets, without a separate 30-second total
deadline. Classification completely bypasses the shared origin gate and the HTTP
adapter's implicit header reconnect; it cannot delay other conversations or
change the service's recovery state. Request failures and invalid output retry
the original private candidate with persistent 5/10/20/30-second capped delay.
After three consecutive request failures, three consecutive invalid responses,
or six total attempts, the candidate is durably deferred: its unpublished text
stays private, and the same turn/Registry/workers continue user work under an
executable goal or active same-thread wake. Without that owner, the turn uses
the existing human Continue/Stop pause. Request failures and invalid output
have separate persistent consecutive counts and reset each other's count;
local storage/configuration failures have separate codes and consume neither
budget. Deferral does not grant a schedule-claim correction or retry the old
answer. The limits (`DefaultScheduleAnswerFailures`,
`DefaultScheduleAnswerInvalids`, `DefaultScheduleAnswerAttempts`) and backoff
live in `internal/config/backoff.go`, with no new YAML fields or Settings controls.
Stop or a superseding human steer cancels verification. Attempts, next retry
time, both consecutive counts and deferral survive restart, so an exhausted
candidate cannot block the goal again or overwrite a newer checkpoint.

A provider is **ready** when it has both `base_url` and `model`. Until the default
provider is ready, `GET /api/meta` reports `configured: false` and the UI shows a
setup banner instead of pretending a turn can run. Listing models only needs the
base URL (`POST /api/models/discover`); the request is capped at 15 seconds so a
hung endpoint cannot freeze Settings. A failed Settings action (listing
models, Phone pairing, saving the yaml, loading the sheet, saving a project)
is a dismissible toast over the UI (title plus the error, with an ×), not a
red line at the top of the page you just scrolled. A refused project working
directory stays under that field — that is the control that caused it.
On macOS, a desktop window that cannot reach a LAN URL (`no route to host`)
while Terminal `curl` can is Local Network privacy: allow **zwai** under
System Settings → Privacy & Security → Local Network.

### Provider cloud tools

Settings → Models → open a provider → select **Responses** and edit the cloud
built-in tools JSON array. For example:

```yaml
models:
  providers:
    - id: cloud
      base_url: https://endpoint.example/v1
      model: your-model
      api_mode: responses
      builtin_tools:
        - type: web_search
          search_context_size: low
```

The example enables search only for that provider; the default configuration
never enables a cloud tool. The endpoint and selected model must actually support
the configured protocol and tools. A compatible Chat Completions endpoint alone
is insufficient. Model discovery remains `GET {base_url}/models`.

Each configured object is merged with the Agent's local function declarations in
the Responses request. Cloud tool calls execute at the provider and never enter
the local tool dispatcher. All calls using this provider (including workers and
auxiliary model calls) receive its tool list. Local Tools-tab switches affect
local tools only. Cloud parameters such as file-search vector store IDs or a code
interpreter container must be supplied according to the provider's documentation;
the app does not upload files or issue separate cloud-resource management calls.
An automatic container option can cause the provider to create and bill a
container when the tool runs; the app does not verify resource availability.
The adapter supports text and image inputs and preserves provider tool outputs;
client-side downloading of authenticated container files and other audio/video/file
input parts are not implemented by this adapter.

Responses requests omit the optional `include` field by default. When the
endpoint returns encrypted reasoning, the adapter preserves it for replay.

Invalid JSON keeps the settings draft and suspends automatic saving. Clear the
list before switching back to Chat Completions. A malformed/unsupported saved
configuration returns an error on load or save rather than silently dropping tools.
Web citations become clickable Markdown source links while answer text continues
to stream. Cloud execution receipts are stored as `builtin_tool` events in the
same turn's Trace; they are distinct from local `tool_call` / `tool_result` events.

See the [official OpenAI web search guide](https://developers.openai.com/api/docs/guides/tools-web-search?api-mode=responses)
for supported search parameters. This integration does not implement client-side
computer control, shell execution, hosted MCP approvals or arbitrary tool types.

### Gateway presets and functional bindings

Gateway catalogs, profiles and credentials remain in memory; they are never
saved as `models.providers`. Provider IDs beginning with `gateway:` are reserved.
The engine reads the selected account source's optional `responses_request_gzip`
capability on each Responses request. When true, it sends JSON bodies of at
least 1 KiB as `Content-Encoding: gzip` if the encoded body is smaller.
Gateways without the capability and personal providers receive ordinary JSON.
LOCAL lists personal providers and separately authorized signed-in account catalogs.
An additional catalog does not hide a distinct source's selectable conversation models.
For the same source ID, the LOCAL projection controls both selection and execution;
an explicit denial there is not bypassed with the signed-in projection.
Each account catalog refreshes independently: a temporary outage retains only
that same connection's previously authorized catalog. Fresh bootstrap revocations,
denied responses and empty catalogs remove their own source even during a sibling outage.
An ordinary gateway lists its catalog and
personal providers. A managed gateway exposes only its own conversation preset;
legacy `account_presentation: full` cannot unlock other models.

`models.bindings` stores explicit functional overrides by scope. The `local` scope
belongs to LOCAL. A gateway scope is a hash of gateway origin, source ID and account
owner; credential renewal keeps that scope, changing accounts does not reuse it.
A LOCAL binding may name a signed-in catalog with a `gateway:` provider id. That
id is still not a `models.providers` entry. After sign-out the binding stays
stored and execution fails closed instead of using a personal provider with the
same model name. An absent binding does not inherit a gateway preset.
Each scope maps `conversation`, `title`, `compact`, `embedding`, `transcription`,
`speech`, `ocr` and `decisions` to `{provider_id, model}`. Settings does not
list `decisions`; that role is a background call. Missing roles follow the
latest gateway default. **Use default** removes the override; server defaults are
never copied into the saved map. Existing conversation-specific selections remain
explicit selections. LOCAL retains the existing title/compression/voice defaults
when no functional override is set.

The Settings API projects only the current scope as `models.bindings`; its PUT
replaces that scope's map and accepts a bindings-only body. Other scopes survive.
The `image` function uses the `images` capability of an authorized gateway
source. A configurable source uses the explicit scoped image binding when saved.
Otherwise the conversation's gateway source supplies the role default, then
the first authorized typed image model in name order. Managed sources use
their published fixed model or role. The picker requires a configurable image
capability to create a binding. Revoked or stale choices stay saved and can be
cleared with **Use default**.

The scoped `conversation` binding takes priority over `default_source`. In
LOCAL, `default_source: account` prefers the authorized original Gateway chat
catalog, then an available account preset, then `models.default`.
An ordinary Gateway's first binding save retires the global `account_model`
pin and resets `default_source` to `account`.
`account_presentation` remains a validated compatibility field (`official`/`full`),
with no model-unlocking UI or authority over a managed policy. Managed settings
reads hide personal providers, auxiliary pins and speech endpoints; unrelated saves
preserve the hidden values for later LOCAL use. Managed writes selecting other
models or discovering a catalog are refused.

Bootstrap can publish `models.configurable` and `models.defaults`, a map from
functional role to model name. Without the permission flag, legacy `profile` or
fixed `roles` means managed; a normal catalog means editable. A managed defaults-only
preset requires a conversation model and never discovers the broader catalog.
Gateway-side publication of these optional fields is a separate deployment.

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

Runtime limits and model-call defaults for the swarm.

| key | default | meaning |
|---|---|---|
| `max_concurrent` | `6` | sub-agents running at the same time for one task. Settings accepts whole numbers of at least 1. Missing or non-positive values in legacy YAML use the default. The manager prompt and the `spawn_agent` / `resume_agent` descriptions state this number as a hard limit. Those tools refuse past it and do not park the extra worker; the manager waits, then resumes a finished id or spawns the next piece. Saving Settings applies it to the live and parked swarm immediately. Workers already queued by a programmatic `Spawn` start as soon as a slot opens under a higher cap. Lowering it stops the workers that took a slot last until the task is back within the number; the earlier ones keep running. A stopped worker needs a free slot before `resume_agent` can continue it. Raise it for wide fan-out; every one of them is a model call in flight. |
| `agent_timeout_seconds` | `3600` | watchdog per sub-agent lifetime, including time queued for a slot. Existing positive saved values, including an old `600` or `1800`, remain unchanged. Saving updates later spawn/resume/restore lifetimes without extending an already created deadline or clearing failure counts. A failed worker's View failure reason opens its recorded error; an exact watchdog error for that worker links to Settings → Swarm → Sub-agent timeout and focuses this field. Provider idle errors, generic deadlines and cancellation do not identify this watchdog and do not offer that link. Saving does not resume the stopped worker. Three consecutive watchdog failures of the same worker stop automatic resume, error the manager and pause its recurring schedule. Provider idle errors and cancellation do not count. Success or explicit human retry resets the budget; increasing this lifetime alone is not a recovery strategy. |
| `max_turns` | `200` | ReAct iterations per sub-agent. A model stuck in a loop ends here instead of spinning. A failed worker's recorded iteration-limit error links from its failure details to Settings → Swarm → Sub-agent tool rounds and focuses this field. Changing it does not resume that worker. |
| `manager_max_iterations` | `200` | iterations for the manager. Lower it and complex plans get truncated mid-way; the manager also spends turns waiting for workers. Reaching the cap **pauses** the turn and asks whether to add another slice of this size, rather than failing with eino's iteration error. On desktop and web, that pause and a later stop open Settings → Swarm → Manager tool rounds and focus this field. The continuation already offered keeps the size recorded on the pause; the next pause reads the saved value. A paired phone names this PC setting and does not edit it. A pause whose reason is `classification_deferred` is an answer-verification deferral: its `limit` is rounds already used, and that card does not open this field. |
| `progress_interval_seconds` | `5` | how often a running turn emits a progress pulse. It is the only thing that moves while every agent sits in a slow tool call, so a higher value makes a busy run look stuck for longer. Zero or negative falls back to the default; pulses cannot be switched off. Settings does not show it. |
| `delta_coalesce_ms` | `50` | how long streamed tokens wait to be sent as one event. A token every few milliseconds would redraw the whole UI; one pulse per interval keeps the screen moving without a frame per token. Zero or negative falls back to the default. Settings does not show it. |
| `auto_title` | `true` | on the first user message, ask the model for a short sidebar name instead of leaving the truncated first message. The same namer labels an untitled scheduled wait from its prompt. Off keeps the placeholder. A title the user typed is never overwritten. An older config file without the key stays on. |
| `composer_predictions` | `true` | after a visible reply, ask the conversation's model for one next message and show it as the empty PC composer's placeholder. Tab inserts it; Enter sends it. Off stops the extra call. An older file without the key stays on. A settings save that omits the field keeps the stored value. The line is Trace metadata, not a message in the next prompt. Errors, cancellation, an empty reply, a quiet completion, and a turn that starts immediately are skipped. |
| `graph` | `true` | mount the work graph: the `update_graph` / `read_graph` tools (planning turns get only `read_graph`), the per-request digest and the read-only Work records panel. Off hides the panel and stops collection. A turn freezes the switch when it starts; turning it off stops writes at once. The older `work_ledger` and `knowledge` keys are ignored if an old file still has them. See [Work graph](GRAPH.md). |
| `incremental_execution` | `false` | opt-in guidance for ordinary manager implementation turns to start with a small runnable increment and verify each requested requirement. Configure in YAML or through the Settings API. Omitted or `null` in a settings save preserves the stored choice. An explicit boolean replaces it. Invalid types return `400` before changing the configuration. Model behavior and benchmark gains require measurement. |
| `default_reasoning_effort` | `""` | default reasoning level for model calls that do not specify one. Empty preserves existing call defaults. Explicit per-call levels, including a conversation's chosen level, take priority. Supports `none` (off) / `low` / `medium` / `high`. Values are trimmed/lowercased and unsupported strings normalize to empty. Configure in YAML or through the Settings API. Omitted or `null` preserves the stored choice. An explicit empty string clears it. Non-string values return `400` before changing live or saved configuration. A new worker pins its effective level when selected. Restoring a worker retains its saved level, including an empty legacy binding. |
| `title_provider` | empty | endpoint the namer calls. Empty follows the conversation's provider. An id that is no longer in `providers` is cleared on load. |
| `title_model` | empty | model name the namer calls. Empty (with an empty provider) follows the conversation's model. A name with no provider stays on the conversation's endpoint. Pin one in Settings → Models when an endpoint lists more than one name and automatic naming is on. The pin is hidden while `auto_title` is off; the saved name stays. |
| `compact_provider` | empty | endpoint `/compact` calls. Same empty-means-follow rule as `title_provider`. A deleted id is cleared on load. |
| `compact_model` | empty | model name `/compact` calls. Same pin rules as `title_model`. Pin one in Settings → Models. |
| `context_char_budget` | `80000` | rune count treated as 100% full on the `/compact` hint when the model has not reported a token window. Zero or negative is repaired to the default. Settings does not show it. |
| `compact_keep_messages` | `6` | recent user/assistant replay messages that stay verbatim after `/compact` or auto-compact. The rest become the briefing. Zero or negative is repaired to the default. |
| `auto_compact_tokens` | `80000` | prompt tokens that trigger compression when the selected model has **no** context window (`0`). A confirmed window ignores this number and uses `goal_auto_compact_percent` of that window. Older replayable tool results are cleared first; if that is not enough, older messages become a briefing and the recent tail stays. The briefing prefers the rolling session memory (refreshed from the event log, newest events that fit a hard rune cap, tool results clipped harder than answers); the same pin as `/compact` (`compact_provider` / `compact_model`) does the summary only when that is empty, and that summarizer call is itself newest-first under the same rune cap. A briefing that is a transcript dump or pasted tool JSON is refused: the thread fields stay, the `compacted` / `session_memory` event carries `err`, and a stored dump is not copied into the next manager prompt. A failed session-memory refresh stamps the token watermark so the next Generate does not resend the same payload. Zero or negative is repaired to the default so a long ReAct loop cannot silently skip compression. The briefing is streamed; silence uses that endpoint's `timeout_seconds` (idle), same as any other model call. Session-memory refresh uses that same idle timeout, not a 15s cap. |
| `goal_max_auto_turns` | `12` | consecutive engine-started turns that may pursue an open `/goal` without another human message. Zero or negative is repaired to the default. A human message resets the count only while pursuit is active and not capped, blocked or idle. Ordinary messages preserve an existing pause/count; explicit goal resume resets them. `block_goal` and a failed turn that is not a recoverable model error stop auto-continue without waiting for the cap. A truncated tool-call JSON, a `429`, a dropped stream, or a mid-stream TCP read timeout retries inside the same turn twice; if that still fails, this turn errors and auto-continue keeps going. |
| `goal_session_max_iterations` | `40` | manager ReAct slice while a `/goal` is pursuing without a cap/block/idle pause. A human reply turn on a paused goal uses `max_iterations` instead. Hitting it extends the same turn (no confirm, no `goal_session`, no auto-continue spent). |
| `goal_auto_compact_percent` | `80` | how full a **confirmed** context window must be before compression, both in-turn and before the next goal auto-continue. The line is `min(window × percent, window − max(compact_output_reserve, max_completion_tokens))` when the window exceeds that reserve. An unknown window uses `auto_compact_tokens` instead. Replayed messages without billed usage use a conservative token estimate that counts non-ASCII text separately from ASCII and reserves message framing, including tool results and arguments; the `/compact` hint for an unknown model window uses `context_char_budget`. The rolling session briefing is caught up first (this session's last manager answers, then a bounded incremental refresh) so the fold copies that view; a session briefing that has moved since the last compact also folds even when the meter is still cold. Zero or negative is repaired to the default; above 100 is clamped. Settings does not show it. |
| `compact_output_reserve` | `8192` | minimum tokens kept free under a confirmed window; the configured completion cap may require more headroom because every request sends `max_tokens`. A window smaller than the larger reserve uses the percent alone and a remaining overflow is reported with recovery advice. Zero or negative is repaired to the default. Settings does not show it. |
| `max_completion_tokens` | `16384` | `max_tokens` sent on every chat request. A thinking model that is left to the endpoint's default can spend that whole default before it emits an answer or a tool call, and the turn used to close as a blank success. Zero or negative is repaired to the default. When a turn fails with that empty length-stop, the desktop transcript links to Settings → Swarm and focuses this field. Closing that sheet offers Retry, which continues the failed turn from the context it had when the call failed, including tool results already recorded. The new completion cap applies to that continuation. A sub-agent with the same exact recorded error offers the same focused field from its failure details, without arming a manager retry or automatically resuming the worker. |
| `recover_reasoning_output_limit` | `false` | Opt-in ordinary Chat Completions manager recovery when one generation uses its output limit on reasoning without any answer or tool chunk. One durable recovery claim per turn applies before or after earlier tool work and survives engine/store reopen. Recovery preserves the full input, system order, original tool IDs and existing same-process worker/exec owners; it consumes the original remaining request and iteration budgets. Stop, unavailable evidence, a partial answer/tool attempt, known zero quota and existing unsupported modes refuse recovery. No cross-engine recovery of the same background PID is implied. |
| `reasoning_output_recovery_effort` | empty | Optional override for exactly the recovery request after a fresh turn's first manager generation, only when that turn has no explicit reasoning effort. Empty preserves the original builder's behavior. Mid-turn and resumed recovery preserve the original effort, including an omitted value; explicit effort is always retained. Requires `recover_reasoning_output_limit`; it does not change the model or completion cap. |
| `schedule_min_interval_seconds` | `30` | shortest cadence a schedule may use, in seconds. Zero or negative is repaired to the default so a hand-edit cannot arm a sub-second loop. Editable in Settings → Swarm. |
| `schedule_tick_ms` | `1000` | how often the process looks for due schedules. Zero or negative is repaired to the default so the ticker cannot silently stop. Settings does not show it; change it in `config.yaml` or `PUT /api/settings`. |
| `schedule_max_active` | `32` | schedule runs that may execute at once. Overflow waits for the next tick. Zero or negative is repaired to the default so a hand-edit cannot refuse every fire or unbounded fan-out. Editable in Settings → Swarm. A supplied `swarm` section replaces its fields before normalization, except omitted or `null` `agent_timeout_seconds`, `composer_predictions`, `graph`, `incremental_execution`, `default_reasoning_effort`, `recover_reasoning_output_limit`, `reasoning_output_recovery_effort`, `schedule_answer_check`, and `schedule_answer_model` keep their latest stored values. Desktop autosave includes the other swarm fields when that section changes. |
| `schedule_answer_check` | `true` | Verify scheduling claims in manager answers, tool commentary and scheduled findings. Turning it off skips this check. Schedule execution and wake authorization still use their own rules. Editable in Settings → Swarm. |
| `schedule_answer_model` | `chat` | `chat` follows the conversation model. `auto` first calls the authorized Gateway Decisions role when available, otherwise it follows the conversation model. `decision` requires the Gateway role and reports `decision_model_unavailable` if it is absent. A probability below 0.1 skips claim extraction. At 0.1 or above, the chat model extracts exact claims for verification against persisted schedules. Request or response errors use the existing answer-check retry budget. A personal conversation in LOCAL mode uses its own provider. Missing and unknown selections default to `chat`. The saved model choice is retained while the check is off. Editable in Settings → Swarm. |
| `policy_reasoning_effort` | `""` | reasoning level for the two policy classifiers: the schedule-answer check that verifies a claimed armed check before a finished answer is published, and the wake-permission check. Empty is the background default, `low`; `inherit` follows the conversation's thinking level; `none` explicitly disables thinking; `low` / `medium` / `high` are sent as chosen. Values are trimmed/lowercased; unknown values normalize to empty. These calls classify, so the default keeps them off the answer's critical path: thinking at a high conversation level once held an answer for 57s and 10K reasoning tokens. Only the empty default is optional: an endpoint that rejects it with HTTP 400 is asked once more without `reasoning_effort`, and Trace keeps the refused call. An explicitly chosen or inherited level keeps the endpoint's error. Editable as **Swarm → Policy check thinking**. |

A leftover `goal_session_max_seconds` from older builds is ignored on load; the next save omits it.

When editing a swarm option through the Settings API, read the current settings
and retain the other swarm fields in the saved object.

The current values are reported in `GET /api/meta` and are part of the manager's
system prompt, so it knows how wide it may fan out.

## `tools`

To enable managed execution on macOS or Linux, add `exec_session` to
`tools.enabled`. This opt-in switches the existing `exec` tool on fresh
ordinary manager turns. The engine starts the command once. `yield_after_ms` can set the initial
observation window up to 30 seconds. The default is one second. If the
command is still running when that window ends, it remains owned by the
turn and the manager receives its session ID. The manager can work on
independent steps. The next model request receives any terminal result that
is already available. When the manager is waiting, it resumes when any
owned command completes. The engine collects every terminal result before
accepting the final answer. Output continues through the existing live
stream. `exec_status` reads the status and bounded recent output of one
owned session. `wait_exec` waits for that same ID and accepts an optional
`idle_timeout_ms`. Omit it or pass zero for the 60-second default. The observation
window starts when waiting begins and renews whenever stdout or stderr
advances. An idle observation expiry returns the running process state and
leaves it running. While that target is still running, another uncollected
background command or a foreground command in this manager model's current
tool batch can end the wait early with `code: peer_exec_settled` and the
target's current status, without an idle-timeout indication. The
`observation_timed_out` flag is false and omitted from JSON; the target
process continues. The
completed command's result still arrives under its original tool call ID.
Session IDs and terminal lookups are scoped to the owning turn. Pass a
previously observed `revision` as `after_revision` to avoid
repeating unchanged output. An explicit `pipefail: true` preserves a
failed Bash pipeline status for build, install or test commands.

Add `exec_adaptive_observation` to `tools.enabled` alongside
`exec_session` to try adaptive observation. After a standalone command
yields and the next standalone `wait_exec` collects that same short
command, the engine can briefly observe later standalone commands before
sending the next model request. A command can use this observation until
five seconds from its launch. The original one-second handoff still applies.
Explicit `yield_after_ms` values and concurrent tool or worker work take
precedence. An observation that reaches its limit returns control and clears
the learned preference. Steering can interrupt the observation. Stop and
command deadlines retain their existing process ownership. The preference
belongs to the current turn and the option is off by default.

If the configured provider reports both `X-Zwai-Model-Request-Limit` and
`X-Zwai-Model-Requests-Remaining`, an eligible manager sees the remaining
forwarded request budget, including supporting calls. The last fifth of
that budget adds guidance to prioritize required verification and delivery.
The budget is shared by model clients for the provider route and resets
when those clients are invalidated.

With an owned background command, a simple literal `sleep` of at
least five seconds can wait for the next process exit. The recognized forms
allow literal status markers and a read-only package listing after the wait.
Those follow-ups run after the wake. Use `command sleep` for an intentional
delay in that shape. An explicit timeout bounds the wait and its follow-up.
The original background command keeps its own deadline. Compound commands
with other work, environment overrides, and standard input keep their
shell behavior. Managed exec uses the requested duration for literal
sleeps. Two consecutive ordinary nonzero exits with identical command
inputs and complete output block a further launch. The refusal carries
the original error. An identical retry in a later model round ends the turn.
The default tool list omits this setting.

Omit `timeout_ms` or pass zero to use the owner's lifetime. A positive whole
number sets a hard command deadline in milliseconds from launch, subject
to any earlier owner deadline. Output advances the observation window for
`wait_exec` and leaves the launch deadline in effect. Invalid or unrepresentable values are rejected before launch.
Stop and handoff cancel and join the owned process group. Keep the job in its
owned process group for that cleanup contract.

The watchdog terminates a group after repeatedly verifying the same live
processes are all in a job-control stop for 30 seconds. A quiet command,
ordinary sleeps, I/O waits, and inconclusive process observations leave the
process under normal ownership.

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
| `exec` | shell | on | run a shell command, including directory listings, filename search and content search |
| `web_search` | web | on | search the web (network) |
| `web_fetch` | web | on | fetch a page and extract text (network) |
| `python_runner` | shell | **off** | needs Python on `PATH` |
| `screenshot` | shell | **off** | needs platform capture support |

```yaml
tools:
    disabled: [exec]            # no shell for the agents
    enabled: [python_runner]    # yes to Python
```

`ls`, `tree`, `glob` and `grep` are not in this catalog and do not appear in
Settings. Agents list directories, find files and search file contents with
`exec`. Names for those four tools in `enabled` or `disabled` have no effect.

The composer Tools menu and Settings → Tools write these same lists as soon as
a box changes. The next model request omits a tool that is off, including a
request in a turn that is already running. A call that still arrives returns
`tool_switched_off` and does not run the original tool. Checking a tool this
turn already mounted puts it back on the following request. A tool that was off
when the turn started is mounted when the next turn builds its toolset; that
next turn also rebuilds the manager prompt. Until then, the prompt for the
current turn can still name a tool that was unchecked.

| key | default | meaning |
|---|---|---|
| `web_search_max_results` | `8` | results per search. More context per call, more tokens. |
| `web_execution` | `auto` | `auto` uses the current Gateway account for Web Search/Fetch after authorization and this device while anonymous; `local` keeps both tools local; `cloud` requires a signed-in server account. Editable as **Tools → Web tools run on**. New turns capture the choice; cloud failures never fall back to local execution. |
| `result_max_chars` | `40000` | most characters one workspace tool call (read, write, edit, exec, web tools, …) puts in the model's context. A longer text result keeps its first two thirds and last third around a notice that names how many characters were omitted and asks the agent to narrow the path or pattern, filter the output, or read a range. A result stays in the conversation and is resent on every later call of the turn, so this is a per-call cost. `exec` without its own `max_output_kb` gets a per-stream capture budget of half this cap. The result cap then measures the complete JSON encoding, shortening stream text at Unicode boundaries while keeping both ends and setting `truncated`; it never cuts the JSON document. An explicit `max_output_kb` still limits capture, but does not bypass the final context cap. If metadata alone exceeds the cap, the result's `full_command` copy can also be shortened; the call arguments retain the actual command. Status, error and path metadata remain intact and form a soft minimum for an extremely small cap. Zero or negative is repaired to the default. Editable as **Tools → Tool result limit**. |
| `proxy.http` / `proxy.https` | empty | outbound proxy for locally executed network tools (`web_search`, `web_fetch`). Cloud execution uses the Gateway's own network configuration. Settings → Tools hides these fields while `web_execution` is `cloud`; the saved values stay and return when execution can use this device again. |
| `proxy.no_proxy` | empty | comma-separated hosts that bypass the proxy. |

Locally executed `web_fetch` starts with HTTP. A 401/403 carrying DataDome response signals
triggers one attempt through its existing browser renderer without requiring
`render: true`; it uses the configured tool proxy and needs an available
Chromium browser. A plain authorization failure does not trigger that fallback.
If rendering fails or still returns a challenge, the tool returns a failure
result rather than challenge text as successful page content. This behavior
does not guarantee access to a protected site.

Cloud execution uses only the authorized account's Gateway origin and the
Go-owned private Host credential, through the dedicated Desktop Search/Fetch
routes. Host credentials do not become general model API credentials or enter
the renderer. The Gateway rechecks the Host, owner and exact bound default Key
for approval, API access, expiry and tool policy. A missing/retired authorization
returns a sign-in-again tool error; an administrator's denial remains a failure.
`render: true` requests the Gateway browser path with its supported one-millisecond
wait action. Search snippets are returned as the existing `summary` result field;
Fetch returns Markdown through the ordinary tool-result limit and Trace.

PC conversations may persist `web_execution` independently of this global
setting. Empty inherits the PC setting; `auto`, `local` and `cloud` override it
from the next turn. Each turn captures its choice for restart/parked-worker
restoration. Only the execution preference is captured: each cloud invocation
rechecks the live server origin/auth mode and current private account receipt.
The phone stores independent direct-model and PC-conversation preferences, scoped
by Gateway origin/account. Direct-model `auto` uses cloud functions with an
approved, enabled, unexpired account session and ordinary chat otherwise;
`cloud` requires that session before model dispatch, and `off` omits these
functions. Standard function calling is required from the selected model.
PC-conversation `inherit` always clears the conversation override and follows
the PC global setting, whether or not the phone is signed in; `local`/`cloud`
are explicit overrides. Cloud on the PC requires its own Gateway receipt. The
phone's two choices do not change each other, personal endpoints or PC global
settings. Previous shared `local` reads as phone `off` and PC `local`; previous
`cloud` remains explicit for both until each preference is changed.

PC `meta`/`catalog` reports only its default route and local receipt state.
This offline snapshot is a next-turn summary, not proof of a current server
grant or the active turn's captured setting. Tool errors never change models,
remove functions and retry, or silently execute locally.

Tool paths resolve against the conversation's workspace, but the workspace is not
a boundary: an absolute path or a `..` reaches outside it and `exec` runs as you.
See the "Full access" note in the [README](../README.md).

## External MCP servers

Desktop and Web **Settings → Tools** keeps built-in tools and MCP connections
on separate tabs, apart from provider cloud tools. No MCP server is configured by default.
Opening Settings reads configuration; **Test connection and list tools** explicitly opens a short
connection, lists the server's tools and closes it. Inspection does not execute a
tool and is available even while that server is disabled.

```yaml
mcp:
    direct_load_percent: 5
    servers:
        - id: local-tools
          name: Local tools
          transport: stdio
          command: /absolute/path/to/mcp-server
          args: []
          env: {}
          enabled: true
          timeout_seconds: 60
```

Each `id` is unique and contains 1–32 ASCII letters, digits, underscores or
hyphens. Settings assigns a new id when a connection is added and keeps that
id when the connection is edited. The form asks for the display name and the
connection details; it does not show the id or rewrite typed text. `name` is
the label shown in Settings and the composer Tools menu. Config files and the
HTTP API still address a server by `id`. `transport` is `stdio`, `streamable_http`
or `sse`; stdio uses `command` and an argument array without a shell, while
remote transports use an HTTP(S) `url` and optional `headers`. URLs must have
no user info or fragment. `timeout_seconds` accepts 0–3600; zero or omission
uses 60 seconds, independently for connection/discovery and each tool call.
For a server that enforces a longer task budget internally, set its MCP timeout
above that budget so the server can finish and return its result. For example,
a 120-second server task can use `timeout_seconds: 150`. Progress notifications
do not extend the configured deadline.

Put credentials in `env` or `headers`, not command arguments or URLs: ordinary
fields are visible in Settings and its API. These two private maps are saved in
`config.yaml` with mode `0600`; their values are write-only through HTTP. The
editor shows existing key names, leaves blank JSON fields unchanged, and uses
`{}` to clear a map. Supplying a JSON object replaces that complete map.
Invalid configuration or failed persistence leaves the saved configuration intact.
MCP uses its own `/api/mcp` endpoints; ordinary Settings saves do not replace it.

**Edit JSON** is the same configuration as one text document, in the
`mcpServers` object used by other clients. The single-server form remains the
way to edit one connection. The text starts from the saved connections and
does not include credential values. Applying it adds or updates the entries in
the text; a connection that is not in the text stays, including its saved
credentials. Leave `env` or `headers` out to keep those values, and use `{}`
to clear one map.

```json
{
  "mcpServers": {
    "local": {
      "command": "server",
      "args": ["--stdio"]
    }
  }
}
```

A command written as one line, with `args` omitted or empty, is split into the
program and its arguments. Quotes group one argument. The split does not start
a shell. A program path that contains spaces and has no separate arguments is
quoted in the text. `envFile` and `cwd` are rejected and are not read. A URL
with no `type` uses Streamable HTTP; `type` may also be `stdio`, `sse`, `http`
or `streamableHttp`. Omitted `enabled` and `timeout_seconds` keep their saved
values. An invalid document changes nothing.

`direct_load_percent` is 0–100 and defaults to 5. Settings → Tools edits it.
`0` always keeps the two catalog proxies. After a turn is admitted, and before
its first model request, zwai estimates the tokens in the MCP schemas it would
mount (remote name, description and JSON schema only; built-in tools are not
included). The estimate uses the same counter as context compression, so CJK
text is not treated as four characters per token. When that estimate is within
the percent of the selected model's context window, each enabled remote tool is
its own function. An unknown window (`0`) uses `swarm.auto_compact_tokens` as
the denominator. Above the percent, or when listing that server fails, the
server keeps `list_tools` and `call_tool`, and those descriptions include the
enabled tool names and a one-line summary. A long index is truncated and says
to query further.

A fresh catalog is listed on the turn's cancellable context. Stop cancels that
list and does not call the model. A catalog saved for the current connection
fingerprint is reused with no connection. The cache file is
`mcp-catalogs/<fingerprint>.json` in the data directory, mode `0600`, and holds
names, descriptions and schemas only. Settings inspection, a successful
in-turn list, and `notifications/tools/list_changed` on an open session update
it. A list change does not alter the tools already mounted for the current
turn. The decision for a turn is frozen in `mcp-mounts/<turn id>.json` and in
the `mcp_catalog` Trace event. Neither record contains the endpoint or
credentials.

Direct function names are a safe form of `mcp_<server id>_<remote tool>`, with
a hash suffix when the name would be too long or collide with a proxy. The
description keeps the server's display name and the original remote name.
Arguments are the remote `input_schema`. One schema that cannot be a function
is skipped and recorded; the other tools on that server stay. Direct mode does
not also mount `list_tools` and `call_tool`. A call connects with that call's
cancellation context. The same server shares one session.

Settings → Tools writes `enabled`. The composer Tools menu does not. It stores
remote names the user switched off in `disabled_tools` (default empty, so every
discovered tool is available). A direct tool with that name is omitted from the
next model request. On the catalog proxies, `list_tools` omits it and
`call_tool` returns `tool_switched_off` without running it, including during a
running turn. Changing `disabled_tools` does not retire the server session.
Turning a name back on restores it on the next request when this turn already
mounted it. A server or remote tool that was off when the turn started is
mounted when the next turn builds its toolset. A name in `disabled_tools` is
not checked against a live catalog. Constructing the admission toolset performs
no connection or command startup. A connection or discovery error leaves the
built-in tools available and keeps that server on the catalog proxies; the
Trace records the failure class, not the endpoint. `list_tools` accepts an
optional `query` (matching names and descriptions, without schemas), `name` for
one exact remote schema, or nonnegative `offset` and positive `limit` for
paging (default limit 10). `call_tool` takes a listed remote `name` and an
`arguments` object. Catalog output uses the same tool-result budget; narrow a
truncated page, or increase `tools.result_max_chars` if one schema still
exceeds it.

Before a local command or a direct API call, the catalog proxy tells the model
to query this catalog for the task. The server title is not evidence that no
tool matches. A matching MCP tool is preferred unless the user specifies
another route. A past failure alone does not justify bypassing the server.

Running MCP calls show progress when the server sends standard
[`notifications/progress`](https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/progress)
with the request's progress token. Servers can send a short, user-visible
phase description and optional total. Until a notification arrives, the
desktop and Web transcript shows that it is waiting for progress.

Stdio uses the conversation workspace as cwd, without confining filesystem access.
A toolset fixes its mounted names and the first session it uses. Plan mode and
read-only toolsets do not mount MCP. After confirmed session expiration,
a running MCP tool makes one renewal attempt for the same configured endpoint
or command and reads its current saved environment or headers. It retries the
call only after a verified rejection before execution. Other failures,
including timeouts and failed renewal, stop further requests for the task and
show a repair message. Verify the outcome of a timed-out remote operation
before manually retrying.
Each server/configuration retains at most eight idle workspace sessions; other
servers do not evict them, but survival between calls is not guaranteed. Calls
and results use the normal tool transcript, result limit and Trace. Only MCP tools
are supported here; resources, prompts and OAuth login are not.

For timing diagnosis, search the engine log for `MCP tool call` and match its
`tool_call_id` and server within the turn's Trace time window. The record
includes the transport, host phase (`session`, `connect`, or `call`), outcome
code, elapsed time and configured deadline. A call with a catalog-confirmed
tool includes its name.
Servers that attach recognized phase metadata to progress notifications also
produce `MCP tool progress` records. Match those records to the outcome using
`progress_token_sha256`. These records contain fixed phase and outcome codes,
bounded timing fields, and at most 64 entries per call. Free-form progress
text, tool arguments, results, raw errors and stderr are excluded.
The metadata keys use the `io.github.lubyruffy/` namespace. `phase` identifies
`task`, `prepare_model`, `model_generate`, `source_access`, `source_schema`, or
`source_read`. `phase-event` is `start` or `finish`. A finish supplies an integer
`phase-duration-ms` in 0–3600000 and a recognized `phase-code` such as `ok`,
`failed`, or `timeout`.

## `memory`

What a project remembers between conversations, and what that costs. Memory is
per project — these keys are the budget every project with it switched on
shares. A conversation outside a project does not grow notes. With `enabled`
on, its prompt lists the shared skill library (`library/skills/` under the
data directory) and `skill_view` opens one. With `auto_review` on, a finished
turn there can record another procedure in that library. Copying a file into
a project is how that project's own agent follows it.

| key | default | meaning |
|---|---|---|
| `enabled` | `true` | the master switch. Off means no project carries notes or skills, no review runs, and a conversation outside a project does not add to the shared skill library, whatever a project's own switch says. What is already stored stays readable in the Memory panel. Settings → Memory then shows only this switch. |
| `auto_review` | `true` | read a turn's event log when it finishes and keep what is worth carrying forward. Off leaves note extraction to the agents' own tools and the "Review now" button. Settings hides this switch while memory is off, and hides review notifications, review thinking, review rounds and skill verification while this is off. Successful turns receive a full review even if the manager already wrote memory. A failed project turn may retain explicit human requirements as notes only, without task-tool/derived evidence, skill edits or skill folding; cancellation starts no automatic review. "Review now" still runs against the latest successful turn. Leftover skill families are folded after successful turns, including when auto-review is off; this is catalog hygiene, not extraction. The Memory panel's **Tidy memory** control still folds leftover stems, then asks the reviewer to curate the live catalog by content (a model call, not a filename scan). A catalog tidy uses at least 24 tool rounds (`DefaultTidyMaxIterations`), or `review_max_iterations` when that is higher, so a large index can actually be merged. |
| `auto_dream` | `true` | **Settings → Memory → Verify and correct project skills**, shown only while memory and automatic review are both on. Verify an existing project skill patch against later tool observations after a successful ordinary conversation. Requires project memory and automatic review. Set `false` to disable verification. Existing config files with no value use the default. An older Settings client that omits this field preserves the selected value. Hiding the switch does not change the saved value. |
| `char_limit` | `2200` | how long the notes may get. They ride in the system prompt of **every** turn in the project, so this is a per-turn cost, not a disk one. A write that would grow past this is refused, including replacing a note with a longer one. The 80% prompt warning is advisory: fitting growth is still accepted. Additions include separator cost, and replacements spend their net growth. Review input shows current usage and limits; refused batches preserve structured `usage`, `over_by`, `current_entries` and replacement `matched` feedback for the model. It may shorten or replace supported related notes before adding, within the same review budget; unrelated notes are not automatically evicted. Failed drafts leave official notes intact. Small on purpose. |
| `entry_max` | `360` | how long **one** note may get on an agent write (`memory` add/replace). Notes are short declarations whose absence would misdirect work; methods, steps and workflow-local constraints belong in a skill. A note that restates a recorded skill is refused; an exact existing-name pointer without a body restatement is allowed. The Memory panel's editor still uses `char_limit` only: a person who pastes a longer note is spending that budget on purpose. Zero or negative is repaired to the default; a value above `char_limit` is clamped. |
| `review_reasoning_effort` | `""` | separate reasoning level for automatic and manual turn reviews. Empty uses the background default, `low` for ordinary reviews or `none` when Dream verification is offered. `inherit` follows the reviewed turn. `none` explicitly disables thinking, and `low` / `medium` / `high` are sent as chosen. Values are trimmed/lowercased; unsupported values normalize to empty. Only the empty default is optional. An HTTP 400 on the audit request continues as ordinary review without the audit. An HTTP 400 on ordinary review retries once without `reasoning_effort`. An explicit or inherited level keeps the endpoint's error. Main conversation work and catalog tidy retain their own effort, model and output cap. Editable as **Memory → Review thinking**. Before this default, an empty value inherited the turn; set `inherit` to keep that behavior. |
| `review_deduplicate_feedback` | `false` | opt-in experiment for automatic and manual turn review. Set `review_deduplicate_feedback: true` under `memory` in `config.yaml` to remove the duplicate first issue at the top level and the duplicate `success` and `note` fields inside aggregate refusal issues. The receipt keeps `success: false`, the shared instruction and every issue's error, index, code and repair details. Receipts without an `issues` array keep their existing format. An omitted key uses the legacy feedback format. |
| `review_max_iterations` | `8` | how many times a post-turn review may think and write before it is stopped. It reads one conversation and makes a handful of tool calls; a large number here buys a slow, expensive review rather than a better one. The index is enough to decide; the reviewer does not open every skill. Same-stem families are folded after the review. A successful complete batch ends without a model closing sentence. If the post-turn reviewer ends without a successful batch, including at the cap, it records an explicit error and `stop_reason`, with submitted-proposal count and bounded safe attempt/refusal metadata. Capacity feedback does not add rounds or raise limits. Pending notes are not committed, while actual skill writes remain recorded. The separate catalog-tidy cap retains its existing behavior. The Memory panel's catalog tidy uses at least 24 rounds (`DefaultTidyMaxIterations`), or this value when it is higher. |
| `skill_stale_days` | `90` | advisory age threshold for Memory badges and catalog tidy. An already-viewed skill ages from its last view; a never-viewed skill uses its content modification time. Exactly N days is a candidate; unknown dates are not. Non-positive values use the default. The reviewer decides whether to keep, merge or delete; age alone never deletes. Editable in Settings → Memory. |
| `skills_index_max` | `50` | how many skills are listed in the prompt (manager and sub-agents). Only names and one-line descriptions are listed; an agent opens the one it needs with `skill_view`. Beyond this cap the prompt says how many were not listed. `skill_view` opens only this index — a procedure sitting in the workspace is a file. Sub-agents get `skill_view` and this index; they cannot call `memory` or `skill_manage`. |
| `notifications` | `on` | how a completed review appears in the transcript. `on` is one line naming what changed (`Memory updated: 1 note stored`), with **View details** to expand recorded additions/deletions or explicitly labelled previews. `verbose` also adds the preview to the summary. `off` writes nothing in the transcript — the review still runs, and the Trace tab's Full log still lists it. An unknown value is repaired to `on`, not to silence. |

Deterministic same-stem folding preserves complete source bodies, allowing only
whitespace normalization. A source section is omitted only if the original keeper
already contains that complete normalized text. A partial filesystem failure
still reports the keeper write and source files actually removed, together with
the error; it does not imply rollback. This rule does not prove the semantic
correctness of model-authored skill edits.

Numbers that are zero or negative fall back to their defaults, so a hand-edited
file cannot leave a project with no room to remember anything.

When automatic verification finds a matching recorded skill revision, it looks
for the original tool evidence and later complete tool results in the same
project and provider scope. The existing review reuses the fully visible later
results and receives a compact appendix with the prior revision and original
observations. When the reasoning setting is left at its default, that audit
requests non-thinking mode. An endpoint that rejects the optional setting can
use the existing compatibility retry without it. Explicit levels and inherit
retain their selected behavior. After the normal batch succeeds, an exact, evidence-backed
revert can restore the prior document if the sources, ownership and full file
revision still match. Trace records the outcome, the lookup and apply timings,
and any actual change.

The files live in the data directory, never in your repository — see
[DATA_MODEL.md](DATA_MODEL.md) for the layout. Edit notes from the Memory
panel. Skill files can be edited on disk; Reload then **Tidy memory**
after a hand edit, rather than expecting the tab to rewrite them on open.
Copying a skill into another project writes a new file there. It does not
link the two: **Update** on the copy is how a later source change arrives,
and only when that copy was not edited.

## `personality`

Install-wide personal preferences for how the manager works with you: tone,
language habits, standing likes. It is **not** a task and **not** a project's
business context. Edit it in **Settings → Personality**.

| key | default | meaning |
|---|---|---|
| `instructions` | empty | added to the manager's system prompt of **every** conversation. Empty omits the section entirely, so a blank setting costs no tokens. The text sits before a project's instruction; when the two conflict, the project wins. Sub-agents do not receive this personality. The shared engine applies the same section, so `zwai tui` sees it too. |

Workers receive their task from the manager without automatically inheriting
the project instruction.

## `log`

| key | default | meaning |
|---|---|---|
| `level` | `info` | `debug`, `info`, `warn` or `error`. `debug` adds one line per HTTP request and per event-stream decision — that plus `zwai trace` is usually enough to explain a bad turn. |

Logs go to stderr. Anything unrecognized falls back to `info`.

## `ui`

Chrome only. Agents still answer in the language you are using.

| key | default | meaning |
|---|---|---|
| `locale` | `system` | `system`, `en` or `zh`. `system` follows the browser (`zh*` → Chinese, everything else English). The app menu pins `en` or `zh`. Desktop binds a random loopback, so this lives in the file rather than in `localStorage` alone. Junk becomes `system`. |
| `font` | `system` | UI typeface: `system`, `serif` or `mono`. Sidebar / Settings / title bar. Junk becomes `system`. |
| `ui_font_size` | `medium` | UI size: `small` / `medium` / `large` (12 / 13 / 16px). Sidebar, Settings, title bar, and directory row/gap density. Junk or blank becomes `medium`. |
| `content_font` | `ui` | Conversation typeface. `ui` follows `font`. Else `system` / `serif` / `mono`. |
| `font_size` | `ui` | Conversation size. `ui` follows `ui_font_size`. Else `small` / `medium` / `large`. A stored `medium` stays 13px. Junk becomes `medium`. Directory row height tracks `ui_font_size`, not this key. |
| `code_font` | `mono` | Fences and inline code. `ui` / `system` / `serif` / `mono`. Blank is `mono`. |
| `code_font_size` | `content` | Code size. `content` follows `font_size`, `ui` follows chrome, or `small` / `medium` / `large`. |
| `content_width` | `comfortable` | `comfortable` keeps the reading column (`max-w-3xl`). `full` fills the space between the sidebars. The app menu and ⌘K toggle the same preference. Junk becomes `comfortable`. |
| `transcript_mode` | `user` | `user` folds consecutive thinking and tool calls behind one live ticker. Answers stay visible and split the group. The current activity swaps in vertically; the line itself marquees left-to-right. Copy is **Thinking** (or the latest thought line), **Planning next moves** between model turns on the live tail only, and **Editing** / **Reading** / **Exec** `{name}` while a tool runs. An answer that already follows the latest fold is that tail: the fold keeps the thought / tool count, a streaming answer has no Planning row, and a closed answer with the turn still in the gap shows Planning under the text. Earlier folds keep the thought / tool count. `developer` keeps every thought and tool row. The app menu and ⌘K toggle the same preference. Junk becomes `user`. |
| `welcome_dismissed` | `false` | Set once the first-run welcome was answered or put off (**Not now**, or **Use my own model service**), so it does not return every launch. Signing in or configuring a model service hides the welcome regardless. A settings save that omits it keeps the stored value. There is no Settings toggle; edit the file or `PUT /api/settings` to see the welcome again. |
| `palette` | `zwai` | Named color set. `zwai` is the current chrome. `fofa` maps [FOFA DESIGN 7.2](https://book.fofa.info/docs/design/DESIGN.html) / fofa.info: navy page, cyan signal, blue embellishment, cyan→blue composer rim. Each set has light and dark; the app menu still pins light / dark / system. Junk becomes `zwai`. Settings → General shows System / Light / Dark cards and a Color theme menu. |

Light / dark stays in the browser; language, typeface, column width, transcript
mode and palette are first-class config so a new window keeps them.

## `remote`

Phone pairing over pairlink. Off by default. Settings → Phone does not show
or edit the hub URL, authentication mode, computer name or pairing limits.

| key | default | meaning |
|---|---|---|
| `enabled` | `false` | register this PC with the hub and accept sealed RPC from bound phones |
| `hub_url` | `https://ai.fofa.info:2440` | Gateway / pairlink hub origin. Blank uses the official default, so normal sign-in requires no server configuration; an explicit self-hosted origin is preserved. Settings does not show or edit it; change this file or `PUT /api/settings`. Account mode accepts an HTTPS origin with no path/query/fragment or user info; HTTP is allowed only on loopback for local testing |
| `auth_mode` | `account` | `account` requires browser sign-in, administrator account approval and explicit device authorization before registration. `independent` keeps independent pairlink registration. A blank value uses `account`. A value this version does not understand is removed, the default is written back, and the desktop explains that before opening the window. Settings does not show this control; change this file or `PUT /api/settings`. A save still rejects an unknown value |
| `display_name` | this machine's hostname | name a bound phone paints on its host chip. Blank seeds `os.Hostname()` (or stays empty if that fails). Settings does not show this field. Clipped to 40 runes. Not a compiled label |
| `thread_limit` | `5` | how many idle recents the phone lists below In progress before More. Live rows (`list.running`) are a separate roster and do not count |
| `summary_chars` | `280` | truncate assistant/summary text and user-input turn previews on the phone. Inbox `action` / `summary` are human one-liners (findings, a command, prose), not raw `tool_call` JSON |
| `open_turns` | `6` | completed turns included when a thread is opened |
| `event_chars` | `4000` | max characters of each watched event body on the phone. `tool_delta` is clipped to `summary_chars`. `spawned` text is always empty. Generation interruption/recovery preserves valid JSON and its `reason`/`recovery` fields; only `detail` is shortened (also capped at 512 characters), so a small display budget cannot erase lifecycle meaning |
| `watch_events` | `80` | page size for `log` when the phone pulls up for older rows (`before` 0 pages older than the first snapshot). A reconnect with `since` > 0 still replays the gap on that same `ready` snapshot. First `watch` (`since` 0) paints at most 24 stored events from the live edge (`DefaultRemoteWatchOpen`) even when this is higher, so a long `/goal` turn is not a multi-second freeze |
| `keep_awake` | `true` | hold a system sleep assertion while `enabled` is on. Settings → Phone shows **Keep this computer awake** only after phone connection is enabled. On macOS this is `caffeinate -s` (idle sleep is blocked only on AC power). Linux uses `systemd-inhibit` when that binary exists; Windows uses `SetThreadExecutionState`. A missing key in an older file stays on. An explicit `false` is kept. The assertion follows this switch and `enabled`, not the hub socket — a reconnect must not drop it |

The Host Token and the long-term X25519 key live as files, not in this YAML:

```
$ZWAI_HOME/remote/host_token   0600
$ZWAI_HOME/remote/identity     0600
```

On Unix, remote credential directories/files reject group/other permission bits;
new directories/files use `0700`/`0600`. On Windows, where Go reports `0777`/`0666`
even for private ACLs, the client instead checks the actual owner and DACL. Only the current
user, SYSTEM and built-in Administrators may own the object or receive a
nonzero allow grant. Missing/NULL DACLs, unknown owners or ACE formats, and
grants to other principals (including inherited grants) are refused. An empty
DACL grants nothing; actual file access may still fail. Newly created credential
files are checked before secret bytes are written. The client does not change
existing ACLs automatically. Regular-file, symlink and size checks still apply.
Credential updates flush their record and parent directory; Windows uses a
writable directory handle with `FlushFileBuffers` and reports failures.

`GET /api/settings` never echoes the token. In `independent` mode, enabling pairing
with a hub URL mints `host_token` if missing; `PUT /api/remote/token` can replace
it. In `account` mode, enabling pairing does not register anonymously. Use
**Sign in** in the desktop app, compare the public browser code and
fingerprint, then confirm device authorization after an administrator approves
the account. The renderer never receives a token, device code or proof verifier.
Manual token writes return `403 remote_login_required` in this mode.

The active file supports a schema-1 JSON credential with origin, authorized
flag, host/account IDs, account label and optional public `avatar_url`, while
legacy raw-token files remain readable. `avatar_url` is not an identity.
Only `fofa.info`, `nosec.org`, `baimaohui.net`, and their subdomains are stored.
An omitted or rejected address leaves a previously accepted one in place. `pending-host-token-*` under the same private directory retain candidate
tokens bound to origin and public key for safe retry after an uncertain result.
Each is limited to 16 KiB; at most 128 are retained (2 MiB maximum), scans are
bounded to 128, and staging an existing candidate reuses its file. Capacity
refuses new candidates without deleting old recovery proofs. They are not
activated by cancellation, expiry or mere account approval.

Only one unresolved candidate is allowed per configured origin and device public
key. A normal sign-in retry prefers that retained candidate over an older active
credential. **Reconnect as a new device** returns local `409` with a message to
retry the previous login while its result is uncertain, because authorization
may already have succeeded remotely. A real `409` from the currently owned
Gateway start attempt records `proven_rejected=true` privately; it does not
remove the recoverable token. Normal retry still uses that token and durably
clears the marker before any network request. Only explicit new-device recovery
may replace a proven-rejected candidate, saving the new candidate before retiring
old rejected records for the same origin/public key. Network failure, cancel and
a lost poll response after `201` never mark a candidate rejected. Successful
activation and same-scope cleanup run under the Host lock with directory sync;
foreign origin/public-key records are retained. Cleanup/storage failure reports
`credential_unavailable` rather than claiming connection success.
Changing Hub or mode, disabling pairing, and Stop cancel/join current login work.
Changing the configured Hub does not reuse a credential bound to the previous
configured origin. Gateway private REST rejects redirects; legacy redirect
handling is unchanged. Deleted/revoked PC recovery requires the explicit **Reconnect as a new device** confirmation;
the old active credential is retained until a new authorized receipt is saved.
Foreign active device ownership remains a Gateway rejection. The offer API
still requires both authorized state and an actual online socket before it
mints a Gateway pairing QR. Settings → Phone does not show that QR.

Downgrade compatibility is one-way: this client can read an older raw
`host_token`, but clients released before schema-1 receipts read the file as
raw text and cannot interpret an activated JSON receipt. Before downgrading,
retain the original credential backup privately. Reconfigure pairing with the
older client or restore its original compatible raw-token backup for the same
Hub. Do not supply the JSON document as a raw token. This change does not
convert or restore private credential files automatically.

## `clients`

Local agent task integration is disabled. Old configuration files and Settings
requests may still contain this section. The app normalizes `enabled` to
`false` on load and save. Directory and pagination values remain readable
for compatibility. The app performs no local agent task scans.

## `search`

Conversation search for ⌘K. Keyword indexing is always on. Configuring an
embedding model automatically enables semantic search; no model means off.
No model name is baked in. Settings → Models → Functional models is the single
current entry point. Its scoped embedding binding or gateway preset takes
priority over the legacy fields below. An explicit empty embedding reference
(`provider_id: ""`, `model: ""`) disables search even with a gateway preset.

| key | default | meaning |
|---|---|---|
| `embedding` | `false` | legacy compatibility value derived from `embedding_model`, no longer an independent switch. Old `false` plus a saved model now enables search; clear the model to keep keyword-only search. |
| `embedding_provider` | empty | endpoint `/embeddings` is sent to. Empty follows `models.default`. A deleted id is cleared on load. |
| `embedding_model` | empty | the name that endpoint expects for embeddings. Never defaulted to a product name. Pin one in Settings → Models. |

The index is SQLite FTS5 (trigram) over titles, standing goals and
user/assistant messages. Queries shorter than three runes use LIKE so a
two-character CJK needle still hits. Tool dumps stay out. Vectors live in
`search_chunks` keyed by model; a pin change drops the previous model's rows
and backfills in the background. A failed embed of the query falls back to
keywords.

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

## Private TestFlight release configuration (F-430 / C-010)

`IOS_RELEASE_CONFIG` selects the private release JSON; the default is
`$ZWAI_HOME/ios-signing/release-config.json`. Optional `marketing_version` is
a numeric `x.y.z` used only for a new TestFlight candidate. When omitted, the
release adapter reuses this app's highest-numbered successfully published
marketing version from its private ledger, or the batch version for the first
publication. Existing candidate state, IPA and source receipts retain their
original marketing version even after this configuration changes. Build numbers
remain package-derived and must increase beyond all remote builds/uploads.
This signing configuration is managed outside the application Settings; see
[release setup and recovery](RELEASE.md#testflight-version-and-build-number).

语音设置入口随模型配置权限显示：LOCAL 和可配置的普通网关账号在 Settings → Models 编辑功能模型及语音参数；托管第三方账号隐藏 Models，只在 Speech 入口显示语音开关、音色和资源参数，不显示模型、Provider 或凭据配置。

旧版 LOCAL 设置没有 `model_policy` 时，明确的本地产品模式仍允许编辑原有语音配置；网关模式缺少已验证的能力清单时不推断语音权限，也不显示语音控制。

语音页面只显示网关声明可编辑的音色、识别语言、音频格式、流式格式、朗读语速等参数。网关固定值和上限继续用于请求和校验。已失效的个人参数仍有警告和恢复网关默认值入口。录音格式、最长录音、超时和客户端资源限制位于默认折叠的「高级设置」。

试录和试听使用已保存的配置，需先开启对应的语音输入或语音朗读；试听还需填写文字。按钮不可用时，设置行说明对应开关关闭、录音环境不支持、服务未就绪或文字为空。已有播放期间保留“停止朗读”操作，即使开关关闭或试听文字清空。

托管账号也可保存语音开关、麦克风、超时和资源限制。读取设置时返回手动关闭偏好，运行时元数据返回按模型可用性解析后的开关状态。保存语音偏好时保护隐藏的模型来源、Provider、接口地址和凭据，也保留切换账号前存储的私人连接配置。

## Task panels

`task_panels` configures the bundled task-panel feature. Settings → Tools → Task panel plugins edits the shared options `enabled:true`, `max_state_bytes:8388608` and `max_update_bytes:1048576`. Configure the FOFA world-map source through this section of `config.yaml` or the task-panel settings API. Its defaults are `fofa_client:fofa`, `max_assets:1000`, `batch_size:100`, `concurrency:16`, `timeout_seconds:8` and `flush_interval_ms:100`. Changes apply to new source runs; turning the feature off also refuses later live publications, causing active sources to cancel/join and retain an interrupted marker. FOFA credentials stay in the CLI existing mechanism. See [Task panels](TASK_PANELS.md).

`task_panels.easm` configures the EASM portrait aggregations. Set `server_id` to bind a specific server. When empty, each tool must have exactly one offering on the turn's mounted MCP surface. An explicitly configured name can use its mounted progressive proxy before the initial catalog is cached. Enabled servers and role bindings are captured during turn admission. New bindings apply on the next turn, and disabling a service or tool can still prevent a current call.

`max_domains:20` (1..200) bounds domain nodes across all batches in a portrait. `max_components:20` (5..100) bounds product buckets per aggregation. `max_clouds:5` (1..5) bounds the hosting provider rows. `max_services:5` (1..10) bounds the services shown per provider. `tools` maps aggregation roles to remote tool names. The defaults are `stats:mcp_fofa_stats`, `heat:fofa_asset_attention_trend` and `product_rules:fofa_product_rule_lookup`. The stats tool serves both totals and component aggregation. An empty name disables the role. Unknown roles and untrimmed names are rejected. Older discovery roles (`organizations`, `relations`, `branches`, `filings`, `asset_rules`) are ignored. Edit these fields in the configuration file. The Settings page exposes the common task-panel options.
