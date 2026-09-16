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
│ ⚙ settings    │     [ ask anything…     ➤ ]   │ output.md   4 KB │
└───────────────┴─────────────────────────────────┴──────────────────┘
```

- **Jump inside a long conversation.** Once you have sent two messages, a short
  tick cluster sits in the middle of the transcript's left edge. Hover it for a
  list of your own turns; click one to scroll there. Auto-follow unpins so a
  live stream does not yank you back to the bottom. Opening another conversation
  from the list lands at its latest turn, not the top of the history. New tokens while you are
  reading light a jump-to-latest control; click it to return and follow again.
- **A ring on the composer shows how full the last manager prompt is.** Hover
  it for the compact count against this model's window, plus this-turn billed
  in/out. The numbers come from the endpoint, not a guessed tokenizer. Discover
  fills per-model windows when the listing includes them; Settings edits
  each name, with a fallback only for names that still have none. Until one
  is set the arc still grows (it is not an empty circle)
  and the label stays a count, not a fake percentage.
- **Paste a screenshot into the box.** It lands as a thumbnail you can drop
  before sending, and goes to the model as visual input — not as a file in the
  workspace. Dragging a file onto the box does the same split: images become
  thumbs, other files become workspace attachments, same as the paperclip.
  Sending names those files on the message, so the model reads what you just
  attached instead of an older leftover sitting in the same folder.
- **Copy or edit a sent message.** Copy and a pencil sit under each of your
  bubbles, with the time you sent it. Copy takes the text. Edit opens the bubble
  in place (Cancel / Send). Sending keeps that bubble, clears everything below
  it, and starts again at that position — it is not a composer prefill, and
  it is not a new turn on top.
- **Deletes ask first.** A conversation, project, provider, workspace file,
  skill, or queued message opens a confirm. Cancel leaves it. A chip on the
  composer that has not been sent yet still drops immediately.
- **Quote a passage into the next message.** Select text in the transcript (or a
  sub-agent's log) and **Add to chat**. It lands as an annotation on the
  composer — hover to read, edit or drop it — and is sent with whatever you type
  next, instead of being dumped into the box.
- **Slash commands in the composer.** Type `/` at the start of the box for the
  same palette Cursor and Codex use. **goal** pins a standing objective this
  conversation keeps pursuing until the manager calls `complete_goal` (or you
  clear it from the banner). If it hits an obstacle it cannot pass, it calls
  `block_goal` and stops instead of retrying forever — **Start** resumes,
  the text is editable in place, and a live turn is told immediately. The
  runtime starts the next turn itself; a queued follow-up still wins. **compact** folds earlier turns into a briefing for later
  prompts; the transcript you see does not change. Pin a cheaper summarizer
  under Settings → Models the same way as the conversation namer.
- **Swarm, visible.** Sub-agents appear as they spawn, with live status and their
  own transcript. Built on [eino](https://github.com/cloudwego/eino) ADK and the
  swarm library in this repository.
- **It says what it is doing, even when it is quiet.** A turn reports in every
  few seconds while it works, so a swarm thinking hard inside a slow tool call
  shows a ticking "Working for 1m 12s · 2 sub-agents running" instead of looking
  frozen. While it is running, that line — and the live tool / sub-agent rows —
  sweep, and scroll if the text does not fit.
- **Thinking stays a 10-line window.** While the model is reasoning, the block
  caps at ten lines and follows new tokens; earlier lines stay reachable by
  scrolling, with a fade at the top so it is obvious they are there. The
  **Thinking** label sweeps like the other live status lines. Click the row to
  hide it while it is still running. After it finishes it collapses to **Thought**.
- **It stays live without freezing the window.** Streamed tokens are folded into
  one event every few milliseconds, answers render as markdown as they arrive
  (finished ones are not re-parsed on every token), and typing in the composer
  does not rebuild the conversation.
- **Steering, not restarting.** Enter while a turn is running **queues** a
  follow-up for after this one finishes (refresh-safe). **Steer** on that row, or
  ⌘Enter, injects into the current turn at the next model boundary — it does not
  kill an in-flight tool. Stop is what cancels. If the manager hits its
  tool-round limit, the transcript asks whether to add another slice rather than
  dying with a graph error.
- **Unfinished work survives a crash.** Kill the process, quit the window, or
  lose power mid-turn: the next start continues every leftover conversation,
  with the answers already on screen still in the model's context. Pressing
  **Stop** is the one thing that does not come back.
- **Projects that remember.** Group conversations under one working directory and
  one instruction, and let them keep what they learn: after each turn the project
  writes down durable facts and records reusable procedures as skills, which
  every later conversation in that project starts with.
- **Conversations name themselves.** The first message is a placeholder so the
  sidebar is readable at once; after the first reply a short title replaces it.
  A name you type is never overwritten. Switch it off in Settings → Swarm, or
  pin a cheaper model under Settings → Models when an endpoint lists more than
  one.
- **Per-conversation model and thinking level.** Settings → Models lists
  providers as rows (open one for URL, key, default). The composer picker
  groups models by provider, searches, refreshes the catalog, and jumps
  to Edit providers. A thinking-level menu (Default / Low / Medium / High)
  sets how hard the models reason. Both apply from the next turn. A
  thought that is still streaming is not cut off by the request timeout;
  raise it in Settings → Models if the endpoint stays silent.
- **Real tools.** File read/write/edit, `ls`/`tree`/`glob`/`grep`, shell `exec`,
  web search and fetch, from [eino-tools](https://github.com/LubyRuffy/eino-tools).
  The transcript shows the command or query, not the JSON envelope; a failed
  `exec` is a red error, not a grey dump. Opening an `exec` row wraps the full
  command with shell highlighting instead of leaving it cut off. Opening a
  `read` paints the file from its suffix (Go, TypeScript, Python, …) instead
  of a grey dump; markdown still renders as prose. Agents are
  told which OS, shell and date they are on, so they stop emitting GNU-only
  flags on a Mac.
- **Links leave the app.** A markdown URL opens in a new browser tab (`zwai web`)
  or the system browser (`zwai desktop`). It does not replace the window.
- **One-id troubleshooting.** Copy a turn id from the UI and
  `zwai trace <id>` replays the whole run: timeline, tool calls, model calls
  with billed tokens. The Trace tab shows the same turn's status and cost; open
  **Full log** for the on-screen timeline.

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
model is configured. Open **Settings** (⌘,) — a full-page sheet, sections in the
left rail (on the desktop window, **Back to app** sits below the traffic
lights). Edits write themselves; **Back to app** flushes the last keystroke.
Chrome language is **Settings → General**, the **中 / EN** control in the title
bar, or ⌘K → Switch language. Agents still answer in the language you are using.
Fill in **Models** (base URL, API key, discover models, pick a
default), or seed it from the environment before the first start:

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
   Open one in the Agents tab to read the system prompt it was given.
2. **A workspace.** Every conversation has its own directory
   (`~/.zwai-swarm/workspaces/<thread-id>/`). Uploads land in `uploads/`, agent
   output lands next to it, and the **Files** tab shows a collapsible tree —
   filter at the top, directories expand like a file explorer, download on
   hover (and, in the desktop app, "Show in Finder"). The tree is the workspace
   root, listed breadth-first so a large generated folder cannot hide the rest.
   Expand a `read` in the transcript to see the file: markdown is rendered,
   other files keep their line numbers.
3. **Steering.** Type while it works. The nudge sits under the working line
   until the manager's next model call — it is queued, not inserted into the
   current tool. Steering that arrives after the last model call becomes a
   follow-up turn.
4. **Quote the conversation.** Select a passage and **Add to chat** when you want
   the next message to point at it. Hover the annotation to edit or drop the
   quote; send with an empty box if the quote is the whole request.
5. **What this turn cost.** The **Trace** tab shows status, duration, billed
   tokens, and the turn id (`zwai trace <id>` replays the full dump). The event
   log stays folded until you open **Full log**.

Keyboard: Enter sends (while a turn is running it queues a follow-up; an IME
confirmation — keeping leftover Latin as typed — is not a send) · `/` at the
start of the box opens built-in commands · ⌘Enter steers
the draft into the current turn · Shift+Enter a newline · `⌘K` command palette · `⌘N` new
conversation · `⌘F` find in the open conversation · `⌘B` hide or show the
conversation list · `⌘\` toggle the right panel · `⌘,` settings · `Esc` stop
the running turn (or close find first, if that bar is open). Drag the border
of the conversation list or the right panel to resize them. Conversations
and projects sort by last update; drag a row to pin a custom order (a click
still opens it). On the
desktop window, drag the title bar to move it; double-click to zoom or restore,
like Finder.

### Projects

Work that comes back — one repository, one report, one recurring chore — belongs
in a project. The title bar prefixes the conversation name with the project's
(`project · title`) so you can see which directory the tools are pointed at.
The project list follows last use, not creation; drag a row to pin it.
**New conversation** at the top of the list is Recents. Hover a project for a
new-conversation control on the row itself — it starts one in that folder.
Click a folder to collapse its topics. Pin a topic from the row menu to keep
it in **Pinned** at the top.
**New project** at the top of the conversation list asks for three
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

The sidebar lists each project's conversations under its name. Click the
folder to collapse it. Pin a topic from the row menu to keep it in **Pinned**
at the top. Conversations that belong to no project sit in **Recents**;
**New conversation** lands there. Skills are behind **View skills** on the
project menu, which opens the **Memory** tab. Notes are editable — **Save notes** appears only after
the draft differs from what is stored — skills can be read and deleted, and
**Review now** (the sparkles on the Memory tab) re-reads the last finished turn.
The panel says when it is reading, and what it decided — including when it kept
nothing. A write that landed is also named in the transcript itself
(`Memory updated: …`), so you do not have to have the tab open to notice. If you were mid-edit when a review wrote, the panel says so
and lets you keep yours or take the new ones — the last save does not silently
win. Correcting a wrong note there matters: it would otherwise be repeated in
every future conversation. Notes are budgeted (`memory.char_limit`, default 2200
characters) because they ride in every prompt — once full, a write that would
grow the notes is refused, even a replace with a longer note. Something
shorter has to land first.

Memory lives in the data directory, never in your working directory, so a project
pointed at a repository leaves nothing in it. A procedure that already lives in
the repository is a file there, not one of these skills. Deleting a project
deletes its conversations and its memory; files in a working directory you chose
are left alone. See [docs/CONFIG.md](docs/CONFIG.md) for the budgets.

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
