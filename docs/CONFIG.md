# Configuration

One YAML file, `config.yaml`, in the data directory:

```
$ZWAI_HOME (default ~/.zwai-swarm)/config.yaml     mode 0600
```

`zwai config path` prints it, `zwai config show` prints its effective contents
with the API key redacted. Everything in it is editable from **Settings** in the
app, and a `PUT /api/settings` rewrites the file atomically — a crash mid-write
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
          timeout_seconds: 300
swarm:
    max_concurrent: 6
    agent_timeout_seconds: 600
    max_turns: 24
    manager_max_iterations: 32
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
    review_max_iterations: 8
    skills_index_max: 50
    notifications: on
log:
    level: info
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
conversations use. Each conversation remembers its own provider, so you can move
a long conversation to a bigger model without changing the default.

| key | meaning |
|---|---|
| `id` | stable identifier, referenced by `models.default` and by each conversation. Blank or duplicate ids are renamed to `provider-N` on load. |
| `label` | what the UI shows. Falls back to `model`, then `id`. |
| `base_url` | the endpoint, including any `/v1`. |
| `api_key` | may be empty: local endpoints frequently need none. Never returned by the API — the settings dialog sees only a "set / not set" flag. |
| `model` | the model name to request. |
| `timeout_seconds` | per model call. Default `300`. Swarm answers are long; a tight timeout shows up as a turn that fails halfway. |

A provider is **ready** when it has both `base_url` and `model`. Until the default
provider is ready, `GET /api/meta` reports `configured: false` and the UI shows a
setup banner instead of pretending a turn can run.

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
| `max_concurrent` | `6` | sub-agents running at the same time. Raise it for wide fan-out work; every one of them is a model call in flight. |
| `agent_timeout_seconds` | `600` | watchdog per sub-agent. A hung endpoint is force-terminated and the agent's result records the timeout. |
| `max_turns` | `24` | ReAct iterations per sub-agent. A model stuck in a loop ends here instead of spinning. |
| `manager_max_iterations` | `32` | iterations for the manager. Lower it and complex plans get truncated mid-way; the manager also spends turns waiting for workers. |
| `progress_interval_seconds` | `5` | how often a running turn emits a progress pulse. It is the only thing that moves while every agent sits in a slow tool call, so a higher value makes a busy run look stuck for longer. Zero or negative falls back to the default; pulses cannot be switched off. |
| `delta_coalesce_ms` | `50` | how long streamed tokens wait to be sent as one event. A token every few milliseconds would redraw the whole UI; one pulse per interval keeps the screen moving without a frame per token. Zero or negative falls back to the default. |

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
| `auto_review` | `true` | read a turn back when it finishes and keep what is worth carrying forward. Off leaves memory to the agents' own tools and the "Review now" button. Only a turn that finished cleanly is reviewed: nobody who pressed stop asked for a half-finished approach to become a skill. |
| `char_limit` | `2200` | how long the notes may get. They ride in the system prompt of **every** turn in the project, so this is a per-turn cost, not a disk one. Once it is full an agent must replace a note to add one — which is the point, and why it is small. |
| `review_max_iterations` | `8` | how many times the review may think and write before it is stopped. It reads one conversation and makes a handful of tool calls; a large number here buys a slow, expensive review rather than a better one. |
| `skills_index_max` | `50` | how many skills are listed in the prompt. Only names and one-line descriptions are listed; an agent opens the one it needs with `skill_view`. Beyond this cap the prompt says how many were not listed. |
| `notifications` | `on` | how a completed review appears in the transcript. `on` is one line naming what changed (`Memory updated: 1 note stored`). `verbose` adds a preview of the written text. `off` writes nothing in the transcript — the review still runs, and the Trace tab still lists it. An unknown value is repaired to `on`, not to silence. |

Numbers that are zero or negative fall back to their defaults, so a hand-edited
file cannot leave a project with no room to remember anything.

The files live in the data directory, never in your repository — see
[DATA_MODEL.md](DATA_MODEL.md) for the layout, and edit them from the Memory
panel rather than by hand while the app is running.

## `log`

| key | default | meaning |
|---|---|---|
| `level` | `info` | `debug`, `info`, `warn` or `error`. `debug` adds one line per HTTP request and per event-stream decision — that plus `zwai trace` is usually enough to explain a bad turn. |

Logs go to stderr. Anything unrecognized falls back to `info`.

## Multiple instances

Everything is scoped to the data directory, so a second instance is one flag away:

```bash
zwai web --data-dir /tmp/zwai-demo --mock --no-open
```

That gets its own `config.yaml`, its own database and its own workspaces, and
touches nothing in `~/.zwai-swarm`. It is how the end-to-end tests run.
