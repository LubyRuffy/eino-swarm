# Command line

```
zwai [desktop] [--data-dir DIR] [--mock]
zwai web  [--addr HOST:PORT] [--no-open] [--data-dir DIR] [--mock]
zwai tui  --task "..." [--goal "..."] [--workspace DIR] [--data-dir DIR] [--mock]
zwai trace <turn-id|conversation-id> [--full] [--data-dir DIR]
zwai config [path|init|show] [--data-dir DIR]
zwai version | help
```

`version` also answers to `-v` and `--version`, `help` to `-h` and `--help`.

Run it from a checkout with `go run ./cmd/zwai <subcommand>`, or build once with
`make build` and use `./bin/zwai`.

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

Opens the app in a native window (Wails 3). The window loads a local HTTP server
on a **random loopback port** — a fixed port would collide with whatever else you
run, and the window is told the URL anyway. Closing the window abandons
in-memory runs and closes the database. Unfinished turns stay `running` so the
next start continues them; a user **Stop** is the only path that records
`cancelled`. The live event stream is cancelled immediately so the window does
not freeze; a stuck REST call still has a 5s ceiling.

Desktop is the only mode that can show a file in the platform file manager; the
UI hides that control everywhere else. On macOS the hidden title bar
drags like a native window; double-click zooms or restores it. The traffic
lights are centred in that bar next to the sidebar toggle. Full-page
Settings keeps the same empty strip so **Back to app** is not under the
lights. The Dock (and the
Windows / Linux taskbar) uses the embedded app icon even under `go run`: a
naked binary has no `.app` bundle, so the PNG has to be set at runtime, inset
to Apple's 824/1024 icon grid, and the corners rounded here — macOS will not
apply its squircle or content margin to a loose executable.

## `zwai web`

Serves the same app over HTTP and opens your browser.

| flag | default | meaning |
|---|---|---|
| `--addr HOST:PORT` | the configured `server.addr` (`127.0.0.1:8787`) | listen address. `:0` or `127.0.0.1:0` picks a free port and prints it. |
| `--no-open` | off | do not open a browser. Also honoured: `server.open_browser: false`. |

The URL is printed on startup. `Ctrl-C` shuts down cleanly: in-memory runs stop
and unfinished turns stay `running`, so the next start continues them. A user
**Stop** in the UI is the only path that records `cancelled`.

```bash
zwai web --addr 0.0.0.0:8787   # reachable from your LAN — see the warning below
```

> No authentication and no CORS restrictions apply to a non-loopback bind, and
> the agents run with full access to the machine. Bind to a public interface only
> on a network you control.

## `zwai tui`

One task, one terminal, no UI — the same swarm, the same config, the same
toolset, and the same host snapshot in the prompt (OS, shell, date), so a
task that misbehaves in the app can be reproduced here.

| flag | meaning |
|---|---|
| `--task "..."` | the task. Words after the flags also count as the task, so quoting is optional: `zwai tui summarise the notes`. |
| `--goal "..."` | standing objective for this one-shot run. The manager gets `complete_goal` and `block_goal` and, if it does not call either, the TUI starts another run (up to `swarm.goal_max_auto_turns`) instead of exiting. The app's `/goal` is the same runtime on a saved conversation. |
| `--workspace DIR` | the directory relative tool paths resolve against. Default: a temporary directory that is removed on exit. |

Nothing is written to the database in this mode; use the app when you want the
conversation kept.

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
