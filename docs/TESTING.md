# Testing

```bash
make test        # go test -race -cover -timeout 20m ./...  +  front-end unit tests + mobile unit tests
make e2e         # Playwright, on the scripted offline provider
make check       # go vet + both of the above
```

No test needs a model endpoint, an API key or a network. Anything that would
otherwise call a model runs on the **scripted offline provider**, so the suite is
deterministic and fast enough to run on every change.

## Layers

| layer | what it covers | command |
|---|---|---|
| Go unit tests | config, store, memory, provider, tools, engine, search, server, CLI, TUI, and the swarm library | `go test -race -cover -timeout 20m ./...` |
| HTTP tests | every endpoint, SSE replay and resume, the tail log page (`GET /log`, including the live-edge roster sidecar), one worker's log (`GET /agents/:agent/log`), upload path traversal, restart recovery (leftover turns, in-flight sub-agents, and the follow-up queue continue; in-flight tools are closed), PTY terminals (`GET /terminal`, same-origin / loopback Origin, DNS-rebind Host refused, project cwd), phone pairing status/token/offer (`/api/remote/*`, token never echoed), conversation search (`GET /api/search` finds a body the title does not contain; embeddings stay off until a model is pinned), SPA freeze (a Vite rebuild of `dist/` cannot steal hashed JS from a live window; a missing `/assets/*` file is 404 text, not the HTML shell) | `go test ./internal/server/` |
| Front-end unit tests | the event reducer that turns the stream into blocks, the store's conversation targeting, quoting selected transcript text into the composer (count chip at rest, hover to read/edit/drop; tagged `<selected_text>` / `<user_request>` on send; Copy message without the wire tags; the Add to chat snapshot surviving a live stream), clipboard image paste, copying transcript text when the Clipboard API refuses (copy event plant, not a silent execCommand true), file drop onto the composer, find-in-conversation matching (count vs a paint window so a live turn does not freeze), http(s) links leaving the window, sidebar drag order (title drag after 8px, first click still opens), Scheduled inbox / wake banner / notice Run now and cancel / live waits sort first and show prompt / Swarm schedule caps, chrome i18n (`en`/`zh` key parity, locale persist through settings), appearance tokens (`font` / `ui_font_size` / `font_size` / `content_width` / `transcript_mode` / `palette`, directory density), tail-first history pages (`thread-log` / `thread-history` / `use-history-window` / `use-turn-jump`), dismissible settings toasts, ⌘K palette body search (`thread-search` / `palette.test`, stale hits cleared while the next query is in flight), semantic-search settings (off until a model is named) | `cd frontend && npm test` |
| End-to-end | a real browser against a real server: conversation, streaming, sub-agents, files, settings (including the per-note memory cap), theme, chrome language, font and conversation width, directory rows tracking UI size, scheduled inbox / wake banner, Phone settings QR control (pairing failure toasts over the sheet; Bound phones paints a reported model) | `cd frontend && npm run e2e` |
| Phone unit tests | Capacitor iOS/Android apps exist with camera permission and no compiled hub URL; offer URI parse, Noise session, scan/paste screen, live viewfinder (frame, sweeping beam, chime on a pairlink QR), saved-ticket host chips + connecting skeleton (not the scan form), multiple tickets, computer name from `hello`/`list` `host` (not the hub hostname), slim list, resume picker (live turn / last thread / parked wait), compact transcript / watch session (goal flags + waiting), ticket-socket keepalive and reconnect banner, goal/wait banners, device model line from platform+UA (`hello`), launcher is the zwai mark not Capacitor's default; Android system back pops Add a PC, then the new-conversation screen, then a conversation, and finishes only from the inbox or the unbound scan screen; inbox search filters the roster already on the phone; a project row starts a conversation in that project; Android release Gradle reads version/signing from env (no password in git); `android-release` refuses a store upload without a keystore, skips JDK 17, copies the APK to `bin/` | `cd mobile && npm test` |
| Phone E2E | scan screen opens a live viewfinder (frame, sweeping beam, fake-camera preview) and paste of the same `pairlink:v1` URI; a saved ticket shows host chips and Connecting, not Scan QR; Add a PC is a menu sheet; New chat and a project row open the start screen, and on a 320px-wide viewport the project row and the message box stay inside it; Check for updates shows fetching, then current, an install question, or the stubbed feed error | `cd mobile && npm run e2e` |
| Phone simulators | packaged iOS/Android apps bind via paste of that URI, list the seed thread, New chat, Start | see `mobile/README.md` (not in `make check`) |

Current Go coverage, from `go test -race -cover -timeout 20m ./...`:

| package | coverage |
|---|---|
| `frontend` | 92.9% |
| `internal/memory` | 97.0% |
| `internal/provider` | 92.1% |
| `.` (swarm library) | 95.0% |
| `internal/tools` | 98.3% |
| `internal/store` | 90.7% |
| `internal/search` | 90.6% |
| `internal/engine` | 90.5% |
| `internal/config` | 91.9% |
| `internal/terminal` | 97.8% |
| `internal/server` | 90.6% |
| `internal/slash` | 92.9% |
| `internal/lease` | 91.7% |
| `internal/tui` | 87.7% |
| `internal/app` | 88.7% |
| `cmd/zwai` | 84.7% |
| `internal/remote` | 94.1% |
| `internal/wakeup` | 95.7% |

`internal/wakeup` is the phone-host sleep assertion. `TestSetDoesNotBounceTheAssertion`
is why a settings reload cannot release and re-acquire (idle sleep wins that
gap). `TestDarwinCaffeinateUsesSystemSleepAndParentPID` freezes `caffeinate -s
-w <pid>`. `TestHostKeepAwakeFollowsPairingAndSurvivesReload` is the host
wiring: pairing on holds, the switch off releases, Reload does not drop.
`TestHelloStoresThePhoneLabelOnThisFingerprint` plus
`TestDecorateBindingsJoinsHubRowsOntoLocalLabels` are why Bound phones
can show a model: `hello` is keyed by the pairlink fingerprint, a quiet
reconnect does not wipe the label, and a phone that has not said hello
stays a fingerprint. `TestHelloAndListNameThisComputer` plus
`mobile/src/lib/store.test.ts` are why the phone chip is this PC's
`display_name`, not the hub hostname, and why a second scan of the same
fingerprint replaces the ticket.
`TestBlankRemoteDisplayNameSeedsTheMachineHostname` is why a blank Settings
name becomes the hostname instead of a compiled label. `TestHostOfferAndServeLinkOverRelay` sends `hello`
and asserts the bindings list carries that label. `mobile/src/lib/device.test.ts`
builds the one-line label from platform + UA.

`internal/remote` is the phone RPC. `TestListDefaultsToFiveAndOmitsProjectSecrets`
is why the phone never sees a project prompt. `TestListRecentPageDoesNotCountLiveRows`
is why In progress does not eat the Recents quota: four parked waits plus
six idle conversations still return five project rows, and More stays on
that idle list. `TestRunningRosterCarriesTheProjectSoTheFolderStillListsIt`
is why a live row still names its project: the phone lists it under that
folder without putting it back on the idle page. `TestCatalogListsReadyModelsAndHidesEndpointSecrets` is why
the phone's model list has a second provider and no endpoint secret.
`TestPngBytesNamedAsTextStayAFile` is why a png named like a text file is
still a file, and `TestABadSelectionDoesNotLeaveAnEmptyConversation` is
why a refused model does not leave an empty thread.
`TestSlimListPayloadStaysBounded`
caps the default list. `TestUDPBlockedListAndSendStayOnRelay` is the
UDP-blocked path: QR pixels round-trip to the same URI, then list and send
stay on `path=relay`. `TestWatchLiveSendAndUnwatch` plus the kinds freeze
in `watch_test.go` keep phone `watch` on the same seq/kind bus as desktop
SSE, clip `spawned` bodies, and stay under the 64KiB pairlink frame.
`TestWatchOpensAtTheLiveEdgeNotTheOldestEvent` is why a long conversation
on the phone does not start at seq 1 — and why that tail arrives on
the `watch` RPC `ready`, not a trickle of `event` frames.
`TestWatchFirstSnapshotFitsAPhoneScreen` is why first `watch` is a
live-edge page (24), not the whole last turn of a pursuing `/goal`.
`TestWatchOpensOnTheLastTurnNotEarlierOnes`
plus `TestLogPagesOlderEventsBeforeTheViewport` are why first paint is the
live edge of the last turn and pulling up loads earlier events. `TestWatchEmptyLastTurnReadyStillPages`
is why an empty live turn still carries a log cursor, and
`TestLogZeroBeforePagesOlderThanLastTurn` is why `log` with `before` 0
pages older than that window.
`TestPhoneListAndOpenSurfaceGoalAndParkedWait` is why a parked `/goal` wait
is on the phone roster (`waiting`) even when it is not on the recent page,
and why `open` carries the objective flags plus the wake snapshot.
`TestHumanLineDropsScheduleAndMemoryJargon` plus
`TestListActionOmitsAWakeEnvelope` are why `list.running[].action` is
findings / a command / assistant prose, not `schedule_wake({…})` or
`memory({…})`, and why a quiet scheduled check does not become the Recents
subtitle.
`TestRunNowCancelWaitAndResumeGoalMapOntoTheEngine` is Run now / Cancel wait /
Start goal over pairlink. `TestWatchReadyAndArmPushCarryWaitingStatus` is why
`watch` `ready` and a live `schedule` frame carry `status.waiting` / `wake`.
`mobile/src/lib/session.test.ts` queues `event` until `ready` so an old
host that still streams catch-up still paints once, a later empty
`ready` must not walk the log cursor forward, and a listing stub stays
on screen until that `ready` (a tap is not a freeze).
`TestStatusGoesOfflineWhenHubCloses` is why a dead hub socket cannot keep
minting a QR the phone will redeem as `host offline`: status follows idle-drop
and reconnect of that WebSocket.
`mobile/src/lib/client.test.ts` is why a phone ticket socket keepalives
with a punch-ping data frame, rejects in-flight RPC on `onclose`, maps an
abnormal close to the can't-reach copy and a close reason to a server
refusal (not one reconnect sentence), and sends `hello` with the model
line once the session is up. `mobile/src/components/host-chrome.test.tsx`
is why that chip is green while the socket is up and gray while it is not.
The Capacitor shell in `mobile/` has its own unit tests
and a Playwright paste/scan screen; `mobile/native-project.test.ts` asserts the
iOS and Android trees ship with camera permission, no compiled hub URL, and
the zwai launcher (not Capacitor's default icon), and that Android release
Gradle takes version/signing from the environment (no password in git).
`mobile/scripts/android-release.test.ts` is why `make mobile-android-release`
refuses a store upload without a keystore, skips JAVA_HOME 17, and copies
the APK into `bin/`. `TestPhoneLauncherSlotsMatchTheDesktopMark`
is why those PNGs are `internal/desktop/appicon.png` painted into the iOS/Android
slots (`go run ./mobile/scripts/genicons.go`).
`mobile/src/lib/resume.test.ts` is why bind opens a live turn (or the last
thread, including a parked wait) instead of parking on New conversation,
and why a tap paints a listing stub instead of freezing on the inbox.
`mobile/src/lib/phone-shell.test.ts` and `mobile/src/app.test.tsx` are why a
saved ticket paints host chips and a connecting skeleton on the first frame
instead of the scan form, and why Add a PC is a sheet. A saved model does the
same: the scan form stays hidden and a Chat tab is the first screen
(`direct-chat-screen.test.tsx`, `provider-sheet.test.tsx`,
`openai-wire.test.ts`, `openai-client.test.ts`). `mobile/e2e/direct-chat.spec.ts`
drives discover and a reply against a routed endpoint, and opens the thought
row after the answer. A multi-event body paints one token per turn
(`openai-client.test.ts`), including a device chunk source that delivers
bytes before the call returns. A responses rejection of an image part is
sent once more as chat completions, and that retry uses `image_url`
(`openai-client.test.ts`). A rejected reasoning summary is omitted and the
responses call is tried again. A different responses 400 is not retried.
`rosterFingerprint` is why a 2s inbox poll that repeats the same roster
does not remount the row mid-tap. `mobile/src/components/markdown.test.tsx` renders `$n$` as KaTeX and copies a
fenced body (including when the Clipboard API refuses), paints `**bold**` and GFM tables, keeps a filesystem path
from becoming a webview navigation, paints a `chart` fence as a plot with a table of the same rows, holds a truncated chart, and leaves a `json` fence and a finished invalid chart as code. `mobile/src/components/phone-chart.test.tsx` covers line, area, bar, stacked bar, and pie marks. `mobile/src/lib/copy-text.test.ts` is why
that copy plants on the `copy` event instead of trusting WebKit's boolean. `mobile/src/components/thread-blocks.test.tsx`
is why a user bubble uses that same markdown, not the source markers.
`mobile/src/components/home-screen.test.tsx` is why a parked wait sits in
In progress as Waiting, not a quiet Recents row, why a live conversation
is also listed under its project, why that project folds (and stays folded
across a remount) while the new-conversation control is an icon, and why a live
`schedule_wake` / `report_schedule` envelope is not the subtitle.
`mobile/src/lib/inbox-groups.test.ts` is why a roster row with a project
lands in that folder even when `threads` omitted it, and why a live row
with no project stays out of Recents.
`mobile/src/lib/inbox-preview.test.ts` pulls findings and drops the tool name.
`mobile/src/components/thread-screen.test.tsx` is why an empty earlier page does not immediately ask again, why Interrupt is only on a queued follow-up, and why a standing `/goal` is
Pursuing / Done / Blocked / Paused with Start, a parked wait is Waiting
with Run now / Cancel wait rather than a silent Send box, a long
objective stays one truncated line so that chrome cannot cover the composer,
and Earlier sits outside the scroller so a live-edge tail can still page.
A long tool path must not stretch the column (`min-w-0` / `break-words`).
`mobile/src/lib/transcript.test.ts` is why a `schedule` payload is **A wait is armed.**, not the JSON, a `progress` pulse is not a notice, a findings `report_schedule` is that prose as a notice rather than a tool chip, adjacent thoughts and tools fold while an answer stays visible and splits the group, and Planning sits under a closed answer instead of on the fold above it.
`mobile/src/lib/tool-preview.test.ts` pulls `findings` (not `prompt`) for the collapsed chip, and a wait roster is counts rather than `elapsed_ms`.
`mobile/src/components/thread-blocks.test.tsx` keeps that roster off the user bubble and the expanded tool body.

`mobile/src/lib/app-update.test.ts` is why the phone asks GitHub Releases
itself, ignores a draft, a prerelease, the version already installed, and
any artefact that is not this repo's `zwai-*-android.apk` on a GitHub host.
A menu check always requests: a fresh cache and Not now do not answer it,
and a failed response is that response's error rather than silence. The
quiet bar still stays silent on failure.
`mobile/src/components/update-notice.test.tsx` is why Update starts that
install and why Not now hides the bar. `mobile/src/components/version-check.test.tsx`
is why the menu shows fetching, the current build, the install question, and
the feed error. The browser walkthrough does not check on its own, so
`mobile/src/app.test.tsx` still sees no Update button.
`mobile/e2e/version-check.spec.ts` drives that menu check against a stubbed feed.

`mobile/src/lib/mock-link.test.ts` is why the walkthrough host answers the
same ops with the same shapes, streams a turn to whoever is watching, says
the turn is over on the frame that ends it, and is one PC seen down two
sockets rather than two PCs (React strict mode opens two). Without it the
inbox and a conversation had no end-to-end coverage, because Playwright
could not get past the scan form. `mobile/e2e/walkthrough.spec.ts` drives
those screens on it: live rows separate from recents with an age on each,
a turn that folds its work behind the answer, a short conversation resting
on the composer rather than under blank space, New chat (and a project row's
icon) opening what it started, a project folder folding while its live row
stays under In progress, inbox search narrowing the rows, a page loaded
with More still there after the inbox refreshes, a loading status while
open is held (`?pause=open`), and the composer's model, thinking level,
and attach control.
`mobile/src/lib/inbox-window.test.ts` is why a later first page does not
drop rows More already loaded, and why a page-one row that left that page
does not stay.

`mobile/src/components/composer.test.tsx` is why Enter sends but Shift+Enter
and an IME candidate list do not, why send stays off until there is
something to send, and why the box grows with the text but stops before the
transcript loses its last answer. `mobile/src/components/inbox-row.test.tsx`
is why a pending question is filled rather than tinted like a wait, and
`mobile/src/lib/when.test.ts` is why a PC whose clock runs ahead reads as
`just now` instead of a negative age.
`TestAParkedWaitCarriesItsSummarySoTheRowIsNotJustABadge` is why that row
has a line and a date at all: `list` keeps a roster thread out of
`threads`, so reading either off the Recents list would have read nothing.
`mobile/src/components/home-screen.test.tsx` is why Running has no date
(it is happening now) and a wait does, and `mobile/src/lib/resume.test.ts`
is why a host that flags the wait on the thread and sends an empty roster
still gets both, without a stale thread line overwriting a roster row the
host did send. `detailFromListing` is why that line does not then read as a
turn: the stub a tap paints would swap the Waiting header and the schedule
banner for Stop for the one frame before `open` answers, which no
Playwright assertion is fast enough to see.
`mobile/src/components/pull-to-refresh.test.tsx` is why a short tug scrolls
and a long one reloads, and why a drag that starts mid-list is never a
reload. Camera on a real device is the product path (`make mobile-ios` /
`make mobile-android`).

`frontend` is the embed plus the incremental Vite rebuild (`Ensure` /
`Load`). The fingerprint and `npm` runner are tested against a fake tree;
`Load` returns the embed under `go test` so `go test ./...` does not
require Node. `TestCommittedLockfilesMatchThePublicNpmRegistry` refuses a
lockfile whose `resolved` hosts are not `registry.npmjs.org` — npm 12
treats those tarballs as remote packages (`EALLOWREMOTE`) on a fresh clone.
`assembleApp` in `cmd/zwai` is the two-line call that
production `desktop` / `web` go through.

What is deliberately not unit-tested: `main`, `runDesktop`/`runWeb`/`runTUI` (thin
wrappers around functions that *are* tested), `desktop.Run` (opens a native
window) and `tui.RunClient` (drives a real terminal). The rest of `internal/desktop`
is tested: traffic-light geometry, hopping AppKit geometry onto the main
thread (Wails delivers window events off-thread), the quit-time
`Window #1 not found` filter, that the Dock icon is a real PNG with
transparent rounded corners on Apple's 824/1024 icon grid, wired into
`application.Options`, that `WritePhoneIcons` paints the same mark into the
iOS App Icon and Android mipmaps (not Capacitor's default lattice), and that
`prepareBundle` writes a Local Network
Info.plist under the cache dir (the `syscall.Exec` re-exec itself cannot
run in a unit test). The window itself still cannot be opened in a unit
test. Each untestable shell was
split so that everything which can fail is testable: `startDesktopServer`
returns the window options without opening a window, and closing that
window does not stop the engine. `serveWeb` takes an injected stop signal
and leaves the engine up. `runTUI` attaches with `ensureEngine` and
`tui.RunClient`. `assembleTUI` / `buildTUISwarm` still build an in-process
swarm for renderer tests. The window itself is verified by hand — see below.

## The scripted offline provider

`--mock` (or `provider.NewMock`) replaces the model with a script that behaves
like a real swarm run: it thinks, spawns two sub-agents (one with
`fork_context`), has a worker call `write` to produce a file in the workspace,
waits for both, then streams a markdown answer that includes a `chart`
fence whose values are the report lengths (so the UI's plot renderer is
on the same path, without a sample domain baked in), a language-tagged
`go` fence, and inline `$n$` math so highlighting and KaTeX ride the
same offline path. Under `go test` those last two extras are omitted:
each extra word is another `mockChunkDelay`, and streaming them on every
manager answer blew the engine package's 10m budget. `zwai --mock` and
Playwright (`go run`) still emit them.

It answers as a **memory reviewer** too, when the agent asking is the review
after a turn: it stores one note and records one skill, both derived from the
event log it was handed (not a compacted ADK transcript), then reports in a
line. Auto-review is skipped when the manager already wrote; Review now still
runs. Without that, a `--mock` run
and the E2E suite would exercise projects but quietly skip the whole memory
path — the tools, the files, the event and the panel.

It also answers as a **conversation namer** (`title-namer`): a short label
derived from the request, shorter than the placeholder, so `--mock` and the
tests can tell a generated name from a quoted first message.
`internal/engine/title_test.go` is why that namer runs from the opening
message, a later turn does not get a second name, and an interrupted
opening turn still keeps the landed name.
`internal/engine/schedule_title_test.go` is why an untitled wait plants the
truncated prompt then the same namer replaces it, an explicit or PATCH
title wins, `AutoTitle` off leaves the placeholder, and naming a wait
does not emit a conversation `title` event.

It answers as a **compact summarizer** (`compact-summarizer`) and as a
**session-memory** writer (`session-memory`) with a short briefing derived
from the messages or events it was handed. A manager Generate whose
billed or estimated prompt tokens exceed `swarm.auto_compact_tokens` (default
80 000) is rewritten in `BeforeModelRewriteState` before the call: older
replayable tool results are cleared first, then older messages become that
briefing (preferring the rolling session memory), the recent tail stays, and
the human transcript is untouched. The briefing is streamed; silence uses the
provider idle timeout. Tests live in `internal/engine/compact_test.go`
(streamed briefing, no compact deadline, session-memory preference, a
transcript dump not persisted),
`internal/engine/compact_briefing_test.go` (refuse `Tool:`/`Human:`/`Assistant:`
and exec JSON, refuse a source-tail clip, keep dense prose, `/compact` and
auto-compact summarizer input newest-first under a rune cap, no task words in
the prompt),
`internal/engine/session_memory_test.go` (token/tool gate, event-log extract,
a force refresh that is already caught up, a short extract still storing a
briefing, cancel while another refresh holds
the gate),
`internal/engine/followup_test.go` (a queued follow-up starts as soon as the
turn is `done`, even while a session briefing is still blocked on the
summarizer),
`internal/engine/session_memory_bound_test.go` (newest-first rune cap, a
multi-thousand-event log staying under the cap, tool results clipped harder than
answers, dump not stamped, a failed refresh stamping the token watermark
so force does not resend, refresh wait is the provider idle timeout),
`internal/engine/microcompact_test.go` (replayable results only),
`internal/engine/goal_session_test.go` (historical wrap leftover stays
generic and is dropped from leftover steers, heat vs
`auto_compact_tokens` not a million-token window, wrap-up copied into
session memory when the meter is still cold), and
`internal/engine/autocompact_test.go` (under-budget
skip, in-turn fire, summarizer errors swallowed, a canceled turn stopping compact, a rejected dump leaving ADK state and `compact_summary` alone, roster rehydrated from
`spawned` events, default budget leaving a mock turn alone). `internal/engine/autocompact_vs_eino_test.go` is the keep-or-delete
gate against eino's `adk/middlewares/summarization`: same fixture, same stub
briefing, score spawn roster / in-flight `wait_agents` / last human as a user
message / swallowed summarizer errors / a task-agnostic summarizer prompt. The
Generate is eino's; Finalize keeps the tail and pins ids from events. It
does not score briefing prose. `internal/engine/autocompact_invariants_test.go`
locks the wiring and the regressions that would silently come back: default
Finalize, fishing spawn pairs from folded text, dropping `wait_agents`,
losing the current user message, stale billed usage, missing finished-worker
pins (including leftovers from an earlier `/goal` session). `TestLiveCompactBriefingQualityVsEino` does
that against the configured endpoint when `ZWAI_LIVE_COMPACT=1`; default
`go test` / `make test` skip it. If the custom Finalize does not beat eino's
`DefaultFinalize` on the structural checks, it should be deleted.
And when the manager prompt
still has an open standing objective, it calls `complete_goal` so a `--mock`
run does not auto-continue until the cap. Tests that need to observe
auto-continue call `provider.SetCompleteOpenGoal(false)`.
`internal/slash` and `internal/engine/slash_test.go` are why a `/goal`
send (glued CJK, fullwidth `／`, IME punctuation `、`) is the command at `StartTurn`, not a
user task; `/goals` still is. `/plan` is the same parse; a live turn is
`ErrBusy`. `internal/engine/ask_test.go` is why `ask_user` blocks this
turn until an answer, idle answers are `ErrIdle`, a wrong `call_id` is
`ErrAskMismatch`, interrupt cancels the wait, resume re-arms an orphaned
questionnaire instead of swallowing it, a worker call fails in JSON, and
`AwaitingAnswer()` lists only the blocked conversation (a working turn is
not an ask; answering clears the list). `internal/server/ask_test.go` is why
the thread listing marks `awaiting_answer` while the questionnaire is open.
`internal/engine/schedule_tool_test.go` is why `schedule_wake` upserts on
this conversation (including when the model omits `id`), and
`internal/engine/schedule_wake_id_test.go` is why that tool's schema has no
`id` — a leftover id that matches no stored wait still arms instead of
returning `store: not found`, it still replaces the open wake, while another
conversation's id or a finished id does not arm a new one. `schedule_task` refuses a `GoalContinue` /
`ScheduleContinue` / plan-implement turn, empty `report_schedule` findings
are quiet, `next_in_s` rearms a delay that claim already marked `done`,
garbage arguments come back as JSON `ok:false`, and workers get a deny stub.
`internal/engine/schedule_tool_info_test.go` is why the tool names stay
stable, Info text stays generic, a remaining-time estimate is biased to
about a third, and `report_schedule` must not stretch the interval.
`internal/engine/schedule_rearm_test.go` is why that recadence refuses
cancelled and paused rows and respects `schedule_max_active`.
`internal/store/schedule_activate_test.go` is why `done` → `active` shares
the cap transaction with create/resume.
`internal/engine/schedule_goal_test.go` is why a pending thread wake
pauses `/goal` auto-continue, why cancelling it while idle starts the next
pursuing turn immediately (`goal_continued`), why a cancel without a
standing objective stays idle, and why paused, cancelled, and
standalone origin-only rows do not suppress. A still-due delay that
has already been claimed (`status=done` plus a `running` run, matching
`advanceAfterFire` before `StartTurn`) is `TestClaimedOneShotStillSuppressesGoalAutoContinue`
— the armed-row tests do not cover that race.
`internal/engine/schedule_report_test.go` is why a scheduled check with
nothing to surface archives (`quiet`, unread cleared, `turn.quiet`; a
standalone fire leaves Recents) while an omitted report with an answer
is `findings` and stays in the sidebar, and why a crashed or cancelled
scheduled turn marks the run `error` so `HasRunningRun` cannot stick.
`TestCreateScheduleUsesFrozenCapWhenTickerIsLive` is why inbox create
(and resume/patch, and a standalone claim's default provider) reads the
same snapshot the ticker froze, not a later `e.cfg.Swarm` write;
`ApplyLiveSwarmLimits` is what Settings uses to refresh it.
`TestResumeCannotRestartClosesTheScheduleRun` and
`TestResumeDropsASupersededScheduledRun` are why a leftover
`ScheduleContinue` that resume cannot continue — empty user text, or
superseded by a later unfinished row — closes that bound fire as
`error` instead of leaving `running`. The surviving run is polled until
`FinishRun` (turn close lands first).
Tests opt into `provider.SetMockScheduleQuiet` / `SetMockScheduleSilent` /
`SetMockScheduleFindings`. The default mock sniffs a scheduled check
(`This turn is a scheduled check.`) and calls `report_schedule` with
generic findings (`ZWAI_MOCK_SCHEDULE_QUIET=1` empties them).
`SetMockScheduleSpawn` restores the old fan-out so omitted-report-with-answer
stays testable. `ZWAI_MOCK_SCHEDULE_WAKE=1` is off unless a test arms a
min-interval wait on the first manager step.
`internal/server/schedules_test.go` is why the inbox HTTP API creates
human waits, lists `{schedules, unread}`, pauses/cancels, run-now on
idle starts a `ScheduleContinue` turn, run-now while the conversation is
busy is `409 skipped_busy`, and reading a findings run clears unread.
An omitted `title` plants a placeholder and the namer replaces it
(`TestUntitledScheduleCreateGetsAGeneratedTitle`).
`TestRunScheduleNowConsumesAFutureCronSlot` is why an early cron run-now
advances past the pending slot instead of leaving `next_run_at` on that
same due time.
`internal/store/schedule_test.go` is why `HasPendingThreadWake` treats
an active `kind=thread` row and a running thread fire as pending, and
why paused/cancelled/done-with-no-run, standalone origin-only, an empty
thread id, and a missing table do not (the last returns the error;
the engine fail-opens). `TestActiveThreadWakeReturnsTheSoonestArmedRow`
is the one parked row the phone banner and Run now / Cancel wait target
(soonest `next_run_at`, not a paused or alien conversation).
`PendingWakeThreadIDs` is the listing batch of
those same conversations so the sidebar does not query per row.
`internal/store/schedule_title_test.go` is why `ApplyAutoScheduleTitle`
is a compare-and-swap: a PATCH in between wins.
`internal/engine/schedule_test.go` is why `Status.Waiting` and `Waiting()`
follow an armed thread wake without starting a turn.
`internal/server/schedules_test.go` is why GET `/api/threads/:id` and the
listing carry `waiting` while a thread wake is parked.
`internal/engine/prompt_test.go` is why the manager prompt has `## Waiting`
(`schedule_wake`, do not wait for the human to remind, `report_schedule`,
a parallel `exec` progress-poll sleep is allowed, do not sleep the full
remaining time, estimated waits about a third, extra checks, do not pad)
without CI / deploy / pull-request / cron-job samples or a sample duration,
why an open `/goal` names a pending wake as the next turn, and why extra
lists this conversation's active wakes from a real `CreateSchedule` row.
`internal/engine/plan_test.go` is why planning unmounts write/exec,
`propose_plan` writes the file, Implement remounts those tools,
deleting a conversation takes `PLAN.md` with it, and
`/goal` auto-continue does not fire while planning. The mock asks first
during planning (`explore → ask → propose`); default non-plan runs do
not, unless a test calls `provider.SetMockAskUser`.
`internal/tui/ask_test.go` is why a missing TTY fails `ask_user` instead of
hanging, and why digits pick an option. `internal/tui/plan_test.go` is why
planning drops write/exec and a paused `/goal` stays held after Implement.
`internal/engine/project_test.go` and `internal/memory/prompt_test.go`
are why a project worker gets `skill_view` and the notes/skills snapshot,
not `memory` / `skill_manage`, and why that worker prompt stays generic.
`internal/memory/families_test.go`, `internal/engine/review_test.go` and
`internal/engine/review_tidy_test.go` are why
a shared name stem is one skill, leftover families fold after a finished turn
without a reviewer call when auto-review is off, the fold is still on the
turn id as `memory_review`, `FoldProjectSkills` still folds stems then asks
the reviewer to curate the live catalog (and refuses after shutdown), a
tidy report names what was created, deleted, patched, and left unchanged, and
catalog-tidy model calls hang on the project's latest finished turn rather
than the first sidebar conversation that happens to have one.

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
go test -race -cover -timeout 20m ./...       # everything
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
`TestTruncateDoesNotBusyAgainstAWriter` is why editing a sent message does
not fail with `SQLITE_BUSY` while the previous turn is still flushing events
(the database is one connection).
`TestListWorkspaceDoesNotHideSiblingsBehindAFatDirectory` is why the Files
panel still shows later siblings when an early directory would otherwise spend
the 2000-entry cap.
`TestMissedHandoffReachesTheWaitingManager` is why a worker that
`send_message`s an invented id still finishes as done and the manager's next
model call sees the handoff text: the miss notifies the host
(`notified: manager`) instead of a `NodeRunError` or a retry roster.
`TestSpawnWithoutATaskDoesNotKillTheManager` /
`TestSpawnAgentRequiresATask` are why a `spawn_agent` that omits `task`
returns `{"error"}` so the manager retries, instead of a `NodeRunError` that
blocks a pursuing `/goal`.
`TestKeepHumanSteersDropsTheSessionWrap` is why an unread `/goal` wrap-up
leftover cannot become a human turn that resets the auto-continue cap.
The front-end reducer also hides the same historical text on older sessions.
`TestRetractUnreadSteerDropsItFromReplay` /
`TestPreemptKeepsTheTurnRunning` are why Delete drops one unread `[steer]`
by event seq (the `steer` row stays; replay does not) and Interrupt does not
cancel the turn. The library side is
`TestPreemptAbortsInFlightToolAndDeliversSteer` /
`TestRetractPendingSteerDropsItFromTheInbox` /
`TestPreemptDoesNotCancelWorkers`.
`TestGoalSessionExtendsTheSameTurnAtTheIterationCap` is why a `/goal`
ReAct slice keeps the same turn instead of asking to extend or cutting a
session; `TestAGoalTurnFinishesAcrossOneRoundSlices` is why `complete_goal` still
lands when each ReAct slice is one round;
`TestStitchManagerToolResultsFillsDroppedWaitResults` /
`TestStitchManagerToolResultsRefreshesAStaleWait` /
`TestManagerToolResultsIgnoresWorkersAndEmptyTurns` are why a slice that
lost its ADK tool results still reads the event-log `wait_agents` report
(including a later wait that reused the same call id);
`TestMockWaitCallIDsStayUniqueAfterAContinuedRun` is why the scripted
manager does not reuse `mock-wait-1` on the next ReAct slice;
`TestSetHistoryKeepsDroppedToolResults` /
`TestRunWithMaxIterationsKeepsToolResultsInTranscript` are why that slice's
tool results are in the transcript the next slice reads;
`TestClosingAGoalAtTheReActSliceDoesNotAskToExtend` /
`TestBlockingAGoalAtTheReActSliceDoesNotAskToExtend` are why
`complete_goal` / `block_goal` mid-slice must not pop the non-goal
confirm card;
`internal/engine/goal_reopen_test.go` is why a mistaken `complete_goal`
can be undone in-turn (`reopen_goal`, which also resets `goal_auto_turns` so
a complete at the cap does not immediately recap) and later from **Start**
(`ResumeThreadGoal` no longer rejects a completed objective);
`TestCatchUpStopsWhenTheWatchIsCancelled` /
`TestWatchCancelSkipsReady` /
`TestWatchReplaysAfterALaggedSubscriber` are why a phone `unwatch` / second
`watch` must not keep pushing the old thread, and a slow phone replays
from the store instead of dropping stored events;
`TestGoalContinuesWhenTheManagerStopsCallingTools` is why the manager
stopping tools starts the next turn with no `goal_session`;
`TestAContinuationWithoutToolsStopsAutoContinue` /
`TestAContinuationWithToolsKeepsAutoContinue` are why an empty
continuation records `goal_idle` instead of looping, and a live
`wait_agents` does not;
`TestHoldGoalIdleIsIdempotentAndSkipsMissingThreads` /
`TestAHumanMessageClearsAHeldGoal` / `TestResumeClearsAHeldGoal` are why
the hold is sticky until a human message or Start;
`TestParkedWorkersStayVisibleBetweenGoalSessions` is why Agents still
sees in-flight sub-agents after the manager yields.
`TestHostNotifyKeepsWorkerEventsAfterRunReturns` /
`TestAttachHostNotifyIgnoresNil` are why a parked worker still records
`finished` after the manager `RunWith` returns, instead of freezing
Agents on starting with an empty pane.
`TestRaisingMaxConcurrentUnblocksQueuedWorkers` /
`TestLoweringMaxConcurrentKeepsWaitersQueuedUntilASlotFrees` /
`TestApplyLiveSwarmLimitsResizesParkedAndRunningRegistries` are why
Settings → Swarm → sub-agents at once takes effect on the live and
parked registry: a higher cap starts waiters immediately, a lower cap
does not kill in-flight workers.
`TestNextGoalSessionAfterRestartResolvesLeftoverWorkers` /
`TestNextGoalSessionAfterRestartRestoresRunningWorkers` are why a process
death between `/goal` sessions does not turn `wait_agents` into
`unknown agent`: leftovers are planted or restored from the conversation
event log, and spawn ids are re-pinned because later turns drop previous
tool results. `TestResumeConversationCompletesADanglingWaitOnTheLiveIds` /
`TestResumeConversationDoesNotPinFinishedLeftoversAsNakedSpawns` /
`TestCloseOrphanedWaitAgentsRecordsATimedOutSnapshot` are why a kill
mid-`wait_agents` completes that wait as a timed-out snapshot of the live
ids instead of dropping it and pinning every finished leftover as
`spawn_agent({role})` (that made the manager recount a two-day roster as a
new naked swarm, and `wait_agents` returned on the first already-done id).
`TestOrphanedWorkersTreatsCleanupAsFinished` is why a
killed leftover is planted as stopped instead of restarted.
`TestCloseAlreadyFinishedIsNotCancelled` is why `close_agent` on a planted
finished worker returns `already_finished` instead of `cancelled: true`
(that lie made a long `/goal` walk the leftover roster).
`TestManagerPromptDoesNotTreatLeftoverWorkersAsSomethingToClose` /
`TestLeftoverWorkerCuesDoNotInviteARosterClose` are why the manager is told
leftover ids stay for `resume_agent`.
`TestAutoCompactRehydratesFinishedWorkersFromEarlierTurn` is why a fold
on the next session still pins those ids.
`TestANormalMockTurnDoesNotAutoCompact` is why the default token budget
leaves a scripted turn alone; `TestAutoCompactFiresDuringATurnWhenOverBudget`
is the in-turn fold.
`TestBuildUserMessagePutsImagesOnUserInputMultiContent` is why a pasted
screenshot with a caption does not send `Content` and `UserInputMultiContent`
together (OpenAI's marshaler rejects that pair);
`TestExclusiveContentDropsCaptionAlreadyInParts` is the client-side strip
so a later concat cannot revive the same error.

`TestAFailedTurnBlocksAnOpenGoalAndDoesNotAutoContinue` is why a crashed
`/goal` turn that is a real refusal shows Blocked with the public turn error on the banner (not a
generic sentinel) and an idle composer instead of auto-continuing into the
same failure; `TestARetryableModelErrorRetriesInsteadOfBlockingTheGoal` is
why truncated tool JSON / a `429` / a dropped stream re-enters the same
turn (`model_retry`) instead; `TestARetryableModelErrorContinuesTheGoalAfterRetriesAreExhausted`
is why exhausting those retries still auto-continues (or `goal_idle`) instead of pinning the banner;
`TestARetryableModelErrorWithoutAGoalDoesNotAutoStart` is why that auto-continue is `/goal` only;
`TestFailedTurnBlockReasonPrefersThePublicError` is the
fallback when the turn row has no error text;
`TestTurnOutcomePrefersAModelFailureOverInterruptNoise`
is why a `NodeRunError` is still an error;
`TestTurnOutcomeTreatsABareCancelAsAnError` is why a cancelled model
call without Interrupt is not a clean `done`.
`TestAnInterruptedTurnDoesNotBlockTheGoal` is why Stop pauses (`goal_capped`)
instead of blocking; `TestPauseOpenGoalOnInterruptMarksAPursuingGoal` is the
direct pause path.

The TUI package also covers the same tool-display rule as the desktop UI: an
`exec` notification whose args are JSON is shown as the command, a non-zero
exit becomes a failed block (not a JSON dump), and `web_search` lists hits.
`TestPanesShowTheConversationAndHideTheBookkeeping` is why TUI drops
`wait_agents` / `close_agent`. Desktop `transcript.test.tsx` and the phone
`thread-blocks.test.tsx` hide `close_agent` the same way (`spawn_agent`
already has a SpawnRow).
A missing `--task` opens the composer instead of failing; typed turns continue
the transcript, and a `--task` run still exits when that swarm finishes.
`--goal` without `--task` is the first user message and starts immediately
(`TestTUIGoalWithoutATaskStartsTheObjective`); leftover words still win as the
task. An empty auto-continue returns the composer
(`TestAGoalContinuationWithoutToolsReturnsTheComposer`) instead of looping
until `goal_max_auto_turns`. Typing `/` opens a command popup (`TestSlashMenuAppearsAboveTheComposer`) with
prefix filter, a `/model` / `/reason` picker, and `/help` / `/clear` / `/exit`.
`shift+tab` still cycles thinking level for the next turn, same choice the
desktop composer makes. The idle composer parks
the real terminal cursor after committed text (`TestComposerIMECursorSitsAfterCommittedText`)
and idle ticks must not rewrite the view (`TestIdleTicksDoNotRepaintTheComposer`),
or CJK IME preedit jumps to the start of the line.

Conventions in these tests:

- Every test gets its own data directory (`t.TempDir()`) and clears the
  `OPENAI_*` variables, so a developer's environment cannot change the outcome.
- Test names say what would break for the user, not which function they call:
  `TestStartTurnIsAcceptedNotAwaited`, `TestSlowSubscriberDoesNotBlockTheRun`.
- Comments in tests explain *why the behaviour matters*, because that is what a
  later reader needs to decide whether a failing assertion is a regression or an
  obsolete expectation.

`cmd/zwai/cli_trace_test.go` is split from `cli_test.go` so neither file
crosses 1000 lines: a memory review on the same turn id shows in `zwai trace`.
`internal/provider/provider_wait_test.go` is wait_agents progress parsing.
`internal/tools/pin_test.go` is why a UTF-8 `read` whose first 4096 bytes
cut a rune stays `encoding=utf-8`, why an empty `edit` `replace_block`
deletes instead of the old catch-all missing-payload error, and why
`write` refuses a missing/`contents` body instead of claiming `Updated file`
while emptying the disk.
`internal/tools/execwait_test.go` is why `exec` still runs a progress-poll
sleep and shortens one of five seconds or more to about a third of that
duration (a sub-second pause between commands is left as written).

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
  `tool_call_id`, a `tool_delta` fills the pending row without closing it,
  `collapseLiveEvents` keeps the latest snapshot per live tool call (two
  parallel `exec`s do not mix), a progress pulse updates the live summary without leaving
  a row in the timeline or reviving an agent that has already finished, a
  `usage` pulse is ignored by the reducer (the composer meter owns it) and
  `collapseLiveEvents` keeps only the latest usage snapshot in a burst, a
  `max_iterations` event becomes a confirm card (and `max_iterations_continued`
  resolves it) rather than an unknown notice. A `model_retry` is a generic
  notice (the `{attempt,cap}` JSON stays in Trace). An interrupt, a `cleanup`, a
  worker `finished`, or a manager `error` closes tools left pending so they
  stop spinning. A clean manager `done` does not: leftover workers are parked
  for the next `/goal` session, and `goal_continued` must not paint them done
  or `wait_agents` sits next to a Done roster while the worker is still in
  `exec`. `cleanup` is the kill signal.
  `collapseLiveEvents` keeps only the latest snapshot per agent and kind from
  a burst of deltas — lossless, because a delta carries the accumulated string.
  Live tool output uses the same rule keyed by `tool_call_id`. Carriage return
  in an expanded tool body is overwrite (`src/lib/carriage.ts`). A pending `exec`
  row starts open and the output box follows the tail (`src/lib/output-scroll.ts`).
  `splitQueuedSteers` pulls unread steering out of the turn body while a turn
  is running (a later model round on the same turn consumes it; a previous
  turn's steer stays put). `steer_retracted` hides that bubble by seq (a
  duplicate caption stays); `steer_preempted` is silent. A retract that
  arrives before its `steer` (history paging) still hides the later row. A `resumed` event keeps leftover sub-agents running
  — a second `spawned` for the same id is the roster coming back, not a twin —
  closes tools left pending so a killed `exec` does not keep spinning, and
  dismisses a leftover tool-round confirm so a crash does not look like
  the turn is still waiting for a click. Engine tests assert the matching
  `tool_result` (`err`: the previous process stopped) is stored before `resumed`.
  A `rewound` event (and `rewindTranscript`) drops
  that `user_message` seq and everything after it without lowering `lastSeq`.
  `placePendingEdit` puts the edited text back at the cut so the bubble stays
  while the replacement `user_message` is in flight. Both are pure functions,
  Standing-objective notices (`goal`, `goal_continued`,
  `goal_idle`, `goal_session`, `goal_resumed`, compact) live in `transcript-goal.test.ts`: auto-continue
  starts the next turn's clock, a parked worker stays running across manager
  `done` and `goal_continued` so Agents cannot say Done while `wait_agents`
  is still blocked, a forced session end does not paste its JSON,
  a session wrap-up `steer` is omitted so it cannot look like human guidance,
  a `goal_capped` interrupt payload notices as paused without pasting `interrupted`
  and both cap and idle notices name Start so a budget pause is not a crash,
  the briefing JSON never becomes a chat row (`detail` holds `summary` so an
  icon can open it), and auto-compact shows compressing then the token counts
  instead of the payload. Scheduled-task kinds live in `transcript-schedule.test.ts`:
  `schedule` / `schedule_fired` / `schedule_cancelled` are manager notices (not
  `user`, not the JSON payload or raw id), `schedule_skipped` is a no-op,
  a quiet `schedule_report` drops that turn's chat bubbles and later events
  cannot grow them back, an omitted report whose `done` text is empty (or
  whitespace) after `schedule_fired` is the same quiet drop, a findings `done`
  or `schedule_report` keeps the fired chip then the answer (including when
  empty `done` follows a findings report), an armed `schedule` plus empty
  `done` keeps the wait notice, and an ordinary empty `done` stays
  visible. `compact-notice.test.tsx` clicks that icon.   `schedule-notice.test.tsx` clicks Run now and Cancel wait on an armed wait when `detail` is a `sch_` id (including a padded id matching the store row) and does not treat a
  cancelled notice as a briefing.   `schedule-view.test.ts` is why the inbox
  filters All / Active / Paused / Completed (`inboxFilteredSchedules`; Active
  is the default tab) and search matches title or prompt, empty titles fall back to `prompt`, an armed
  chip paints the store before GET, a replayed arm chip cannot rewind a
  later `next_run_at`, a fire marks that conversation's live
  wake done, and a stale empty GET cannot wipe the
  open conversation's live wait.   `schedule-inbox.test.tsx` /
  `schedule-dest.test.ts` cover Runs in (standalone vs a thread wake), a
  Create drawer with a Task field and no Title, click-to-edit (`schedule-edit-drawer`)
  that PATCHes title / prompt / cadence without resending an unchanged interval,
  pause/run-now in that drawer, a finished wait read-only, and
  `schedule-banner.test.tsx` / `app-schedule.test.ts` cover the sidebar
  page control (`aria-current="page"` while open, unread in the accessible name),
  pause/run-now/create labels, unread badge, busy run-now `skipped_busy` as a
  labelled alert on the scheduled page, the composer wake banner's labeled
  Run now / Cancel wait (`schedule-wait-actions.test.tsx`), the banner hiding
  while the conversation is working, a parked wait painting a breathing clock
  on the sidebar row and Waiting on the title bar (`waitingThreadIds`,
  `wait-mark.tsx`), run-now painting the open conversation Working,
  a `schedule_skipped` stream event refreshing the list, and a silent
  schedule GET that must not toast. The inbox is the main column (`schedule-page`,
  `absolute inset-0`); it defaults to Active; Completed
  lists done and cancelled waits. The conversation side panel is hidden
  (`side-panel`). Create opens `schedule-create-drawer` so the form does not
  sit on the list; Expand fills the column (`data-expanded`, list pane hidden;
  Escape collapses first). The form is a Task field plus Details (Runs in:
  new conversation each run, or wake a listed chat; Project only for a minted
  conversation) and Frequency (Repeat + delay/every/cron). Click a row for
  `schedule-edit-drawer` (title in the header, Task + locked destination +
  cadence; Save is dirty-only). Pause keeps a selected wait on the Active
  tab while the editor is open. Escape then closes the drawer, then the page. Several unread fires on one wait collapse
  to one Open findings control (a count when there is more than one) so
  finished reports stay off the row (`unreadFindings` newest-first; opening
  a conversation marks every unread fire on that thread; wait rows are
  `shrink-0`). A `session_memory` event is
  quiet on the manager like a generated title — no chat row, no extra worker.
  Finished `goal_session` /
  `goal_continued` turns fold behind a one-line Worked-for row in
  `transcript-session.test.tsx` (CJK preview truncates; duration does not wrap;
  an armed wait and a budget-cap notice stay visible outside the fold;
  the hover clock is the last finished answer, not every answer in the turn).
  A `goal_capped` / `goal_idle` / `goal_blocked` notice after that work stays
  outside the fold so a budget pause cannot hide behind Worked-for.
  A failed session stays expanded as Stopped-after with the error visible,
  and a collapsed preview prefers that error over the last answer.
  Folded successful work is not mounted until the row is opened, so a long `/goal`
  cannot re-parse every past answer on each streamed token.
- **`src/lib/turn-nav.ts`**, **`src/lib/use-turn-jump.ts`** and **`src/components/app/turn-nav.tsx`**: human
  `user_message` rows become jump targets (steering does not; `goal_continue`
  and `schedule_continue` share the last human tick even when `user_text` is
  the protocol prompt). Ticks pack into a compact
  cluster in the middle of the pane rather than stretching it, and a long
  conversation shares a fixed height instead of overflowing a second
  scrollbar. The hover list is a wider two-line preview of those sends, not a
  shadowed popover of fake bubbles. The active tick
  is the last human turn that has entered the viewport (not a 96px line
  from the top — a wheel-up used to keep an earlier tick current while
  the latest send was already on screen), or the
  latest turn when the scroller is at the bottom or still following the live
  edge (opening a conversation used to measure at scrollTop 0 and keep the
  first tick current). A click sticks until the reader wheels again, and
  the pending jump is re-applied after a lazy prepend (clearing it on the
  first scrollIntoView used to swallow the click). A click pins the user
  send at the top of the pane (`scrollTop` on the transcript scroller —
  `scrollIntoView` on the turn group left earlier work peeking above the
  first tick). Turns not yet mounted are skipped rather than treated
  as offset 0, and a
  missing id after a conversation switch is a no-op. The rail stays hidden until
  there are two user turns; hover opens the list; a click (or arrow keys) jumps.
  A click whose row is not mounted yet pages then scrolls after commit — not
  in the fetch callback.
- **`src/lib/thread-log.ts`**, **`src/lib/use-history-window.ts`**, **`src/store/thread-history.ts`**, **`src/lib/welcome.ts`**: opening a
  conversation paints one viewport of the live edge (`GET /log`) and resumes
  SSE after that seq; scrolling up prepends older pages without jumping the
  viewport. The top sentinel pages even when IntersectionObserver fires in
  its 80px lead (`scrollTop` still 80), and a later scroll to 0 pages even
  if that first intersection already happened — IO does not re-fire while
  the sentinel stays on screen. A scroll also pages once the reader has left
  the live edge and the oldest loaded turn-nav row is inside one viewport
  (or the scroller is already inside that top viewport). `scrollTop < 48`
  alone left that row on screen with older history still above it. An older
  page that does not move the seq cursor clears `has_more`. Prepend keeps the same row on screen unless
  the reader already reached the top, in which case they stay on the newly
  loaded rows. A jump to an unloaded turn keeps paging until that user row exists
  (`use-turn-jump`: the scroll waits for the row to mount — scrolling in the
  fetch callback missed; a click that already found its row stays pending
  across a lazy prepend, because a sentinel page started at the top used
  to land after the jump, skip restore, and swallow the click; `loadUntilTurn`
  joins an in-flight sentinel page
  instead of treating `historyLoading` as the end of the log, and fetches max
  pages rather than the 24-row viewport).
  A worker-only tail keeps paging until a non-quiet, non-spawn manager row exists so the
  empty-state idea cards cannot cover a running conversation. The live-edge
  page also folds a `roster` sidecar (`spawned` / `finished` / `cleanup`
  outside the viewport) so a manager-only tail does not empty the Agents tab.
  Roster sidecar rows do not become manager "Started" lines. Opening a
  worker off the live edge fetches `GET /api/threads/:id/agents/:agent/log`.
  `isWelcomePane`
  stays false while a turn is running, workers are on the roster, or older
  history is still above the tail.
- **`src/store/app.ts`**, against a fake API: which conversation an action lands
  in when New conversation is clicked twice, when a send races a slow create,
  and when a file is dropped on the empty state; sending while a conversation is
  still being created must wait for it, or the turn runs in the conversation the
  user just left; Enter while a turn is running queues a follow-up instead of
  steering, except the live turn's own user text (a leftover Enter / IME echo
  must not enqueue the request that just started); ⌘Enter / `{steer:true}` injects now
  and drops a queued copy of that text; `{fromEventSeq}` starts a turn
  (never a follow-up) after clearing everything below that bubble and leaving
  the edited text in place, an edited follow-up is moved to the back of the
  queue, and an idle enqueue falls
  through to starting a turn; a title event renaming the open conversation (and a failed
  namer leaving the placeholder, and a `done` `refreshThreads` whose GET
  is still `title_auto` — or omits the field — not stomping that name — `src/lib/thread-title.ts`); `refreshCatalogs` rediscovering every
  endpoint with a URL without rebooting the open conversation; a live
  `usage` event filling the composer snapshot (reload reads it from GET);
  and opening a conversation whose live-edge page is only worker tools pages
  older events until a manager row exists before painting as loaded.
  Switching away from a live turn keeps `thread.running` on that row so the
  sidebar progress does not wait for you to click back in. `setThreadRunning`
  lives in `src/lib/thread-title.ts` and also stamps `awaiting_answer` so a
  blocked question is not a working pulse after you leave. A listing refresh
  is the source of truth for every other row: `mergeThreadList` takes
  `running` / `awaiting_answer` / `waiting` from `GET /api/threads`, and only the open
  conversation keeps a live overlay so an Enter/`done`/arm race cannot flicker.
  That is how a `/goal` auto-continue or a schedule fire on a conversation
  you are not looking at still lights the folder without a click — and how
  a finished background turn goes dark. `startSidebarSync` re-reads the
  listing every 2s while the window is visible (`src/lib/sidebar-sync.ts`);
  a hidden tab skips the tick, becoming visible is an immediate catch-up,
  and a dropped packet does not toast (`syncThreads`). A parked thread wake is not `running`:
  `waitingThreadIds` from the schedule list plus listing `waiting` paints the breathing clock,
  and `status.waiting` from GET/SSE (kept across `done`) is why the title bar says Waiting
  instead of Idle while `/goal` is parked on that wait. A live `schedule` event sets
  `waiting` even before GET returns; a stale GET cannot wipe a newer arm
  (`scheduleFetch` generation plus `keepArmedWakes` in `app-schedule.ts`). The goal banner repeats that this is
  parked, not an error. An armed-wait notice is pinned after the work of a finished
  turn so it does not sit above a long report.
  `askingThreadIds` unions listing `awaiting_answer` with the open
  conversation's live status.
  Live SSE folding lives in `src/store/app-stream.ts` so `app.ts` stays under
  1000 lines. `title` flushes immediately like `done`. A live `compacted`
  `phase: "start"` pulse sets `status.compressing` (title bar Compressing,
  composer banner) and does not mark the thread compacted; `done` clears it.
  `app-steer.test.ts` is why Interrupt-inject and per-bubble retract hit the
  open conversation and swallow `no_steer` / `404` races.
  `app-followup.test.ts` is why a duplicate send of the live turn does not sit
  in the tray, why Steer on a row / ⌘Enter of the same words empties that copy,
  and why a stale follow-up GET cannot resurrect a steered row.
  `followup-sync.ts` holds the generation token and the text match so
  `app.ts` stays under 1000 lines.
  A roster sidecar of `spawned` rows must not stop that paging or paint a
  wall of "Started" lines. Opening a worker off the live edge fetches
  `GET /agents/:agent/log` even when paging already claimed the log was
  complete but that worker's pane is still empty
  (`src/store/app-history.test.ts`). Opening paints the tail and resumes
  the stream after that seq (also here, split from `app.test.ts`). A jump
  to a turn still above the tail pages until that user row exists, and
  joins an in-flight sentinel page instead of bailing.
- **`src/store/app-goal.test.ts`**: `/goal` pursuing until `complete_goal` or
  `block_goal` (edit and resume from the banner, including Start after a
  mistaken complete); setting a standing
  objective while idle starts that turn; setting one during a run does not
  queue a second turn; `/compact` landing on the
  open conversation, with the hint hidden at 0% (and a compact with nothing
  open being a no-op); a live compressing pulse (`seq` 0) not marking the
  conversation compacted. A `done` then `goal_continued` (or `goal_resumed`)
  keeps `status.started_at` so the title-bar Working clock is the current
  turn, not a frozen 1s. An `error` then `goal_blocked` idles the composer
  and marks the objective blocked, so a crashed `/goal` turn is not still
  Working — a real refusal, not truncated tool JSON / `429` / a dropped
  stream (those `model_retry` then auto-continue). A `goal_idle` event holds the banner until a human message or Start.
  Split from `app.test.ts` so neither file crosses 1000 lines.
- **`src/store/app-plan.ts`**: `/plan` enters planning and starts a turn with
  an argument (not during a run, not when the argument is empty); a human
  edit PATCHes the body; Leave does not start a turn; Implement starts one;
  ask_user answers POST the open conversation; nothing open is a no-op. A
  live `plan_updated` folds into the banner thread. Split from `app.test.ts`
  so neither file crosses 1000 lines.
- **`src/store/plan-events.ts`**: thread flags for `/plan` events live here so
  `app.ts` is not the owner of that protocol.
- **`src/lib/transcript-review.test.ts`**: memory review notices, title and
  session-memory rows, rewind, `ask_user` cards, and plan notices. A merged
  skill is named in the transcript the same way a recorded one is. Split
  from `transcript.test.ts` so neither file crosses 1000 lines.
- **`src/lib/transcript-ask.ts`** and **`src/components/app/ask-card.tsx`**: an
  `ask_user` tool_call becomes a question block; the host injects Other; a
  result settles the card; a numbered choice plus Submit POSTs answers. The
  card is `w-full` in the conversation column (no `max-w-*` dialog cap).
  A pending card says **Your answer needed**, carries a still `ask-mark`
  and `border-ask` (no ring or ping on the dialog); a settled card drops
  both. Sidebar and title-bar `ask-mark` still ping.
- **`src/components/app/plan-banner.tsx`**: Planning, editable markdown,
  Implement, Leave. Same one-line composer pin as the goal banner. Split
  from the goal banner so neither file grows past 1000 lines.
- **`src/store/goal-events.ts`**: thread flags for `/goal` events live here so
  `app.ts` stays under 1000 lines. `goal-events.test.ts` covers the idle hold.
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
- **`src/lib/work-fold.ts`**, **`src/components/app/work-fold.tsx`**, and
  **`src/components/app/swap-line.tsx`**: user mode groups consecutive
  thinking / tools / spawns; developer mode is a flat list.
  Answers stay visible and split the group. `ask_user` / errors / notices split the group. A live ticker is one
  current activity on the newest running turn's latest work row only
  (a pending tool beats a streaming thought; a live gap with neither is
  **Planning next moves**). An answer after that fold is the tail, so the
  fold keeps its count; Planning renders under a closed answer and stays
  off while the answer is still streaming. Older folds keep the thought / tool count.
  Copy is **Thinking** or the latest thought line, **Editing** / **Reading** /
  **Exec** `{name}` for file and shell tools. `SwapLine` always wraps `MarqueeText` so a
  long line still scrolls left-to-right; changing the activity key slides
  the previous line up.
- **`src/lib/chart-spec.ts`**: a `chart` fence body is JSON for bar / line /
  area / pie. Parallel `labels`+`values`, a values object, and missing x/y
  still parse; a single row, an unknown type, and junk do not; a truncated
  object is incomplete so a live fence can wait; rows and series are capped.
  Two parses of the same fence compare equal so a live answer does not treat
  a new object as a new chart.
- **`src/components/app/transcript-chart.tsx`**: a valid spec is a figure with
  Chart / Table tabs (plot first; the table is the same rows, not a
  screen-reader-only dump); a live incomplete fence is a
  placeholder; fills and axes use `var(--chart-*)` / muted / border tokens
  so a theme switch restyles the same plot. Re-rendering with a new but equal
  spec object keeps the same SVG; the plot only redraws when the spec or the
  measured box size changes.
- **`src/lib/stream-markdown.ts`**: trailing `**` / `` ` `` / `~~` in the
  current prose region are closed so a streaming answer can render; an open
  fence is left alone (it is already a code block), a marker with no
  content yet is not turned into `****`, and an unclosed `$$` with a body
  is closed so KaTeX can paint live (`$HOME` is not treated as math).
- **`src/components/app/markdown.tsx`**, rendered in jsdom: a streaming
  `## heading` is a heading, trailing bold renders as `<strong>`, a
  finished answer is not rewritten, a `chart` fence paints, a `json` fence
  does not, a live truncated chart fence is a placeholder, a finished
  invalid chart fence stays code, and a closed chart keeps the same instance
  while later tokens append prose after it. A `go` fence paints keywords
  onto tokens and copies the original body; `cpp` maps onto the C
  highlighter; an unlabeled fence stays plain and still copies; `$n$` /
  `$$…$$` and a `math` fence render as KaTeX.
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
  force it back open.   User mode (`transcript.test.tsx`, `transcript-fold.test.tsx`, `work-fold.test.tsx`)
  folds adjacent thoughts and tools behind one `WorkFold`; answers stay
  visible and split the group. The live summary is a `SwapLine` of **Thinking**
  / the latest thought line, **Planning next moves**, or **Editing** /
  **Reading** / **Exec**, still on `MarqueeText`.
  Developer mode is the previous every-row log. Opening an `exec` row wraps the full command with shell
  highlighting instead of leaving it truncated (`transcript-exec.test.tsx`).
  A pending `exec` does not paint stdout until that click
  (`tool-row.test.tsx`); the latest line stays on the summary. A finished
  non-exec tool that was watched live folds unless the reader opened it.
  A refused `memory` write shows the refusal on the collapsed row, not only
  inside the disclosure. An exec still open when the turn is interrupted loses its spinner rather
  than running forever.   Hovering a user bubble or the last finished answer fades in
  the event clock without changing the row height (`transcript-message-time.test.tsx`); copy and a pencil sit
  under each user bubble (copy is hidden when there is no text); a denied
  Clipboard API still copies via `execCommand` planting `text/plain` on the
  `copy` event (`copy-text.test.ts`); a silent `true` is not Copied; edit opens
  the bubble in place and Send restarts from that `user_message` seq, clearing
  everything below (`transcript-user.test.tsx`).
  A compact notice stays a one-liner; `compact-notice.test.tsx` opens the
  briefing from the icon without pasting it into the row.
  Each user send carries `data-turn-nav` so the rail can jump (the thought
  fold is not inside that target); one turn
  keeps the rail hidden, two turns show it. A live thought's top fade is
  trapped in its own stacking context so it cannot cover the rail.   Unread steering is pinned below
  the heartbeat (`queued-steers`), not above the working line; a steer the
  manager has already read stays in the turn body. That pin has Interrupt
  (all unread) and per-bubble Delete (`queued-steers.test.tsx`). Auto-follow unpins on
  wheel-up even inside the old 80px slack; new tokens then show a jump-to-latest
  control, and a skeleton→loaded remount still attaches the listener. Switching
  conversations re-pins even if the previous one was scrolled up; a layout
  scroll at 0 while the scroller is opening does not unpin.
- **`src/lib/follow-scroll.ts`**: whether the reader is at the live edge, whether
  a jump control should show (only after content arrived while unpinned), and
  whether a wheel started in a nested scroller (a thought, a tool payload)
  should leave the transcript pinned. `reset` restores the initial pin when the
  open conversation (or the open sub-agent) changes.
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
  control that would send a meaningless choice.   The input is owned by the
  composer, not the app shell, so typing cannot re-render the transcript.
  A live `usage` pulse is read by the ring child (`ComposerContextMeter`),
  not by the textarea's parent, so an in-flight turn cannot rewrite the
  field while a CJK IME is composing. Auto-resize skips `height: auto`
  during composition (`resizeComposerArea`); the confirming
  `compositionend` measures once.
  Enter while composition is live, or on the key that just confirmed it, leaves
  the draft in the box; the next settled Enter sends. A CJK IME writing the
  committed string back after send is swallowed so a leftover Enter cannot
  queue the live turn; a different draft still sends.   The chrome is a fade over
  the transcript, not a top border, so a docked toolbar cannot regress in.
  Goal / plan / wait pins sit in the dock with the input; a slab behind
  that plate hides chip gaps, and the fade is only the join above it.
  The draft uses `--ui-font-size` (content type), not Textarea `text-sm`.
  The model control is always a switcher, even with one ready name.
  ⌘Enter marks the send as steer; Enter while running queues. The Queued tray
  (`queue-tray.tsx`) names the count, edits a row in place (Enter saves and
  that message goes to the back of the FIFO; Escape cancels), steers or drops
  a row after a confirm, and Clear queue asks first.
  File drop onto the box (`composer-drop.ts`) arms a dashed overlay, then
  splits images into vision thumbs and other files into workspace chips
  (`composer-attachments.tsx`); a disabled composer ignores the drop.
  Send names the uploaded paths on the turn (`files` on `POST /turns`); a
  failed upload keeps the chip and does not start a turn.
  `/plan` waits for the work after picking the command (the box shows
  `/plan ` plus a hint); `/plan <task>` submits in one go. Picking **goal**
  writes `/goal ` the same way — wiping the token looked like a missed
  click. While a turn is running the corner button is Stop until the draft
  is submittable (`composerCornerActions`): text, a quote, a file, or an
  image replaces Stop with Send, and clearing the draft brings Stop back.
  Whitespace stays Stop. A `/goal` or `/plan` prompt that still needs its
  argument keeps Send disabled beside Stop; once the argument is there,
  Stop leaves. While `awaiting_answer` the placeholder is the Other prompt.
- **`src/components/app/goal-banner.tsx`**: Pursuing / Blocked / Paused / Done,
  Start when Paused, Blocked, or Done (not while Pursuing between sessions),
  a Paused/idle reason line that this is not an error and Start resumes,
  a Done reason line if the close was early,
  icon Start with an accessible name (Codex-density pin above the composer),
  inline edit that saves on blur and cancels on Escape, and
  elapsed time from `goal_started_at`. A failed-turn sentinel is localized
  and yields to the turn's public error when that is on the transcript.
  A parked wait does not reprint the wait essay — the schedule chip owns that.
- **`src/lib/models.ts`**: choice ids round-trip through a tab separator,
  `groupModels` keeps server order, and auxiliary choices skip blanks.
- **`src/components/app/model-settings.tsx`**, rendered in jsdom: providers
  start as a collapsed list (name, model, default badge) so a second
  endpoint is not a wall of fields; opening a row reveals Provider / URL /
  key / default; Add a provider appends a row and opens it; a settings
  search that matches a field opens the matching rows. Discover fills the
  default-model dropdown from the catalog. A catalog of two names gets two
  window fields — typing one must not write the provider fallback.
  Removing a provider asks first. A failed listing toasts over the sheet
  (× dismisses it) instead of a red line under the Models heading.
- **`src/lib/composer-chrome.ts`**: the composer writes `--composer-pad` onto the
  conversation stage from its own height; a zero height (jsdom) leaves the CSS
  fallback so a unit test cannot collapse the transcript into the box.
  `composerPadPx` is dock height plus join air so goal / plan / wait pins
  stay in the dock (the slab behind it hides chip gaps) and the last answer
  is not under a % opaque hang. Fade overhang is px on the same stage so
  content font-size cannot grow a rem wash. `resizeComposerArea` skips the
  `height: auto` measure while `composing` is set, so an IME candidate
  window is not laid out on every preedit key. Overflow is `hidden` until
  `scrollHeight` exceeds the cap, then `auto`.
- **`src/lib/tool-view.ts`**: a built-in tool's JSON args collapse to the
  command / query / path the user needs to see, `execCommand` keeps newlines
  so an expanded row can show a heredoc, `exec` payloads become stdout
  plus a failed flag when the exit code is not 0, and `web_search` payloads
  become a list of hits. An `edit` or `write` summary is the path;
  `+N −M` lives on `ToolView.diff` (that hunk, parsed once) and the row
  paints it as a sibling of the path. An empty write is the path with
  no `+0`. A refused memory write puts the refusal on the collapsed
  row (`toolRowSummary`), not only inside the disclosure. Assertions
  check structure, not any particular query.
- **`src/lib/edit-diff.ts`**: an `edit` call's `search_block` / `replace_block`
  (or apply_patch `patch`) becomes line hunks. Shared prefix/suffix stay
  context; the middle is an LCS (capped so a huge replace cannot freeze
  the transcript). An empty `replace_block` is a deletion hunk (`−N`, no
  `+0`). A `write` call's `content` is all additions; empty
  content is still a hunk (zero lines). `clipDiff` keeps the first 400
  painted lines and reports the rest as hidden. The tool result is ignored
  — it is only a status sentence. CRLF matches LF. A path-only call has no
  hunk. `parseFileChange` dispatches edit/write and ignores other tools.
- **`src/lib/shell-highlight.ts`**: an `exec` command tokenizes into
  keywords / strings / flags / variables without dropping characters; a
  too-long line falls back to plain text.
- **`src/lib/math-fence.ts`**: `math` / `latex` / `tex` / `katex` fences are
  formulas; `go` / `chart` / empty are not.
- **`src/lib/source-highlight.ts`** / **`src/lib/source-lang.ts`**: a `read`
  body tokenizes from the path suffix into the same token kinds; concatenating
  tokens reproduces the file; markdown and unknown suffixes are left alone.
  A markdown fence info-string (`cpp`, `javascript`, `python`) maps onto
  the same dialects; `chart` / `math` / unknown tags stay undefined so the
  body is not guessed. A C `#include` line is a keyword, not an operator.
- **`src/lib/read-result.ts`**, a `read` tool payload becomes a file listing:
  newline-separated bodies, and the older flattened one-liners, both recover
  the path and the lines; other tool output is left alone.
- **`src/components/app/tool-row.tsx`**, rendered in jsdom: a pending `exec`
  does not paint `tool-output` until the row is opened (latest line on the
  summary); a finished exec stays collapsed unless the reader clicked; a
  pending `web_search` still opens and folds when the result lands; a
  finished `edit` or `write` stays collapsed (`+N −M` beside the path,
  muted until hover/focus) until the reader opens it, including while
  the call is still pending.
- **`src/components/app/tool-result.tsx`**, rendered in jsdom: a markdown `read`
  renders headings, a `*.go` `read` paints keywords onto tokens and keeps line
  numbers, an unknown suffix stays uncoloured, `exec` shows
  the full wrapped command plus stdout (and a role=alert error when it failed),
  `web_search` lists hits instead of dumping JSON, and an `edit` / `write`
  paints the reconstructed hunk (not `ok: replaced block` / `Updated file`)
  with add/delete rows. A failed call keeps the error above that hunk. An
  empty write shows the empty-file line and not `+0`. A 401-line write
  paints 400 rows plus the leftover count.
- **`src/components/app/source-code.tsx`**, rendered in jsdom: a numbered
  listing maps token kinds onto the syntax CSS variables.
- **`src/components/app/edit-diff.tsx`**, rendered in jsdom (via `tool-result`
  and `edit-diff.test.tsx`): add/delete rows carry `data-diff`, a `*.go`
  hunk still paints keywords, an empty write shows the empty-file copy,
  and `EditCountMarks` inherit the row until `group-hover` /
  `group-focus-within` split add from delete. Tokenizing is per painted
  hunk, not per table row.
- **`src/components/app/shell-command.tsx`**, rendered in jsdom: the expanded
  command wraps instead of truncating, and the collapsed preview stays one
  line.
- **`src/components/app/sidebar.tsx`**,
  **`src/components/app/sidebar-section.tsx`**,
  **`src/components/app/sidebar-slots.tsx`**,
  **`src/components/app/sidebar-thread-row.tsx`**, and
  **`src/components/app/sidebar-thread-group.tsx`**, rendered in jsdom
  (`sidebar-slots.test.ts` pins chrome-relative density, not rem): the list
  starts with New conversation; there is no title-bar chrome row and no hide
  control — those live on the window title bar. Settings is a full-width
  rounded pill at the bottom that fills `sidebar-accent` on hover. Projects and Recents share
  `sidebar-section-label` (same px gutter as the rows). Wrapping the project
  section in a second `px-2` is rejected so it cannot sit further in than Recents. The list starts at
  256px, the arrow keys change that width (CSS variable, not a React `width`
  style), a remembered width is restored, and the resize strip sits on the
  right edge (`z-20`) with the aside stacked above the transcript (`z-10`) so
  the 4px overlap is not painted over by the main column. Dropping one Recents
  row on another reports the new id order; a drag that starts on the row menu
  is ignored. A title click still opens when a dragstart races it; dragging
  the title past 8px reorders. There is no grip glyph. A pinned project
  topic sits in Pinned and still under its folder. The folder itself is
  not pressed; the open topic is `aria-current`. The folder icon is the
  fold control: open directory vs closed directory. A running
  conversation's progress sits in that same icon column, including after
  switching away and when a turn starts on a conversation you have not
  opened (`GET /api/threads` `running`, re-read while the window is
  visible), and an explicitly collapsed folder that still has a live
  turn keeps the progress on the directory glyph, clipped so the breathe
  animation cannot smear the icon. A blocked `ask_user` replaces that
  progress with a pinging question mark (`ask-mark`; the ping is allowed to
  overflow). Recents,
  Pinned and nested titles keep a `sidebar-kind` spacer so they share a
  column with the project name. A section chevron is hover-only
  while that section is open. Folder and topic rows are `sidebar-row`
  (`--sidebar-row-height` from UI chrome size, not rem) with a 2px stack gap so the
  selected pill floats; row menus stay `icon-xs` so they cannot pad the
  list out. Content font size must not shrink that directory; UI size grows it
  (12px chrome → 26px row, 13 → 28, 16 → 34). Clicking Pinned, Projects,
  or Recents folds that section (`aria-expanded`);
  a reload keeps the fold. A project folder and Recents show at most
  five conversations from the last seven days; **Show more** reveals
  the rest and **Show less** folds them. The open or running
  conversation stays in the preview. Deleting a conversation asks first.
- **`src/lib/sortable.ts`**: the row is never HTML5-`draggable`. A title
  click still opens; a pointer move of 8px from the title reports the move;
  a twitch under that threshold is still a click. Synthetic row `dragstart`
  (jsdom / Playwright) still reorders. A dragstart with no `dataTransfer`
  is ignored. A disabled bind does not arm.
- **`src/lib/reorder.ts`**, **`src/lib/sidebar-groups.ts`**,
  **`src/lib/sidebar-preview.ts`**,
  **`src/lib/sidebar-collapse.ts`**,
  **`src/lib/thread-title.ts`**, **`src/lib/sidebar-sync.ts`**: moving a row is a splice; Recents and a
  project sort their own ranks, then unranked rows interleave by last
  activity so a stale global cannot sit above a ranked topic that just ran.
  Pinned order is `pinned_at`. A folder without an override follows
  the open conversation (or a selected empty project, or a folder that
  has a running conversation); an explicit collapse still wins. Garbage storage is
  an empty map. Pinned / Projects / Recents default open; a remembered
  fold is the three booleans in `zwai.sidebar.section-expanded`. A
  sidebar group preview keeps the first five conversations active in
  the last seven days; older or extra rows sit behind Show more unless
  they are the open or running conversation. `mergeThreadList` takes
  listing `running` for background rows and only overlays the open
  conversation; `startSidebarSync` re-reads the listing while visible.
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
  lives on the composer. A running turn counts from `status.started_at`
  (hours included); a missing start time is `Working` with no invented 1s.
  `awaiting_answer` is **Your turn** with `ask-mark`, not Waiting / Working;
  `awaiting_continue` stays Waiting. A width control flips `comfortable` / `full` the same way the theme
  control flips light / dark; `aria-pressed` is on while the column is wide.
- **`src/lib/settings-persist.ts`**: edits coalesce into one `PUT` after
  400ms; `flush` writes immediately; a failed write does not block the next.
  The `ui` object always carries locale, font, size, column width and palette so a
  swarm edit cannot reset General.
- **`src/lib/appearance.ts`**: chrome tokens (`system`/`serif`/`mono`,
  `small`/`medium`/`large`, `comfortable`/`full`, `zwai`/`fofa`) map to CSS variables and
  `data-*` attributes. Junk becomes the current defaults. A `localStorage`
  cache paints the first frame; `GET /api/meta` is the source of truth.
  `toggleContentWidth` is the title-bar / ⌘K flip. `--content-gutter`
  shrinks in `full` so the column sits against the sidebars. UI size
  writes `--chrome-font-size` and the directory density tokens
  (`src/lib/chrome-density.ts`); content size writes `--ui-font-size`
  (`ui` follows chrome) and must not change row height. Code size writes `--code-font-size`.
  `palette` writes `data-palette` so `index.css` can swap the FOFA token sheet.
- **`src/components/app/theme-cards.tsx`**, rendered in jsdom: Settings →
  General paints System / Light / Dark preview cards and a Color theme
  menu (ZWAI / FOFA). The cards restyle with the named set.
- **`src/lib/chrome-type.ts`**: the shared chrome-size regular class for window
  chrome (sidebar, Settings, title bar, composer controls), plus the
  muted opaque chip (`composerPinClass`) for goal / plan / wait above
  the composer so transcript lines cannot show through. Content type includes `--ui-font-size` so it beats
  Textarea `text-sm`.
- **`src/components/app/settings-dialog.tsx`**, rendered in jsdom: Settings
  is a full-page sheet (`h-dvh`) with **Back to app**, a labelled search
  box, and a left rail of tabs. Opening the sheet lands on **General**
  unless the caller passed `initialSection` (the model picker uses Models).
  The dialog title is screen-reader only —
  the visible page heading is the section name (General, Models, …). There
  is no Save/Cancel: edits debounce into
  `PUT /api/settings` (locale rides along) and **Back to app** flushes.
  Searching a Swarm-only word jumps to that
  section and hides Models. The Personality tab writes personal preferences
  into `personality.instructions`. The Swarm tab exposes compact keep-messages,
  the auto-compact token budget. The selected page is a `tabpanel` switch
  (not Radix `TabsContent`, which hid every pane after settings loaded).
  Every page scrolls with `overflow-auto` and
  bottom padding, so an outline at the
  end of the tab (Add a provider) is not clipped. `overflow-y-auto` is
  rejected because it would leave the shared `overflow-hidden` in place.
  With `trafficInset`, an `h-12` `data-drag-region` strip sits above the
  rail so **Back to app** is not a descendant of that chrome (macOS
  traffic lights live there); a browser sheet has no strip. The sheet is
  `modal={false}` so Radix does not `aria-hide` the conversation underneath.
  A failed write toasts and keeps the sheet open.
- **`src/components/app/settings-remote.tsx`**, rendered in jsdom: a pairing
  offer paints a QR; a stored host token is never echoed; a failed offer
  toasts over the sheet instead of a red line under Phone; **Keep this
  computer awake** defaults on and toggles `remote.keep_awake`. A binding
  with a reported model paints that line, not a bare fingerprint.
  `src/lib/remote-binding.ts` is the title / last-seen join.
- **`src/store/settings-sheet.ts`**: sidebar / ⌘, / ⌘K open General;
  a provider shortcut opens Models.
- **`src/store/toasts.ts`**: same-id replace, a cap of 4, `toastError` as
  the settings failure helper.
- **`src/components/app/settings-field.tsx`**, rendered in jsdom: a row
  puts the label left of the control; a query hides non-matching rows;
  a section with no remaining rows is omitted. Title and hint share
  chrome type (`--chrome-font-size` / `font-normal`; color is the hierarchy).
  Menus hug their value (`w-auto`, `bg-background`, same chrome type). The
  page column is centered (`max-w-3xl mx-auto`, `gap-8`).
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
  the transcript must not paint through the back button. The worker log
  lands at the latest line, re-pins when switching workers, follows tokens
  at the live edge, and a wheel-up leaves the viewport put until Jump to
  latest. A prompt control opens the worker's recorded instruction and is
  absent when the event only stored the role name. The roster lists
  `agent_id` next to the role so two workers with the same name are
  distinct. The resize strip sits above that chrome (`z-20`) and the arrow
  keys still change the width. A pointer drag on the strip must not start a
  text selection. The Files pane is a flex column (`overflow-hidden`) so the
  filter stays put while the tree scrolls. Inactive Files/Agents panes are
  `hidden` when Memory is selected, and the active pane is an opaque
  `bg-card` so a leaked flex sibling cannot paint through the skill list.
- **`src/lib/selection.ts`**: while a gesture holds the pointer, existing
  ranges are cleared and `selectstart` is cancelled, then the previous
  `user-select` is restored. Dragging a panel over a transcript is a selection
  as far as the browser is concerned.
- **`src/lib/titlebar.ts`**: a double-click on the desktop title bar (not on a
  button in it) sends `wails:drag:doubleclick` so the native window zooms; a
  browser has no bridge and the click is left alone. Dragging is native and is
  not this module.
- **`src/lib/slash.ts`** and **`src/components/app/slash-menu.tsx`**: `/` as a
  token (start of the box, after whitespace, or after CJK — no space yet)
  is a menu; a space after the name makes it a submit. `foo/bar` and
  `https://` are not a menu. A known command name followed by a non-ASCII
  token is a submit too, so a CJK objective glued to `/goal` is not a user
  message, and a fullwidth `／` or IME punctuation `、` is still a slash
  (the composer rewrites those to `/`). `/plan` is in the same catalog.
  Picking a command that needs an argument completes `/name ` in the box
  and keeps the textarea focused (menu `mousedown` is cancelled so the
  click cannot steal the caret); wiping the token is not a pick.
  Filtering is
  prefix-or-contains, compact's hint is a percentage (hidden when used is 0),
  and unknown names are not commands. The menu is a listbox; the textarea
  owns the keys.
- **`src/lib/stream.ts`**, against a fake `EventSource`: every kind the server
  sends is subscribed to (including `goal_session`), a review that lands after
  `done` is delivered, the event name wins over a disagreeing payload, and a
  connection that dropped on its own is not reported as a failure. The first of
  those is a regression test: a kind missing from the list is stored, traceable,
  and invisible until the page is reloaded. Live session fold depends on this —
  a `goal_session` that only exists in the database looks like a TurnFooter
  until refresh.
- **`src/store/projects.ts`**, against a fake API: a project deleted elsewhere
  stops being selected, a slow memory response for a project that is
  no longer open is ignored (one project's notes under another's name is worse
  than none), a refused create or edit reaches the dialog so it can show the
  message against the field that caused it, and a memory reload copies the skill
  index onto the project so the Memory tab updates without a second listing. A
  tidy click folds overlapping skills, copies the new index onto the project,
  and keeps the tidy card when the same project's memory is re-read (a catalog
  tidy records `memory_review`, which would otherwise wipe the result). The
  card is per project: a slow tidy does not paint onto the project now open,
  opening the project that was tidied shows the in-flight progress or the
  finished report, a failed tidy stays on the project that asked, a memory
  read that started during the tidy does not put the pre-tidy list back, and
  dismissing the card or deleting the project does not bring it back.
- **`src/components/app/project-dialog.tsx`**, rendered in jsdom: a rejected
  working directory is shown under that field and the dialog stays open, a
  generic save failure toasts (× dismisses it), a
  nameless project cannot be created, the instruction textarea uses the same
  `border-input` chrome as a one-line field, and the memory switch is disabled
  when the install has memory off.
- **`src/components/app/project-list.tsx`** and
  **`src/components/app/delete-project-dialog.tsx`**: the folder is not
  pressed (the open topic is `aria-current`), each row's menu is named after
  its project, a hover control on the row starts a conversation in that
  project (named after it, so it cannot collide with the list's New
  conversation), conversations nest under an expanded folder whose glyph
  is an open directory (closed when collapsed), a running topic's progress
  sits in that icon column (and stays there after switching away; a
  collapsed folder that still has a live turn keeps the progress on the
  directory glyph, clipped so the breathe animation cannot smear the
  icon), skills are opened from the row menu rather than
  listed under the name, the section does not add a second horizontal gutter
  on top of the sidebar scrollport, a sixth topic (or one idle longer than
  a week) sits behind **Show more**, dropping a project row on another reports
  the new id order (title drag, no grip), a name click still selects when a
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
  asks first. **Tidy skills** sits on the Skills heading, walks
  scan → review (held while the model runs), then a report of merged /
  deleted / created / patched names plus counts; a model that kept the
  catalog still shows the zeros.
- **`src/components/app/skill-tidy-card.tsx`** and **`src/lib/skill-tidy.ts`**:
  progress parks on the model step until the POST returns, then the card lists
  what was merged, deleted, created and patched; dismiss clears it.

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
that is already listening on the port. `ZWAI_MOCK_WORKER_DELAY_MS=1500` holds
each mock worker before its first tool call so `wait_agents` stays pending
long enough for Steer; unit tests leave it unset.

| spec | covers |
|---|---|
| `e2e/work-fold.spec.ts` | user-mode work folded behind one live ticker (`work-fold` with the spinner, not every finished fold); click expands the thought box and click again collapses it |
| `e2e/conversation.spec.ts` | a full swarm turn, user-mode work folded behind one live ticker (`work-fold` + `swap-line` + `MarqueeText` shimmer or left-to-right scroll), a live status line marked as sweeping while the turn runs, opening a sub-agent (back control beside the scroller, not sticky on it; system prompt from the chrome; log at the live edge; a collapsed `write` opens to the file body, not `Updated file`), a generated sidebar title from the opening message (not the raw request, not a transcript row), a chart in the scripted answer with Chart/Table tabs (and after reload), scrolling up mid-stream leaving the viewport put and a jump-to-latest control returning to the live edge (developer view, so spawn rows exist to overflow), switching conversations landing at the latest turn rather than the top of the history (latest jump-rail tick current), context carried across turns, jumping to an earlier user message from the left rail (latest tick current while idle at the live edge), Enter while `wait_agents` is pending queuing a follow-up until the turn finishes, the corner button swapping from Stop to Send once that draft is typed and the click queueing without leaving Working, **Steer** on that queued row injecting and emptying the tray, editing a queued row and submitting it so that message goes to the back of the FIFO, **Steer** (⌘Enter while `wait_agents` is pending) pinning unread steering under the working line with Interrupt and Delete, retracting an unread steer so the turn stays Working, Interrupt aborting the current tool without cancelling the turn, **Stop** while a tool is in flight leaving no spinner next to the interrupted banner, hovering a user request or the last finished answer fading in the reserved event clock without a layout jump, copying or editing a sent message in place so Send restarts from that bubble and clears everything below, file upload appearing in the Files panel with the user bubble naming `uploads/brief.txt`, collapsing a workspace directory in Files and filtering to a nested file, dropping a file and an image onto the composer (overlay, then a workspace chip vs a vision thumb), the turn id on the Trace summary with the event log folded until Full log, an IME-confirming Enter leaving the draft in the box, the manager tool-round cap pausing for Continue/Stop instead of dumping eino's iteration error, and switching the catalog model from a grouped searchable picker (Refresh models / Edit providers) so a reload still sends that name, and the composer context ring plus Trace usage after a turn (reload keeps the ring; the snapshot never lands as a transcript row), `/` listing goal, plan and compact without a 0% hint on an empty chat, pinning a standing objective, starting it from the banner without a human message, editing it in place, compacting without rewriting user bubbles (an icon opens the briefing), auto-compacting at a low token budget with a visible compressed notice and the same briefing icon, a scripted run with a goal finishing as Done, and a one-round ReAct slice leaving a standing objective running until Done instead of pausing it as two Worked-for sessions, `/plan` showing a Planning banner and an `ask_user` dialog that spans the conversation column (**Your answer needed**, still `border-ask`, no ring or ping on the card; **Your turn** on the title bar with a pinging `ask-mark`; a numbered choice then Submit continues the same turn), then Implement remounting work and leaving planning |
| `e2e/quotes.spec.ts` | quoting selected transcript text into the next send as an editable composer annotation (count chip at rest, hover for the snippet, not `<selected_text>` tags in the bubble), and Add to chat still landing while a turn is streaming (user-mode work fold, not the inner thought box) |
| `e2e/projects.spec.ts` | a project created from the sidebar, a conversation started from the project row that says so with the project name prefixing the title on one line, the review named in the transcript without opening a tab, **View skills** on the project menu opening the Memory tab with that skill expanded and in view (body inside its card, not over Files), the notes in the panel without a reload, the review in the same Full log as the turn, a second conversation starting with the first one's memory, a hand-edited note surviving a reload (Save notes absent until the draft changes), a deleted project taking its conversations with it after a confirm, the Memory tab not leaving a blank Agents pane above the notes or clipping Skills off the window or painting inactive Files beside Memory, Review now saying when there is nothing to review, Tidy skills saying when the catalog is empty (counts, no fake model review) and asking the model when a skill is already there (scan/review, then a reviewed report with a non-zero scanned count), hovering a project row revealing a new-conversation control that starts one in that project rather than Recents (the folder is not pressed; the open topic is `aria-current`; the folder glyph is open when expanded and closed when collapsed; a running conversation's progress sits in that same icon column; topic names sit under the project name; there is no drag-grip glyph), pinning a project topic to the top across reload, dragging a project pinning that order across reload, a sixth topic in the folder sitting behind **Show more** until it is opened, and a running conversation keeping its sidebar progress after switching to a new conversation (an explicit folder collapse keeps a clipped breathe-dot on the glyph, not a smear) |
| `e2e/markdown.spec.ts` | the scripted answer paints a tagged `go` fence (Copy code + syntax colour), `$n$` as KaTeX, a GFM table that stays inside the conversation column (not under the side panel), and a heading rendered as a heading while the turn is still Working |
| `e2e/goal-resume.spec.ts` | `/goal` on the mock provider reaches Done, then **Start** on the banner reopens pursuit (Working) |
| `e2e/schedules.spec.ts` | a standalone wait created from the Scheduled page in the main column (Create opens the right drawer, Task only — the inbox name is generated — Repeat → On an interval, Every (seconds) 60, Add wait; Agents/Files/Trace rail hidden; Expand fills the page (`data-expanded`) then Collapse restores the list), click the row to open the editor, Run now, Escape back to the conversation, unread / Open findings landing on the minted conversation whose title matches that generated name, with a `Scheduled check.` chip and no user bubble of the protocol wrapper; REST create then click-to-edit title/prompt/cadence and Save; a REST `kind=thread` wake on the open conversation showing the composer banner with Run now and Cancel wait, a breathing wait clock on the sidebar row and **Waiting** on the title bar, Cancel wait removing the chip and returning Idle, Run now starting Working, hiding the wait banner, a `Scheduled check.` chip, then Waiting again with the clock once the check finishes. Mock provider, no `ZWAI_MOCK_SCHEDULE_WAKE` |
| `e2e/remote.spec.ts` | Settings → Phone: Hub URL, no Host Token field, Event text on the phone, Events on the phone, Keep this computer awake, Bound phones empty copy, Show pairing QR, no QR pixels while the hub is unset; the failure toasts over the sheet in viewport (× dismisses it). A stubbed binding paints the reported model and last-connected, not a bare fingerprint. |
| `mobile/e2e/scan.spec.ts` | Capacitor shell Scan QR opens a live viewfinder (four corners, a beam whose `scan-beam` animation is running, a fake-camera preview); junk paste errors; a syntactically valid URI uses the same bind path; a saved ticket shows host chips and Connecting, not the scan form; Add a PC is a sheet |
| `e2e/settings.spec.ts` | Settings sheet: tool catalogue on a never-saved config, round-trip through the config file, per-note memory cap, personality, pinning a title-generation model, discovering models into the default dropdown, a failed listing toasting over the open provider, **Back to app** on a short window when Swarm is long, the Add-a-provider outline inside the Models scrollport, semantic search off by default, Color theme / font / conversation width, directory rows tracking UI size not conversation size, chrome language switching (restored to English) |
| `e2e/shell.spec.ts` | keyboard shortcuts (including hiding the conversation list, `⌘F` find in the conversation, and `⌘J` / the title-bar terminal opening a PTY in the conversation workspace — and in a project's working directory when the conversation belongs to one), dragging the conversation list and the side panel without selecting transcript text (the list width is remembered across reload and the title-bar leading cluster tracks it), the composer sitting on the transcript with a fade instead of a dock hairline (pins and the box share one slab; the join sits on that plate), Projects and Recents sharing one left gutter (conversation titles in the icon column), collapsing Recents so its conversations stay hidden across reload, an external link opening a new window instead of replacing the app, ⌘K finding a conversation by words in its body, theme switching persisted, the title-bar width control filling the pane in wide mode and restoring the reading column (also persisted), renaming a conversation and deleting it after a confirm, and dragging a Recents conversation pinning that order across reload |

E2E tests run against `frontend/dist`. Playwright starts
`go run ./cmd/zwai web --mock --no-open`, which rebuilds that bundle when the
TypeScript sources changed; `make e2e` still runs `go generate ./frontend`
first so the server is not stuck on a cold Vite build inside the 180s
webServer timeout. If you invoke `npx playwright test` directly, a stale
`dist/` is rebuilt on server start.

The progress pulse has no positive E2E assertion on purpose: even with the
worker pause, a scripted turn usually finishes near the 5-second default
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
  starts on the row below.   Opening Settings, **Back to app** must sit on the
  row under the lights, not in the drag strip. On macOS, the first desktop
  launch may prompt for Local Network access (LAN model endpoints); denying
  it looks like `no route to host` while Terminal curl still works. ⌘Q should return the shell promptly without
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
- **The phone apps**: `make mobile-ios` / `make mobile-android`. Scan QR opens
  a live viewfinder (frame, sweeping beam, chime) on the pairing QR from
  Settings → Phone. Paste is the same URI when the camera is missing. After bind, a live turn (or the last thread this phone opened)
  opens; a later launch with saved tickets shows host chips and a connecting
  skeleton, not the scan form. Otherwise the inbox is the compact screen on the same conversation
  bus (send / follow-up / steer / stop / ask); Settings, Files, PTY and Trace
  stay on the PC. The hub hostname is typed on the PC, never shipped in the binary.
  Simulators: iOS can use a loopback hub; Android needs `adb reverse` onto
  that same hub port. `mobile/ios/App/AppUITests/BindFlowTests.swift` pastes
  the offer from `simctl pbcopy`, taps Back if a thread already opened, asserts
  `path=` plus the seed conversation, and Starts a thread. Android is the debug
  APK plus `adb reverse`, or `make mobile-android-release` for the release
  APK/AAB in `bin/`. Neither is in `make check`.

## When a turn misbehaves

Reproduce it, then read it:

```bash
zwai trace <turn-id>          # the timeline plus every model call
zwai trace <turn-id> --full    # untruncated text
zwai tui                       # interactive client of the same engine
zwai tui --goal "..."          # standing objective; starts immediately
zwai tui --task "..."          # one stored turn; this process can exit when it settles
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
  Scheduled-task kinds (`schedule`, `schedule_fired`, `schedule_skipped`,
  `schedule_report`, `schedule_cancelled`) live in that table and in
  `frontend/src/lib/stream.test.ts` `wireKinds`.
- A new user-visible flow needs an E2E test. If it cannot be driven in the
  browser, it probably cannot be driven by a user either.
