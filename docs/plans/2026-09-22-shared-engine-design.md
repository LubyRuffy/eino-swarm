# One engine per data directory

> Product decisions are locked. Desktop, web, TUI, and the phone are clients
> of one engine. The engine outlives the shell that started it.

**Goal:** A conversation started in any shell is in the same database, and a
second shell can attach to it — watch the same event stream and send, steer,
interrupt, and answer — without opening a second engine. Closing the shell
that started the engine does not kill the shells that attached later.

## Why the old split fails

`zwai desktop` / `zwai web` run `App`: one SQLite file, in-memory sequence
numbers, one scheduler, `ResumeOrphanedTurns` on start. `zwai tui` does not.
It builds its own swarm, writes into a scratch directory, and exits with
nothing in the sidebar. `cmd/zwai` claims every shell shares the data
directory. That sentence is only true for desktop and web.

Two `App` processes on one `zwai.db` are also wrong. Sequence numbers live in
the process, and startup resumes leftover turns. A second engine double-fires
schedules and splits the timeline.

Binding the engine to whichever window was opened first is the other failure.
That window is the lock. Close it, and every later shell loses the process
that was actually running the turn.

## Decisions

1. **One engine per data directory.** Exclusive lock
   `$ZWAI_HOME/engine.lock` (released by the kernel when the process dies,
   including `kill -9`). `$ZWAI_HOME/engine.json` records `pid`, `url`,
   `mock`, `started_at`. A live engine answers `GET /api/meta` and its
   `data_dir` is this directory.
2. **Shells are clients.** `desktop`, `web`, and `tui` do not open the
   database when an engine is already live. They use the loopback HTTP API
   the window already uses: list, log, SSE, turns, steer, interrupt, answers.
   Empty `Origin` stays allowed. No CORS.
3. **The engine is not the window.** The first shell starts `zwai engine` as
   its own process (new session, so the parent's exit does not deliver
   `SIGHUP`). Later shells only attach. A second desktop opens a window on
   the existing URL. It does not call `App.New`.
4. **Input stays enabled on every client.** The phone already sends through
   the one engine. TUI and a second window do the same. Two Enters while a
   turn runs still become a follow-up. alt+enter steers the running turn.
   ctrl+x stops it and stays on the screen; ctrl+c only disconnects. The
   first `ask_user` answer wins; the other gets `ask_mismatch`.
5. **Status names the clients, not a writer lock.** `GET /api/meta` lists
   who is connected (`desktop`, `web`, `tui`). The title bar shows that list.
   Nothing in the composer is disabled because another shell is open.
6. **Lifetime.** The engine exits only when all of these have been true for
   the idle grace: no presence connection, the phone hub socket is down, and
   no turn is running. A start grace covers the gap between process start and
   the first shell connecting. A running turn keeps the process up with zero
   windows.
7. **Takeover.** `flock` dies with the process. The next shell finds no live
   `/api/meta`, takes the lock, and `App.New` resumes orphaned turns. A
   wedged process that still holds the lock is reported, not killed.
8. **TUI conversations are threads.** Interactive use and `--task` go through
   `POST /api/threads` and `POST /api/threads/:id/turns`. No scratch
   workspace. `--workspace` is a project workdir on that thread, same as the
   app. `--task` waits until the turn finishes, then the TUI process exits;
   the row stays in Recents.
9. **Mock is part of the lease.** A `--mock` client attaches only to a mock
   engine for that data dir. A mismatch is an error, not a second engine.
10. **Phone counts as a client** via the existing pairlink socket
    (`remote.Host` online), not via presence. Pairing stays off the loopback
    API.

## Out of scope

- A per-thread column that says which shell "owns" the keyboard.
- Two engines sharing `zwai.db`.
- Stealing the lock from a live wedged process.
- Changing phone RPC frames. The phone already talks to whatever process
  hosts the engine; that process is now `zwai engine`.

## Process

```
zwai desktop / web / tui
        │  Discover(engine.json + /api/meta)
        ├─ live ── HTTP + SSE + presence ──────────────┐
        └─ none ── spawn `zwai engine` (setsid) ───┐   │
                                                   ▼   ▼
                                            App + Engine + SQLite
                                            presence connections
                                            phone socket
                                            idle exit
```

`zwai engine` is a real subcommand so the process list and `zwai help` name
it. People still launch `desktop`, `web`, or `tui`. Those commands spawn the
engine when the lease is free.

Closing the desktop window quits the window process only. `OnShutdown` must
not shut the engine down. The macOS "terminate after last window" rule
applies to that window process, not to the engine.

`zwai web` still prints the URL and waits in the foreground. Ctrl-C stops
that waiter. It does not stop the engine while a browser tab, a TUI, a phone,
or a turn is still attached. With nothing left, the idle grace exits the
engine.

## Presence

A client holds `GET /api/presence/:id` open after `POST /api/presence`
`{surface, pid}`. Dropping the connection drops the client. Surfaces are
`desktop`, `web`, and `tui` — the shell, not a product name baked into a
prompt.

`GET /api/meta` gains:

```json
"clients": [{"id": "…", "surface": "tui", "pid": 4242}]
```

The title bar reads the list. One client is quiet. Two or more name each
surface so it is obvious the turn is shared.

## Idle rule

Pure function, so the loop stays a clock:

| clients | phone online | running turns | age < start grace | quiet for idle grace | exit |
|---|---|---|---|---|---|
| >0 | * | * | * | * | no |
| 0 | yes | * | * | * | no |
| 0 | no | >0 | * | * | no |
| 0 | no | 0 | yes | * | no |
| 0 | no | 0 | no | no | no |
| 0 | no | 0 | no | yes | yes |

Start grace is 15s. Idle grace is 3s. Both live next to the function, not in
a config key: they are process lifetime, not a user setting.

## TUI client

The terminal renderer stays. The in-process `Registry.RunWith` loop is no
longer what `zwai tui` executes. The session:

1. Ensure the engine (spawn or attach).
2. Hold a `tui` presence connection.
3. List threads. With no `--task` / `--goal` / `--plan`, open the most recent
   thread or create one. Flags create or continue a thread and start that turn.
4. Paint `GET /log` history, then `GET /events` SSE, into the existing blocks.
5. Enter calls `POST /turns`. A running turn queues a follow-up. Slash
   commands that the engine already understands (`/goal`, `/plan`,
   `/implement`) are sent as the turn text. `/model` and `/reason` patch the
   thread. `/exit` drops presence and leaves. `/clear` is still local display
   unless it maps to an existing engine action; it must not pretend the
   database was wiped.
6. `ask_user` uses the same overlay, and the answer is
   `POST /api/threads/:id/answers`.

SSE `kind` values are the engine's event kinds. Map the ones the transcript
already draws (`user_message`, `reasoning`, `reasoning_delta`, `delta`,
`agent_message`, `tool_call`, `tool_result`, `tool_delta`, `spawned`,
`finished`, `cleanup`). Other kinds stay one-line notices, the way schedule
events already do.

## Takeover after a crash

1. Client reads `engine.json`.
2. Pid is dead, or `/api/meta` fails, or `data_dir` does not match.
3. `flock` is free (the corpse released it).
4. Client spawns `zwai engine`.
5. `App.New` runs `ResumeOrphanedTurns` before serving.
6. The client attaches. The stream blips, then continues from the stored seq.

## Docs and tests

Same change updates `ARCHITECTURE.md`, `docs/CLI.md`, `docs/API.md`,
`docs/DATA_MODEL.md` (on-disk lease files), `README.md` if the launch story
changes, and `CHANGELOG.md`.

Tests that must exist before the behaviour is called done:

- A second process cannot take `engine.lock` while the first holds it.
- `engine.json` plus a live `/api/meta` for the same `data_dir` is "already
  running". A dead pid is not. A mock mismatch is an error.
- The idle table above.
- Launcher exit does not remove a second client's presence or stop a running
  turn. The engine exits only under the idle rule.
- A killed engine is replaced and an orphaned turn is resumed by the new
  process.
- `zwai tui --task` leaves a thread the desktop API can list. It does not
  create a scratch directory.
- The title bar names two surfaces and the composer still submits.
