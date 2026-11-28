# zwai phone

Automatic voice capture converts MP4/AAC, WebM/Opus and Ogg recordings to
16 kHz mono PCM WAV before recognition. Newer iPhones may record WebM; those
recordings use the same conversion instead of uploading an unsupported container.
Recognition results remove a complete leading `language …<asr_text>` envelope
before inserting the transcript into the draft; ordinary text remains intact.
Voice service failures show the returned HTTP status without exposing upstream
error bodies, separately from network and microphone-permission failures.

Gateway model catalogs refresh after the selected PC acknowledges a mode change
and when account model permissions change. Older PCs that omit model capability
metadata show an update notice; the phone does not borrow its own grant to
authorize a PC model. Unreadable voice HTTP responses show a network/Gateway
configuration error separately from microphone permission failures. A Gateway
CORS response defect requires a Gateway update before voice can succeed.

iOS and Android apps. **Sign in** uses the built-in default Gateway
(`https://ai.fofa.info:2440`). No manual address, Key or Pairlink setup is
required. Finish device authorization in the system browser. Pending/rejected
accounts show only review status. Approved accounts automatically connect all enabled online PCs, including on a
returning launch. **User center** and the PC rail show online PCs first and
offline PCs afterwards. An existing online selection stays selected; otherwise
the first available PC opens without a QR or a manual Connect action. PC
conversations require a same-account PC and a ready engine. The waiting page
explains how to connect the desktop app and keeps **Chat** in the top bar.
Chat talks directly to the Gateway using the mobile login and its authorized
conversation models, including when no PC is online. Background PC connections
leave Chat in place. A disconnect on the PC page preserves its mounted draft
and history for reconnection.

In a PC conversation or the phone's direct Chat, ask for an image or enter
`/image` followed by a description. The selected chat needs an authorized
gateway image source. The paired PC owns its generation. Direct Chat sends
using this phone's account and saves the pixels on this device. Tap the finished
image to enlarge it or use the save action to export the original file.

Voice input and read aloud automatically use this phone account’s authorized
Gateway models, even if the PC has no voice configuration. Each direction follows
its own advertised role and source. Missing role defaults are resolved from the
source’s authorized typed model catalog; no model-name guessing is used. Calls
use this phone’s same-origin Mobile Bearer and the source’s API base. Gateway
voice does not need a live PC voice service. Account-mode direct Chat uses the
phone's Gateway speech settings. Dictation remains an editable draft and never
sends automatically.

During recording the composer shows an inline timer, microphone levels and
separate icon buttons to cancel or transcribe. While recognition runs, the
draft is visible and editable again, with recognition status in the composer.

An enabled personal PC voice configuration overrides its direction while that
PC is live and ready. LOCAL phone personal voice settings can also override each
direction. Private PC credentials never enter the phone. Unknown Gateway grants
keep its voice controls unavailable with **User center → Retry**; saved personal
choices remain intact. Ordinary PC metadata polling and PC disconnection do not
interrupt unchanged phone-owned Gateway speech.

Each account-managed PC connects with its own Pairlink ticket and WebSocket.
The Gateway's online badge reflects PC presence. The mobile connection becomes
ready after the phone and PC complete their encrypted handshake and the PC
engine reports readiness. Legacy QR connections can share a socket per phone
public key and full Hub endpoint. Every PC retains its encrypted session and
RPC waiters. Closing one PC releases only its channel. Heartbeats and
background warming reuse live connections.

The phone account’s automatic Gateway output uses **PCM (streaming playback)**
with SSE and 200-character segments, allowing audio to play before a segment
finishes. The speech service must provide mono signed-16 LE PCM at 24 kHz.
Explicit PC or personal output settings retain their format and segment limits.
Select PCM in those settings for incremental direct HTTP playback; MP3/WAV and
the encrypted PC-local route wait for a completed segment.
A paired PC’s explicit Voice input or Read aloud opt-out applies to that direction.
When connected, the phone rechecks the preference before a direct Gateway call.
Standalone phone voice follows its own settings.
The PC snapshot declares read-aloud execution separately from dictation: an
ordinary configurable Gateway output uses the phone's matching Mobile login
directly, while personal PC outputs use encrypted PC RPC. Older PC snapshots
without the output route retain their global execution setting. Gateway PCM
requests select SSE; standalone personal services retain their own audio protocol.
User center shows the execution source beside each direction so mixed PC/Gateway
settings remain visible.

After connecting to a PC, open **Menu → Manage projects**, or **Manage projects**
from New conversation. Create or edit the PC's project name, shared instruction,
existing PC working directory (blank uses a managed directory) and project memory.
Settings apply to subsequent tasks, while running workers retain their model.

In a paired conversation, **Files** opens the task's **Results** first. Its
working files and uploads are separate. **Project folder** is a read-only
view of the current project's files. Older PCs show their existing single
file view.
The phone sends only changed fields when editing, so changing a name does not
overwrite an instruction changed on the PC. A failed save retains the draft for
retry. Switching PCs or leaving the editor invalidates its pending UI responses.
This requires a PC version supporting the project RPCs; older PCs show an update
message. No project settings are stored in the phone's direct-model Chat scope.
Recent desktop/mobile differences are tracked in [the parity audit](../docs/MOBILE_DESKTOP_PARITY.md).

In a paired conversation, **Work records** shows what the agents found as the
same graph the desktop panel draws, with their ordered to-dos below. Tap a to-do
to mark what it touched, or a subject to filter the list. It is read-only. A
turn that used tools and has not recorded them gets one chance to write the
graph before its answer is kept; the phone shows that wait as a short notice. The
phone has no event stream: it polls the PC with the revision and graph owner
it holds, every 3 seconds while the task runs and every 10 seconds otherwise.
The PC answers unchanged only when both still match, and a failed poll keeps
the picture on screen. A
graph too large for one frame is refused with a message; open it on the desktop.
The entry is hidden when the PC has work records switched off or is too old to
serve them. Credential values never reach the phone.

Paired PC conversations can show the same task panels as desktop. A matching
panel opens once, and **Task panels** in the conversation header reopens it.
The full-screen view supports touch zoom, rotation and switching between
available panels. Returning to the conversation keeps the map position and collection continues. **Close panel instance** removes the selected instance across app restarts and interrupts only its collection. Progress and asset points update through the existing encrypted
connection; reconnecting reads saved results without restarting collection.
Conversations without a panel keep their ordinary header. An incompatible
plugin reports an update requirement rather than loading different code.
In the EASM panel, Android Back closes an open supply-chain detail first.
The next Back hides the panel sheet and returns to the underlying screen.
See [Task panels](../docs/TASK_PANELS.md) for plugin authoring and data rules.

Conversation Mermaid fences open as fit-to-width/height diagrams with touch zoom,
actual size and copyable source. Incomplete, invalid, resource-bearing, or
over-paint-budget diagrams remain code. In paired PC conversations, an idle empty composer can show the
PC's latest next-step suggestion. Tap it or press Tab to edit it as a draft;
Send remains explicit. The PC settings switch also controls this phone hint.
Selected text in the main transcript or a child agent's log opens the same
high-contrast action panel, with 44px touch targets and a close button.
On a child agent page, **Add to chat** returns to the parent conversation with
the quote in the draft; it does not send the message. Read aloud is available when the
selected PC’s speech output is enabled and ready, using the same phone speech
route and player as completed answers. In a Chinese UI, English selections
also offer Translate; the PC uses its translation role or current conversation
model. The translation stays in a bounded panel and creates no turn or quote.
Older PCs show an update message. Closing the panel, changing selection or
conversation, leaving the screen or backgrounding cancels owned work. Tapping
outside the panel dismisses it without replacing the system text menu.

The iOS shell uses UIKit's single-window scene lifecycle, including when built
with the iOS 27 SDK. The scene loads the existing Main storyboard and retains
Capacitor URL and universal-link delivery on cold launch and while running.
Its iOS sign-in browser presents from the existing scene so the login sheet
remains visible after scene adoption.
For packaged cold-launch/background/relaunch regression, use
`e2e/ios-scene-lifecycle.swift` with the disposable native fixture described in
[Testing](../docs/TESTING.md#ios-scene-startup-regression).

A new phone opens on **Sign in**. Account mode's **Chat** uses the account's
authorized Gateway sources. Personal models and anonymous QR are LOCAL features.
The existing three taps on the User center
portrait toggle LOCAL debug mode, retaining login and saved data. When a selected
PC is connected, the gesture first requests its install-wide execution mode
(`local`, or `fofa` when returning to Account) over encrypted Pairlink. The PC
uses its own login to list authorized original and third-party catalogs.
A running chat reply keeps its original model while the mode changes. Only
an exact success acknowledgement commits the phone preference; busy, unsupported,
failed or changed-link responses keep the prior phone mode and show retry guidance.
A timeout may have changed that PC: inspect it before explicitly retrying. There is
no queued switch on reconnect or when choosing another PC. With no connected PC,
only the phone preference changes. Each touch counts once. A successful switch
keeps User center open, including during automatic PC connection. Close reveals
the selected mode. Switching rebuilds the phone page; unsent composer
drafts are not retained. The debug switch works even when a managed third-party
account hides its model catalog. LOCAL restores personal models and anonymous
pairing under **Other ways**. When signed in, it independently reads the authorized
private preset and Gateway catalogs and groups them beside personal providers.
A managed preset contributes only its authorized conversation binding to the
picker. Translation, titles, and compaction remain service functions.
A denied or empty source has no group; model names shared across sources keep
separate routes. Source credentials and catalogs stay in memory.
The phone prepares each online PC independently, reuses saved pairing tickets
and retains its live connections. A background connection never switches an
already selected view. Offline PCs remain visible and are not dialed until the
Gateway reports them online.

User center shows the Gateway and PC display names provided by the Gateway.
A safe public `avatar_url` on `fofa.info`, `nosec.org`, `baimaohui.net`, or a subdomain of one of those roots is shown
beside the account name without sending a referrer. Another host is not
requested. If the address is missing or the picture fails to load, the name's
initial stays visible.
For older Gateways, it uses the readable part of a generated Gateway username
and the PC name or hostname when available. The PC name selected from the
account list is also shown after connecting.

If this phone already paired anonymously, signing both devices into the same
account moves that saved PC to account management automatically on account
refresh (also when the desktop comes online later). This requires the same
complete Hub address and full PC public key in the owner's device list. It
keeps one saved PC, its readable label, last thread and current selection; only the
selected PC reconnects, closing the previous transport first. Other Hubs and
other accounts are not merged. Older Gateways without the owner/public-key
fields retain manual PC selection.

If the account Connect request fails, the anonymous record stays available for
retry. Once the server rotates its ticket, the old ticket is retired: a local
save failure pauses that connection and retains the new credential in memory
until a later account refresh saves it, without rotating again. Transport
failure after saving uses the managed ticket for ordinary reconnection. Do not
close/reload the app while a save error is unresolved; otherwise explicitly
select the PC again after fixing local storage. QR re-pairing and account
rotation for the same PC cannot run together. Signing out removes a migrated
PC's managed ticket; it does not restore its retired anonymous ticket.

On the LOCAL welcome page, **Sign in** starts Gateway authorization in one tap.
The welcome page displays progress, review and errors. **Other ways** is
available before sign-in and after a cancelled or failed attempt.
Android sign-in opens the validated verification page in the default external
browser; return to zwai after authorizing. iOS retains its Browser sheet.
Version-read and browser-launch acknowledgements each stop waiting after fifteen
seconds or Sign out. A failed launch can be retried explicitly; the app does not
automatically open another browser or claim to cancel an OS action already sent.

The selected PC publishes `model_policy`. An editable gateway permits the PC's
model picker; a managed third-party preset hides it and project planner/executor
selectors. After a connected-PC debug switch to LOCAL, the composer exposes
**Original interface** groups, and third-party groups titled with the name the
gateway returned, using that PC’s authorized catalogs. Identical model names retain their source IDs and routes.
The paired PC also supplies the hosted conversation role’s display name. The
composer’s Model sheet shows that name while requests keep the role ID.
The phone has no second Official/All models switch. LOCAL Chat
shows third-party presets and Gateway models only when the server authorizes
those sources. A source returning 403 disappears without signing out or hiding
other sources. Refresh retries transient failures while retaining this account's
last catalog. The same authorized Gateway sources back Account-mode Chat. Its
composer reports when no conversation model is available. Sign-out removes account
credentials and managed tickets, retaining private providers and anonymous pairings.
User center does not offer another server address.

Open **User center → Web tools** from the menu or the settings icon in an open
LOCAL conversation after the account is authorized. A signed-out User center
does not show these controls, and Account mode does not show them either. The
preferences and their status summaries live there. Each
Gateway/account has two independent preferences:

- **Phone model calls**: Automatic, Cloud server, or Off. Automatic attaches cloud
  Search/Fetch when the signed-in account is approved, enabled and unexpired;
  otherwise it keeps ordinary model chat. Explicit Cloud server requires that
  session before sending any model request. Off sends ordinary chat without
  these functions. This applies to LOCAL personal endpoints;
  the selected model must support standard function calling.
- **PC conversations**: Follow PC, This PC, or Server cloud. Follow PC always
  clears the conversation override and uses that PC's global setting. Phone
  sign-in does not change it. Explicit choices affect only the target PC
  conversation; Cloud uses the PC's own Gateway authorization.

Changing one preference does not change the other. Earlier shared Local choices
read as phone Off plus PC This PC, while earlier Cloud choices remain explicit
for both. The summary describes the next turn. PC route/account information is
an offline metadata snapshot; a signed-in receipt does not prove live server
API/tool permission. Errors remain visible without switching models, removing
functions and retrying, or executing tools locally.

Phone-direct Chat and Responses calls use ordinary client functions named
`web_search` and `web_fetch`. The complete model function round is validated
before calling `/v1/web/search` or `/v1/web/fetch` with the Mobile credential;
that credential is never used for a personal model provider. Responses resumes
with its response ID when available, otherwise with function-call history.
`render: true` requests the existing browser-action path, including an explicit
minimal wait, so the server must actually render the page. Tool progress and
bounded result previews appear in the existing expandable tool cards. Stop,
sign-out, account replacement and changing the execution location cancel an
active cloud call. Calls are bounded to eight model rounds, sixteen tool calls,
four calls per round, 64 KiB arguments, 128 KiB per result and 512 KiB combined
results for one turn.

Responses terminal output must contain every streamed function call with its
complete, consistent identity and arguments. Terminal frames seal execution.
Repeated response/call receipts fail before another cloud request; reusing a
call ID without a response identity is rejected as ambiguous within that turn.

PC execution requires a connected desktop client. Requests carry only the
execution preference, never the Mobile credential. The PC persists the
conversation preference; changes apply from its next turn, while a resumed
turn keeps its original execution position. Older PCs that cannot acknowledge
this capability refuse an explicit preference and retain the draft; inheritance
remains compatible. The phone has no independent local Web-tool executor.

Gateway model/tool 401 responses invalidate only the matching current account
session. A 403 pauses new account API calls and marks approval unverified while
keeping the current conversation mounted. A successful approved recheck resumes
access; confirmed pending/rejected or a failed recheck isolates account access.
The denial alone does not mean pending review. Gateway 401/403 headers release
the unread response body immediately. A personal model provider's 401 does not
sign out the Gateway account. Responses from an older session cannot clear a
newly signed-in session. Stop/logout also cancels native stream preparation,
including pending listener registration before the request opens.

Anonymous use stays available. Scan QR opens a live viewfinder on a `pairlink:v1:…`
QR: a frame, a beam that sweeps up and down, and a short chime when it reads.
Paste is the same URI, not a second protocol. After bind, a live
turn (or the last thread this phone opened) opens immediately; otherwise
the inbox lists projects and threads. A later launch with saved tickets
paints host chips (the name this PC sent on `hello`/`list`) and a connecting
skeleton; the unbound root page (sign in first, scan under **Other ways**) is only when this
phone has never bound (or after Unlink of the last PC). Add a PC is a
side control that opens a sheet. Android's system back returns from a
conversation, or from that sheet, to the screen under it. The activity
finishes only from the inbox, or from the unbound root page when this phone has
never bound (the header Back control does the same pop). The phone talks to the PC over
pairlink (WebSocket relay). After the ticket socket is up it sends `hello`
with a one-line model (OS + version + device) so Settings → Phone can
name the binding. It keepalives that socket (the hub idle-drops
a quiet connection) and reconnects a drop without unlinking. The chip dot
is green while that socket is up and gray while it is not; relay versus
direct stays on the dot's title, not its color. A failure says whether
nothing answered (server off, network cut, or a timeout, with the machine
reason) or the server refused (its own text). `host offline` is still
"this PC is not on the hub". It never
calls zwai `/api`.

Focus the message field to see the selected model. The thinking icon opens
the level picker. The microphone and Send or Stop share the controls row.

In a PC conversation or New chat, open the model picker and tap **Refresh
models / 刷新模型** to discover the current upstream choices on that PC. The
button shows progress; a failure keeps the old list and can be retried there.
An older PC asks you to update its app. New models appear and retired models
leave the choices, while an explicit selection remains until you change it.
Refreshing does not save a catalog or change the PC's configured default.

On a bound PC's inbox, **Menu → PC timeouts / 电脑超时** loads that PC's saved
worker lifetime and each provider's first-byte and stream-idle limits, in
seconds. Save sends only edited fields. The first-byte limit defaults to 30
seconds and is independent of the provider's stream-idle limit. Increase
first-byte waiting for a slow initial response; stream-idle waiting starts
after the response begins.
New worker settings default to one hour; existing explicit values remain.
Changes affect subsequent model calls and worker lifetimes, without extending
an active worker or restarting a task stopped by three watchdog failures.
Loading or saving failures remain in this sheet with retry; an older PC asks
for an update. Closing or pressing Back dismisses the sheet before leaving the
inbox. Reopening reloads the PC values, including after an uncertain save.
**Menu → Models** configures phone-direct providers separately.

A reasoning-only group has one **Thought / 思考** control per entry, initially
closed. One tap reveals its complete text; no extra activity-summary fold sits
above it. Groups with tools retain their compact work summary.

Switching PCs keeps the already-open connections alive and selects only the
visible PC. Switching back reuses its live connection. A first connection or
lost connection shows the selected PC's connecting state until its engine is
ready, without opening User center. Inactive PCs cannot update the visible
conversation. Removing a PC, changing its credential, signing out or leaving
the phone page releases the affected connections.

The phone is a compact screen on the same conversation as the desktop: same
event kinds and seq (`watch` / `unwatch` / `log`), clipped bodies so a frame
stays under 64KiB. The initial wire snapshot is the live edge on the `watch` RPC
`ready`, already at the tail; a tap shows the chrome before that returns.
Current PCs include the header and tail in that one reply when the phone sends
`include_detail: true`. Older PCs remain usable through a separate header read.
Reconnecting replaces the old device subscription instead of accumulating
duplicate event senders. The PC reads previews and recent turns in indexed
pages; opening a long task does not decode its complete history. If the ready
snapshot has more history but no visible main-conversation items after worker
separation and phone folding, the phone reads existing `log` pages automatically.
It stops at the first visible item, exhausted history or a cursor that cannot
advance. A completion divider already counts as visible; this does not promise
an older answer body whenever the tail contains only a divider. The current
turn, live sequence and streaming output remain current while pages arrive.
A failed page keeps the ready conversation, reports the error once and does not
retry automatically. **Earlier** jumps to the preceding assistant reply.
When the paired computer's gateway blocks a chat request for safety reasons,
the phone shows one notice and lets you compose a different request.
Current PCs send compact history for completed conversations and older pages.
Expand **Thinking and tools** to request those steps, with further pages
available inside the fold when needed. A running conversation retains its live
activity. From three turns onward, the navigation strip highlights the current
prompt as you read. Each button shows up to two lines of the user input.
While following the transcript, the strip keeps the current and previous
buttons fully in view when both fit.
Swipe the strip sideways to browse other prompts. It keeps that position
through live updates until you scroll the transcript again or tap a turn.
Opening a running paired conversation highlights its latest turn, even if
the live page contains only tool activity. When a prompt is outside the live
page and the PC has not supplied a preview, the phone reads a bounded number
of history pages without moving the transcript. Entries with missing input
show **No prompt preview**. Tap a turn to jump to its prompt. When the reply
and prompt are on separate history pages, the phone keeps loading until it
reaches the prompt or the start of the turn. Direct chat uses the same
navigation.
On the inbox, **Chat** and all PC tabs scroll in the same strip. User and
assistant rows render markdown; a path link does not navigate the
webview. Recognised language fences use the existing source syntax colours in
both themes, including C++ tags `cpp`, `c++`, and `cxx`. The same rendering
serves direct model chat. Empty or unknown language tags stay plain; no language
is guessed. Copy code retains the original code text and whitespace as the
stream grows. A `chart` fence paints as a plot with a table of the same rows;
a truncated fence waits, and a finished invalid body stays code. Adjacent thoughts
and tool calls fold into one process row. Opening it shows the individual entries;
each thought has its own **Thought / 思考** control, initially closed, like tool
details. A second tap reveals its complete original text. Each thought retains
your open or closed choice while that entry receives more text or finishes.
Closing the process row and opening it again starts its details closed. An
answer stays on screen and splits that row. The live tail is **Thinking**
/ **Planning next moves** / **Editing** / **Reading** / **Exec**; open
the row for the pieces. A `progress` pulse is not a chat
row. `wait_agents` is a status count, not the `elapsed_ms` roster.

In a paired conversation, each attachment image has a **View image / 查看图片**
button, including in older messages. Tap it to load the selected image. If a
read fails, tap **Retry / 重试**.

A conversation with sub-agents offers **Agents / 子 Agent** above the message
box. Its number counts running workers. That entry and count are visible as
soon as the conversation opens while any worker is still running, including
when the worker started before the first page. The list separates running workers
from finished and failed history. Finished workers and older logs still arrive
with **Earlier**. Opening the list or returning from a worker
log starts at the top, with running workers first. The list does not follow the
chat tail while you browse it. Each row shows role, ID, status, and latest
permitted activity; open a row
for that agent's separate thought, tool, and answer log. **Earlier** loads
older events into the same list and log. Worker answers do not appear as
manager answers. Pairlink strips worker launch instructions and clips event
bodies; the phone does not offer the desktop-only full instruction view.
The header Back button and Android system Back return from a worker log to
the Agents list, then to the conversation, before leaving for the inbox.
Loading Earlier history after several streamed replies keeps the Agents list free
of manager paragraphs and work folds; those remain in the manager conversation.
On iOS, swipe right from the left edge as before, or swipe from the middle
of the content to near the right edge. One completed gesture returns one
layer, dismissing an open model picker first; root screens stay open.
The middle swipe leaves horizontally scrollable content and text editing alone.
Cancelled, vertical, short or incomplete swipes do not return, and swiping
left does not go forward. The gesture does not use browser history.
The `schedule` event is still **A wait is armed.** in the transcript; the live
wait is the banner (next check, **Run now**, **Cancel wait**).
Each finished PC turn leaves a divider stamped with its host completion time,
including scheduled checks whose automatic input is hidden on the phone.
`schedule_report` stays on the `report_schedule` chip.
An unfinished `/goal` shows an explicit **Goal mode** strip with its objective
and Pursuing / Auto-run paused status. The strip stays visible in the chat,
Agents, worker log and Outputs pages. Resume controls stay in the main chat; paused or blocked
objectives retain Start. Completion removes the goal strip immediately and
keeps it hidden after reopening; the stored objective, completion state and
conversation history remain on the PC. A localized completion notice stays in
the phone transcript, including for the JSON `goal_complete` payload. A new
`/goal` displays the strip again.
The objective is one truncated line (the full text stays on the PC); a novel
cannot cover Run now or the composer. Send,
follow-up, steer, stop, ask, `/goal` and `/plan` land on the PC engine; the
other window sees them live. Sending `<objective> /goal` activates that
objective through the PC's shared command parser, including after the old
objective completed. The suffix is not sent to the model as task text.
The paired composer refuses a recognized goal or plan command when at least
one selected-text quotation is nonblank. Send, Follow-up, Steer or Enter then shows a local message asking
you to remove the quotation first. Your text, quotations and selected files
stay in the composer; this refusal does not set a goal, enter plan mode or send
anything to the PC. Remove the nonblank quotations, or change the request into
an ordinary message, then submit again explicitly.
Ordinary quoted requests, paths and literal command text keep their usual
behavior. Pure-text commands still use the PC engine's existing semantics.
**Outputs** above the message box lists the paired PC conversation’s files.
Tap a file path in a paired conversation to open its task preview from the
message. When several files match a name, choose the full path. Back returns
to the message or worker log and keeps the draft and scroll position.
Tasks with their own file root open to **Results**. Tap a folder to load its
complete contents, or search to list every match. A search does not show a shorter list first. Open a
file to preview Markdown (.md/.markdown/.mdx) as a document, known source
suffixes with syntax colors, plain text, a common image, PDF, tables, or supported
Office and EPUB documents. HTML artifacts render in an isolated frame. SVG stays
source text and MDX components never execute. Unknown text stays plain. Text over
256 Ki characters stays complete but skips document parsing and syntax colors.
PNG, JPEG, GIF and WebP can be previewed up to the 32 MiB phone read limit.
Other previews are limited to 8 MiB. While loading, the preview shows received
bytes, total bytes and a percentage. Each new chunk gets a fresh waiting
window. If data is delayed, the preview shows that it is still waiting.
**Download** is available during loading. An early tap uses the active
transfer and starts the download once all bytes arrive. Returning to the
file list cancels the read and any queued download. Other readable files
also show a download action. In the Android/iOS app, **Download** opens the
system save/export picker: choose a location to save the original bytes. A
cancelled picker does not report success; a failed save keeps the preview and
lets you retry. In a browser, Download remains a browser download.
Image previews offer zoom in/out, a percentage, actual size (100%) and fit to
window. They initially fit without enlarging small images; manual zoom spans
10%–400%, with both axes scrollable. Loading or undecodable images disable
zoom. Markdown, source and plain-text previews offer smaller/larger text
(12–28 px, initially 14 px) and reset; headings, tables and code scale together,
including their line spacing.
Preview tables scroll horizontally; vertical swipes continue through the document.
Image and text controls and Download remain outside the scrolling content.
An EPUB shows its filename once in the header. Tap the left or right side of
the reading area to turn one page, including across chapters. Tap the middle
to show or hide the chapter list and Download. They start hidden and appear
over the reading area, so the current page stays in place. Reopening a file
resets its view. Preview adjustments preserve the original download bytes.
Full Settings, PTY and Trace stay on the PC; paired phones also expose PC timeout controls.

The square **Stop** button stays at the right edge of the composer during a
PC turn. A draft also exposes **Follow-up** and, when available, send
options for inserting it into the current turn. Stop keeps the unsent draft.
A parked wait has no live turn to stop. In direct model chat, the square Stop
cancels the stream, keeps the text received so far, and restores Send.

When a PC turn asks a question, pick an option or type Other and tap Submit.
The phone uses the PC's normalized question ID in the answer; a rejected RPC
shows its error and keeps the draft so Submit can be retried. An accepted
answer shows Submitted until the PC resumes the turn.

A conversation sits on the composer rather than under a screen of blank:
a short thread is bottom-aligned, and scrolling away from the tail raises
**Jump to latest**. Answering inside a conversation is one rounded box
(`components/composer.tsx`) that grows with the text up to a cap, Enter
sends (Shift+Enter is a newline, and an IME candidate list is never a
send). Tap the message field to expand it and see the selected model name
and current thinking level. Tap a configurable setting to open its sheet.
Managed selections appear as read-only labels. A long model name can be
truncated to leave room for the thinking level and message actions.
Closing a sheet or choosing an option returns focus to the message field,
keeping the composer expanded and preserving the draft and caret. The composer
and its actions stay above the software keyboard when it appears.
**Thinking off** is saved with the conversation and applies to the next request
in direct Chat or the next turn on the paired PC. If the model still reports
reasoning after the request disables it, the conversation shows an error.
The leftmost control is **Add**. It attaches files and lists `/goal`,
`/plan` and `/image` in a paired PC conversation or new chat. Direct chat
offers `/image` when the selected Gateway source has an authorized image model.
Typing `/` at the start or after draft text opens the same command list, including
with a fullwidth slash or the IME slash punctuation. Paths and URLs stay literal.
Choosing `/plan` or `/image` places it at the start and uses the existing draft
as its argument. The command requires an argument and a separate send action.
`/compact` stays on the PC. Android Back closes Add first. When the slash list
is open, Back removes only the unfinished slash token.
In a paired PC conversation, tap the pencil below a previous message to open
its original text. **Resend** keeps its images and quotations and runs from
that message again. It removes the conversation after the edited message and
clears queued follow-ups. **Cancel** discards the edit. If the phone asks for a
PC update, update the paired PC to enable the editor.

While a paired PC turn is live, the send arrow queues ordinary messages,
including attachments. Open **Send options** beside the arrow to insert the draft
into the next model call of this turn, or to interrupt the active manager step
and insert immediately. Each queued row offers both actions for that row. Stop
ends the turn and preserves the other queued messages.
Message, quote and Ask Other editors use 16px text (`C-017`). On iOS, focusing and
dismissing the keyboard must leave the header in its safe area and the wait
actions and Send/Follow-up/Steer inside the screen, including long drafts;
no horizontal dragging is needed to submit. Smaller editable text used to leave the WebView
zoomed and the top/right controls clipped. User pinch zoom remains available.
The waiting-message tray offers **Insert** on each row. **Interrupt and insert**
promotes the oldest waiting message into the current turn, then interrupts
the current manager step so it can read that message sooner; later messages
remain queued. Stop also leaves waiting rows in place. The selected row's
Steer action starts it as a new turn when the PC is idle; the other rows stay
queued while that turn runs. A later completion or failure continues the FIFO, whereas
a second Stop leaves the remaining rows waiting. This queue runs on the paired PC;
completion/error recovery requires the updated PC engine, not a new phone app.
A failed scheduled check keeps its failed result and future wake.
Select text in the PC conversation and tap **Add to chat** near the selection,
below the system Copy/Select menu when there is room, to attach it to
the next message. The quote appears above the box and can be read, edited, or removed before
sending. A quote can be sent by itself or with a typed request. The phone
sends the same `<selected_text>` and `<user_request>` blocks as the desktop;
the quote is not part of the editable message text. Direct model chat does
not have a PC transcript to quote.
The command refusal above preserves the current phone draft; it does not add a
cross-restart draft-storage guarantee or silently discard selected context.

Search and **New chat** sit under the inbox list. The header holds the menu,
the PC tabs, and User center. **Add a PC** is in the menu. Each project row folds.
The icon on its right starts a conversation already in that project,
including a project that has no threads yet. A conversation shown under
In progress stays listed under its project too. **New chat** is its own
screen: which PC, which project, then that same box. Android's system back
leaves the screen
before it leaves a conversation.

Inbox rows are cards, not a settings list. State is a badge — Running /
Waiting / Waiting for an answer — and an idle row carries its age
(`lib/when.ts`), so a stale thread is not mistaken for today's. A parked
wait is an In progress row with the thread's own summary, because the host
sends that as the row's line when there is no live turn to preview. The
header is one row: which PC this is, not the app's own name. Pulling the
list down past the top reloads the roster; an empty inbox says so. Search
filters the roster this phone already holds.

The inbox shows Zwai conversations. Local Claude, Codex and Cursor tasks are
disabled and are omitted even when an older PC advertises them.

## A model on the phone

Menu → Models saves an OpenAI-compatible endpoint: base URL, optional key,
chat completions or responses, discover, timeout. After one is saved, a
Chat tab sits on the top row, including when no PC is bound. That chat uses
the same composer as a PC thread. Tap the message field, then the model
icon to choose from an in-app, provider-grouped list. Long names wrap.
Back closes the list.
The reply streams. Its process row shows the latest activity while it is arriving.
Open that row, then the thought's own **Thought / 思考** control to read the complete
reasoning; merely opening the process row keeps that reasoning closed. Chat Completions accepts both legacy
`reasoning_content` and newer vLLM `reasoning` in streamed deltas and JSON replies.
A nonempty legacy field wins; empty or null legacy values fall back to the new
field. No additional setting is needed. Discover uses the platform HTTP stack so
the webview origin is not the caller. The completion is read as it arrives
(`StreamBody` on the device) because that stack returns a POST body in one
piece. On iOS the reader retains incomplete UTF-8 bytes between callbacks,
including after consuming earlier chunks, so Chinese and emoji split across
network packets arrive unchanged. An image is vision; a text file is sent as text. A responses front
that rejects the image part is asked once more as chat completions. A
rejected reasoning summary is omitted and the responses call is tried
again. The key stays on
the phone.

## Walking the screens without a PC

`?mock=1` boots onto the inbox against a scripted host (`lib/mock-link.ts`):
projects, a live turn, a parked wait and idle recents. The fixture also
advertises a legacy local-task catalog for the inbox exclusion regression.
The fixture needs no network or model, so the inbox and a
conversation can be driven in a browser and in Playwright — the screens
behind the scan form used to have no end-to-end coverage at all. Add
`&tick=0` to play a turn as fast as the browser paints; the default paces it
so a human can watch. Conversations live on the host rather than on a link,
because one PC seen down two sockets is still one PC (React strict mode
opens two).

```bash
npm run dev     # then open http://127.0.0.1:5173/?mock=1
```

The native trees `ios/` and `android/` live in git. `npx cap add` is already
done. A re-add restores Capacitor's cyan launcher; the zwai mark is
`internal/desktop/appicon.png` painted into those slots by
`go run ./mobile/scripts/genicons.go`. iOS 15+ (Xcode 27 dropped 14). Capacitor CLI 7.6 lowercases
`--packagemanager` and then compares it to `SPM`, so a re-add on a machine
without CocoaPods must use `node scripts/add-ios-spm.cjs` instead of
`npx cap add ios`. `npx cap sync` rewrites `CapApp-SPM/Package.swift`; keep
`.iOS(.v15)` if it snaps back to v14.

```bash
npm install              # uses registry.npmjs.org (project .npmrc)
npm test
npm run e2e
npm run cap:sync          # rebuild dist and copy into both apps
npx cap open ios          # Xcode
npx cap open android      # Android Studio
```

Concurrent browser suites use separate ports and output directories, for example:
`ZWAI_MOBILE_E2E_PORT=43187 npm run e2e -- e2e/pc-timeouts.spec.ts --output=/tmp/zwai-phone-timeouts`.
The walkthrough stores this case's PC timeout values in memory.

From the repo root: `make mobile-ios` / `make mobile-android`.
Android debug builds need JDK 21 (`JAVA_HOME` pointing at it). iOS needs Xcode
and a 15.0+ deployment target.

## Android release

Sideload APKs ship on [GitHub Releases](https://github.com/LubyRuffy/eino-swarm/releases).
[v0.1.12](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.12) attaches
[`zwai-0.1.12-android.apk`](https://github.com/LubyRuffy/eino-swarm/releases/download/v0.1.12/zwai-0.1.12-android.apk)
(versionName `0.1.12`, versionCode `112`). iOS is `0.1.12` (112).
[v0.1.11](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.11),
[v0.1.10](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.10),
[v0.1.9](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.9),
[v0.1.8](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.8),
[v0.1.7](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.7),
[v0.1.6](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.6),
[v0.1.5](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.5),
[v0.1.4](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.4),
[v0.1.3](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.3),
[v0.1.2](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.2),
[v0.1.1](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.1) and
[v0.1.0](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.0) are the
previous sideloads; they share the debug key, so each updates the last in
place. A later APK signed with a different key cannot.

`make mobile-android-release` syncs the web bundle, builds the Gradle
`release` variant, and copies the APK/AAB to `bin/`. Play/store signing
needs `ANDROID_KEYSTORE*` or a gitignored `mobile/android/keystore.properties`
— missing those fails on purpose so a debug-signed APK cannot ship as a
store build. `ANDROID_UNSIGNED=1` is the sideload path: debug-signed APK,
no AAB (`adb install` works; Play will not).

For a new mobile release, `npm version patch --no-git-tag-version` updates
`package.json` and its lockfile; then `npm run version:sync-ios` derives every
development Xcode marketing/build setting from that package version. The TestFlight
release archive keeps the last successfully published marketing version and advances
only the build number; optional `IOS_RELEASE_CONFIG.marketing_version` starts a new
version for a new candidate. Verify the candidate
against the latest Android Release and TestFlight build before tagging. This
keeps the two platforms tied to one source batch without editing each Xcode
configuration by hand.

```bash
# Sideload (emulator / adb). No tags on this checkout is fine.
ANDROID_UNSIGNED=1 make mobile-android-release

# Signed APK + AAB (Play / sideload with your upload key)
export ANDROID_KEYSTORE="$HOME/.zwai-android-upload.jks"
export ANDROID_KEYSTORE_PASSWORD
export ANDROID_KEY_ALIAS=upload
make mobile-android-release
```

`keystore.properties` next to `mobile/android/build.gradle` is the same
four keys (`storeFile`, `storePassword`, `keyAlias`, `keyPassword`) if you
do not want them in the shell. Do not commit that file or `*.jks`.

Version: a `vX.Y.Z` git tag becomes `versionName` X.Y.Z and `versionCode`
`major*10000+minor*100+patch`. This checkout has no tags, so the script
falls back to `mobile/package.json` `version`. Override with
`ANDROID_VERSION` / `ANDROID_VERSION_CODE`. `ANDROID_ARTIFACT=apk|aab|both`
picks the Gradle task (default both). JDK 21 is required (Capacitor
`compileOptions`); JAVA_HOME 17 is skipped.

Anonymous pairing reads the Hub URL from the PC QR (Settings → Phone).
Account mode uses the configurable default Gateway and stores its Mobile
credential separately from personal endpoints; device identity and Pairlink
tickets also stay on the phone. Camera
and cleartext (user-typed hub URLs, including `http` on a LAN) are declared
in `Info.plist` and `AndroidManifest.xml`. The iOS camera component also
references the photo library, so those purpose strings are declared too;
the app itself only asks for the camera. User center does not edit that Gateway address. Anonymous QR offers continue to select their own hub.

## Updates

The installed Android app checks [GitHub Releases](https://github.com/LubyRuffy/eino-swarm/releases)
itself (`https://api.github.com/repos/LubyRuffy/eino-swarm/releases/latest`).
If the newest Release has no matching Android APK, it searches the release
list for the newest version with an installable APK; a desktop-only version
does not offer a phone update.
It does not ask the PC. The browser walkthrough (`?mock=1`) does not check,
so a desk session never phones GitHub. On Android the offer is pinned at the
top of User center, above the title and account content, including the
signed-out account page, and it stays put while that page scrolls. A newer `zwai-*-android.apk` on
Android is downloaded and handed to the system installer. While the bytes
are moving, the banner shows the percent when the response has a length,
otherwise how much has arrived. The sheet still
needs a tap, and the first time Android may ask to allow installs from this
app. iOS beta builds are installed and updated through TestFlight; the GitHub
update check and installer are not shown there. **Not now** hides the Android
offer until a later one is published. A failed automatic check stays quiet.
On Android and in the browser walkthrough, **Menu → Check for updates** requests:
it shows that it is fetching, says when this build is current, asks before
an install, and shows the response's own error. The download only follows `github.com/LubyRuffy/eino-swarm`
and GitHub's release-asset hosts. The Mac desktop app uses that same
release: `zwai-<version>-darwin-<arch>.zip` (a `zwai.app`), built with
`make release VERSION=x.y.z` publishes only the affected platforms in the
qualified batch. A desktop-only batch uploads only the Mac zip; a mobile
batch publishes Android and iOS under one version. See
[release setup](../docs/RELEASE.md).

### Linux host updates

On Android or iOS, select a paired Linux host with the [boot service enabled](../docs/RELEASE.md#unattended-linux-host-updates).
A newer compatible release
appears with the host name, running version and target version. **Menu → Check
this host for updates** refreshes that host's offer. Confirm **Update this host**
to have the host download the package, save its running tasks and restart.
Keep the initiating phone connected or let it reconnect during the trial.
It verifies that exact candidate through the saved binding. If the trial
cannot be verified, the boot service restores the previous version and data.

## Simulators

The viewfinder is the product path on a device with a camera. Simulators paste the same `pairlink:v1`
URI minted by `POST /api/remote/offer`. Do not log that URI.

iOS Simulator shares the Mac loopback, so a hub on `127.0.0.1` is reachable.
Android emulator loopback is not the host; keep the hub URL as the PC's
address and publish the port:

```bash
adb reverse tcp:7780 tcp:7780   # when the hub listens on 7780
```

A hub redeem `host offline` means this PC's WebSocket is not in the hub's
live map. Settings → Phone must show online **when you mint a new QR**; a
code already spent on that error will not redeem again.

Capacitor Android WebView origin is `https://localhost`; the hub must answer
CORS on HTTP redeem. After minting an offer:

```bash
xcrun simctl pbcopy booted < offer.txt
cd ios/App
xcodebuild test -project App.xcodeproj -scheme App \
  -destination 'platform=iOS Simulator,name=iPhone 17' \
  -only-testing:AppUITests/BindFlowTests/testPasteBindListsTheSeedConversation \
  CODE_SIGNING_ALLOWED=NO
```

`BindFlowTests` reuses a saved bind when the scan screen is gone. A live
or last thread may already be open; the test taps Back to reach the inbox,
then asserts `path=` plus the seed conversation, opens New chat, and Starts a new thread. It is
not part of `make check`. English accessibility names (`Pairing URI`,
`Paste and bind`, `New message`, `Start`, `Back`, `Stop` / `Follow-up` /
`Send`) are the unit-test locale (`en`); Playwright `e2e/scan.spec.ts` uses
`zh-CN`.

The iOS update-menu regression uses the same packaged app and needs no hub:

```bash
cd ios/App
xcodebuild test -project App.xcodeproj -scheme App \
  -destination 'platform=iOS Simulator,name=iPhone 17' \
  -only-testing:AppUITests/BindFlowTests/testNativeIOSMenuHidesAndroidUpdateCheck \
  CODE_SIGNING_ALLOWED=NO
```

On a fresh simulator it saves a loopback model endpoint without contacting it;
on a reused simulator it opens the existing local chat. It opens the native
app's Menu, confirms the version is shown, and checks that the Android-only
update action is absent. The test runs with XCTest even when the host desktop
session is locked.

Composer width regressions run on Chromium and iPhone WebKit:
`npx playwright install chromium webkit && npm run e2e`.
The packaged iOS keyboard-dismissal and submission checks use the disposable
fixture documented in [TESTING](../docs/TESTING.md).

The paired-PC model picker and PC timeout sheet have packaged iPhone checks
in the disposable layout fixture. They use offline PC state, not a live provider or
saved PC credentials. Set `IOS_LAYOUT_SIMULATOR` to an isolated iPhone 17 and run:

```bash
cd mobile
npm run build
npx cap copy ios
fixture="$(IOS_LAYOUT_QUERY='/?mock=1&tick=0' python3 scripts/ios-layout-fixture.py)"
xcodebuild -project "$fixture/mobile/ios/App/App.xcodeproj" \
  -scheme App -configuration Debug \
  -destination "platform=iOS Simulator,id=$IOS_LAYOUT_SIMULATOR" \
  -derivedDataPath "$fixture/DerivedData" CODE_SIGNING_ALLOWED=NO \
  -only-testing:AppUITests/BindFlowTests/testModelRefreshReplacesNamesAndKeepsExplicitSelection \
  -parallel-testing-enabled NO -resultBundlePath "$fixture/refresh.xcresult" test
```

For each additional case create a new fixture with its matching query and
select its test method instead:

| `IOS_LAYOUT_QUERY` | `BindFlowTests` method |
| --- | --- |
| `/?mock=1&tick=0&catalog=retry` | `testModelRefreshFailureKeepsChoicesAndCanRetryLocally` |
| `/?mock=1&tick=0&catalog=legacy` | `testModelRefreshLegacyHostFeedbackDoesNotBlockConversation` |
| `/?mock=1&tick=0` | `testPCTimeoutsSaveReopenAfterNumericKeyboard` |
| `/?mock=1&tick=0&timeouts=legacy` | `testPCTimeoutsLegacyHostFeedbackStaysLocal` |

These cases retain screenshots and accessibility trees before/after refresh,
local failure/retry, and old-host feedback. The fixture copies current assets
and injects only its query; it never edits the shipping native project or marks
a private development build as a published release.
The timeout case focuses the numeric input, waits for the keyboard, then uses
the input's updated frame to position the caret and replace its value. It
asserts both the empty intermediate value and the exact replacement before
saving; a misplaced caret must not turn a test edit into an appended duration.
The reached WKWebView exposes menu entries as menu items, not buttons.

## iOS TestFlight release

`make release VERSION=x.y.z` includes TestFlight. `make mobile-ios-release VERSION=x.y.z` resumes only that platform. Routine releases retain the last successful TestFlight marketing version and increase the build number; the batch version remains the source/tag identity. Existing candidate receipts freeze both iOS version fields on retry. Apple may still review later builds of the same version. Configure the existing private signing/API client and explicit testing groups through `IOS_RELEASE_CONFIG`; see [docs/RELEASE.md](../docs/RELEASE.md). `make mobile-ios` still only opens Xcode. The authorized export declaration is retained in the app's Info.plist and verified against the signed IPA; unchanged encryption use does not require repeated questions for a new build. Reassess relevant encryption changes. Frozen sources without the field require their exact private source/build confirmation.

### Native account-flow fixture

`e2e/ios-gateway-account.swift` drives the actual Capacitor Browser and return,
pending/rejected isolation, a phone model call without a PC, and selection of an
account-owned PC over Pairlink/RPC. It also exercises native control and model
catalog redirect rejection, the control request's 15-second cancellation, and
managed model stream redirect rejection and Stop cancellation. The fixture must
observe zero requests at the redirect target and real HTTP cancellation before
its delayed response; each stage attaches only the fixture's counters alongside
the UI evidence. It shares the loopback Go service in
`e2e/account-fixture` with Android. This is synthetic CAS and model data, not
production Gateway acceptance or a real-model probe.

After freezing a candidate, use a fresh private fixture and simulator under the
existing native admission/cleanup rules. Set `IOS_LAYOUT_PUBLIC` to that
candidate's packaged `public` directory, `IOS_LAYOUT_PRESERVE_PUBLIC=1`,
`IOS_LAYOUT_SCENARIO=ios-gateway-account.swift`, `IOS_LAYOUT_QUERY=/`,
`IOS_LAYOUT_LOCALE=en`, and `IOS_LAYOUT_GATEWAY_ORIGIN` to the fixture's returned
`http://127.0.0.1:<port>` origin before running `scripts/ios-layout-fixture.py`.
The helper keeps every packaged web file unchanged and configures only its
private native wrapper; it never injects a logged-in session or token. Hash the
packaged web files before and after, preserve the XCTest screenshots/trees and
fixture counts, then confirm the case's processes and simulator data are cleaned.
The source tests for this preparation are `scripts/ios-layout-fixture.test.ts`.
Do not copy a fixture pass to another source, installer or live CAS deployment.

## Model API errors

Chat shows the host turn's HTTP status and concrete API rejection or response-decoding cause. An empty HTTP 502 response does not mean a tool had invalid JSON arguments. Public errors omit raw response bodies and graph paths, mask authentication values and bound the cause to 512 runes. Phone has no Trace page; use the same turn ID on the PC (`zwai trace <id>` or the desktop Trace tab) for original model calls and retries.

### Completed unread tasks

The inbox keeps completed unread PC tasks above project folders until you tap
and view their result. The static completion mark differs from the running
pulse; these rows do not start or resume a task. Automatic session recovery,
failed opens, background views and pagination do not acknowledge results. A
read error preserves the row and displays an error; entering it again retries.
Read state is stored on the host and follows the paired host identity.
Expanded completion pages also refresh through their previously loaded oldest
row, so viewing a result on the PC clears its phone tail row. Failed refreshes
keep the displayed window; a late list cannot relight a confirmed read.
An older PC without a numeric `open.detail.completed_seq` does not implement
completion acknowledgements. The phone skips those optional reads while
conversation streaming, scheduled waits and sending remain available. Zero
is a supported PC's initial watermark, so its first visible completion still
receives an acknowledgement. A host that advertises completion support but
rejects a read still shows the actual error and retains unread state.

### PC startup recovery

The paired phone polls the read-only `meta` RPC for `startup` and `restore`
conversation progress for the host's selected recovery work. While `restoring`, it displays the PC recovery banner,
retains unsent input and prevents new work until `ready`. Optional `restore.failed`
keeps a static failure warning after recovery, with other work still usable. The PC independently
rejects work mutations with `code: restoring`; a rejected Run now never marks
the thread running. Stop and saved-history reads remain available. Internal
`engine_restore` events advance replay cursors without rendering diagnostic JSON.

Gateway bootstrap 请求带 `X-AIGateway-Model-Roles: 2`，声明支持十一角色列表；原有九角色响应仍兼容，模型配置权限继续由网关策略决定。

Gateway discovery uses bootstrap `capabilities_url` (same origin, private device
Bearer), capability schema 1 and `X-AIGateway-Model-Roles: 3`. Preset and native
Gateway sources have separate grants. Features need an available supported role
and an implemented endpoint; model selectors additionally need role
`configurable`. Legacy bootstrap booleans cannot authorize a missing manifest.
Title, compact and translation are Chat roles; OCR gates image input. The image
generation role is parsed but has no phone adapter. Paired capability refresh
runs without blocking Stop; the desktop projects no endpoint or credential.
