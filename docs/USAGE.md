# Using zwai

This is the day-to-day guide. The [README](../README.md) is the entry and the
worked example. Keys and defaults are in [CONFIG](CONFIG.md), commands in
[CLI](CLI.md), the phone app in [mobile/README](../mobile/README.md), and
behavior boundaries in [FEATURES](FEATURES.md) and [CONTRACTS](CONTRACTS.md).

## The window

The left rail switches **Projects** (the default), **Conversations**,
**Scheduled**, and **Clients**. Projects holds pinned topics and project
folders. Conversations is the list of threads that belong to no project.
Launch and reload show the home page and select no conversation. Restart
leaves the rail where you left it.

The transcript is the middle. Once you have sent two messages, a tick cluster
on its left edge lists your own turns; click one to pin that send at the top.
A long conversation packs the ticks into a minimap. Auto-follow unpins so a
live stream does not yank you back to the bottom. Opening another conversation
lands at its latest turn. A long log loads from the live edge first; older
turns appear when you scroll up. New tokens while you are reading light a
jump-to-latest control.

Each visible finished turn has one duration and local completion time after
its last pre-completion content. Reasoning and tool details start collapsed.
A late memory-review notice stays at its event position and does not repeat
that end marker.

A ring on the composer shows how full the last manager prompt is. Hover it
for the compact count against this model's window, plus this-turn billed
in/out. The numbers come from the endpoint. Until a window is set, the arc
still grows and the label stays a count.

Drag the conversation list or the right panel to resize them. Conversations
and projects sort by last update; drag a row to pin a custom order. On the
desktop window, drag the title bar to move it; double-click to zoom or
restore, like Finder.

On the Mac, an independent native controller checks actual painted pixels,
tries one reload, then replaces the window on the same engine if needed.
Background work continues, and the restored window returns to its conversation
and saved draft. Repeated failures stop with a native error. A system WebKit
fault can still occur after a successful recovery. Diagnostics are in
[desktop recovery](API.md#desktop-recovery-diagnostics).

## First launch and Settings

Open **Settings** (⌘,) — a full-page sheet that starts on **General**, with
sections in the left rail. On the desktop window, **Back to app** sits below
the traffic lights. Edits write themselves; **Back to app** flushes the last
keystroke. A failed Settings save, Phone pairing, model discovery, or project
save is a dismissible toast, not a red line under a heading you have scrolled
past. A refused project directory stays under that field.

Chrome language is **Settings → General**, the app menu at the bottom of the
icon rail, or ⌘K → Switch language. Agents still answer in the language you
are using. Theme is System / Light / Dark, then **Color theme** ZWAI (default)
or FOFA. That same menu flips light / dark, conversation width, and developer
view, and it shows the build version plus which other shells are open. Font
and size are **Settings → General**. Size scales the conversation; the chrome
stays the same.

**Settings → Personality** is how you want the manager to work with you. A
project's instruction is the business context; when the two conflict, the
project wins.

**Settings → Models** is base URL, API key, discover, and the default. The
switch on a provider row shows that endpoint in the composer; off hides it
from the model list without dropping a conversation that already uses it.
Hiding the default moves new conversations to an endpoint that is still
listed. The last endpoint stays on. The composer picker groups models by
provider, searches, refreshes the catalog, and jumps to Edit providers. A
thinking-level menu (Default / Low / Medium / High) applies from the next
turn. A thought that is still streaming is not cut off by the request timeout.

If the model sends no first byte, open its provider and raise **First-byte
timeout (seconds)** (default 30). The error's **Configure first-byte timeout**
button opens that field. **Request timeout (seconds)** is silence after the
stream starts. On macOS desktop, a LAN endpoint needs Local Network permission.
The window is not Terminal; `curl` working does not mean zwai can dial.

Conversation search is always keyword (FTS5). **Settings → Models → Semantic
search** is off until you turn it on and name an embedding model; then ⌘K
also ranks by meaning.

For a new setup, new conversations start on the account's models when those
are available. An existing setup with a working provider keeps starting on it
(`models.default_source`). An own provider is marked default only when new
conversations start on it.

When the signed-in server publishes an official profile, the composer shows
that server's name and does not open the model menu. Settings → Models offers
**Official model** or **All models** on the account card. Official model hides
this machine's own providers, auxiliary models and embedding names. Choosing
All models asks first. After the switch, those services return, and they do
not have this account's online capabilities. A conversation that already uses one of them keeps that
choice and uses it again after the switch. The official name and the request
alias stay on the server; this setting does not reveal or select the real
model id. Signing out clears the profile.

## Composer

The composer saves text, quotations, original files and pasted images together
in the local engine database, per conversation or new-conversation project.
Normal loading and saving stay quiet. Only a failed save or restore, a
conflict, or an uncertain send shows draft guidance. Only an acknowledged save
is durable. Sending retains the snapshot until the server accepts it; an
uncertain result keeps the draft and is never sent again automatically.
Resolve a conflicting window explicitly. Leaving a saved idle draft releases
its attachment previews; returning restores the original files.

`/goal` and `/plan` accept text only. A draft with attachments or quotations
stays intact and shows an error until those items are sent separately or
removed. Draft text and quotes share a 1 MiB limit. Up to 32 attachments share
256 MiB. Vision input is at most eight images, 8 MiB each.

**Tools**, next to the model, lists the built-in catalog and each configured
MCP server. Each row is a switch. A tool that is on by default starts on;
Python and screenshot stay off until you turn them on. Turning one off saves
immediately. The next model request omits that tool, including a turn that is
already running. Turning a tool this turn already had back on brings it back
on the next request. A tool or server that was off when the turn started is
added when the next turn builds its tools. A call that still arrives after the switch returns
`tool_switched_off` and does not run.

After a visible reply, one likely next message can appear as the placeholder.
Tab inserts it; Enter sends it. **Settings → Swarm → Suggest a next step**
turns the extra call off. It is on by default.

Paste a screenshot into the box. It lands as a thumbnail you can drop before
sending, and goes to the model as visual input, not as a file in the
workspace. Dragging a file onto the box does the same split: images become
thumbs, other files become workspace attachments. A photo whose drop reports
a generic binary type, or only a `png` / `jpg` / `gif` / `webp` name, is still
a thumb. The server sniffs the bytes and refuses unsupported or mismatched
type claims. Sending names those files on the message, so the model reads what
you just attached.

Click a sent image, or focus it and press Enter or Space, to open it over the
conversation. Escape, the backdrop, or Close returns to the thumbnail.
Closing the preview keeps a running task going.

Hover a user bubble or a finished answer to see the event time. Copy takes
the text. Edit opens the bubble in place. Sending keeps that bubble, clears
everything below it, and starts again at that position.

A conversation, project, provider, workspace file, skill, or queued message
asks before it deletes. Cancel leaves it. A chip on the composer that has not
been sent yet drops immediately.

Select text in the transcript, a sub-agent's log, or the terminal and **Add
to chat**. It works while a turn is streaming. The snippet is a count chip —
hover to read, edit, or drop it — and is sent in `<selected_text>` beside
`<user_request>`. On the phone, the same action sits near the selection. The
phone's selected text appears above the message box. Terminal selections
include their source tab, conversation or project, initial directory and
shell, and they go into the current composer even when the terminal belongs
to another conversation. Only selected text is included. The initial directory
is where the shell started, not a claim about its directory after `cd` or
`ssh`. Quotes are saved with the draft.

## One turn

Enter sends. While a turn is running, Enter queues a follow-up for after this
one finishes. The live turn's own words are not queued. An IME confirmation
is not a send. Whitespace does not count as something to send. The corner
button is Stop while a turn is running and the box is empty, and Send once
the box has text, a quote, a file, or an image.

The queued row is refresh-safe. A post-turn session briefing or memory review
does not hold the next start. An ordinary error or a failed scheduled check
also starts the next queued message and keeps the failed result. Stop,
cancellation, and an engine restart keep the queue waiting. Paired phones use
the PC queue. Click a waiting row to pull it into the composer; Enter queues
that draft at the back. **Steer** on that row, or ⌘Enter, injects into the
current turn at the next model boundary. It does not kill an in-flight tool,
and it drops a queued copy of the same text. After Stop, Steer on a leftover
row starts that text as the next turn. Repeated Steer clicks while that
request is pending send it once. If the row has already left the queue, the
app refreshes it and does not send the old text again.

Unread steering sits under the working line. **Interrupt** aborts the current
manager tool so those nudges land now; workers stay up. The pencil pulls one
bubble into the composer and retracts it. **Delete** retracts one bubble so
the model never sees it. **Stop** cancels the whole turn. Escape never stops
it. Escape closes the active preview, find bar, or other local overlay.

The PC and phone Agents panels list unfinished workers under **Stopped**,
separate from successful or failed workers. Reopening keeps that state even
when a cancellation receipt arrives after cleanup. Cleanup alone never means
a task completed. Explicitly resuming a worker can run it again.

When the latest human turn fails on desktop or web, **Retry** continues from
the context stored at that failure, including tool results already recorded.
The same turn continues. Retry stays disabled while Settings is open or the
conversation is busy.

If the manager hits its tool-round limit, the transcript asks whether to add
another slice. A paired phone shows the same prompt with **Continue** and
**Stop**. Reopening restores the paused controls. A refused action keeps the
prompt and its error. Update both the phone and the PC to use these controls.

### Asking you, and side chat

When a preference would waste work if guessed, the manager pauses with a
numbered question (`ask_user`). The card says **Your answer needed**, the
title bar says **Your turn**, and the sidebar swaps the working pulse for a
pinging question mark. Pick a row, then Submit. Other opens a box. The same
turn continues after the answer. Workers cannot ask. While a card is waiting,
Enter in the composer is Other, not a follow-up. On a paired phone, a rejected
submission shows the PC error and keeps the selected option or Other text.
Options whose IDs are omitted still appear, including after you reopen the
conversation.

**Side chat** is separate from the turn. Open it from the conversation header,
enter `/side` or `/side <question>`, or select text and choose **Ask in side
chat**. The panel uses the task's recent context and your selection. Drag its
left edge to resize it. Its model picker can follow the task model or use
another one. Closing it, switching conversations, or reloading clears its
temporary history. It does not write into the turn, the transcript, or the
schedule.

### How workers are used

The manager uses sub-agents when they would save time or improve quality, and
reassesses that as the task develops. You do not have to request delegation.
One worker can provide an independent review or a second approach even when
the manager must wait for it. Work without a useful delegation benefit stays
on the manager. There is no target worker count. Your explicit constraints
and the configured concurrency cap still apply. Past that cap, `spawn_agent`
and `resume_agent` refuse and start nothing. The manager waits for one to
finish, then resumes that worker or starts the next piece.

A new worker that needs this conversation so far is spawned with
`fork_context`. That supplies historical conversation evidence. It does not
copy the manager's system role or personal preferences. A later task for the
same role stays on that agent — new description, same `agent_id` — whether it
is still running or already finished. `resume_agent` targets a specific
leftover sibling. The Agents tab lists every worker this conversation started.
Open one to read the system prompt it was given. The roster shows that
`agent_id`, the same string `resume_agent` takes. The log opens at the latest
line. Continuing a goal or scheduled check, compacting, retrying a model call,
or restarting retains those IDs and their current states. Finished workers
stay stopped and can still be inspected or explicitly resumed. The manager
receives that history as runtime state. It does not count the old roster as
workers created by the new turn. Use that turn's Trace to check actual
creation and resume activity.

An ordinary turn without an executable goal or a next conversation wake must
collect each worker's current result with `wait_agents`, or explicitly discard
that work with `close_agent`, before it can complete. A finished worker still
needs collection. A wait timeout is not enough. If the manager ends early, the
engine withholds that answer and lets it correct once in the same turn. A
repeated early ending retains the original workers and shows why collection
is waiting. Once per turn, if every pending worker was still running, their
completion can automatically resume collection. The manager must still receive
the actual results. Other collection pauses require **Continue** or **Stop**.
Continue grants one further attempt. Stop cancels them. Restart preserves the
wait identities and the consumed allowance. The turn's Trace records
`workers_uncollected`.

Invalid delegation calls do not create workers. Within one manager reply,
repeated spawn argument errors are reported once per error with their rejected
count. The manager can supply both role and task and continue. See
[the lifecycle tools](LIBRARY.md).

Sub-agents appear as they spawn, with live status and their own transcript. A
failed row offers **View failure reason**. A recorded overall runtime limit
links to **Settings → Swarm → Sub-agent timeout**. An exact output-budget
failure links to **Max completion tokens**. A recorded worker iteration limit
links to **Sub-agent tool rounds**. An identified provider stream-idle timeout
opens **Settings → Models**. Saving a limit does not resume the stopped
worker. A worker still running when a parent-turn error arrives keeps that
error as its stopping reason, marked as a parent error, unless the engine
explicitly retained its independent ownership after a failed scheduled check.
Generic deadline or cancellation errors do not suggest a timeout setting.

The response area shows a spinning "working…" as soon as the turn starts.
Server progress then updates it with a ticking elapsed time and how many
sub-agents are running. **Sub-agents at once** in Settings → Swarm takes
effect immediately. Queued workers start under the new cap. Lowering it does
not kill anyone already running.

While the model is reasoning, developer view caps the block at ten lines and
follows new tokens. Click the row to hide it while it is still running. After
it finishes it collapses to **Thought**. Chat Completions accepts both
`reasoning_content` and newer `reasoning` fields. No field-name setting is
needed. **User view** (the default) shows a lone thought or tool call with its
own disclosure. Multiple adjacent work rows fold into one live ticker. Answers
stay on screen and split the fold. Settings → General, the app menu, or ⌘K
switches to developer view.

A background worker can keep generating while the manager ends a scheduled
check. Its growing thought or answer updates the same row, including after
reload. Independent model outputs remain separate. The transcript and Agents
detail retain the cause and recovery state of each interrupted model output.
Its old thought stops saying **Thinking**. A resumed worker keeps its original
identity and can continue running. A recovery notice means generation resumed,
not that the task succeeded. Engine handoff, cancellation, output limits, and
model failures remain distinct. An unrecorded process-exit cause is shown as
unknown. The same evidence is in Trace after reload. See the
[cancellation investigation](diagnostics/issue88-cancellation-evidence.md)
for how to distinguish engine handoff and failed-check cleanup from provider
timeouts.

### Tools in the transcript

File read/write/edit, shell `exec` (also directory listings and filename and
content search), web search and fetch come from
[eino-tools](https://github.com/LubyRuffy/eino-tools). The transcript shows the
command or query. A failed `exec` is a red error. A running `exec` stays one
line; open the row to watch the stream. A `\r` overwrites the current line.
The output box follows the tail; wheel-up unpins. The body does not stay open
after the command returns unless you opened it. Opening an `exec` row wraps
the full command. Opening a `read` paints the file from its suffix. An `edit`
or `write` stays collapsed; open the row for a highlighted diff. A huge write
is clipped in that view. The collapsed row still shows the path and the real
`+N` or `+N −M`.

A `skill_manage patch` row shows added and deleted line counts. Click it to
compare the original text with its replacement. The same diff remains after
reload, and a failed patch keeps its error beside the attempted change.

Agents are told which OS, shell, and date they are on.

### Answers

Streamed tokens are folded into one event every few milliseconds. Answers
render as markdown as they arrive. A fenced block with a language tag is
highlighted and has a copy control, including paired and direct phone
conversations. Unknown or empty language tags stay plain. `$…$`, `$$…$$`, and
`math` fences paint as formulas. A GFM table stays inside the column. A
markdown URL opens in a new browser tab (`zwai web`) or the system browser
(`zwai desktop`).

A `mermaid` fence renders a diagram in desktop and Web conversations, tool
output, and Markdown file previews. Incomplete live fences, invalid diagrams,
and diagrams that require external image or CSS resources keep readable source.
The phone opens Mermaid as a fit-to-width diagram. See the
[phone guide](../mobile/README.md).

When numbers are easier to see as a comparison, the manager can emit a `chart`
fence (bar, line, area, pie). The transcript paints the plot. A Table tab
shows the same rows. The phone paints that same fence. A truncated fence
waits, and a finished invalid body stays a code block. `zwai tui` still has
the JSON fence.

### When the model service fails

After a chat service fails, manager and sub-agents on that same service share
a 5 → 10 → 20 → 30-second capped recovery backoff, including new turns and
provider aliases. One probe tests recovery at a time. Stop cancels waiting and
the active request. Read-only answer classification uses its own recovery and
does not start this shared gate. A gateway 503 that explicitly reports no
healthy provider waits for route recovery inside the same model call, without
spending the turn's two model-error retries. Three consecutive refusals fail
the turn. Trace records `model_backoff` waits separately from actual calls.
PC and phone chat show one timed explanation per conversation and cooldown
generation. On desktop and Web, expand **Retry details** for the waiting steps
and Agent identities.

Model API errors retain their HTTP status and concrete rejection or decoding
cause. An empty 502 response does not imply invalid tool arguments.
Empty or HTML errors, and malformed successful streams, show a response-decoding
retry. Rate limits and 5xx keep their status category. Actual tool-argument
failures keep their own diagnosis. The public error omits raw bodies. Inspect
the original call through the same turn's Trace. An immediate completion stays
Idle when its send acknowledgement arrives later.

Copy a turn id from the UI. `zwai trace <id>` replays the whole run: timeline,
tool calls, and model calls with billed tokens. The Trace tab shows the same
turn's status and cost. Open **Full log** for the on-screen timeline.

## Goals, plans, and compact

Type `/` in the box, at the start or after what you already wrote. Paths
(`foo/bar`) and URLs do not open the palette. Picking **goal** or **plan**
writes the command. After existing text, the menu offers **goal** (as the
trailing marker) and **compact** only. Any other command token in the middle
of a message is ordinary text, on the PC, on the phone, and over HTTP.

A standalone `/goal` at the end of a message uses the preceding text as the
objective. This starts a new pursuit even when the previous objective is Done.
A bare `/goal` waits for an objective. Quotes, code, and paths containing the
literal suffix remain ordinary content. Leading `/goal` and `/plan` keep their
entire argument, including command tokens inside it. An IME objective glued
to the command, a fullwidth `／`, or the CJK punctuation comma `、` is still
the command. A raw `POST /turns` of that line is intercepted the same way.

**goal** sets a standing objective and starts pursuing it in this turn. The
conversation keeps pursuing it until the manager calls `complete_goal`, or you
clear it from the banner. A scheduled check cannot complete the objective
while its own manager-created conversation wait still has another fire. The
manager must first finish that wait with `report_schedule(keep:false)` or
cancel it. The final check can then complete the objective. Conflicting calls
return a recoverable tool error. Human-created waits and independent jobs keep
their own lifecycle.

Reports that a future check was arranged are checked against saved, currently
effective plans before the manager's prose is shown. A failed or missing
scheduling call cannot support a success report. One same-turn correction is
allowed for a confirmed unsupported claim. Repeating it ends with an error.
Classification retries only that private candidate. A candidate is deferred
after three consecutive request failures, three consecutive invalid responses,
or six total attempts, and stays unpublished. Deferral does not consume the
unsupported-claim correction or replay tools. Desktop and phone retry details
show a safe reason, without the private candidate. Stop cancels verification.
Human guidance supersedes the old candidate.

Completion removes the goal strip above the composer immediately on PC and
phone. The stored objective, completion state, results, and history remain.
A mistaken `complete_goal` can be undone in that turn with `reopen_goal`, or
through the explicit `goal_resume` API or phone `resume_goal` RPC. Existing
completed records are not reopened by ordinary messages. A new explicit
`/goal` replaces the old objective, clears its completed state, and displays
the strip again. A live turn is steered so this round sees the new text.

A turn finishes after answer verification and worker ownership or collection
are settled. The runtime then continues, unless an active thread wake is
waiting on this conversation. Context pressure compacts in place. Hitting the
manager tool-round slice keeps the same turn going. In-flight sub-agents
survive a pursuing turn that ends while they are still running. A continuation
that makes no tool progress stops auto-continue until you hit **Start**.
Hitting the auto-continue budget, or a stop, pauses the same way. The final
automatic continuation requires collecting or explicitly discarding pending
workers when no active conversation wake will take over. If it hits an
obstacle it cannot pass, it calls `block_goal` and stops. A crashed ordinary
turn that is not a recoverable model error does the same, and the banner shows
the public error.

An unverified scheduling claim gets one correction in that turn. Three
consecutive turns rejected for that reason pause pursuit with a separate limit
and retain each turn's error. Ordinary messages still leave that pause in
place. **Start** explicitly resumes it. Truncated tool JSON, a `429`, a
dropped stream, or a mid-stream TCP read timeout retries in-turn. The
transcript names the model service for a rate limit, an outage, or a dropped
connection. A concrete API rejection stays as that status and cause. Three
consecutive worker lifetime watchdog failures stop automatic resume: the turn
errors, its goal blocks, and its recurring check pauses. Inspect the worker
trace, model, and task before an explicit new message or **Start** retries.
Provider idle timeouts are separate. A context-window rejection is named and
compacted once. If the next call still does not fit, the turn stops. A
repeating scheduled check is paused so it cannot send the same prompt again.

**Start** resumes after a block, a cap, a hold, or a stop. An ordinary PC or
phone send while paused answers only that message. It preserves the pause and
the auto-turn count, and does not restart goal work. The paused strip and
**Start** remain after that answer. Active pursuit still resets its consecutive
auto-turn count when you send a message. If a goal has no observable
deliverable or stopping condition, the manager is instructed to use `ask_user`
once for that objective text before a new investigation slice. Answering that
question does not mark the goal complete. The goal strip stays available while
unfinished, including when parked on a scheduled wait. Start does not appear
while the objective is still pursuing between auto-continue sessions.

The goal text is editable in place, and a live turn is told immediately. The
runtime starts the next turn itself. A queued follow-up still wins.

**plan** unmounts write, edit, exec, and similar tools. The manager explores,
asks, and writes `$ZWAI_HOME/plans/<thread>/PLAN.md`. Edit it on the banner,
then **Implement** to remount those tools and start the work. Entering plan
pauses an open `/goal`. Implement does not resume it.

**compact** folds earlier turns into a briefing for later prompts. The
transcript you see does not change. A secondary **Context compressed** status
offers **View compressed briefing**. Old tool results that can be re-read are
cleared first. The briefing prefers a rolling session summary extracted from
the event log, so a long `/goal` does not teach the project's memory from a
compacted view. The same fold also runs on its own before a manager call would
exceed the model window after its configured maximum output is included. With
no known window it uses **Settings → Swarm → Auto-compact at (tokens)**
(default 80 000). Pin a cheaper summarizer under Settings → Models the same
way as the conversation namer. The compressed-context preview shows the
briefing prose. Internal lifecycle records remain available to later turns
and diagnostics.

Caps live in Settings → Swarm. See [swarm](CONFIG.md#swarm).

## Scheduled waits

The manager can arm a wake on this conversation (`schedule_wake`) or, on a
human turn, an independent job (`schedule_task`). `schedule_wake` does not
take an id. It arms or replaces the open wait. The host assigns the id.
Cancel copies an id from the open-wait list.

A successful check with another wake scheduled keeps its unfinished sub-agents
running for that next check. Pausing or cancelling the last wake stops workers
that have no continuing goal. A failed check also keeps workers that still
belong to an active conversation wake or an executable standing goal. Their
original IDs stay active in Agents. The check stays failed and unread. It does
not block the goal or immediately start another goal turn. The next scheduled
check, or your next message, can collect those workers. A consumed one-shot,
a paused, blocked, or completed goal, plan mode, or an exhausted goal budget
cannot keep an otherwise unowned worker running. Stop and resource or watchdog
termination still stop the workers.

If a recurring check still exceeds the model context window, its failed run
shows how to reduce the output budget or request, and that check pauses.
Replayed messages without billed usage, including Chinese and other non-ASCII
text, count toward compaction before the next model call. Resume a paused
check after changing the setting or the conversation.

If you explicitly prohibit parking this conversation, the engine preserves
that instruction and refuses both a new or replaced wake and a scheduled
report's `next_in_s`. The wait row stays unchanged. The human source is in
that turn's Trace. Assistant prose cannot create an armed-wait card. Only a
later explicit human allowance releases the prohibition. Reporting findings
without rescheduling, and cancelling a wait, still work. A source awaiting
policy classification also prevents arming until it can be checked. Manually
cancelling a conversation wait also withdraws that permission, including a
consumed one-shot or a wait the manager already cancelled. An ordinary
continuation keeps the restriction. Repeating the old cancellation preserves
any later explicit permission.

When progress is gated on time or a condition not worth polling now, the
manager is told to wake and end the turn when your conversation policy allows
it. A parallel `exec` that sleeps and then checks progress is fine. That sleep
and `schedule_wake` both bias the estimated remaining time short, so a check
lands before the work is already done. Extra checks are expected. A named
clock time you asked for is still honored.

**Scheduled** opens the wait list in the left column. The title sits beside a
search icon. **New task** is the next row. **Upcoming** is the active list.
The menu on that row filters **Active / Paused / Completed**. **New task**
opens the editor in the task column: the task description, then Details
(**Runs in**: a new conversation each run, or wake an existing one;
**Project** is the workspace for a minted conversation) and Frequency.
**Expand** hides the wait list so a long instruction can use the task column.
Click a wait to edit it. Changing cadence recomputes the next run from now.
Changing Runs in or Project does not. **Runs in** searches live conversations,
or starts a new one each fire. **Project** is only for that new conversation.
Pause, resume, cancel, and Run now live there too. A finished wait keeps the
destination it was given. A run already in progress stays on the conversation
it started on. A short inbox name is generated, the same namer as conversations.

An open conversation's right rail lists active or paused waits aimed at that
chat, and any job whose run created it. Completed or cancelled conversation
wakes leave the rail. Their history remains in Scheduled → Completed. With no
current waits or creating job, the rail's scheduled section is hidden. A
one-shot leaves that section when claimed. Its current execution can still be
Working. Click a row to open that task.

An active wake on the open conversation shows a banner with the next check and
the wait's title or prompt, **Run now**, and **Cancel wait**. The same actions
sit on the armed-wait notice. Cancelling a thread wake also prevents the
manager from arming another wait in that conversation until you explicitly
allow waiting again. Sending “continue” resumes work and keeps that
restriction. Cancelling an active or paused wait while idle continues a
standing `/goal`. Run now fires the check without waiting for the timer. The
next-check time comes from the live schedule row.

A scheduled check can return its result directly as text. Tools remain
optional. `report_schedule` can also report the result. Explicit empty
findings stay quiet. Only a blank response with no tool result or report
continues once in the same turn. A second empty response visibly fails. This
also applies to **Run now**. Quiet standalone runs stay out of Recents.
Findings open from the inbox: **Open findings** on a live wait, with a count
when several fires are unread. Workers cannot schedule.

A conversation whose next turn is a parked wait keeps a clock in the list, so
it does not look Idle. The title bar says **Waiting**. That banner is not a
row in **Pinned**. A collapsed folder that still has a live turn or a wait
keeps the mark on the directory.

A task that finishes while you are elsewhere keeps a static completion mark,
different from the running pulse. Collapsed projects show it at the right of
their title. The paired phone keeps unread completions above its folders. Open
a task to read its rendered result and clear the mark. Automatic reconnect and
background polls leave it unread. A failed read acknowledgement keeps the mark
and shows the error. Quiet checks and user cancellation do not create a
completion mark. The phone hides an earlier unread completion while that
conversation is running, waiting, or asking for an answer. The result stays
unread and can reappear when the conversation becomes idle. That does not send
a read acknowledgement. Status is matched by conversation ID, so separate
tasks with the same title remain separate. Older PCs that omit completion
watermarks keep ordinary phone conversations and scheduled checks usable. The
phone does not send the optional read acknowledgement to those hosts. See
[F-125](FEATURES.md#f-125-unread-completion-attention).

## Files and the workspace

Every standalone conversation has its own directory
(`~/.zwai-swarm/workspaces/<thread-id>/`). Uploads land in `uploads/`. Agent
output lands next to it. The **Files** tab is a collapsible tree: filter at
the top, directories expand, download on hover. In the desktop app the toolbar
opens the workspace in the file manager. The selected directory keeps a button
to open that folder. Click a file or press Enter to preview it. HTML opens in
a sandbox without access to the app origin and can switch back to source. CSV
and TSV open as tables. DOCX, XLSX, PPTX, and EPUB show their saved package
content. SVG stays source. Images offer zoom, actual size, and fit to window.
Enlarged images scroll in both directions. Refreshing or reopening restores
the default fit. The tree is the workspace root, listed breadth-first so a
large generated folder cannot hide the rest.

The title-bar terminal (⌘J, and every click on the icon) starts a real shell
in that same directory — a project's working directory when the conversation
is in one. The integrated terminal follows the app's light, dark, and color
theme, including a hidden tab. Appearance **System** follows the system while
it is running. Explicit Light or Dark stays fixed.

## Projects

Work that comes back belongs in a project. The title bar prefixes the
conversation name with the project's (`project · title`). The project list
follows last use. Drag a row to pin it. **New conversation** is on
**Conversations**, not on the project list. Hover a project for a
new-conversation control on the row itself. Click a folder to collapse its
topics. On **Projects**, click **Pinned** or **Projects** to fold that
section. Pin a topic from the row menu to keep it in **Pinned**.

The sidebar lists each project's conversations under its name. A folder, and
Conversations, shows the five conversations active in the last seven days.
**Show more** reveals the rest. A conversation that is mid-turn keeps a
progress mark in that column even if you are looking at another one.

**New project** asks for three things:

- **an instruction**, added to the system prompt of every conversation in the
  project. It is the business context. Settings → Personality is how you like
  to be worked with. When the two conflict, this project wins;
- **a working directory**, an absolute path that already exists. Every
  conversation in the project reads and writes it directly, with your
  permissions. Leave it empty and zwai keeps one for you;
- **memory**, on by default.

When a durable project instruction changes, ask the manager in a conversation
in that project to update its global description. It saves the revised
description separately from Memory and Skills, even if Memory is disabled.
The new instruction reaches the manager on subsequent turns, including in
other conversations in the same project. If someone edited the description
while the manager was working, start a new turn to reconcile the changes.
You can inspect the saved description in the project's **Edit** dialog.

In the desktop window, **Choose directory** opens the system folder picker.
Cancelling keeps the current value. A selection error appears below the field.
The browser keeps manual path entry and the empty managed-directory option.
Saving checks that the selected directory still exists. A non-empty working
directory keeps its path whitespace, including a folder name ending in a
space. Empty or all-whitespace still uses the managed directory. See
[C-026](CONTRACTS.md#c-026-project-working-directory-selection).

Use the project's menu → **Summarize project history** to open an explicitly
titled conversation in that project and request a source-backed report. This
keeps your existing composer draft in its original conversation. You can also
ask an existing conversation about earlier discussions. The manager and its
workers choose their own read-only SQL through `query_history`. Memory can be
off. Only the menu entry produces the host-rendered source-backed report.
An ordinary history request gets an ordinary answer. The SQL appears in that
tool's row and the Trace tab.

In a project conversation, the SQL views expose only that project's retained
records. Query predicates cannot switch projects. A standalone conversation
retains access to history across projects. Agents keep their existing full
filesystem and exec access. The report displays complete cited original
records, followed by labelled suggestions and open questions. Historical
statements do not establish approval, current implementation, or independent
verification. Only the cited range is reported. Reading history does not
itself save a summary or change Memory. See
[C-036](CONTRACTS.md#c-036-original-history-is-queryable-through-read-only-sql)
and [table semantics](DATA_MODEL.md#read-only-history-sql).

**Nuwa collaboration** is enabled on the project, with a planner model and an
executor model. Mode changes apply to subsequent turns. Current workers keep
their configuration. See [Nuwa](NUWA.md).

## Memory and skills

With memory and auto-review on, each successful turn is reviewed even if the
manager already wrote memory. A failed project turn may retain explicit human
requirements as notes, without learning skills from failed work. Cancellation
does not start an automatic review. Notes are short facts whose absence would
misdirect future work. Skills are reusable procedures. Constraints needed only
inside one workflow belong in that skill.

The next conversation in that project starts with the notes in its prompt and
an index of the skills, and opens a skill when it needs one. Sub-agents on
that conversation get the same snapshot and can open a skill. They cannot
write the store. Their report back to the manager is the task result, plus at
most a short durable note if something would change later work.

A conversation that belongs to no project does not grow those notes. A finished
turn can still record a procedure in the shared skill library
(`library/skills/` under the data directory). The next conversation outside a
project starts with that index. Open **Memory** on that conversation to read
the files, then **Copy** one into a project. **Update** on the copy pulls a
later change from the library.

Skills are behind **View skills** on the project menu, which opens the
**Memory** tab. Notes are editable. **Save notes** appears only after the
draft differs from what is stored. Skills can be read, copied, and deleted.
**Copy** writes a separate copy into another project. Editing either side
leaves the other alone. The copy remembers where it came from. **Update**
pulls a later change from that project when this copy was not edited. If both
sides changed, it asks before replacing the local steps. Nothing is written
back. An unmodified copy still tracks the original project. An edited copy is
what the next project receives.

**Review now** re-reads the last successfully completed turn. Note changes
stay in a draft until the complete proposal passes validation and its original
revision still matches. A refusal or conflict leaves the stored notes intact.
Skill changes remain immediate and may survive a later note refusal. This is
not one transaction across notes and skills. If review fails, the transcript
and Memory panel show its stopping reason, proposal count, and refusal
categories. They distinguish notes that did not commit from skill changes
already saved. The failure's **Trace** button opens the original turn's Full
log. These checks verify source identity, exact quotes, and numeric spelling.
They do not prove semantic truth. Set **Settings → Memory → Review thinking**
to choose a separate reasoning level. The default uses **Low**. Choose
**Inherit conversation** to preserve the reviewed turn's choice. Only the
default is optional: if the endpoint rejects it with HTTP 400, review retries
once without `reasoning_effort`. An explicitly chosen or inherited level keeps
the endpoint's error. See
[C-025](CONTRACTS.md#c-025-memory-review-evidence-and-commit).

The panel says when it is reading and what it decided, including when it kept
nothing. A write that landed is also named in the transcript (`Memory updated:
…`). **View details** expands the saved additions and deletions. If you were
mid-edit when a review wrote, the panel lets you keep yours or take the new
ones. Correcting a wrong note matters: it would otherwise be repeated in every
future conversation.

Notes are budgeted (`memory.char_limit`, default 2200 characters; one agent
note is also capped at `memory.entry_max`, default 360). Once full, a write
that would grow the notes is refused. Review can shorten or replace
source-supported related notes before adding a new one. It must not clear
unrelated notes to make room. Creating a skill that already covers the same
subject is refused. The result names the existing skill so the next call is a
patch. Skills that share a name stem are refused the same way, and leftover
families are folded into one skill after a successful turn, including when
auto-review is off. Failed-turn requirements-only reviews do not fold skills.
Editing a skill file by hand does not fold until the next turn. **Tidy memory**
on the Memory heading still folds leftover name-stem families, then asks the
model to curate the whole catalog. That is a real reviewer call. Reload first
if you edited files outside the app.

Requested project **Tidy memory** also merges duplicate skills, reviews stale
candidates, and corrects notes/skills classification. Moves preserve source
text. The target is saved before deleting the source, so failure may leave
duplicates. Retry reuses an identical target and never overwrites different
content. Post-turn review and workers cannot move memory.

The Memory panel shows agent view counts, the last view time, what each skill
costs, and how much of the index comes from skills no agent has opened.
Settings → Memory controls the stale-candidate threshold
(`memory.skill_stale_days`, default 90 days). Stale skills are candidates for
review, never automatically deleted. Usage does not rewrite skill content.

Session briefings and successful-turn reviews also receive worker lifecycle
facts from the stored event log. A historical worker being restored is
different from a new worker, and a refused creation call creates none. Earlier
briefings and assistant claims are labelled as model-written claims. This does
not delete existing notes or skills. Correct a wrong saved note in Memory and
inspect the relevant turn in Trace.

Memory lives in the data directory, never in your working directory. A
procedure that already lives in the repository is a file there, not one of
these skills. Deleting a project deletes its conversations and its memory.
Files in a working directory you chose are left alone. Budgets are in
[memory](CONFIG.md#memory).

### External skills

From the project menu, open **External skills** and choose **Install external
skill**. Select a ZIP with one [Agent Skills](https://agentskills.io/specification)
package. Put `SKILL.md` and its resources at the ZIP root or in one enclosing
directory. Packages from Codex, Hermes, or Claude can use the same layout.
zwai reads the standard `name` and `description` fields and preserves the full
package.

After installation, a new turn in that project can select an external skill by
its summary or by the name you give it. `load_skill` opens the instructions
and supplies the package directory. Install packages from sources you trust.
Any later script execution uses the agent's existing tools and access.
External skills work with project memory disabled and may share a name with a
learned skill. To replace one, remove that external package before installing
its replacement. A downloaded package stays in the selected project.

## Keyboard

Enter sends (or queues a follow-up while a turn is running) · ⌘Enter steers
the draft into the current turn · Shift+Enter a newline · `/` at the start of
the box opens built-in commands · `⌘K` command palette (titles, bodies, and
meaning when embeddings are on) · `⌘N` new conversation · `⌘F` find in the
open conversation · `⌘B` hide or show the list beside the icon rail · `⌘\`
toggle the right panel · `⌘J` open a terminal in the current project or
conversation directory · `⌘,` settings · `Esc` closes the active preview, find
bar, or other local overlay, and never stops a turn.

## Account and a paired phone

The circular **Account** button at the bottom of the left rail opens the
account menu, even when the conversation list is hidden. **Sign in with
Gateway** opens its own account dialog and the system browser. Opening the
menu does not start sign-in. Compare the public code and the PC fingerprint
before authorizing the device. Explicit sign-in enables Gateway and phone
connections and preserves your configured Hub and other settings. The address
stays in configuration. Settings does not show or edit the server, Hub
address, Host Token, authentication mode, computer name, or pairing limits.

**Settings → Phone** shows account status, **Sign in**, and a switch for this
PC's connection. Opening Phone does not start sign-in. If a login result and
the configuration read-back are both unavailable, Phone offers **Reload
account settings** before allowing another login. An authorized account uses
its name's initial as a local avatar. **Signed in · PC offline** distinguishes
account authorization from the actual hub connection. A failed registration
keeps that sign-in and retries on its own. **Refresh account status** only
reads the current result. Stop, sign-out, and saving settings cancel the
pending retry. Local conversations remain usable without signing in.

**Account → Sign out** stops this PC's remote connections and confirms device
revocation before removing the corresponding local private credential. During
**Signing out of this PC…**, another login or logout cannot start. A failed
confirmation retains private credentials for retry. An older Gateway without
device logout must be upgraded first. Successful sign-out preserves local
conversations, model configuration, device identity, and unrelated Hub
credentials. It does not sign out every device or the browser account.
**Cancel sign-in** only stops the current flow. To sign in again, explicitly
choose **Sign in**.

**Keep this computer awake** stays on by default so a plugged-in host does not
idle-sleep while a phone still expects it. Settings → Phone shows that switch
only after phone connection is enabled. **Bound phones** names each device by
the model the phone reports after it connects. A phone that is already bound
stays listed so it can be revoked. Settings → Phone does not show a pairing
QR. Sign in on the phone with the same account, then select this computer.

If an administrator deletes this PC or revokes its connection credential, use
**Reconnect as a new device** and confirm, then authorize in the browser
again. A revoked credential stays revoked. The existing local credential is
replaced only after the new authorization succeeds. If it asks you to retry
the previous login first, use **Sign in**. Do not create another device while
the earlier result is uncertain.

Anonymous pairing uses the configured Hub and `remote.enabled` /
`remote.auth_mode`. Settings does not show or edit those fields. Legacy
remains the compatible configuration default: this PC mints its hidden Host
Token locally. Existing legacy connections keep their mode until you change
the configuration or choose account sign-in. A new account waits for
administrator approval. Approval alone does not authorize a device: return to
the browser confirmation and confirm it. An existing anonymous pairing moves
to account management when both devices sign in to the same account, matching
the complete Hub address and full PC public key.

Traffic starts on the hub as ciphertext and upgrades to UDP when punching
works. Conversations never enter the hub database. Keys are in
[remote](CONFIG.md#remote). Everything the phone itself does — inbox, files,
direct chat, updates — is in [mobile/README.md](../mobile/README.md).

Signed-in accounts use the server's **Web Search** and **Web Fetch** by
default. On Desktop, **Settings → Tools → Web tools run on** selects
**Automatic**, **This device**, or **Server cloud**. Automatic uses the
account after Gateway authorization and stays local while anonymous. A cloud
failure remains visible. New execution choices apply from the next turn. The
phone has its own two choices, saved by Gateway and account. See
[tools](CONFIG.md#tools) and the [phone guide](../mobile/README.md).

## Updates

Released macOS apps check for updates at launch and every six hours while
open. Returning to the window or reconnecting the network checks again when
due. Background failures stay quiet. A newer Mac installer shows **Update**
and **Not now**. Update downloads and replaces this `zwai.app`, then reopens
it with the same data directory. The banner shows downloaded bytes and, when
the installer size is known, a percentage, then installation and restart.
Errors use a red alert. **Retry** after installation failure retries the
confirmed version. A failed check retries the check. **Menu → Check for
updates** checks immediately. A launch-command failure restores the previous
app and leaves its window open. Two engines using different data directories
still share one installer lock for this app. A competing install reports busy.
Separate app bundles can update independently. Installation requires
confirmation. Windows amd64 and arm64 builds are separate downloads,
`zwai-<version>-windows-<arch>.zip`, each containing `zwai.exe`. Replace the
old program with that file yourself. The desktop window needs the WebView2
runtime. The in-app updater does not install Windows or Linux builds, and an
unversioned development build has no installer in this channel.

Android sideload builds offer **Update** on the phone and install from GitHub
Releases. iOS beta builds update through TestFlight. The app does not show
the GitHub update check there. See the [phone guide](../mobile/README.md) and
[release](RELEASE.md).

## Startup and a killed process

The desktop confirms an engine presence connection before loading its window
and keeps it while the native app is open, including page reloads. Reopening
as the old engine becomes idle waits for that holder to leave or be replaced,
including a new process at the same address, and checks its identity again.
If the held connection drops, the native app retries that engine within a
bounded recovery budget. If it cannot restore the connection, it records the
cause in the desktop log before closing. Native shutdown may terminate the
process without returning to the CLI.

A cold launch allows up to two minutes for local initialization after frontend
preparation and reports progress every ten seconds. Phone registration runs in
the background, so its network wait does not hold the window closed. Startup
errors show the current attempt's log and actual process failure.

The desktop and Web window can attach while unfinished work is restoring. PC
and phone show recovery progress and keep saved history available. New work
waits until recovery ends. Individual recovery failures leave a visible
failure count and the original Trace. Other work stays usable. Stop remains
available and is persisted before that conversation can resume. Goal editing
and its keyboard shortcuts pause during recovery. A refused goal edit stays
open with its text. Readiness never submits it automatically. The scheduler
starts after recovery.

Opening a newer or different build prepares the checkout frontend while the
old engine keeps serving, then requests a handoff and replaces the engine
within a 30-second handoff and listener budget. A frontend preparation error
leaves the old engine serving. Active and background sub-agents retain their
IDs, histories, unread messages, and confirmed results. A task does not become
completed because it was summarized. Questions and iteration-cap pauses still
wait for you. Checkpoint failures stop replacement with an explicit error. A
failed handoff keeps execution frozen and its checkpoints retryable. It does
not automatically resume unknown external operations. An older engine without
handoff support uses bounded event-based recovery. Its unsaved messages cannot
be reconstructed. A replacement that has already accepted handoff is not killed
by the old 30-second launch budget. The command-line detail is in
[desktop](CLI.md#zwai-desktop).

Kill the process, quit the window, or lose power mid-turn: the next start
continues every leftover conversation. Answers already on screen stay in the
model's context. Sub-agents that were still working restart under the same
ids. Queued follow-ups still wait to run after that turn. A file or shell tool
that was in flight is marked stopped. Pressing **Stop** is the one thing that
does not come back.

Conversations name themselves. The first message is a placeholder. After the
first reply a short title replaces it. A name you type is never overwritten.
Switch it off in Settings → Swarm, or pin a cheaper model under Settings →
Models.

## Other tasks on this PC

Managers can discover other active tasks on this PC with `list_tasks`, compare
actual workspaces, and negotiate through `send_task_message` and
`read_task_messages`. Messages show their source in desktop and paired-phone
conversations and remain in the event log when a recipient stops or restarts.
Reading uses a saved cursor by default. Explicit cursors recover earlier
pages. The capability is advisory: wait for a peer's explicit agreement before
relying on file division or execution order. It does not lock resources, wake
idle tasks, interrupt work, answer a human pause, or grant new permissions.
See [F-135](FEATURES.md#f-135-independent-task-coordination).

`clients.enabled` lists Claude, Codex, and Cursor sessions already on this
machine. Settings does not show that page. The behavior of the list is in
[clients](CONFIG.md#clients).

## External MCP tools

Open **Settings → MCP** on Desktop or Web to add a local stdio command or a
remote Streamable HTTP/SSE server. Enter a display name and the connection.
The app assigns the id and leaves the name, command, arguments and URL as
typed. Save the connection, then choose **Test connection and list tools**. You can inspect a disabled server before enabling
it. A failed inspection stays visible and can be retried. The row uses one
colored mark for enabled or disabled, and another for the latest check.
Opening a tool highlights its input schema.

Enabled servers add two namespaced Agent tools: `list_tools` and `call_tool`.
They connect only when called, so an unavailable server does not hold up
starting a turn. The composer Tools menu and Settings → MCP share the
server's enabled switch. Plan and Nuwa read-only toolsets do not mount MCP
tools. Other Nuwa turns make them available to workers and keep the planner's
toolset separate. If a captured session is closed, old tools return
`session_expired` without reconnecting. Continue with a new turn. Keep secrets
in the write-only environment and header fields. Blank preserves the saved
map. `{}` clears it. Local commands run with the conversation workspace as
their working directory and retain the application's filesystem access. This
release supports MCP tools. Resources, prompts, and OAuth login are not
included. See [MCP](CONFIG.md#external-mcp-servers) and the
[MCP API](API.md#get-apimcp).

## Provider cloud tools

Settings → Models → open a provider, select **Responses**, and configure its
cloud tools JSON, for example `[{"type":"web_search"}]`. The endpoint and
model must support that API and tool. The default remains Chat Completions
with no cloud tools. Cloud calls run at the provider, coexist with local Agent
functions, and appear in the same turn's Trace. Web sources remain clickable
in answers. Paired phone sessions using the PC inherit its provider
configuration. Independent phone direct chat has its own configuration. See
[provider cloud tools](CONFIG.md#provider-cloud-tools).
