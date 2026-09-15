# Testing

```bash
make test        # go test -race -cover ./...  +  front-end unit tests
make e2e         # Playwright, on the scripted offline provider
make check       # go vet + both of the above
```

No test needs a model endpoint, an API key or a network. Anything that would
otherwise call a model runs on the **scripted offline provider**, so the suite is
deterministic and fast enough to run on every change.

## Layers

| layer | what it covers | command |
|---|---|---|
| Go unit tests | config, store, provider, tools, engine, server, CLI, TUI, and the swarm library | `go test -race -cover ./...` |
| HTTP tests | every endpoint, SSE replay and resume, upload path traversal, restart recovery | `go test ./internal/server/` |
| Front-end unit tests | the event reducer that turns the stream into blocks, and the store's conversation targeting | `cd frontend && npm test` |
| End-to-end | a real browser against a real server: conversation, streaming, sub-agents, files, settings, theme | `cd frontend && npm run e2e` |

Current Go coverage, from `go test -race -cover ./...`:

| package | coverage |
|---|---|
| `internal/provider` | 95.9% |
| `.` (swarm library) | 95.3% |
| `internal/tools` | 95.1% |
| `internal/store` | 94.3% |
| `internal/engine` | 89.6% |
| `internal/config` | 89.0% |
| `internal/server` | 88.6% |
| `internal/tui` | 87.7% |
| `internal/app` | 87.6% |
| `cmd/zwai` | 82.6% |

What is deliberately not unit-tested: `main`, `runDesktop`/`runWeb`/`runTUI` (thin
wrappers around functions that *are* tested), `internal/desktop` (opens a native
window) and `tui.Run` (drives a real terminal). Each of those was split so that
everything which can fail is testable and only the untestable shell is left:
`startDesktopServer` returns the window options and the live server without
opening a window, `serveWeb` takes an injected stop signal, and `buildTUISwarm`
assembles the swarm without a terminal. The desktop window itself is verified by
hand — see below.

## The scripted offline provider

`--mock` (or `provider.NewMock`) replaces the model with a script that behaves
like a real swarm run: it thinks, spawns two sub-agents (one with
`fork_context`), has a worker call `write` to produce a file in the workspace,
waits for both, then streams a markdown answer.

```bash
go run ./cmd/zwai web --mock --no-open --data-dir /tmp/zwai-demo
```

Its output is derived from the input, so tests can assert on what they sent
without hardcoding a script. **It must stay generic**: no example task, filename
or domain word from a user request belongs in the script, the manager prompt, or
any test expectation.

## Go tests

```bash
go test -race -cover ./...                    # everything
go test -race ./internal/engine/              # one package
go test -race -run TestSteer ./internal/engine/
go test ./internal/server/ -coverprofile=/tmp/c.out && go tool cover -html=/tmp/c.out
```

`-race` is not optional here. The engine runs a swarm, an event bus and an HTTP
server concurrently, and the interesting bugs in it have all been ordering bugs:
a subscriber that misses the "done" event, a runtime that is still marked busy
when the next turn starts, a listener read while another goroutine binds it.

Conventions in these tests:

- Every test gets its own data directory (`t.TempDir()`) and clears the
  `OPENAI_*` variables, so a developer's environment cannot change the outcome.
- Test names say what would break for the user, not which function they call:
  `TestStartTurnIsAcceptedNotAwaited`, `TestSlowSubscriberDoesNotBlockTheRun`.
- Comments in tests explain *why the behaviour matters*, because that is what a
  later reader needs to decide whether a failing assertion is a regression or an
  obsolete expectation.

## Front-end unit tests

```bash
cd frontend
npm test          # vitest, once
npm run test:watch
```

Three things are tested here, all without the DOM:

- **`src/lib/transcript.ts`**, where the stream becomes UI: streamed text
  replaces rather than appends, a completed block folds into the streamed one
  instead of duplicating it, and a tool call pairs with its result by
  `tool_call_id`. It is a pure function, so it is called directly.
- **`src/store/app.ts`**, against a fake API: which conversation an action lands
  in. Sending while a conversation is still being created must wait for it, or
  the turn runs in the conversation the user just left — invisibly.
- **`src/lib/api.ts`**, against a stubbed `fetch`: the server's error message and
  code reach the UI instead of a bare status line, and a tool list that arrives
  as `null` is read as an empty list rather than crashing the settings dialog.

## End-to-end tests

```bash
cd frontend
npx playwright install chromium   # once
npm run e2e
npx playwright test --headed      # watch it happen
npx playwright test e2e/shell.spec.ts -g "settings"
```

Playwright starts the real server itself (`go run ./cmd/zwai web --mock --no-open`),
so a run exercises the whole stack: gin, the engine, SQLite, SSE and the built
front end.

**The data directory is deleted before the server starts**, so every run is a
first install. This matters: a config file written by an earlier run has already
been normalized, which once hid a crash that only happened on a fresh one. The
wipe is part of the `webServer.command` rather than a `globalSetup` hook, because
Playwright starts the web server first and a hook would delete the directory the
server had already opened. For the same reason the suite does not reuse a server
that is already listening on the port.

| spec | covers |
|---|---|
| `e2e/conversation.spec.ts` | a full swarm turn, context carried across turns, file upload appearing in the Files panel, the turn id shown for tracing |
| `e2e/shell.spec.ts` | keyboard shortcuts, the tool catalogue on a never-saved config, settings written to the config file and read back, theme switching persisted, renaming and deleting a conversation |

E2E tests run against `frontend/dist`, so **run `make frontend` after changing
anything under `frontend/src`** or you will be testing the previous bundle.

## Manual checks that no automated test replaces

- **The desktop window**: `go run ./cmd/zwai desktop`. Check that the window
  opens, the UI loads, a turn runs, an upload opens a native file dialog, and
  "Show in Finder" works. Wails cannot be driven by these tests.
- **A real model**: the offline provider proves the plumbing, not that a prompt
  works. Run one real conversation before shipping a change to the manager
  prompt or the toolset.
- **A UI change**: look at it, in both light and dark, before and while a turn
  runs. The panel, the transcript and the composer all change shape mid-turn.

## When a turn misbehaves

Reproduce it, then read it:

```bash
zwai trace <turn-id>          # the timeline plus every model call
zwai trace <turn-id> --full    # untruncated text
zwai tui --task "..."          # the same swarm, no UI in the way
```

Raise `log.level` to `debug` in the config for one line per HTTP request and per
event-stream decision. If a turn is slow, the model-call table at the end of
`zwai trace` usually shows why: `input_msgs` growing, or one call eating the wall
clock.

## Adding tests

- New Go code needs unit tests with it, in the same package, above 90% of the new
  statements. If a function cannot be tested, split it until the untestable part
  is a two-line wrapper.
- A new endpoint needs a test for its success path **and** its refusals — a `404`
  for an unknown conversation, a `409` when busy, a rejected path traversal.
- A new event kind needs a reducer test, and usually an assertion in
  `TestNotifyKindsAreStableAcrossTheWire`: the names travel over the network and
  are stored in the database, so renaming one breaks replay of old conversations.
- A new user-visible flow needs an E2E test. If it cannot be driven in the
  browser, it probably cannot be driven by a user either.
