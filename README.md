# zwai

A desktop co-working app for a **swarm of agents**: you describe a task, a manager
agent plans it, spawns sub-agents in parallel, steers them mid-flight, and reports
back — while you watch every agent, every tool call and every file it produced.

One Go binary. The native window, the browser and the terminal are clients of the
same engine, so uploads, downloads and the live event stream have one implementation.

Projects can opt into **Nuwa collaboration**: a planner model coordinates, and an
executor model does the workspace and external-service work. See [Nuwa](docs/NUWA.md).
Day-to-day use is in [Using zwai](docs/USAGE.md).

```
┌───────────────┬─────────────────────────────────┬──────────────────┐
│ Conversations │  Transcript                     │ Agents / Files   │
│               │  ┌───────────────────────────┐  │ /Trace           │
│ ● Working     │  │ thought for 4s            │  │                  │
│   Yesterday   │  │ tool  read notes.md       │  │ researcher  done │
│   Earlier     │  │ Started 2 sub-agents      │  │ writer    active │
│               │  └───────────────────────────┘  │                  │
│ ⚙ settings    │     [ ask anything…     ➤ ]   │ output.md   4 KB │
└───────────────┴─────────────────────────────────┴──────────────────┘
```

## Quick start

Requirements: Go 1.26+ and Node.js when a checkout UI bundle is missing or stale
(`npm install --include=dev` uses the public registry). The desktop window uses
[Wails 3](https://wails.io) and needs a C toolchain (macOS: Xcode Command Line
Tools; Linux: `webkit2gtk` dev packages).

```bash
git clone https://github.com/LubyRuffy/eino-swarm
cd eino-swarm

# first run builds frontend/dist (needs Node), then opens the native window
go run ./cmd/zwai desktop
# macOS may ask to allow local network (LAN model endpoints). Allow it.

# or the same app in your browser
go run ./cmd/zwai web
```

First launch writes `~/.zwai-swarm/config.yaml`. Until a model can answer, the
welcome dialog offers **Sign in to the official server** or **Use my own model
service**. Either answer is remembered. Signed-in models stay a temporary list
and are not written into `config.yaml`. Any OpenAI-compatible endpoint works,
including a local one; the API key may be empty when the endpoint does not
need one.

```bash
export OPENAI_BASE_URL=https://your-endpoint/v1
export OPENAI_API_KEY=sk-...
export OPENAI_MODEL=your-model
go run ./cmd/zwai desktop
```

`OPENAI_*` only fills blank fields on first run. Settings, themes, search and
the rest of the keys are in [docs/CONFIG.md](docs/CONFIG.md).

### Try it without a model

```bash
go run ./cmd/zwai web --mock
```

`--mock` runs a scripted offline provider: two sub-agents, tool calls, and one
written file. No network and no API key. The end-to-end tests use the same
provider.

### Phone

Official phone apps sign in to the default Gateway, then connect to an online
PC on the same account. Scanning a QR code, pasting a pairing link and using
your own model service are under **Other ways**. The phone guide is
[mobile/README.md](mobile/README.md).

## Using it

You do not have to ask for sub-agents. The manager delegates when that saves
time or improves quality, and it checks the workers' evidence before reporting
completion. One conversation has one turn. Enter while a turn is running queues
a follow-up; ⌘Enter steers the current turn. **Stop** cancels it.

> Go through the three files I just uploaded, pull out every deadline, and leave me
> a single summary.md with one table.

What you get is a plan, workers in the Agents tab, a workspace in the Files tab,
and a Trace tab with the turn id (`zwai trace <id>`). `/goal` keeps pursuing a
standing objective. `/plan` explores and writes a plan before changing files.
Composer, schedule, projects, memory and skills are in [Using zwai](docs/USAGE.md).

## Common commands

```bash
zwai                        # same as zwai desktop
zwai web --addr :9000       # serve on another port
zwai web --no-open          # do not open a browser
zwai tui --task "..."       # one task, then this process can exit
zwai trace tn_ab12 --full   # replay one turn
zwai backup --output ~/zwai.zwai-backup
zwai --data-dir /tmp/demo  # a separate data directory
```

Every flag is in [docs/CLI.md](docs/CLI.md). Desktop, web and the terminal share
one engine per data directory. Closing a window does not stop a running turn.

## Where things live

```
~/.zwai-swarm/            $ZWAI_HOME overrides this
├── config.yaml           settings (0600; the API key is in here)
├── data-generation       how many restores have committed; not part of a backup
├── engine.lock           one engine process for this directory
├── engine.json           pid and URL of that process
├── zwai.db               conversations, transcripts, event timeline, model calls
├── remote/               Host Token and device identity; not in yaml
├── workspaces/<thread>/  one directory per standalone conversation, `uploads/` inside it
├── library/skills/       procedures recorded from conversations in no project
└── projects/<project>/   a managed working directory, and the project's memory
```

Settings → Backup and `zwai backup` / `zwai restore` write a `.zwai-backup`
file. The page shows progress while the copy is running. Symbolic links
inside the copied directories are left out, and the page says how many.
If a conversation is still running, you can cancel or leave it out of this file
(`zwai backup --skip-active`). Restore waits until nothing is running.
Use these commands while the app is running. Copying the directory still works
when the engine is stopped.

Deleting a conversation deletes its workspace when zwai created it. A
conversation in a project shares that project's directory, which may be your
own repository, so those files are left alone. Nothing is sent anywhere except
to the model endpoint you configured — and to whatever the agents fetch when
you ask them to search the web.

## Full access, deliberately

Agents run with the same rights you have: `exec` runs arbitrary commands, and an
absolute path or a `..` reaches outside the workspace. The workspace is where
relative paths resolve, **not** a sandbox, which is why the UI says "Full access".
Run it on tasks and machines where that is acceptable. There is no approval flow.

## Documentation

| document | for |
|---|---|
| [docs/USAGE.md](docs/USAGE.md) | using the app: composer, turns, goals, projects, memory |
| [mobile/README.md](mobile/README.md) | the iOS and Android apps |
| [docs/CLI.md](docs/CLI.md) | every subcommand and flag |
| [docs/CONFIG.md](docs/CONFIG.md) | every configuration key |
| [docs/API.md](docs/API.md) | the HTTP/SSE API both front ends use |
| [ARCHITECTURE.md](ARCHITECTURE.md) | modules, and how a turn flows through them |
| [docs/DATA_MODEL.md](docs/DATA_MODEL.md) | the SQLite schema |
| [docs/FEATURES.md](docs/FEATURES.md) | the capability map |
| [docs/CONTRACTS.md](docs/CONTRACTS.md) | behavior boundaries |
| [docs/TESTING.md](docs/TESTING.md) | how to run and extend the tests |
| [docs/LIBRARY.md](docs/LIBRARY.md) | the swarm library on its own, for your code |
| [docs/RELEASE.md](docs/RELEASE.md) | issue closure and publishing a batch |
| [docs/NUWA.md](docs/NUWA.md) | Nuwa collaboration |
| [AGENTS.md](AGENTS.md) | rules for anyone (human or AI) changing this repo |
| [CHANGELOG.md](CHANGELOG.md) | what changed |

## Development

```bash
make test        # go test -race -cover ./... + front-end unit tests
make e2e         # Playwright, on the offline provider
make model-check # TLC: lifecycle, event stream, scheduling authorization and consent timeline
make frontend    # rebuild frontend/dist if the TypeScript sources changed
make build       # ./bin/zwai
```

The front end is React + TypeScript + Tailwind + shadcn/ui under `frontend/`.
`frontend/dist` is a build artefact; `go run ./cmd/zwai desktop` (and `web`)
rebuild it when the sources changed. `make build` generates first so the
embed inside the binary is current.

Rules for changing this code are in [AGENTS.md](AGENTS.md). How to run and
extend tests, including native desktop cases, is in [docs/TESTING.md](docs/TESTING.md).
