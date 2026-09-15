# Command line

```
zwai [desktop] [--data-dir DIR] [--mock]
zwai web  [--addr HOST:PORT] [--no-open] [--data-dir DIR] [--mock]
zwai tui  --task "..." [--workspace DIR] [--data-dir DIR] [--mock]
zwai trace <turn-id|conversation-id> [--full] [--data-dir DIR]
zwai config [path|init|show] [--data-dir DIR]
zwai version | help
```

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
run, and the window is told the URL anyway. Closing the window cancels running
turns and closes the database, with a 5s grace period for in-flight requests.

Desktop is the only mode that can show a file in the platform file manager; the
UI hides that control everywhere else.

## `zwai web`

Serves the same app over HTTP and opens your browser.

| flag | default | meaning |
|---|---|---|
| `--addr HOST:PORT` | the configured `server.addr` (`127.0.0.1:8787`) | listen address. `:0` or `127.0.0.1:0` picks a free port and prints it. |
| `--no-open` | off | do not open a browser. Also honoured: `server.open_browser: false`. |

The URL is printed on startup. `Ctrl-C` shuts down cleanly: running turns are
cancelled and recorded as `cancelled`, so the next start does not show
conversations frozen mid-answer.

```bash
zwai web --addr 0.0.0.0:8787   # reachable from your LAN — see the warning below
```

> No authentication and no CORS restrictions apply to a non-loopback bind, and
> the agents run with full access to the machine. Bind to a public interface only
> on a network you control.

## `zwai tui`

One task, one terminal, no UI — the same swarm, the same config, the same
toolset, so a task that misbehaves in the app can be reproduced here.

| flag | meaning |
|---|---|
| `--task "..."` | the task. Words after the flags also count as the task, so quoting is optional: `zwai tui summarise the notes`. |
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
  started  2026-09-15T11:31:00+08:00
  took     12.48s
  asked    go through the three files and pull out every deadline

  timeline (42 events)
        0s  user_message   manager            go through the three files…
      120ms reasoning      manager            three files, one table…
      480ms tool_call      manager            ls(uploads)
      512ms tool_result    manager            uploads/a.md uploads/b.md…
      1.1s  spawned        researcher-1       researcher
      …

  model calls (7)
    manager            some-model             6 msgs  in   4210 ch  out 880 ch   2210ms
    researcher-1       some-model             4 msgs  in   2100 ch  out 640 ch   1980ms
    …
    total              9.8s
```

| flag | meaning |
|---|---|
| `--full` | print event text in full instead of one line each. Use it for a stack trace or a truncated answer. |

Given a **conversation** id it prints every turn of that conversation in order.
Given an unknown id it fails with a message naming the data directory it looked
in — usually the sign that you meant to pass `--data-dir`.

The same data is available in the UI's Trace tab and over HTTP at
`GET /api/trace/:turn`.

## `zwai config`

| subcommand | does |
|---|---|
| `path` | print the config file's path, creating a default file if there is none |
| `init` | create the file and say what to do next if no model is configured |
| `show` | print the effective configuration, **with the API key redacted** |

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
