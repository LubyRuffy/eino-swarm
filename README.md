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
  two-line list of your own turns; click one to pin that send at the top of the pane. A long
  conversation packs the ticks into a minimap instead of stacking a second
  scrollbar on the rail. Auto-follow unpins so a
  live stream does not yank you back to the bottom. Opening another conversation
  from the list lands at its latest turn — and lights that tick — not the top
  of the history. A long log loads from the live edge first; older turns appear
  when you scroll up, and clicking a tick still above that tail pages it in
  before scrolling. A click is re-applied after those older pages land, so a
  load that started at the top cannot swallow it. New tokens while you are
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
- **Copy or edit a sent message.** Hover a user bubble or a finished answer
  to see the event time (touch keeps it visible). Copy and a pencil sit under
  your bubbles. Copy takes the text. Edit opens the bubble in place
  (Cancel / Send). Sending keeps that bubble, clears everything below it, and
  starts again at that position — it is not a composer prefill, and it is not
  a new turn on top.
- **Deletes ask first.** A conversation, project, provider, workspace file,
  skill, or queued message opens a confirm. Cancel leaves it. A chip on the
  composer that has not been sent yet still drops immediately.
- **Quote a passage into the next message.** Select text in the transcript (or a
  sub-agent's log) and **Add to chat**. It still works while a turn is
  streaming. The snippet lands as a count chip on the composer — hover to
  read, edit or drop it — and is sent in `<selected_text>` next to whatever
  you type (`<user_request>`), instead of being dumped into the box.
- **Interactive questions (`ask_user`).** When a preference would waste work
  if guessed, the manager pauses this turn with a numbered question dialog.
  The card says **Your answer needed**, the title bar says **Your turn**, and
  the sidebar swaps the working pulse for a pinging question mark — pick a
  row, then Submit. Other opens a box instead of sitting empty under
  every card. The same ReAct turn continues after the answer. Workers cannot
  ask. While a card is waiting, Enter in the composer is Other, not a
  follow-up.
- **Scheduled waits.** The manager can arm a wake on this conversation
  (`schedule_wake`) or, on a human turn, an independent job (`schedule_task`).
  `schedule_wake` does not take an id. It arms or replaces the open wait;
  the host assigns the id. Cancel copies an id from the open-wait list.
  When progress is gated on time or a condition not worth polling now, it is
  told to wake and end the turn instead of spinning or asking you to remind
  it. A parallel `exec` that sleeps and then checks progress is fine; that
  sleep and `schedule_wake` both bias estimated remaining time short (about
  a third, then that interval) so a check lands before the work is already
  done; extra checks are expected. A named clock time you asked for is still
  honored.
  The sidebar **Scheduled** control opens the list in the main column: search
  and **All / Active / Paused / Completed**, defaulting to Active.
  **Create** opens a right-hand drawer (**New**) for the add-wait form — the task
  description only (a short inbox name is generated, same namer as conversations),
  then Details (**Runs in**: new conversation each run, or wake an
  existing one; **Project** is the workspace for a minted conversation) and
  Frequency. **Expand** fills that column so a long instruction is readable.
  Click a wait to edit it in the same drawer: title, task, and cadence
  (`PATCH`; changing cadence recomputes the next run from now). Pause, resume,
  cancel, and Run now live there too. Destination (Runs in / Project) stays
  as created. The conversation
  Agents/Files/Trace rail stays hidden on this page, like Settings. Rows stay
  title plus cadence and next check; click one for the editor.
  An active wake on the open conversation
  shows a banner with the next check and the wait's title or prompt, **Run now**, and **Cancel wait** (not
  an icon-only dismiss — that sat under the goal banner's X). Run now and
  Cancel wait also sit on the armed-wait notice, which shows the same
  instruction. Cancel of a thread wake
  while idle continues a standing `/goal`; Run now fires the check without
  waiting for the timer (the wait banner hides while that turn is working,
  and an early cron check consumes the pending slot). The next-check time
  comes from the live schedule row, not the first arm snapshot — a refresh
  used to look like the timer jumped. A scheduled check reports through `report_schedule`;
  empty findings stay quiet. Quiet standalone runs stay out of Recents;
  findings open from the inbox — **Open findings** on a live wait, with a
  count when several fires are unread, so finished reports stay off the row.
  Workers cannot schedule. Caps live in
  Settings → Swarm.
- **`/plan` before changing anything.** Planning unmounts write/edit/exec
  (and similar). The manager explores, asks, and writes `$ZWAI_HOME/plans/<thread>/PLAN.md`.
  Edit it on the banner, then **Implement** to remount those tools and start
  the work. Entering plan pauses an open `/goal`; Implement does not resume it.
- **Slash commands in the composer.** Type `/` in the box — at the start or
  after what you already wrote, the same way Cursor does. Paths (`foo/bar`)
  and URLs do not open the palette. Picking **goal** or **plan** from the
  menu writes `/goal ` / `/plan ` in the box; type the rest and Enter.
  **goal** sets a standing objective and
  starts pursuing it in this turn. **plan** explores and writes a plan before
  changing anything. Codex cuts the command name at a space;
  an IME objective glued to `/goal` or `/plan` (or typed with a fullwidth `／`,
  or the CJK punctuation comma `、` the Slash key emits) is still
  the command, never a user message. A raw `POST /turns` of that line is
  intercepted the same way. The
  conversation keeps pursuing a goal until the manager calls `complete_goal` (or you
  clear it from the banner). A mistaken `complete_goal` can be undone in that
  turn with `reopen_goal`, or later with **Start** on the Done banner. A live turn is steered so this round sees the
  new text. A turn ends when the manager stops calling tools; the runtime
  then continues, unless an active thread wake is waiting on this
  conversation — that wait is the next turn until it fires or you cancel it. Context pressure compact in place. Hitting the manager
  tool-round slice keeps the same turn going. In-flight sub-agents survive
  a pursuing turn that ends while they are still running. A continuation
  that makes no tool progress stops auto-continue until you send a message
  or hit **Start**. Hitting the auto-continue budget (or a stop) pauses
  the same way — the banner says this is not an error, and **Start**
  keeps going. If it hits an
  obstacle it cannot pass, it calls
  `block_goal` and stops instead of retrying forever — a crashed turn that
  is not a recoverable model error does the same, and the banner shows the
  public error. Truncated tool JSON / a `429` / a dropped stream retry
  in-turn, then auto-continue. **Start** resumes after a block, a cap, a hold, a stop, or a completed
  objective that finished too early. It does not
  appear while the objective is still pursuing between auto-continue
  sessions. The
  text is editable in place, and a live turn is told immediately. The
  runtime starts the next turn itself; a queued follow-up still wins. **compact** folds earlier turns into a briefing for later
  prompts; the transcript you see does not change. An icon on the notice
  opens the briefing. Old tool results that can
  be re-read are cleared first. The briefing prefers a rolling session
  summary extracted from the event log, so a long `/goal` does not teach
  the project's memory from a compacted view. The same fold also runs
  on its own once a manager call would exceed **Settings → Swarm → Auto-compact
  at (tokens)** (default 80 000). Pin a cheaper summarizer
  under Settings → Models the same way as the conversation namer.
- **Swarm, visible.** Sub-agents appear as they spawn, with live status and their
  own transcript. Built on [eino](https://github.com/cloudwego/eino) ADK and the
  swarm library in this repository.
- **It says what it is doing, even when it is quiet.** A turn reports in every
  few seconds while it works, so a swarm thinking hard inside a slow tool call
  shows a ticking "Working for 1m 12s · 2 sub-agents running" instead of looking
  frozen. While it is running, that line — and the live tool / sub-agent rows —
  sweep, and scroll if the text does not fit.
- **Thinking stays a 10-line window in developer view.** While the model is reasoning, the block
  caps at ten lines and follows new tokens; earlier lines stay reachable by
  scrolling, with a fade at the top so it is obvious they are there. The
  **Thinking** label sweeps like the other live status lines. Click the row to
  hide it while it is still running. After it finishes it collapses to **Thought**.
  **User view** (the default) folds adjacent thoughts and tool calls into
  one live ticker. Answers stay on screen and split the fold. The current
  activity swaps in vertically, and
  the line itself still scrolls left-to-right when it does not fit:
  thinking text (or **Thinking**), **Planning next moves** between model
  turns (under a closed answer, not on the fold above it), **Editing** /
  **Reading** / **Exec** while a tool is in flight.
  Settings → General, the app menu (`…` at the bottom-left of the
  conversation list), or ⌘K switches to developer view.
- **It stays live without freezing the window.** Streamed tokens are folded into
  one event every few milliseconds, answers render as markdown as they arrive
  (finished ones are not re-parsed on every token; fenced code is highlighted
  and copyable; `$…$` / `$$…$$` render as formulas; a GFM table stays inside
  the column), a `chart` fence becomes a
  plot when the JSON is a comparison and does not flicker while later tokens
  arrive, and typing in the composer
  does not rebuild the conversation.
- **Steering, not restarting.** Enter while a turn is running **queues** a
  follow-up for after this one finishes (refresh-safe; a post-turn session
  briefing or memory review does not hold that start), except the live turn's
  own words — those are already in flight. Click a waiting row to
  edit it; submitting that edit sends it to the back of the queue. **Steer** on
  that row, or ⌘Enter, injects into the current turn at the next model boundary
  — it does not kill an in-flight tool, and it drops a queued copy of the same
  text. Unread steering sits under the working line: **Interrupt** aborts the current manager tool so those nudges land now
  (workers stay up); **Delete** retracts one bubble so the model never sees it.
  Stop is what cancels the whole turn. If the manager
  hits its tool-round limit, the transcript asks whether to add another slice
  rather than dying with a graph error.
- **Unfinished work survives a crash.** Kill the process, quit the window, or
  lose power mid-turn: the next start continues every leftover conversation,
  with the answers already on screen still in the model's context, any
  sub-agents that were still working restarted under the same ids, and queued
  follow-ups still waiting to run after that turn. A file/shell tool that was
  in flight is marked stopped. A `wait_agents` that was blocked is completed
  as a timed-out wait on those same ids so the manager keeps waiting instead
  of recounting finished leftovers as a new swarm. Pressing **Stop** is the
  one thing that does not come back.
- **Projects that remember.** Group conversations under one working directory and
  one instruction, and let them keep what they learn: after each turn the project
  writes down durable facts and records reusable procedures as skills, which
  every later conversation in that project starts with.
- **Conversations name themselves.** The first message is a placeholder so the
  sidebar is readable at once; after the first reply a short title replaces it.
  A name you type is never overwritten. Switch it off in Settings → Swarm, or
  pin a cheaper model under Settings → Models when an endpoint lists more than
  one. **Sub-agents at once** in that same page takes effect immediately:
  queued workers start under the new cap; lowering it does not kill anyone
  already running.
- **Per-conversation model and thinking level.** Settings → Models lists
  providers as rows (open one for URL, key, default). Discovering a catalog
  that fails toasts over the sheet (× to close) instead of a red line at
  the top of that page. The same toast is how Phone pairing, a failed
  Settings save, and a failed project save report — never a red line under
  the heading you already scrolled past. The composer picker
  groups models by provider, searches, refreshes the catalog, and jumps
  to Edit providers. A thinking-level menu (Default / Low / Medium / High)
  sets how hard the models reason. Both apply from the next turn. A
  thought that is still streaming is not cut off by the request timeout;
  raise it in Settings → Models if the endpoint stays silent.
- **Real tools.** File read/write/edit, `ls`/`tree`/`glob`/`grep`, shell `exec`,
  web search and fetch, from [eino-tools](https://github.com/LubyRuffy/eino-tools).
  The transcript shows the command or query, not the JSON envelope; a failed
  `exec` is a red error, not a grey dump. A running `exec` stays one line
  (the latest stdout/stderr); open the row to watch the stream (a `\r`
  overwrites the current line the way a terminal does; the output box
  follows the tail, wheel-up unpins). The body does not stay open after
  the command returns unless you opened it. Opening an
  `exec` row wraps the full
  command with shell highlighting instead of leaving it cut off. Opening a
  `read` paints the file from its suffix (Go, TypeScript, Python, …) instead
  of a grey dump; markdown still renders as prose. An `edit` or `write`
  stays collapsed like `exec`; open the row to see a highlighted diff of
  what landed — red gone, green new — instead of the one-line status the
  tool returns to the model (`ok: replaced block`, `Updated file … (N bytes)`). A huge
  write is clipped in that view; the collapsed row still shows the path
  plus the real `+N` (write) or `+N −M` (edit). Those counts stay the
  row colour until hover (or focus), then additions go green and
  deletions red. Agents are
  told which OS, shell and date they are on, so they stop emitting GNU-only
  flags on a Mac.
- **Answers can include charts.** When the numbers in a reply are easier to
  see as a comparison than as prose, the manager emits a `chart` fence
  (bar, line, area, pie) and leads with the takeaway — it is told not to
  dump the same series as a list, a markdown table, or emoji. The transcript
  paints the plot; a Table tab shows the same rows. The phone paints that
  same fence (plot, then table); a truncated fence waits, and a finished
  invalid body stays a code block. `zwai tui` still has the JSON fence.
- **Code and formulas render.** A fenced block with a language tag is
  highlighted (same colours as a `read` of that suffix) and has a copy
  control. `$…$` / `$$…$$` and `math` fences paint as formulas. The manager
  is told to tag fences and to write mathematics as LaTeX, not as a code dump.
- **Links leave the app.** A markdown URL opens in a new browser tab (`zwai web`)
  or the system browser (`zwai desktop`). It does not replace the window.
- **One-id troubleshooting.** Copy a turn id from the UI and
  `zwai trace <id>` replays the whole run: timeline, tool calls, model calls
  with billed tokens. The Trace tab shows the same turn's status and cost; open
  **Full log** for the on-screen timeline.

## Quick start

Requirements: Go 1.26+ and Node.js (for the UI bundle on first run; `npm install`
uses the public registry). The desktop window uses
[Wails 3](https://wails.io) and needs a C toolchain (macOS: Xcode Command Line
Tools; Linux: `webkit2gtk` dev packages).

```bash
git clone https://github.com/LubyRuffy/eino-swarm
cd eino-swarm

# first run builds frontend/dist (needs Node), then opens the native window
go run ./cmd/zwai desktop
# macOS may ask to allow local network (LAN model endpoints). Allow it.

# or the same app in your browser
go run ./cmd/zwai web
```

First launch writes `~/.zwai-swarm/config.yaml` and shows a setup banner until a
model is configured. Open **Settings** (⌘,) — a full-page sheet that starts on
**General**, with sections in the
left rail (on the desktop window, **Back to app** sits below the traffic
lights). Each page is a list of compact rows (name and hint left, control
right). Edits write themselves; **Back to app** flushes the last keystroke.
Chrome language is **Settings → General**, the app menu at the bottom-left of
the conversation list, or ⌘K → Switch language. Agents still answer in the language you are using.
Theme is **Settings → General**: System / Light / Dark cards, then **Color theme**
ZWAI (default chrome) or FOFA (intelligence-console palette, both light and dark).
That same menu flips light / dark, conversation width (standard reading column
vs wide), and developer view, and it shows the build version plus which other
shells are open. Font and size are **Settings → General**. Size scales the conversation; chrome
(sidebar, Settings, title bar) stays the same. Conversation width is that app
menu, **Settings → General**, or ⌘K. How you want the manager to work
with you — tone, language habits, standing preferences — is
**Settings → Personality**. It is added to every conversation's
system prompt. A project's instruction is the business context; when the two
conflict, the project wins.
Fill in **Models** (base URL, API key, discover models, pick a
default), or seed it from the environment before the first start.
Conversation search is always keyword (FTS5). **Settings → Models → Semantic
search** is off until you turn it on and name an embedding model; then ⌘K
also ranks by meaning.
On macOS desktop, a LAN endpoint needs Local Network permission (the
window is not Terminal; `curl` working does not mean zwai can dial).

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

### Scan a phone to this PC

1. zwai **Settings → Phone**: turn pairing on and paste the hub URL
   (`https://…`). The Host Token is minted on this PC and hidden; it is not
   a Gateway Key. A missing hub toasts over the sheet (× to close).
   **Keep this computer awake** stays on by default so a plugged-in host
   does not idle-sleep while a phone still expects it. Turn it off if you
   want the machine to sleep. **This computer's name** is what the phone
   paints on the host chip (blank uses this machine's hostname). That same
   name is sent when this PC registers with the hub. **Bound phones** names each device by the
   model the phone reports after it connects, not only the fingerprint. The
   phone also sends that model line when it redeems the QR, so a gateway
   list can show it without reading the sealed session.
2. **Show pairing QR**. The plate is large and high-contrast. The same URI can
   be pasted if the camera is missing. While that plate is up, **Bound phones**
   refreshes on its own, so a phone that just bound shows up without leaving
   the page.
3. On the phone, open the **zwai** iOS or Android app. The Android sideload
   APK is on [GitHub Releases](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.11)
   (`zwai-0.1.11-android.apk`). A newer Android build offers **Update** on the
   phone: it downloads that APK from GitHub Releases and opens the system
   installer. iOS opens the release page. **Not now** hides that version from
   the automatic bar. **Menu → Check for updates** asks immediately and shows
   that it is fetching. The current build says so. A newer one asks before
   installing. A failed request shows the response's own error.
   Building from this repo is `mobile/ios` /
   `mobile/android`. The home-screen icon is the same ZWAI mark as the Dock.
   **Scan QR** opens a live viewfinder: a frame, a beam that sweeps up and
   down, and a short chime when the pairing QR is read. After bind, a live turn
   (or the last thread this phone opened) opens immediately; otherwise the
   inbox lists In progress, then projects / Recents with the latest 5 idle
   threads (live rows do not consume that quota). **More** loads the next
   idle page, and a later refresh keeps those rows. The list does not compose:
   search and **New chat** sit under it, the header corner is **New chat**,
   and each project row's right side starts one already in that project.
   A later launch with
   saved tickets shows host chips (the name this PC sent) and a connecting skeleton — not the scan
   form, which would look like the phone was never bound. **Add a PC** is on
   the menu. Switching a chip opens that PC. In-progress rows show
   findings or **Waiting**, not `schedule_wake` / `report_schedule` JSON.
   The transcript opens on the
   last turn, already at the live edge; Earlier or pull up loads older rows. A tap
   paints the chrome immediately; until that conversation arrives the body
   shows a loading status, not a blank screen. The transcript is a short tail, not the
   whole turn. The composer matches the PC: a model, a thinking level, and a
   file or image. An image is shown to the model; other files go into the
   workspace. **Menu → Models** (on the scan screen, **Connect a model**) saves
   an OpenAI-compatible endpoint on the phone: base URL, optional API key,
   **Chat completions** or **Responses**, Discover, and a timeout. After one
   is saved, a **Chat** tab appears on the top row. That chat uses the same
  composer. An image is vision; a text file is sent as text. A responses
  front that rejects the image part is asked once more as chat completions,
  so the image still arrives. The reply
  streams, and the thought is the same live row as a PC thread. The key stays
  on the phone and is not sent to a paired PC. Markdown
   renders in the transcript. Adjacent thoughts and tool calls fold into one
   row; an answer stays on screen and splits that row. Open it to see each
   tool, still collapsed to a one-line preview.
   A `wait_agents` chip is a status count, not the roster JSON. A parked
   `/goal` wait paints **Pursuing** plus **Waiting** (next check, Run now,
   Cancel wait) instead of a silent composer. A long objective stays one
   line so that chrome cannot cover the wait or the box. A dropped hub socket is a
   reconnect banner, not a frozen inbox. The file browser, Settings,
   PTY and Trace stay on the PC. The composer can still attach a file or an image.
   `make mobile-ios` / `make mobile-android` open Xcode or Android Studio
   after copying the web bundle. `make mobile-android-release` writes the
   signed APK/AAB to `bin/` (set `ANDROID_KEYSTORE*` or a gitignored
   `mobile/android/keystore.properties`; `ANDROID_UNSIGNED=1` is a debug-signed
   sideload APK, not a Play bundle). Published sideload APKs also land on
   [GitHub Releases](https://github.com/LubyRuffy/eino-swarm/releases).

Traffic starts on the hub as ciphertext and upgrades to UDP when punching
works. Conversations never enter the hub database.

## Using it

The manager fans out when parallel work would save time or improve quality —
you do not have to ask it to. Spawning one worker and waiting is not that:
it is an extra hop. A request with independent parts is the clearest win:

> Go through the three files I just uploaded, pull out every deadline, and leave me
> a single summary.md with one table.

What you get:

1. **A plan, then sub-agents.** The manager spawns workers (`fork_context` when a
   new worker needs this conversation so far). A later task for the same role
   stays on that agent — new description, same `agent_id` — whether it is still
   running or already finished. `resume_agent` targets a specific leftover
   sibling. Open one in the Agents tab to read the system prompt it was given;
   the log opens at the latest line, not the first tool call. The tab lists
   every worker this conversation started, not only the ones whose tools are
   still in the live-edge viewport.
2. **A workspace.** Every conversation has its own directory
   (`~/.zwai-swarm/workspaces/<thread-id>/`). Uploads land in `uploads/`, agent
   output lands next to it, and the **Files** tab shows a collapsible tree —
   filter at the top, directories expand like a file explorer, download on
   hover (and, in the desktop app, "Show in Finder"). The title-bar terminal
   (⌘J, and every click on the icon) starts a real shell in that same
   directory — a project's working directory when the conversation is in one.
   The tree is the workspace
   root, listed breadth-first so a large generated folder cannot hide the rest.
   Expand a `read` in the transcript to see the file: markdown is rendered,
   other files keep their line numbers. Open an `edit` or `write` to paint
   the hunk from the call's args (search/replace, patch, or the written body)
   with add/delete highlighting.
3. **Steering.** Type while it works. The nudge sits under the working line
   until the manager's next model call — it is queued, not inserted into the
   current tool. **Interrupt** on that pin injects it now by aborting the
   current manager tool; **Delete** retracts one unread bubble. Steering that
   arrives after the last model call becomes a follow-up turn.
4. **Quote the conversation.** Select a passage and **Add to chat** when you want
   the next message to point at it — including while a turn is still streaming.
   The composer shows a count chip at rest (hover to read, edit or drop);
   send with an empty box if the quote is the whole request. The model sees
   `<selected_text>` and `<user_request>` as separate blocks.
5. **What this turn cost.** The **Trace** tab shows status, duration, billed
   tokens, and the turn id (`zwai trace <id>` replays the full dump). The event
   log stays folded until you open **Full log**.

Keyboard: Enter sends (while a turn is running it queues a follow-up; the live
turn's own words are not queued; an IME
confirmation — keeping leftover Latin as typed — is not a send) · the corner
button is Stop while a turn is running and the box has nothing to submit, and
Send once it does (text, a quote, a file, or an image; whitespace does not
count) · `/` at the
start of the box opens built-in commands · ⌘Enter steers
the draft into the current turn · Shift+Enter a newline · `⌘K` command palette
(titles, bodies, and meaning when embeddings are on) · `⌘N` new
conversation · `⌘F` find in the open conversation · `⌘B` hide or show the
conversation list · `⌘\` toggle the right panel · `⌘J` open a terminal in the
current project (or conversation) directory · `⌘,` settings · `Esc` stop
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
Click a folder to collapse its topics — the directory icon is closed
when collapsed and open when expanded. Click **Pinned**, **Projects**, or
**Recents** to fold the whole section (the arrow after the name shows
on hover while the section is open). Pin a topic from the row menu to keep
it in **Pinned** at the top.
**New project** at the top of the conversation list asks for three
things:

- **an instruction**, added to the system prompt of every conversation in the
  project, so you stop repeating how you want this work done. It is the
  business context. Settings → Personality is how you like to be worked with;
  when the two conflict, this project wins;
- **a working directory**, an absolute path that already exists. Every
  conversation in the project reads and writes it directly, with your
  permissions. Leave it empty and zwai keeps one for you;
- **memory**, on by default.

With memory on, each finished turn is read back and what is worth carrying
forward is kept: short notes about how this project works, and *skills* —
step-by-step procedures the agents wrote for themselves. The next conversation in
that project starts with the notes in its prompt and an index of the skills, and
opens a skill when it needs one. Sub-agents on that conversation get the same
snapshot and can open a skill; they cannot write the store. Their report back to
the manager is the task result, plus at most a short durable note if something
would change later work.

The sidebar lists each project's conversations under its name. A folder
(and Recents) shows the five conversations active in the last seven days;
**Show more** reveals the rest, **Show less** folds them again. Click the
folder to collapse it. The open conversation is marked; the folder is not.
A conversation that is mid-turn keeps a progress mark in that same column
even if you are looking at another one; a conversation whose next turn is a
parked wait keeps a clock there instead, so it does not look Idle. The
title bar says **Waiting**, and the wait itself is the composer banner
(Run now / Cancel wait) — that is not a row in **Pinned**. A
collapsed folder that still has a live turn or a wait keeps the mark on the
directory. Click **Pinned**, **Projects**, or **Recents** to fold
that section. Pin a topic from the row menu to keep it in **Pinned**
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
characters; one agent note is also capped at `memory.entry_max`, default 360)
because they ride in every prompt — once full, a write that would
grow the notes is refused, even a replace with a longer note. Something
shorter has to land first. Creating a skill that already covers the same
subject is refused: the result names the existing skill so the next call is
a patch, not a second name. Skills that share a name stem (the same procedure
split into chapter-skills) are refused the same way, and leftover families are
folded into one skill after a finished turn — automatically, including when
auto-review is off or the manager already wrote this turn. The fold is reported
as a `memory_review` when it changed the catalog. Editing a skill file by hand
does **not** fold until the next turn; **Tidy skills** on the Skills
heading still folds leftover name-stem families, then asks the model to curate
the whole catalog by content. That is a real reviewer call — it takes as long
as the model takes. While it runs, the panel estimates progress (the bar stays
short of full) and scrolls the model's reply in a short box. Reload first if
you edited files outside the app. When it finishes, the card names what was
merged, deleted, created and patched, with counts (scanned / merged / deleted /
created / updated / remaining).

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
zwai tui                    # interactive terminal; type / for commands
zwai tui --goal "..."       # standing objective; starts immediately, composer stays after
zwai tui --plan "..."       # planning first; write/edit/exec unmounted until Implement
zwai tui --task "..."       # one task, then this process can exit; the conversation stays
zwai tui --model NAME --reasoning high   # same session, pinned model and thinking level
zwai trace tn_ab12…         # replay one turn; also accepts a conversation id
zwai trace th_cd34… --full  # untruncated event text
zwai config path            # where the config file is
zwai config show            # what it says (the API key is redacted)
zwai --data-dir /tmp/demo   # use a throwaway data directory
```

`--data-dir` works on every subcommand, as does `--mock`. Desktop, web, and
the terminal share one engine for that directory: the first shell starts
it, the next one attaches, and closing a window does not stop a turn that
is still running. See [docs/CLI.md](docs/CLI.md).

## Where things live

```
~/.zwai-swarm/            $ZWAI_HOME overrides this
├── config.yaml           settings (0600; the API key is in here)
├── engine.lock           one engine process for this directory
├── engine.json           pid and URL of that process
├── zwai.db               conversations, transcripts, event timeline, model calls
├── remote/               Host Token and device identity; not in yaml
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
make frontend    # rebuild frontend/dist if the TypeScript sources changed
make build       # ./bin/zwai
```

The front end is React + TypeScript + Tailwind + shadcn/ui under `frontend/`.
`frontend/dist` is a build artefact; `go run ./cmd/zwai desktop` (and `web`)
rebuild it when the sources changed. `make build` generates first so the
embed inside the binary is current. `make frontend` is `go generate ./frontend`.

Rules for changing this code — coverage bars, prompt hygiene, documentation
duties — are in [AGENTS.md](AGENTS.md). Read it before opening a pull request.
