# Command line

```
zwai [desktop] [--data-dir DIR] [--mock]
zwai web  [--addr HOST:PORT] [--no-open] [--data-dir DIR] [--mock]
zwai engine [--addr HOST:PORT] [--data-dir DIR] [--mock]
zwai tui  [--task "..."] [--goal "..."] [--plan "..."] [--model NAME] [--reasoning LEVEL] [--workspace DIR] [--data-dir DIR] [--mock]
zwai trace <turn-id|conversation-id> [--full] [--data-dir DIR]
zwai config [path|init|show] [--data-dir DIR]
zwai version | help
```

`version` also answers to `-v` and `--version`, `help` to `-h` and `--help`.

Run it from a checkout with `go run ./cmd/zwai <subcommand>`, or build once with
`make build` and use `./bin/zwai`. From a checkout, `desktop` and `web` rebuild
`frontend/dist` when the TypeScript sources changed (needs Node on the first
run, or after a UI edit). A copied binary with no checkout serves the bundle
embedded at `go build` time.

Three conveniences worth knowing:

- **No subcommand means `desktop`**, so a launcher entry needs no arguments —
  and a flag on its own counts as no subcommand, so `zwai --mock` and
  `zwai --data-dir /tmp/demo` open the app too.
- **Flags may come after the positional argument.** `zwai trace tn_ab12 --full`
  works, even though Go's flag package normally stops at the first non-flag word.
- **Exit codes**: `0` success, `1` the command failed, `2` you typed it wrong.

## Common flags

| flag | applies to | meaning |
|---|---|---|
| `--data-dir DIR` | all | use another data directory instead of `$ZWAI_HOME` / `~/.zwai-swarm`. Config, database and workspaces all live there, so this gives you a completely separate instance. |
| `--mock` | `desktop`, `web`, `tui` | run on the scripted offline provider: no network, no API key. Spawns two sub-agents, calls tools, writes a file. Use it to try the UI, to reproduce a UI bug, or in tests. |

## `zwai desktop`

Opens the app in a native window (Wails 3). The window loads the data
directory's engine on a **random loopback port** — a fixed port would collide
with whatever else you run, and the window is told the URL anyway
(`?shell=desktop`, so the traffic lights belong to this window). Closing the
window disconnects it. It does not stop the engine, and it does not mark a
turn `cancelled`. The engine exits on its own after a few seconds once no
shell is connected, the phone hub is down, and nothing is running. A turn
that is still going keeps the process up. A user **Stop** is the only path
that records `cancelled`. If the engine process is killed, the next shell
takes the lock and resumes orphaned turns.

Opening a **different build** (another version, or a binary that replaced
the file on disk) waits until no conversation is in a model call, then
stops that engine and starts one from the binary you just launched. Every
10s it prints a check while a turn is still in that call. At 30s, and again
every 30s after the answer, a terminal asks whether to force-stop; `y`,
`yes`, or `是` kills the old process and switches. Anything else keeps
waiting. If that process exits during the wait, this shell starts its own.
With no terminal it does not force. A question waiting on the human, and a schedule
that has not fired, do not count as that call. A second window of the same
build still attaches.

A second `zwai desktop` on the same data directory opens another window on
the same engine. Both can type. The app menu (the `…` at the bottom-left of
the conversation list) names the connected shells when more than one is attached.

A checkout with no `frontend/dist/index.html` (or with TypeScript newer than
the last build) runs `npm install` then `npm run build` before the window
opens, against `registry.npmjs.org` (the project `.npmrc`, not a user-level
mirror). Ctrl-C during that first install stops it.

A loopback engine can show a file in the platform file manager and open
http(s) links in the system browser. A non-loopback bind cannot; the UI
hides those controls. On macOS the hidden title bar
drags like a native window; double-click zooms or restores it. The traffic
lights are centred in that bar next to the sidebar toggle. Full-page
Settings keeps the same empty strip so **Back to app** is not under the
lights. The Dock (and the
Windows / Linux taskbar) uses the embedded app icon even under `go run`: a
naked binary has no `.app` bundle, so the PNG has to be set at runtime, inset
to Apple's 824/1024 icon grid, and the corners rounded here — macOS will not
apply its squircle or content margin to a loose executable.
On macOS, `desktop` copies itself into `~/Library/Caches/zwai/zwai.app` and
re-execs before opening the window. Sequoia+ Local Network privacy keys off a
bundle id; a `go run` binary is `a.out` with no Info.plist, so listing models
on a LAN endpoint fails with `no route to host` while Terminal `curl` works.
The first launch may prompt to allow local network access.

## `zwai web`

Serves the same app over HTTP and opens your browser.

| flag | default | meaning |
|---|---|---|
| `--addr HOST:PORT` | the configured `server.addr` (`127.0.0.1:8787`) | listen address. `:0` or `127.0.0.1:0` picks a free port and prints it. |
| `--no-open` | off | do not open a browser. Also honoured: `server.open_browser: false`. |

The URL is printed on startup. `Ctrl-C` stops this waiter. It does not stop
the engine while another shell, the phone, or a turn is still using it.
Unfinished turns are not marked `cancelled`. A user **Stop** in the UI is the
only path that records `cancelled`.

`--mock` attaches only to a mock engine. A live non-mock engine on the same
data directory is an error, not a second process.

```bash
zwai web --addr 0.0.0.0:8787   # reachable from your LAN — see the warning below
```

> No authentication and no CORS restrictions apply to a non-loopback bind, and
> the agents run with full access to the machine. Bind to a public interface only
> on a network you control.

## `zwai engine`

The process that owns one data directory. You do not have to start it:
`desktop`, `web`, and `tui` spawn it when `engine.json` is missing or the
pid is dead, then attach. A second spawn attaches to the URL already
published. The child is in its own session, so the shell that started it
can exit.

| flag | default | meaning |
|---|---|---|
| `--addr HOST:PORT` | `127.0.0.1:0` | listen address. `:0` picks a free port and writes it to `engine.json`. |
| `--mock` | off | this lease is the scripted offline provider. A client with the other setting gets an error instead of a second engine. |

The process exits after a short idle grace when three things are true
together: no shell holds a presence connection, the phone hub socket is
down, and no turn is running. A reserve that never connects does not count.
Killing the process releases the lock. A process that still holds the lock
but does not answer `/api/meta` is reported; it is not killed.

## `zwai tui`

The terminal is a client of the same engine as the window. It reads and
writes the same conversations. Launch it with no task and it stays open at
a composer on the latest conversation: type a task, Enter sends, the
transcript stays, the next Enter while a turn is running becomes a
follow-up, alt+enter injects into the running turn, and ctrl+x stops
that turn without leaving the screen. An `ask_user` that is waiting still
accepts a typed answer.
The idle composer parks
the real terminal cursor at the insert point so CJK IME preedit follows the
committed text (a painted block caret left the hardware cursor at column 0,
which is where the IME attached). Type `/` for a Codex-style command popup
(`/goal`, `/plan`, `/model`, `/reason`, `/clear`, `/help`, `/exit`; `/quit` appears once you
type it). `/goal`, `/plan` and `/implement` are sent to the engine.
`/model NAME` and `/reason LEVEL` patch that conversation.
`/clear` only clears this screen. `ctrl+c` leaves the terminal; it does
not stop a turn the engine is still running. `ctrl+x` does. `--task` stores a turn and
this process can exit when that turn settles; the row stays in Recents.
`--goal` alone sends `/goal`. `--goal` with a separate task sets the
objective, then sends the task. `--plan` alone sends `/plan`. `--plan`
with a task turns planning on, then sends the task. `--workspace` is a
project working directory, not a scratch folder.

| flag | meaning |
|---|---|
| `--task "..."` | run this task immediately. The process can exit when the turn settles. Words after the flags also count as the task, so quoting is optional: `zwai tui summarise the notes`. Omit it to wait at the composer (`ctrl+c` leaves), unless `--goal` or `--plan` is set. |
| `--goal "..."` | standing objective on the conversation. Omit `--task` and the terminal sends `/goal` with this text and keeps the composer. Pass `--task` (or leftover words) when the first line should differ from the objective; the objective is patched first. |
| `--plan "..."` | enter planning. Omit `--task` and the text is sent as `/plan`. With `--task`, planning is turned on and the task is the first user line. |
| `--model NAME` | model name patched onto the conversation before the first line. Default: the provider's configured model. |
| `--reasoning LEVEL` | thinking level patched onto the conversation: empty/`default`, `low`, `medium`, or `high`. Empty sends no `reasoning_effort`. |
| `--workspace DIR` | project working directory for this conversation. Omit it and a new conversation uses the engine's usual workspace. An interactive attach with no task reuses the latest conversation and does not create a project. |

## `zwai trace`

The one-id troubleshooting path. Copy a turn id from the UI header, or a
conversation id, and get everything back:

```bash
zwai trace tn_3c6d0df94ee9e287
```

```
turn tn_3c6d0df94ee9e287  conversation th_cc107f8e87d220b5
  status   done
  model    some-model via default
  thinking high
  started  2026-09-15T11:31:00+08:00
  took     12.48s
  asked    go through the three files and pull out every deadline

  timeline (42 events)
        0s  user_message   manager            go through the three files…
      120ms reasoning      manager            three files, one table…
      480ms tool_call      manager            ls(uploads)
      512ms tool_result    manager            uploads/a.md uploads/b.md…
      1.1s  spawned        researcher-1       ## Environment  ⏎ The tools run on this machine…
      …

  model calls (7)
    manager            some-model             6 msgs  in   4210 ch  out 880 ch   2210ms  90/12 tok  cache 20
    researcher-1       some-model             4 msgs  in   2100 ch  out 640 ch   1980ms  40/8 tok
    …
    total              9.8s
```

| flag | meaning |
|---|---|
| `--full` | print event text in full instead of one line each. Use it for a stack trace or a truncated answer. |

A turn in a project with memory on ends with the review that followed it: a
`memory_review` event saying what was kept, and the reviewer's own model calls
under the agent `memory-reviewer`. The review is part of the turn, so the same
id explains both what the swarm answered and what it wrote down.

Given a **conversation** id it prints every turn of that conversation in order.
Given an unknown id it fails with a message naming the data directory it looked
in — usually the sign that you meant to pass `--data-dir`.

The same data is available over HTTP at `GET /api/trace/:turn`. The UI Trace
tab shows that turn's status, error and billed tokens; **Full log** is the
on-screen timeline, folded by default so a long run is not a sidebar syslog.

## `zwai config`

| subcommand | does |
|---|---|
| `path` | print the config file's path, creating a default file if there is none |
| `init` | create the file and say what to do next if no model is configured |
| `show` | print the effective configuration, **with the API key redacted**. The default, so `zwai config` on its own shows it. |

`show` is what to paste into a bug report: a key that is set prints as
`<set, hidden>`, while a key that is genuinely empty prints as `""`, so the
output still tells you which one it is.

```bash
zwai config path                     # ~/.zwai-swarm/config.yaml
zwai config show --data-dir /tmp/x   # a throwaway instance's settings
```

See [CONFIG.md](CONFIG.md) for what the keys mean.

## Environment

| variable | effect |
|---|---|
| `ZWAI_HOME` | data directory, unless `--data-dir` overrides it |
| `ZWAI_MODEL_BASE_URL`, `OPENAI_BASE_URL` | seed the default provider's endpoint on first run |
| `ZWAI_MODEL_API_KEY`, `OPENAI_API_KEY` | seed its API key |
| `ZWAI_MODEL_NAME`, `OPENAI_MODEL` | seed its model name |

The `ZWAI_*` names win over the `OPENAI_*` ones. Seeding only fills fields that
are **blank**, so an environment variable can never silently override what you
configured in Settings.
