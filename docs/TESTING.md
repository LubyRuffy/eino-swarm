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
| HTTP tests | every endpoint, SSE replay and resume, upload path traversal, restart recovery (leftover turns continue) | `go test ./internal/server/` |
| Front-end unit tests | the event reducer that turns the stream into blocks, the store's conversation targeting, quoting selected transcript text into the composer, clipboard image paste, file drop onto the composer, find-in-conversation matching (count vs a paint window so a live turn does not freeze), http(s) links leaving the window, sidebar drag order (title drag after 8px, first click still opens), chrome i18n (`en`/`zh` key parity, locale persist through settings) | `cd frontend && npm test` |
| End-to-end | a real browser against a real server: conversation, streaming, sub-agents, files, settings, theme, chrome language | `cd frontend && npm run e2e` |

Current Go coverage, from `go test -race -cover ./...`:

| package | coverage |
|---|---|
| `internal/memory` | 97.9% |
| `internal/provider` | 91.6% |
| `.` (swarm library) | 95.0% |
| `internal/tools` | 95.1% |
| `internal/store` | 92.0% |
| `internal/engine` | 93.6% |
| `internal/config` | 92.0% |
| `internal/server` | 89.6% |
| `internal/tui` | 91.1% |
| `internal/app` | 87.6% |
| `cmd/zwai` | 83.5% |

What is deliberately not unit-tested: `main`, `runDesktop`/`runWeb`/`runTUI` (thin
wrappers around functions that *are* tested), `desktop.Run` (opens a native
window) and `tui.Run` (drives a real terminal; `runConfig` is the testable
payload). The rest of `internal/desktop`
is tested: traffic-light geometry, hopping AppKit geometry onto the main
thread (Wails delivers window events off-thread), the quit-time
`Window #1 not found` filter, and that the Dock icon is a real PNG with
transparent rounded corners on Apple's 824/1024 icon grid, wired into
`application.Options` (the window itself still cannot be opened in a unit
test). Each untestable shell was
split so that everything which can fail is testable: `startDesktopServer`
returns the window options and the live server without opening a window,
`serveWeb` takes an injected stop signal, and `buildTUISwarm` assembles the
swarm without a terminal. The window itself is verified by hand — see below.

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

It also answers as a **conversation namer** (`title-namer`): a short label
derived from the request, shorter than the placeholder, so `--mock` and the
tests can tell a generated name from a quoted first message.

It answers as a **compact summarizer** (`compact-summarizer`) with a short
briefing derived from the messages it was handed. And when the manager prompt
still has an open standing objective, it calls `complete_goal` so a `--mock`
run does not auto-continue until the cap. Tests that need to observe
auto-continue call `provider.SetCompleteOpenGoal(false)`.

```bash
go run ./cmd/zwai web --mock --no-open --data-dir /tmp/zwai-demo
```

Its output is derived from the input, so tests can assert on what they sent
without hardcoding a script. **It must stay generic**: no example task, filename
or domain word from a user request belongs in the script, the manager prompt, or
any test expectation. The host snapshot in the prompt is live (`runtime.GOOS`,
today's date); tests assert those values, never a sample OS or a sample command.

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
`TestShutdownDoesNotWaitForTheEventStream` is why ⌘Q does not freeze the
window: the EventSource is cancelled instead of waited out for five seconds.
`TestListWorkspaceDoesNotHideSiblingsBehindAFatDirectory` is why the Files
panel still shows later siblings when an early directory would otherwise spend
the 2000-entry cap.

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

`src/test/setup.ts` replaces `localStorage` with a Map. Node 25's global
`localStorage` is not Web Storage and it shadows jsdom's; chrome preferences
(theme, conversation-list width) would otherwise silently no-op in unit
tests. The Map is cleared after every test.

Several things are tested here, some as pure logic and some in jsdom:

- **`src/lib/transcript.ts`**, where the stream becomes UI: streamed text
  replaces rather than appends, a completed block folds into the streamed one
  instead of duplicating it, a tool call pairs with its result by
  `tool_call_id`, a progress pulse updates the live summary without leaving
  a row in the timeline or reviving an agent that has already finished, a
  `usage` pulse is ignored by the reducer (the composer meter owns it) and
  `collapseLiveEvents` keeps only the latest usage snapshot in a burst, a
  `max_iterations` event becomes a confirm card (and `max_iterations_continued`
  resolves it) rather than an unknown notice, an interrupt (or any terminal
  `done`/`error`, or a worker `finished`) closes tools left pending so they
  stop spinning, and
  `collapseLiveEvents` keeps only the latest snapshot per agent and kind from
  a burst of deltas — lossless, because a delta carries the accumulated string.
  `splitQueuedSteers` pulls unread steering out of the turn body while a turn
  is running (a later model round on the same turn consumes it; a previous
  turn's steer stays put). A `rewound` event (and `rewindTranscript`) drops
  that `user_message` seq and everything after it without lowering `lastSeq`.
  `placePendingEdit` puts the edited text back at the cut so the bubble stays
  while the replacement `user_message` is in flight. Both are pure functions,
  called directly.
- **`src/lib/turn-nav.ts`** and **`src/components/app/turn-nav.tsx`**: user
  turns become jump targets (steering does not), ticks pack into a compact
  cluster in the middle of the pane rather than stretching it,   the active tick
  is the last message whose top has crossed a probe near the viewport, or the
  latest turn when the scroller is at the bottom (composer padding would
  otherwise leave the last row below that probe), and a
  missing id after a conversation switch is a no-op. The rail stays hidden until
  there are two user turns; hover opens the list; a click (or arrow keys) jumps.
- **`src/store/app.ts`**, against a fake API: which conversation an action lands
  in when New conversation is clicked twice, when a send races a slow create,
  and when a file is dropped on the empty state; sending while a conversation is
  still being created must wait for it, or the turn runs in the conversation the
  user just left; Enter while a turn is running queues a follow-up instead of
  steering, ⌘Enter / `{steer:true}` injects now, `{fromEventSeq}` starts a turn
  (never a follow-up) after clearing everything below that bubble and leaving
  the edited text in place, and an idle enqueue falls
  through to starting a turn; a title event renaming the open conversation (and a failed
  namer leaving the placeholder); `/goal` pursuing until `complete_goal` or
  `block_goal` (edit and resume from the banner); `/compact` landing on the open
  conversation, with the hint hidden at 0% (and a compact with nothing open being a no-op); `refreshCatalogs` rediscovering every
  endpoint with a URL without rebooting the open conversation; and a live
  `usage` event filling the composer snapshot (reload reads it from GET).
- **`src/lib/usage.ts`** and **`src/components/app/context-meter.tsx`**: compact
  Cursor counts (`71.3K`), a percentage that is undefined without a window
  rather than a fake 0%, a saturating arc so an unknown window is not a dead
  empty circle, Discover merging per-name windows, selecting a listed name
  without stealing a sibling's window, and the ring's accessible name.
- **`src/lib/api.ts`**, against a stubbed `fetch`: the server's error message and
  code reach the UI instead of a bare status line, a tool list that arrives
  as `null` is read as an empty list rather than crashing the settings dialog,
  and `POST /compact` / `PATCH goal` hit the right paths.
- **`src/lib/marquee.ts`** and **`src/components/app/marquee.tsx`**: overflow is
  a width comparison (jsdom cannot layout a real line), a longer line gets a
  longer loop, and a live `MarqueeText` is marked `data-marquee="shimmer"` while
  idle text stays `"off"`.
- **`src/lib/stream-markdown.ts`**: trailing `**` / `` ` `` / `~~` in the
  current prose region are closed so a streaming answer can render; an open
  fence is left alone (it is already a code block), and a marker with no
  content yet is not turned into `****`.
- **`src/components/app/markdown.tsx`**, rendered in jsdom: a streaming
  `## heading` is a heading, trailing bold renders as `<strong>`, and a
  finished answer is not rewritten.
- **`src/components/app/transcript.tsx`**, rendered in jsdom: while `wait_agents`
  is pending the transcript shows the sub-agents it is waiting on, each one's
  live activity and the age the latest pulse gave it — the roll-up that keeps a
  running swarm from looking frozen — and clicking a row opens that agent. The
  heartbeat line is tested separately: it reports the turn's age and the number
  of sub-agents still working, keeps ticking on fake timers between pulses, and
  renders nothing before the first pulse or after the turn ends. The id parser
  behind the roll-up is also unit tested for half-streamed and malformed
  arguments. A streaming answer (manager transcript and a worker's panel) is a
  heading, not raw hashes. A live thought is a 10-line scrolling box that
  follows new tokens and fades at the top once earlier lines have left; the
  **Thinking** label sweeps (`MarqueeText`) until it collapses to **Thought**.
  Clicking the row hides it while tokens still arrive; later deltas do not
  force it back open.   Opening an `exec` row wraps the full command with shell
  highlighting instead of leaving it truncated (`transcript-exec.test.tsx`).
  A refused `memory` write shows the refusal on the collapsed row, not only
  inside the disclosure. An exec still open when the turn is interrupted loses its spinner rather
  than running forever. Copy and a pencil sit under each user bubble (copy
  is hidden when there is no text); edit opens the bubble in place and Send
  restarts from that `user_message` seq, clearing everything below
  (`transcript-user.test.tsx`).
  Each turn group carries `data-turn-nav` so the rail can jump; one turn
  keeps the rail hidden, two turns show it. A live thought's top fade is
  trapped in its own stacking context so it cannot cover the rail. Unread steering is pinned below
  the heartbeat (`queued-steers`), not above the working line; a steer the
  manager has already read stays in the turn body. Auto-follow unpins on
  wheel-up even inside the old 80px slack; new tokens then show a jump-to-latest
  control, and a skeleton→loaded remount still attaches the listener. Switching
  conversations re-pins even if the previous one was scrolled up; a layout
  scroll at 0 while the scroller is opening does not unpin.
- **`src/lib/follow-scroll.ts`**: whether the reader is at the live edge, whether
  a jump control should show (only after content arrived while unpinned), and
  whether a wheel started in a nested scroller (a thought, a tool payload)
  should leave the transcript pinned. `reset` restores the initial pin when the
  open conversation changes.
- **`src/lib/thought-scroll.ts`**: whether the top fade should show, whether
  new tokens should pin the box to the bottom — the same "do not yank a reader
  who scrolled up" contract as the transcript scroller — and whether a live
  thought stays open (`thoughtExpanded`: the reader's click wins over streaming).
- **`src/lib/ime.ts`**: Enter sends only when composition is settled. An IME
  Process key (`keyCode` 229), a live `isComposing` flag, or composition that
  ended in this frame (WebKit fires compositionend *before* the confirming
  Enter) must not send; leftover Latin staying as typed is the point.
- **`src/components/app/composer.tsx`**, rendered in jsdom: the thinking-level
  menu labels the empty default as "Default" and a set level by name, and it is
  absent when the server offers no levels, so an older server never draws a
  control that would send a meaningless choice. The input is owned by the
  composer, not the app shell, so typing cannot re-render the transcript.
  Enter while composition is live, or on the key that just confirmed it, leaves
  the draft in the box; the next settled Enter sends. The chrome is a fade over
  the transcript, not a top border, so a docked toolbar cannot regress in.
  The model control is always a switcher, even with one ready name.
  ⌘Enter marks the send as steer; Enter while running queues. The Queued tray
  (`queue-tray.tsx`) names the count and steers or drops a row after a
  confirm, and Clear queue asks first.
  File drop onto the box (`composer-drop.ts`) arms a dashed overlay, then
  splits images into vision thumbs and other files into workspace chips
  (`composer-attachments.tsx`); a disabled composer ignores the drop.
  Send names the uploaded paths on the turn (`files` on `POST /turns`); a
  failed upload keeps the chip and does not start a turn.
- **`src/components/app/goal-banner.tsx`**: Pursuing / Blocked / Paused / Done,
  Start when idle, inline edit that saves on blur and cancels on Escape, and
  elapsed time from `goal_started_at`.
- **`src/lib/models.ts`**: choice ids round-trip through a tab separator,
  `groupModels` keeps server order, and auxiliary choices skip blanks.
- **`src/components/app/model-settings.tsx`**, rendered in jsdom: providers
  start as a collapsed list (name, model, default badge) so a second
  endpoint is not a wall of fields; opening a row reveals Provider / URL /
  key / default; Add a provider appends a row and opens it; a settings
  search that matches a field opens the matching rows. Discover fills the
  default-model dropdown from the catalog. A catalog of two names gets two
  window fields — typing one must not write the provider fallback.
  Removing a provider asks first.
- **`src/lib/composer-chrome.ts`**: the composer writes `--composer-pad` onto the
  conversation stage from its own height; a zero height (jsdom) leaves the CSS
  fallback so a unit test cannot collapse the transcript into the box.
- **`src/lib/tool-view.ts`**: a built-in tool's JSON args collapse to the
  command / query / path the user needs to see, `execCommand` keeps newlines
  so an expanded row can show a heredoc, `exec` payloads become stdout
  plus a failed flag when the exit code is not 0, and `web_search` payloads
  become a list of hits. A refused memory write puts the refusal on the
  collapsed row (`toolRowSummary`), not only inside the disclosure. Assertions
  check structure, not any particular query.
- **`src/lib/shell-highlight.ts`**: an `exec` command tokenizes into
  keywords / strings / flags / variables without dropping characters; a
  too-long line falls back to plain text.
- **`src/lib/source-highlight.ts`** / **`src/lib/source-lang.ts`**: a `read`
  body tokenizes from the path suffix into the same token kinds; concatenating
  tokens reproduces the file; markdown and unknown suffixes are left alone.
- **`src/lib/read-result.ts`**, a `read` tool payload becomes a file listing:
  newline-separated bodies, and the older flattened one-liners, both recover
  the path and the lines; other tool output is left alone.
- **`src/components/app/tool-result.tsx`**, rendered in jsdom: a markdown `read`
  renders headings, a `*.go` `read` paints keywords onto tokens and keeps line
  numbers, an unknown suffix stays uncoloured, `exec` shows
  the full wrapped command plus stdout (and a role=alert error when it failed),
  and `web_search` lists hits instead of dumping JSON.
- **`src/components/app/source-code.tsx`**, rendered in jsdom: a numbered
  listing maps token kinds onto the syntax CSS variables.
- **`src/components/app/shell-command.tsx`**, rendered in jsdom: the expanded
  command wraps instead of truncating, and the collapsed preview stays one
  line.
- **`src/components/app/sidebar.tsx`** and
  **`src/components/app/sidebar-thread-row.tsx`**, rendered in jsdom: the list
  starts with New conversation; there is no title-bar chrome row and no hide
  control — those live on the window title bar. Projects and Recents share
  the scrollport gutter: wrapping the project section in a second `px-2` is
  rejected so it cannot sit 8px further in than Recents. The list starts at
  256px, the arrow keys change that width (CSS variable, not a React `width`
  style), a remembered width is restored, and the resize strip sits on the
  right edge (`z-20`) with the aside stacked above the transcript (`z-10`) so
  the 4px overlap is not painted over by the main column. Dropping one Recents
  row on another reports the new id order; a drag that starts on the row menu
  is ignored. A title click still opens when a dragstart races it; dragging
  the title past 8px reorders. A click on the grip opens too. A pinned project
  topic sits in Pinned and still under its folder. Deleting a conversation
  asks first.
- **`src/lib/sortable.ts`**: the row is never HTML5-`draggable`. A title
  click still opens; a pointer move of 8px from the title reports the move;
  a twitch under that threshold is still a click. Synthetic grip `dragstart`
  (jsdom / Playwright) still reorders. A disabled bind does not arm.
- **`src/lib/reorder.ts`**, **`src/lib/sidebar-groups.ts`**,
  **`src/lib/sidebar-collapse.ts`**: moving a row is a splice; Recents and a
  project sort their own ranks, then unranked rows interleave by last
  activity so a stale global cannot sit above a ranked topic that just ran.
  Pinned order is `pinned_at`. A folder without an override follows
  the open conversation (or a selected empty project); garbage storage is
  an empty map.
- **`src/lib/sidebar-width.ts`**: missing or garbage storage is the default
  column, out-of-range values are clamped, a live drag paints
  `--zwai-sidebar-width` without writing storage, and a commit (pointer up
  or arrow key) is what remembers the width.
- **`src/components/app/resize-handle.tsx`**, rendered in jsdom: a right-edge
  strip widens on ArrowRight; a left-edge strip widens when the separator
  moves left (the right panel). A pointer drag does not start a text
  selection. Width stops at the min and max. Keyboard (and pointer up) fire
  an optional commit so the parent can skip React state until the gesture
  ends.
- **`src/components/app/header.tsx`**, rendered in jsdom: the title bar always
  has a Hide/Show conversations control, pads for traffic lights on desktop,
  sizes the leading cluster from `--zwai-sidebar-width` while the list is
  open so the title starts with the transcript, is marked as window chrome so
  a double-click can zoom, and a conversation in a project prefixes the title
  with the project name on the same line — which directory the tools are
  pointed at is otherwise invisible. The model name is not repeated here; it
  lives on the composer.
- **`src/lib/settings-persist.ts`**: edits coalesce into one `PUT` after
  400ms; `flush` writes immediately; a failed write does not block the next.
- **`src/components/app/settings-dialog.tsx`**, rendered in jsdom: Settings
  is a full-page sheet (`h-dvh`) with **Back to app**, a labelled search
  box, and a left rail of tabs. There is no Save/Cancel: edits debounce into
  `PUT /api/settings` (locale rides along) and **Back to app** flushes.
  Searching a Swarm-only word jumps to that
  section and hides Models. Every page scrolls with `overflow-auto` and
  bottom padding, so an outline at the
  end of the tab (Add a provider) is not clipped. `overflow-y-auto` is
  rejected because it would leave the shared `overflow-hidden` in place.
  With `trafficInset`, an `h-12` `data-drag-region` strip sits above the
  rail so **Back to app** is not a descendant of that chrome (macOS
  traffic lights live there); a browser sheet has no strip.
- **`src/components/app/settings-field.tsx`**, rendered in jsdom: a row
  puts the label left of the control; a query hides non-matching rows;
  a section with no remaining rows is omitted.
- **`src/lib/file-tree.ts`**: a flat workspace listing becomes a nested tree
  (missing parents are synthesised), filtering keeps ancestors, collapse hides
  children unless a query is forcing matches open, and a unique directory
  chain starts expanded while a repository-shaped root stays collapsed.
  Keyboard actions are pure: arrows move, Right expands, Left collapses or
  walks to the parent, Enter opens a file.
- **`src/components/app/files-tab.tsx`**, rendered in jsdom: the filter is
  labelled, a repository listing starts collapsed, clicking a folder toggles
  it, a query surfaces a nested file without expanding first, download/delete
  stay off directory rows, deleting a file asks first, and the tree takes
  arrow keys.
- **`src/components/app/panel.tsx`**, rendered in jsdom: opening a sub-agent
  puts the back row *beside* the scroller, not sticky on top of it — dragging
  the transcript must not paint through the back button. A prompt control
  opens the worker's recorded instruction and is absent when the event only
  stored the role name. The resize strip sits
  above that chrome (`z-20`) and the arrow keys still change the width. A
  pointer drag on the strip must not start a text selection. The Files pane
  is a flex column (`overflow-hidden`) so the filter stays put while the
  tree scrolls. Inactive Files/Agents panes are `hidden` when Memory is
  selected, and the active pane is an opaque `bg-card` so a leaked flex
  sibling cannot paint through the skill list.
- **`src/lib/selection.ts`**: while a gesture holds the pointer, existing
  ranges are cleared and `selectstart` is cancelled, then the previous
  `user-select` is restored. Dragging a panel over a transcript is a selection
  as far as the browser is concerned.
- **`src/lib/titlebar.ts`**: a double-click on the desktop title bar (not on a
  button in it) sends `wails:drag:doubleclick` so the native window zooms; a
  browser has no bridge and the click is left alone. Dragging is native and is
  not this module.
- **`src/lib/slash.ts`** and **`src/components/app/slash-menu.tsx`**: `/` at
  the start of the box (no space yet) is a menu; a space makes it a submit.
  Filtering is prefix-or-contains, compact's hint is a percentage (hidden when
  used is 0), and unknown names are not commands. The menu is a listbox; the
  textarea owns the keys.
- **`src/lib/stream.ts`**, against a fake `EventSource`: every kind the server
  sends is subscribed to, a review that lands after `done` is delivered, the
  event name wins over a disagreeing payload, and a connection that dropped on
  its own is not reported as a failure. The first of those is a regression test:
  a kind missing from the list is stored, traceable, and invisible until the
  page is reloaded.
- **`src/store/projects.ts`**, against a fake API: a project deleted elsewhere
  stops being selected, a slow memory response for a project that is
  no longer open is ignored (one project's notes under another's name is worse
  than none), a refused create or edit reaches the dialog so it can show the
  message against the field that caused it, and a memory reload copies the skill
  index onto the project so the Memory tab updates without a second listing.
- **`src/components/app/project-dialog.tsx`**, rendered in jsdom: a rejected
  working directory is shown under that field and the dialog stays open, a
  nameless project cannot be created, and the memory switch is disabled when the
  install has memory off.
- **`src/components/app/project-list.tsx`** and
  **`src/components/app/delete-project-dialog.tsx`**: the selected project is
  marked as pressed, each row's menu is named after its project, a hover
  control on the row starts a conversation in that project (named after it, so
  it cannot collide with the list's New conversation), conversations nest
  under an expanded folder, skills are opened from the row menu rather than
  listed under the name, the section does not add a second horizontal gutter
  on top of the sidebar scrollport, dropping a project row on another reports
  the new id order (title or grip), a name click still selects when a
  dragstart races it, and the delete dialog says that the conversations and
  the memory go too while the user's own directory does not.
- **`src/components/app/confirm-delete-dialog.tsx`**, rendered in jsdom: Cancel
  leaves the thing in place; the destructive button is the only path that
  fires onConfirm; a closed dialog is not in the document.
- **`src/components/app/memory-panel.tsx`**, rendered in jsdom: notes and their
  budget are shown, **Save notes** is absent until the draft differs from what
  is stored and leaves again after a save or a revert, a failed save stays on
  screen, a skill's body is fetched only when it is opened, a skill named by
  the sidebar is opened and fetched on arrival, an opened body stays inside
  its card (overflow clipped, the next skill still follows it in the list)
  rather than overlaying Files, notes that arrive from a
  review are adopted unless the user is mid-edit — in which case a conflict
  banner keeps what they typed and offers Reload — and a write that landed
  while the tab was closed is a badge, not a silent panel. Deleting a skill
  asks first.

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
| `e2e/conversation.spec.ts` | a full swarm turn, a live thought in a 10-line scrolling box whose **Thinking** label sweeps, clicking that row hiding the thought while it still streams, a live status line marked as sweeping while the turn runs, opening a sub-agent (back control beside the scroller, not sticky on it; system prompt from the chrome), a generated sidebar title after the first turn (not the raw request, not a transcript row), a heading rendered as a heading while the turn is still Working, scrolling up mid-stream leaving the viewport put and a jump-to-latest control returning to the live edge, switching conversations landing at the latest turn rather than the top of the history, context carried across turns, jumping to an earlier user message from the left rail (latest tick current while idle at the live edge), Enter while `wait_agents` is pending queuing a follow-up until the turn finishes, **Steer** on that row pinning unread steering under the working line, **Stop** while a tool is in flight leaving no spinner next to the interrupted banner, quoting selected transcript text into the next send as an editable composer annotation, copying or editing a sent message in place so Send restarts from that bubble and clears everything below, file upload appearing in the Files panel with the user bubble naming `uploads/brief.txt`, collapsing a workspace directory in Files and filtering to a nested file, dropping a file and an image onto the composer (overlay, then a workspace chip vs a vision thumb), the turn id on the Trace summary with the event log folded until Full log, an IME-confirming Enter leaving the draft in the box, the manager tool-round cap pausing for Continue/Stop instead of dumping eino's iteration error, and switching the catalog model from a grouped searchable picker (Refresh models / Edit providers) so a reload still sends that name, and the composer context ring plus Trace usage after a turn (reload keeps the ring; the snapshot never lands as a transcript row), `/` listing goal and compact without a 0% hint on an empty chat, pinning a standing objective, starting it from the banner without a human message, editing it in place, compacting without rewriting user bubbles, and a scripted run with a goal finishing as Done |
| `e2e/projects.spec.ts` | a project created from the sidebar, a conversation started from the project row that says so with the project name prefixing the title on one line, the review named in the transcript without opening a tab, **View skills** on the project menu opening the Memory tab with that skill expanded and in view (body inside its card, not over Files), the notes in the panel without a reload, the review in the same Full log as the turn, a second conversation starting with the first one's memory, a hand-edited note surviving a reload (Save notes absent until the draft changes), a deleted project taking its conversations with it after a confirm, the Memory tab not leaving a blank Agents pane above the notes or clipping Skills off the window or painting inactive Files beside Memory, Review now saying when there is nothing to review, hovering a project row revealing a new-conversation control that starts one in that project rather than Recents, pinning a project topic to the top across reload, and dragging a project pinning that order across reload |
| `e2e/shell.spec.ts` | keyboard shortcuts (including hiding the conversation list and `⌘F` find in the conversation), dragging the conversation list and the side panel without selecting transcript text (the list width is remembered across reload and the title-bar leading cluster tracks it), the composer sitting on the transcript with a fade instead of a dock hairline, Projects and Recents sharing one left gutter, an external link opening a new window instead of replacing the app, the tool catalogue on a never-saved config, settings written to the config file and read back, pinning a title-generation model when more than one name is listed, opening a collapsed provider row then discovering models into the default-model dropdown, **Back to app** remaining on screen on a short window when the Swarm page is long, Back to app sitting in the first 48px of a browser sheet (the desktop title-bar strip is not shipped to the tab), the Add-a-provider outline staying inside the Models scrollport, theme switching persisted, chrome language switching (restored to English because locale is in the shared yaml), renaming a conversation and deleting it after a confirm, and dragging a Recents conversation pinning that order across reload |

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
  "Show in Finder" works. A link in the transcript must open the system
  browser, not replace the window. The Dock / taskbar should show the rounded
  app mark at the same size as a bundled `.app`, not a square canvas, not a
  full-bleed tile, and not the generic Unix-exec glyph.
  Double-click the title bar: the window should zoom,
  then restore on the next double-click. On macOS the traffic lights and the
  sidebar toggle must share one baseline in that title bar; New conversation
  starts on the row below. Opening Settings, **Back to app** must sit on the
  row under the lights, not in the drag strip. ⌘Q should return the shell promptly without
  `Window #1 not found` warnings. Wails cannot be driven by these tests.
- **CJK IME**: leftover Latin confirmed with Enter must stay in the composer;
  the next Enter sends. The desktop window is WebKit, which fires
  `compositionend` before that key — Chromium unit tests cannot replace a real
  input method.
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
`zwai trace` usually shows why: `input_msgs` growing, billed tokens climbing,
or one call eating the wall clock.

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
