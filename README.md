# zwai

A desktop co-working app for a **swarm of agents**: you describe a task, a manager
agent plans it, spawns sub-agents in parallel, steers them mid-flight, and reports
back — while you watch every agent, every tool call and every file it produced.

One Go binary. The native window and the browser run the **same** HTTP server, so
uploads, downloads and the live event stream have exactly one implementation.

```
┌───────────────┬─────────────────────────────────┬──────────────────┐
│ Conversations │  Transcript                     │ Agents / Files   │
│               │  ┌───────────────────────────┐  │ /Trace           │
│ ● Working     │  │ thought for 4s            │  │                  │
│   Yesterday   │  │ tool  read notes.md       │  │ researcher  done │
│   Earlier     │  │ Started 2 sub-agents      │  │ writer    active │
│               │  └───────────────────────────┘  │                  │
│ ⚙ settings    │  [ ask anything…      ] [ ➤ ]   │ output.md   4 KB │
└───────────────┴─────────────────────────────────┴──────────────────┘
```

- **Swarm, visible.** Sub-agents appear as they spawn, with live status and their
  own transcript. Built on [eino](https://github.com/cloudwego/eino) ADK and the
  swarm library in this repository.
- **Steering, not restarting.** Press Enter while a turn is running and your text
  is injected at the next turn boundary instead of starting over.
- **Per-conversation model and thinking level.** When more than one endpoint is
  configured, the composer switches models per conversation; a thinking-level
  menu (Default / Low / Medium / High) sets how hard the models reason. Both
  apply from the next turn.
- **Real tools.** File read/write/edit, `ls`/`tree`/`glob`/`grep`, shell `exec`,
  web search and fetch, from [eino-tools](https://github.com/LubyRuffy/eino-tools).
- **Nothing hardcoded.** Endpoints, models, concurrency and tool switches live in
  one YAML file, editable from Settings.
- **One-id troubleshooting.** Copy a turn id from the UI and
  `zwai trace <id>` replays the whole run: timeline, tool calls, model calls.

## Quick start

Requirements: Go 1.26+. The desktop window uses [Wails 3](https://wails.io) and
needs a C toolchain (macOS: Xcode Command Line Tools; Linux: `webkit2gtk` dev
packages). `frontend/dist` is committed, so no Node toolchain is needed to run.

```bash
git clone https://github.com/LubyRuffy/eino-swarm
cd eino-swarm

# native window (the default subcommand)
go run ./cmd/zwai desktop

# or the same app in your browser
go run ./cmd/zwai web
```

First launch writes `~/.zwai-swarm/config.yaml` and shows a setup banner until a
model is configured. Fill in **Settings → Models** (base URL, API key, model
name), or seed it from the environment before the first start:

```bash
export OPENAI_BASE_URL=https://your-endpoint/v1
export OPENAI_API_KEY=sk-...
export OPENAI_MODEL=your-model
go run ./cmd/zwai desktop
```

Any OpenAI-compatible endpoint works, including a local one; the API key may be
empty when the endpoint does not need one.

### Try it without a model

```bash
go run ./cmd/zwai web --mock
```

`--mock` runs a scripted offline provider that spawns two sub-agents, calls tools
and writes a file into the workspace. No network, no key — it is what the
end-to-end tests run on and the fastest way to see the UI work.

## Using it

Ask for something that has parts, because that is when a swarm beats one agent:

> Go through the three files I just uploaded, pull out every deadline, and leave me
> a single summary.md with one table.

What you get:

1. **A plan, then sub-agents.** The manager spawns workers (`fork_context` when a
   worker needs the conversation so far) and you see each one appear.
2. **A workspace.** Every conversation has its own directory
   (`~/.zwai-swarm/workspaces/<thread-id>/`). Uploads land in `uploads/`, agent
   output lands next to it, and the **Files** tab lists it all with download
   (and, in the desktop app, "Show in Finder").
3. **Steering.** Type while it works — "skip the third file, it's a duplicate" —
   and the manager picks it up at its next turn instead of after finishing.
4. **A reason for everything.** The **Trace** tab and `zwai trace <turn-id>` show
   the ordered timeline plus every model call with its size and duration.

Keyboard: `⌘K` command palette · `⌘N` new conversation · `⌘\` toggle the right
panel · `⌘,` settings · `Esc` stop the running turn.

## Common ways to run it

```bash
zwai                        # same as `zwai desktop`
zwai web --addr :9000       # serve on another port
zwai web --no-open          # do not open a browser
zwai tui --task "..."       # one task, in the terminal, no UI
zwai trace tn_ab12…         # replay one turn; also accepts a conversation id
zwai trace th_cd34… --full  # untruncated event text
zwai config path            # where the config file is
zwai config show            # what it says (the API key is redacted)
zwai --data-dir /tmp/demo   # use a throwaway data directory
```

`--data-dir` works on every subcommand, as does `--mock`. See [docs/CLI.md](docs/CLI.md).

## Where things live

```
~/.zwai-swarm/            $ZWAI_HOME overrides this
├── config.yaml           settings (0600; the API key is in here)
├── zwai.db               conversations, transcripts, event timeline, model calls
└── workspaces/<thread>/  one directory per conversation, `uploads/` inside it
```

Deleting a conversation deletes its workspace. Nothing is sent anywhere except to
the model endpoint you configured — and to whatever the agents fetch when you ask
them to search the web.

## Full access, deliberately

Agents run with the same rights you have: `exec` runs arbitrary commands, and an
absolute path or a `..` reaches outside the workspace. The workspace is where
relative paths resolve, **not** a sandbox, which is why the UI says "Full access".
Run it on tasks and machines where that is acceptable. There is no approval flow.

## Documentation

| document | for |
|---|---|
| [docs/CLI.md](docs/CLI.md) | every subcommand and flag |
| [docs/CONFIG.md](docs/CONFIG.md) | every configuration key |
| [docs/API.md](docs/API.md) | the HTTP/SSE API both front ends use |
| [ARCHITECTURE.md](ARCHITECTURE.md) | modules, and how a turn flows through them |
| [docs/DATA_MODEL.md](docs/DATA_MODEL.md) | the SQLite schema |
| [docs/TESTING.md](docs/TESTING.md) | how to run and extend the tests |
| [docs/LIBRARY.md](docs/LIBRARY.md) | the swarm library on its own, for your code |
| [AGENTS.md](AGENTS.md) | rules for anyone (human or AI) changing this repo |
| [CHANGELOG.md](CHANGELOG.md) | what changed |

## Development

```bash
make test        # go test -race -cover ./... + front-end unit tests
make e2e         # Playwright, on the offline provider
make frontend    # rebuild frontend/dist after editing frontend/src
make build       # ./bin/zwai
```

The front end is React + TypeScript + Tailwind + shadcn/ui under `frontend/`.
`frontend/dist` is committed on purpose; `make build` refreshes it.

Rules for changing this code — coverage bars, prompt hygiene, documentation
duties — are in [AGENTS.md](AGENTS.md). Read it before opening a pull request.
