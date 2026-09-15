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
- **It says what it is doing, even when it is quiet.** A turn reports in every
  few seconds while it works, so a swarm thinking hard inside a slow tool call
  shows a ticking "Working for 1m 12s · 2 sub-agents running" instead of looking
  frozen. While it is running, that line — and the live tool / sub-agent rows —
  sweep, and scroll if the text does not fit.
- **It stays live without freezing the window.** Streamed tokens are folded into
  one event every few milliseconds, completed markdown is not re-parsed on
  every token, and typing in the composer does not rebuild the conversation.
- **Steering, not restarting.** Press Enter while a turn is running and your text
  is injected at the next turn boundary instead of starting over.
- **Projects that remember.** Group conversations under one working directory and
  one instruction, and let them keep what they learn: after each turn the project
  writes down durable facts and records reusable procedures as skills, which
  every later conversation in that project starts with.
- **Per-conversation model and thinking level.** When more than one endpoint is
  configured, the composer switches models per conversation; a thinking-level
  menu (Default / Low / Medium / High) sets how hard the models reason. Both
  apply from the next turn.
- **Real tools.** File read/write/edit, `ls`/`tree`/`glob`/`grep`, shell `exec`,
  web search and fetch, from [eino-tools](https://github.com/LubyRuffy/eino-tools).
  The transcript shows the command or query, not the JSON envelope; a failed
  `exec` is a red error, not a grey dump.
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
   worker needs this conversation so far; `resume_agent` when more work depends
   on a finished worker — same id, not a twin with the same name) and you see each one appear.
2. **A workspace.** Every conversation has its own directory
   (`~/.zwai-swarm/workspaces/<thread-id>/`). Uploads land in `uploads/`, agent
   output lands next to it, and the **Files** tab lists it all with download
   (and, in the desktop app, "Show in Finder"). Expand a `read` in the transcript
   to see the file: markdown is rendered, other files keep their line numbers.
3. **Steering.** Type while it works — "skip the third file, it's a duplicate" —
   and the manager picks it up at its next turn instead of after finishing.
4. **A reason for everything.** The **Trace** tab and `zwai trace <turn-id>` show
   the ordered timeline plus every model call with its size and duration.

Keyboard: `⌘K` command palette · `⌘N` new conversation · `⌘B` hide or show the
conversation list · `⌘\` toggle the right panel · `⌘,` settings · `Esc` stop the
running turn.

### Projects

Work that comes back — one repository, one report, one recurring chore — belongs
in a project. **New project** at the top of the conversation list asks for three
things:

- **an instruction**, added to the system prompt of every conversation in the
  project, so you stop repeating how you want things done;
- **a working directory**, an absolute path that already exists. Every
  conversation in the project reads and writes it directly, with your
  permissions. Leave it empty and zwai keeps one for you;
- **memory**, on by default.

With memory on, each finished turn is read back and what is worth carrying
forward is kept: short notes about how this project works, and *skills* —
step-by-step procedures the agents wrote for themselves. The next conversation in
that project starts with the notes in its prompt and an index of the skills, and
opens a skill when it needs one.

The **Memory** tab shows both. Notes are editable, skills can be read and
deleted, and **Review now** re-reads the last finished turn. A write that landed
is also named in the transcript itself (`Memory updated: …`), so you do not have
to have the tab open to notice. If you were mid-edit when a review wrote, the
panel says so and lets you keep yours or take the new ones — the last save does
not silently win. Correcting a wrong note there matters: it would otherwise be
repeated in every future conversation. Notes are budgeted (`memory.char_limit`,
default 2200 characters) because they ride in every prompt — once full, something
has to be replaced to make room.

Memory lives in the data directory, never in your working directory, so a project
pointed at a repository leaves nothing in it. Deleting a project deletes its
conversations and its memory; files in a working directory you chose are left
alone. See [docs/CONFIG.md](docs/CONFIG.md) for the budgets.

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
├── workspaces/<thread>/  one directory per standalone conversation, `uploads/` inside it
└── projects/<project>/   a managed working directory, and the project's memory
```

Deleting a conversation deletes its workspace when zwai created it; a
conversation in a project shares the project's directory, which may be your own
repository, so that is left alone. Nothing is sent anywhere except to the model
endpoint you configured — and to whatever the agents fetch when you ask them to
search the web.

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
