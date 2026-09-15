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
| Go unit tests | config, store, memory, provider, tools, engine, server, CLI, TUI, and the swarm library | `go test -race -cover ./...` |
| HTTP tests | every endpoint, SSE replay and resume, upload path traversal, restart recovery | `go test ./internal/server/` |
| Front-end unit tests | the event reducer that turns the stream into blocks, and the store's conversation targeting | `cd frontend && npm test` |
| End-to-end | a real browser against a real server: conversation, streaming, sub-agents, files, settings, theme | `cd frontend && npm run e2e` |

Current Go coverage, from `go test -race -cover ./...`:

| package | coverage |
|---|---|
| `internal/memory` | 97.9% |
| `internal/provider` | 96.0% |
| `.` (swarm library) | 95.0% |
| `internal/tools` | 95.1% |
| `internal/store` | 94.8% |
| `internal/engine` | 91.4% |
| `internal/config` | 90.6% |
| `internal/server` | 89.9% |
| `internal/tui` | 91.1% |
| `internal/app` | 87.6% |
| `cmd/zwai` | 83.5% |

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

It answers as a **memory reviewer** too, when the agent asking is the review
after a turn: it stores one note and records one skill, both derived from the
conversation it was handed, then reports in a line. Without that, a `--mock` run
and the E2E suite would exercise projects but quietly skip the whole memory
path — the tools, the files, the event and the panel.

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

The TUI package also covers the same tool-display rule as the desktop UI: an
`exec` notification whose args are JSON is shown as the command, a non-zero
exit becomes a failed block (not a JSON dump), and `web_search` lists hits.

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

Several things are tested here, some as pure logic and some in jsdom:

- **`src/lib/transcript.ts`**, where the stream becomes UI: streamed text
  replaces rather than appends, a completed block folds into the streamed one
  instead of duplicating it, a tool call pairs with its result by
  `tool_call_id`, a progress pulse updates the live summary without leaving
  a row in the timeline or reviving an agent that has already finished, and
  `collapseLiveEvents` keeps only the latest snapshot per agent and kind from
  a burst of deltas — lossless, because a delta carries the accumulated string.
  It is a pure function, so it is called directly.
- **`src/store/app.ts`**, against a fake API: which conversation an action lands
  in. Sending while a conversation is still being created must wait for it, or
  the turn runs in the conversation the user just left — invisibly.
- **`src/lib/api.ts`**, against a stubbed `fetch`: the server's error message and
  code reach the UI instead of a bare status line, and a tool list that arrives
  as `null` is read as an empty list rather than crashing the settings dialog.
- **`src/lib/marquee.ts`** and **`src/components/app/marquee.tsx`**: overflow is
  a width comparison (jsdom cannot layout a real line), a longer line gets a
  longer loop, and a live `MarqueeText` is marked `data-marquee="shimmer"` while
  idle text stays `"off"`.
- **`src/components/app/transcript.tsx`**, rendered in jsdom: while `wait_agents`
  is pending the transcript shows the sub-agents it is waiting on, each one's
  live activity and the age the latest pulse gave it — the roll-up that keeps a
  running swarm from looking frozen — and clicking a row opens that agent. The
  heartbeat line is tested separately: it reports the turn's age and the number
  of sub-agents still working, keeps ticking on fake timers between pulses, and
  renders nothing before the first pulse or after the turn ends. The id parser
  behind the roll-up is also unit tested for half-streamed and malformed
  arguments.
- **`src/components/app/composer.tsx`**, rendered in jsdom: the thinking-level
  menu labels the empty default as "Default" and a set level by name, and it is
  absent when the server offers no levels, so an older server never draws a
  control that would send a meaningless choice. The input is owned by the
  composer, not the app shell, so typing cannot re-render the transcript.
- **`src/lib/tool-view.ts`**: a built-in tool's JSON args collapse to the
  command / query / path the user needs to see, `exec` payloads become stdout
  plus a failed flag when the exit code is not 0, and `web_search` payloads
  become a list of hits. Assertions check structure, not any particular query.
- **`src/lib/read-result.ts`**, a `read` tool payload becomes a file listing:
  newline-separated bodies, and the older flattened one-liners, both recover
  the path and the lines; other tool output is left alone.
- **`src/components/app/tool-result.tsx`**, rendered in jsdom: a markdown `read`
  renders headings, a non-markdown `read` keeps line numbers, `exec` shows
  stdout (and a role=alert error when it failed), and `web_search` lists hits
  instead of dumping JSON.
- **`src/components/app/sidebar.tsx`**, rendered in jsdom: desktop macOS chrome
  pads for the traffic lights and does not contain New conversation; the hide
  toggle calls `onCollapse`. A browser does not pad.
- **`src/components/app/header.tsx`**, rendered in jsdom: when the conversation
  list is hidden a Show conversations control appears, the title bar pads
  for traffic lights if they now sit on it, and a conversation in a project is
  named after it — which directory the tools are pointed at is otherwise
  invisible.
- **`src/lib/stream.ts`**, against a fake `EventSource`: every kind the server
  sends is subscribed to, a review that lands after `done` is delivered, the
  event name wins over a disagreeing payload, and a connection that dropped on
  its own is not reported as a failure. The first of those is a regression test:
  a kind missing from the list is stored, traceable, and invisible until the
  page is reloaded.
- **`src/store/projects.ts`**, against a fake API: a project deleted elsewhere
  stops being the sidebar's filter, a slow memory response for a project that is
  no longer open is ignored (one project's notes under another's name is worse
  than none), a refused create or edit reaches the dialog so it can show the
  message against the field that caused it, and a memory reload copies the skill
  index onto the project so the sidebar list updates without a second listing.
- **`src/components/app/project-dialog.tsx`**, rendered in jsdom: a rejected
  working directory is shown under that field and the dialog stays open, a
  nameless project cannot be created, and the memory switch is disabled when the
  install has memory off.
- **`src/components/app/project-list.tsx`** and
  **`src/components/app/delete-project-dialog.tsx`**: the selected project is
  marked as pressed, each row's menu is named after its project, skills recorded
  for a project are listed under it (capped; the rest is a pointer at Memory),
  and the delete dialog says that the conversations and the memory go too while
  the user's own directory does not.
- **`src/components/app/memory-panel.tsx`**, rendered in jsdom: notes and their
  budget are shown, an edit saves and a failed save stays on screen, a skill's
  body is fetched only when it is opened, a skill named by the sidebar is opened
  and fetched on arrival, notes that arrive from a review are adopted unless the
  user is mid-edit — in which case a conflict banner keeps what they typed and
  offers Reload — and a write that landed while the tab was closed is a badge,
  not a silent panel.

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
| `e2e/conversation.spec.ts` | a full swarm turn, a live status line marked as sweeping while the turn runs, context carried across turns, file upload appearing in the Files panel, the turn id shown for tracing, and the manager tool-round cap pausing for Continue/Stop instead of dumping eino's iteration error |
| `e2e/projects.spec.ts` | a project created from the sidebar, a conversation that lands in it and says so, the review named in the transcript without opening a tab, the review's skill appearing under the project in the sidebar, clicking it opening the Memory tab with that skill expanded, the notes in the panel without a reload, the review in the same trace as the turn, a second conversation starting with the first one's memory, a hand-edited note surviving a reload, and a deleted project taking its conversations with it |
| `e2e/shell.spec.ts` | keyboard shortcuts (including hiding the conversation list), the tool catalogue on a never-saved config, settings written to the config file and read back, theme switching persisted, renaming and deleting a conversation |

E2E tests run against `frontend/dist`, so **run `make frontend` after changing
anything under `frontend/src`** or you will be testing the previous bundle.

The progress pulse has no positive E2E assertion on purpose: a turn on the
scripted provider finishes in about 3.6 seconds, under the 5-second default
interval, so a test that waited for a pulse would pass or fail on timing. The
suite asserts instead that a finished turn leaves no pulse behind — the
regression that actually bites is an unhandled event kind landing in the
timeline as a raw row, or the heartbeat outliving the answer. That pulses reach
a client at all is verified in the engine and reducer tests.

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
