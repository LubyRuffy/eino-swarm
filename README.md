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
  two-line list of your own turns; click one to pin that send at the top of the pane.
  The first tick opens the top of the log, so anything above that first send stays
  on screen. A long
  conversation packs the ticks into a minimap instead of stacking a second
  scrollbar on the rail. Auto-follow unpins so a
  live stream does not yank you back to the bottom. Opening another conversation
  from the list lands at its latest turn — and lights that tick — not the top
  of the history. A long log loads from the live edge first; older turns appear
  when you scroll up, and clicking a tick still above that tail pages it in
  before scrolling. A click is re-applied after those older pages land, so a
  load that started at the top cannot swallow it. New tokens while you are
  reading light a jump-to-latest control; click it to return and follow again.
- **Completed turns keep one end marker.** Each visible finished turn has one
  duration and local completion time after its last pre-completion content.
  Every answer stays visible, including finished goal sessions. Reasoning and
  tool details start collapsed and open from their own work rows. A late memory-review notice stays at its event position, including
  after a newer turn, without repeating the old end marker or hiding inside
  a completed goal session's process details. It also leaves the running turn's
  live activity visible.
- **A ring on the composer shows how full the last manager prompt is.** Hover
  it for the compact count against this model's window, plus this-turn billed
  in/out. The numbers come from the endpoint, not a guessed tokenizer. Discover
  fills per-model windows when the listing includes them; Settings edits
  each name, with a fallback only for names that still have none. Until one
  is set the arc still grows (it is not an empty circle)
  and the label stays a count, not a fake percentage.
- **Unsent content survives a reload.** The composer saves text, quotations,
  original files and pasted images together in the local engine database, per
  conversation or new-conversation project. Normal loading and saving stay quiet,
  with no draft status or Delete draft control occupying the composer. Only a
  failed save/restore, a conflict or an uncertain send shows draft guidance and
  recovery controls. Only an acknowledged save is durable. Sending retains the
  snapshot until the server accepts it; an uncertain result keeps the draft for inspection
  and is never automatically sent again. Resolve a conflicting window explicitly.
  Leaving a saved idle draft releases its attachment previews from memory;
  returning restores its original files from the local database.
  `/goal` and `/plan` accept text only; a draft with attachments or quotations
  stays intact with an error until those items are sent separately or removed.
  Draft text and quotes share a 1 MiB limit; up to 32 attachments share 256 MiB,
  with the existing eight images / 8 MiB per-image vision limits.
- **The Mac window has bounded rendering recovery.** An independent native
  controller checks actual painted pixels, tries one reload, then replaces the
  window on the same engine if needed. Its pixel marker is covered in the native
  window, while WebView snapshots still verify the actual random pattern. The
  cover follows theme and dialog backgrounds and does not receive input.
  The original shell coordinates one focus request for its identified successor
  while it still owns the foreground; switching to another app is respected.
  A focus request still needs real painted pixels before takeover.
  Background work continues; the restored
  window returns to its conversation and saved draft. Conversation/project
  selection reaches that controller independently of the five-second paint
  probe, so a failure immediately after navigation retains the selected scope.
  Reload and replacement keep that scope while the new page initializes;
  selection metadata alone never proves that the page painted. Repeated failures stop
  with a native error instead of an endless restart loop. A system WebKit fault
  can still occur; successful recovery is not evidence that the system fault has
  been removed. The journal and original-turn Trace distinguish probe boundaries
  and replacement lease failures. See [recovery diagnostics](docs/API.md#desktop-recovery-diagnostics).
- **Paste a screenshot into the box.** It lands as a thumbnail you can drop
  before sending, and goes to the model as visual input — not as a file in the
  workspace. Dragging a file onto the box does the same split: images become
  thumbs, other files become workspace attachments, same as the paperclip.
  A photo whose drop reports a generic binary type, or only a `png` / `jpg` /
  `gif` / `webp` name, is still a thumb. The server sniffs the bytes and
  refuses unsupported or mismatched type claims. The client preserves the original bytes; a filename alone is not content validation.
  Sending names those files on the message, so the model reads what you just
  attached instead of an older leftover sitting in the same folder.
- **Enlarge an image in a sent message.** Click its thumbnail, or focus it and
  press Enter or Space, to open that image over the conversation. The full image
  keeps its aspect ratio and fits the window, including after a reload. Escape,
  the backdrop or Close returns to the thumbnail; loading and failures are shown
  in the preview. Closing the preview keeps a running task going.
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
  read it (three wrapped lines, then an ellipsis), edit or drop it — and is
  sent in `<selected_text>` next to whatever you type (`<user_request>`),
  instead of being dumped into the box.
  On the phone, select text in the PC conversation and tap **Add to chat** near
  the selection, below the system's text menu when there is room. Copy and
  Select all remain available in that menu.
  The selected text appears above the message box, where it can be edited or removed;
  sending it alone or with a message uses the same tagged format.
- **Interactive questions (`ask_user`).** When a preference would waste work
  if guessed, the manager pauses this turn with a numbered question dialog.
  The card says **Your answer needed**, the title bar says **Your turn**, and
  the sidebar swaps the working pulse for a pinging question mark — pick a
  row, then Submit. Other opens a box instead of sitting empty under
  every card. The same ReAct turn continues after the answer. Workers cannot
  ask. While a card is waiting, Enter in the composer is Other, not a
  follow-up. On a paired phone, a rejected submission shows the PC error and
  retains the selected option or Other text for retry. Options whose IDs are
  omitted still appear on PC and phone, including after reopening the conversation.
- **Scheduled waits.** The manager can arm a wake on this conversation
  (`schedule_wake`) or, on a human turn, an independent job (`schedule_task`).
  A successful check with another wake scheduled keeps its unfinished
  sub-agents running for that next check; pausing or cancelling the last
  wake stops workers that have no continuing goal. A failed check also keeps
  workers that still belong to an active conversation wake or executable
  standing goal. Their original IDs, model requests and pending tools remain
  active in Agents after live updates or reload. The check stays failed and
  unread; it does not block the goal or immediately start another goal turn.
  The next scheduled check or your next message can collect those workers.
  A consumed one-shot, paused/blocked/completed goal, plan mode or exhausted
  goal budget cannot keep an otherwise unowned worker running. Stop and
  resource/watchdog termination still stop the workers. If a recurring check still
  exceeds the model context window, its failed run shows how to reduce the
  output budget or request, and that check pauses instead of repeating the
  same error. Replayed Chinese and other non-ASCII messages without billed
  usage count toward compaction before the next model call. Resume a paused
  check after changing the setting or conversation.
  `schedule_wake` does not take an id. It arms or replaces the open wait;
  the host assigns the id. Cancel copies an id from the open-wait list.
  If you explicitly prohibit parking this conversation, the engine preserves
  that instruction independently of summaries and refuses both a new/replaced
  wake and a scheduled report's `next_in_s`. The refusal leaves the wait row
  unchanged and records the human source in the same turn's Trace; assistant
  prose cannot create an armed-wait card. Only a later explicit human allowance
  releases the prohibition. Reporting findings without rescheduling and
  cancelling a wait still work. A source awaiting policy classification also
  prevents arming until it can be checked.
  When progress is gated on time or a condition not worth polling now, it is
  told to wake and end the turn when your conversation policy allows it.
  A parallel `exec` that sleeps and then checks progress is fine; that
  sleep and `schedule_wake` both bias estimated remaining time short (about
  a third, then that interval) so a check lands before the work is already
  done; extra checks are expected. A named clock time you asked for is still
  honored.
  The leftmost navigation switches Projects, Conversations, Scheduled, and Clients. Projects is the default list (pinned topics and project folders). Conversations is the list of threads that belong to no project. **Scheduled**
  opens the wait list in that column, not over the conversation. The title
  sits beside a search icon (the field opens on click). **New task** is the
  next row. **Upcoming** is the active list; the menu on that row filters
  **Active / Paused / Completed**.
  **New task** opens the editor in the task column (**New**) for the add-wait form — the task
  description only (a short inbox name is generated, same namer as conversations),
  then Details (**Runs in**: new conversation each run, or wake an
  existing one; **Project** is the workspace for a minted conversation) and
  Frequency. **Expand** hides the wait list so a long instruction can use the task column.
  Click a wait to edit it in that column: title, task, cadence, and where
  the next run lands (`PATCH`; changing cadence recomputes the next run from
  now; changing Runs in or Project does not). **Runs in** searches live
  conversations, or starts a new one each fire. **Project** is only for that
  new conversation. Pause, resume,
  cancel, and Run now live there too. A finished wait keeps the destination
  it was given. A run already in progress stays on the conversation it
  started on. The conversation
  Agents/Files/Trace rail stays hidden on this page, like Settings. A row is
  the title, then the next time (active) or status (paused/completed/cancelled)
  and cadence, then the task when it is not
  already the title. Click one for the editor.
  An open conversation's right rail lists active or paused waits aimed at that
  chat and any job whose run created it (including completed or cancelled jobs,
  labelled with their status). Completed/cancelled conversation wakes leave
  the rail; their history remains in Scheduled → Completed. With no current
  waits or creating job, the rail's scheduled section is hidden.
  A one-shot leaves that section when claimed; its current execution can still
  be Working. The turn and run result show when execution actually finishes.
  Click a row to open that task. An active wake on the open conversation
  shows a banner with the next check and the wait's title or prompt, **Run now**, and **Cancel wait** (not
  an icon-only dismiss — that sat under the goal banner's X). Run now and
  Cancel wait also sit on the armed-wait notice, which shows the same
  instruction. Cancel of a thread wake
  while idle continues a standing `/goal`; Run now fires the check without
  waiting for the timer (the wait banner hides while that turn is working,
  and an early cron check consumes the pending slot). The next-check time
  comes from the live schedule row, not the first arm snapshot — a refresh
  used to look like the timer jumped. A scheduled check can return its result
  directly as text, with tools remaining optional. `report_schedule` can also
  report the result; explicit empty findings stay quiet. Only a blank response
  with no tool result or report continues once in the same turn; a second
  empty response visibly fails. This also applies to **Run now**. Quiet standalone runs stay out of Recents;
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
  A standalone `/goal` at the end of a message uses the preceding text as
  the objective. This explicit command starts a new pursuit even when the
  conversation's previous objective is Done; trailing whitespace is allowed.
  Menu completion after existing text uses the same rule. A bare `/goal` waits
  for an objective; quotes, code and paths (including CJK paths) containing the
  literal suffix remain ordinary content. Leading `/goal` and `/plan` commands
  keep their entire argument, including command tokens inside it, across PC,
  HTTP and paired-phone sends.
  **goal** sets a standing objective and
  starts pursuing it in this turn. **plan** explores and writes a plan before
  changing anything. Codex cuts the command name at a space;
  an IME objective glued to `/goal` or `/plan` (or typed with a fullwidth `／`,
  or the CJK punctuation comma `、` the Slash key emits) is still
  the command, never a user message. A raw `POST /turns` of that line is
  intercepted the same way. The
  conversation keeps pursuing a goal until the manager calls `complete_goal` (or you
  clear it from the banner). A scheduled check cannot complete the objective
  while its own manager-created conversation wait still has another fire,
  including a new wait armed in that turn after consuming a one-shot;
  the manager must first finish that wait with `report_schedule(keep:false)`
  or cancel it. The final check can then complete the objective. Conflicting
  calls return a recoverable tool error rather than marking a partial check Done.
  Human-created waits and independent jobs retain their separate lifecycle.
  Reports that a future check was arranged are checked against saved, currently
  effective plans before the manager's prose is shown. A failed or missing
  scheduling call cannot support a success report. One same-turn correction is
  allowed for a confirmed unsupported claim; repeating it ends with an error.
  Classification retries only the private candidate with capped delays,
  using the full provider first-byte/body-idle budgets while bypassing shared
  service cooldowns and implicit reconnects. After three consecutive request
  failures, that candidate is durably deferred and stays unpublished. The same
  turn, Registry and workers continue user work under an executable goal or
  active conversation wake; other turns use the existing Continue/Stop pause.
  Invalid classifier output resets that failure streak and retries the candidate;
  neither case consumes the unsupported-claim correction or replays tools.
  Stop cancels verification; human guidance supersedes the old candidate.
  Recovery preserves deferral and cannot let old prose block new work or replace
  a newer checkpoint. Tool, thinking and worker activity remain live.
  Completion removes the goal strip above the composer immediately on PC and
  phone. The stored objective, completion state, results and history remain;
  reloading or reopening keeps the completed strip hidden. Its completion notice
  stays visible outside collapsed goal sessions. A mistaken `complete_goal`
  can still be undone in that turn with `reopen_goal`, or through the explicit
  `goal_resume` API or phone `resume_goal` RPC. Existing completed records are
  not reopened by ordinary messages. A new explicit `/goal` replaces the old
  objective, clears its completed state and displays the strip again.
  A live turn is steered so this round sees the
  new text. A turn finishes after answer verification and worker ownership or
  collection are settled. The runtime then continues, unless an active thread wake is waiting on this
  conversation — that wait is the next turn until it fires or you cancel it. Context pressure compact in place. Hitting the manager
  tool-round slice keeps the same turn going. In-flight sub-agents survive
  a pursuing turn that ends while they are still running, including when a
  thread wake is the next turn instead of an immediate auto-continue. A continuation
  that makes no tool progress stops auto-continue until you hit **Start**. Hitting the auto-continue budget (or a stop) pauses
  the same way, and **Start** keeps going. If it hits an
  obstacle it cannot pass, it calls
  `block_goal` and stops instead of retrying forever — a crashed ordinary turn that
  is not a recoverable model error does the same, and the banner shows the
  public error. An unverified scheduling claim instead gets one correction in
  that turn; three consecutive turns rejected for that reason pause pursuit with a separate
  limit and retain each turn's error. Ordinary messages still leave that pause
  in place; **Start** explicitly resumes it. Truncated tool JSON / a `429` / a dropped stream / a
  mid-stream TCP read timeout retry in-turn, and the transcript names the model service for a rate limit, an outage, or a dropped connection. A concrete API rejection stays as that status and cause. Three consecutive worker lifetime watchdog failures stop automatic resume: the turn errors, its goal blocks and its recurring check pauses. Inspect the worker trace, model and task before an explicit new message or **Start** retries. Provider idle timeouts are separate. A
  context-window rejection is named in the transcript and compacted once;
  if the next call still does not fit, the turn stops. A repeating
  scheduled check is paused so it cannot send the same prompt again.
  **Start** resumes after a block, a cap, a hold, or a stop. An ordinary PC or phone send while paused answers only that message: it preserves the pause and auto-turn count, and does not restart goal work. The paused strip and **Start** remain after that answer. Active pursuit still resets its consecutive auto-turn count when you send a message. If a goal has no observable deliverable or stopping condition, the manager is instructed to use `ask_user` once for that objective text before a new investigation slice; answering that question does not mark the goal complete. The goal strip
  stays available while unfinished, including when parked on a scheduled wait;
  Start does not appear while the objective is still pursuing between
  auto-continue sessions. Resuming an idle goal shows one continuation notice
  when its next turn starts. Late memory-review feedback keeps its place without splitting
  that restore into two notices. Later automatic continuations and manager
  reopen actions keep their own notices; each original resume and continuation event remains in
  its turn's Trace tab and `zwai trace <turn-id>`. The
  text is editable in place, and a live turn is told immediately. The
  runtime starts the next turn itself; a queued follow-up still wins. **compact** folds earlier turns into a briefing for later
  prompts; the transcript you see does not change. A secondary **Context
  compressed** status offers **View compressed briefing** to expand the full
  briefing in place; **Hide briefing** collapses it again. Old tool results that can
  be re-read are cleared first. The briefing prefers a rolling session
  summary extracted from the event log, so a long `/goal` does not teach
  the project's memory from a compacted view. The same fold also runs
  on its own before a manager call would exceed the model window after its
  configured maximum output is included. With no known window it uses
  **Settings → Swarm → Auto-compact at (tokens)** (default 80 000).
  Pin a cheaper summarizer
  under Settings → Models the same way as the conversation namer.
- **Swarm, visible.** Sub-agents appear as they spawn, with live status and their
  own transcript. A failed row offers **View failure reason**. Opening it keeps
  the recorded error above the scrolling log; old records without a reason say
  so. A recorded overall runtime limit links to **Settings → Swarm → Sub-agent
  timeout** (`agent_timeout_seconds`); an exact output-budget failure links to
  **Max completion tokens** (`max_completion_tokens`), and a recorded worker
  iteration limit links to **Sub-agent tool rounds** (`max_turns`). Each link focuses that
  field. An identified provider stream-idle timeout opens **Settings → Models**
  so you can inspect the endpoint's idle timeout; it does not guess which provider
  to edit. Saving a limit does not resume the stopped worker. A worker still
  running when a parent-turn error arrives keeps that error as its stopping
  reason, marked as a parent error, unless the engine explicitly retained its
  independent ownership after a failed scheduled check. Retained workers stay
  running without inheriting that check's error. Generic deadline or cancellation errors do
  not suggest a timeout setting.
  Built on [eino](https://github.com/cloudwego/eino) ADK and the swarm library in
  this repository.
- **It says what it is doing, even when it is quiet.** The response area shows
  a spinning "working…" as soon as the turn starts, including while a local
  model loads. Server progress then updates it every few seconds with a ticking
  "Working for 1m 12s · 2 sub-agents running". While it is running, that line
  and the live tool / sub-agent rows sweep and scroll if the text does not fit.
- **Thinking stays a 10-line window in developer view.** While the model is reasoning, the block
  caps at ten lines and follows new tokens; earlier lines stay reachable by
  scrolling, with a fade at the top so it is obvious they are there. The
  **Thinking** label sweeps like the other live status lines. Click the row to
  hide it while it is still running. After it finishes it collapses to **Thought**.
  Chat Completions accepts both `reasoning_content` and newer vLLM `reasoning`
  in JSON replies and streamed deltas, including direct phone chat. No field-name
  setting is needed; a nonempty legacy field wins when both are returned.
  **User view** (the default) shows a lone thought or tool call with its own
  disclosure: one click opens its content, with no outer group. A live lone
  thought and tool details start collapsed, including while running. Multiple
  adjacent work rows fold into one live ticker. Answers stay on screen and
  split the fold, including after a goal session finishes. The current
  activity swaps in vertically, and
  the line itself still scrolls left-to-right when it does not fit:
  thinking text (or **Thinking**), **Planning next moves** between model
  turns (under a closed answer, not on the fold above it), **Editing** /
  **Reading** / **Exec** while a tool is in flight.
  Settings → General, the app menu (`…` at the bottom of the icon rail),
  or ⌘K switches to developer view.
- **PC worker thoughts keep their identity across checks.** A background worker
  can keep generating while the manager ends a scheduled check. Its growing
  thought or answer updates the same row, including after reload, instead of
  adding another copy on every check. Independent model outputs remain separate.
  Overlapping historical pages and live events do not duplicate rows or trigger
  the same completion refresh again. Legacy records without output identity
  remain separate. See the [cancellation investigation](docs/diagnostics/issue88-cancellation-evidence.md)
  for how to distinguish engine handoff and failed-check cleanup from provider
  timeouts using Trace.
- **Interrupted generations explain what happened.** The transcript and Agents
  detail retain the cause and recovery state of each interrupted model output.
  Its old thought stops saying **Thinking**, while a resumed worker keeps its
  original identity and can continue running. A recovery notice means generation
  resumed, not that the task succeeded. Engine handoff, cancellation, output
  limits and model failures remain distinct; an unrecorded process-exit cause is
  shown as unknown. The same evidence is available in Trace after reload.
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
  own words — those are already in flight. An ordinary error or failed scheduled
  check also starts the next queued message, retaining the failed result and
  future wake. Stop, cancellation and engine restart keep the queue waiting.
  Paired phones use the PC queue, so this recovery requires the updated PC.
  Click a waiting row to
  pull it into the composer; the row leaves, and Enter queues that draft at the back. **Steer** on
  that row, or ⌘Enter, injects into the current turn at the next model boundary
  — it does not kill an in-flight tool, and it drops a queued copy of the same
  text. After Stop, Steer on a leftover row starts that text as the next turn.
  Repeated Steer clicks while that request is pending send it once. If the row
  has already left the queue, the app refreshes it and explains the missing
  waiting message; it does not send the old text again. Unread steering sits
  under the working line: **Interrupt** aborts the current manager tool so those nudges land now
  (workers stay up); the pencil pulls one bubble into the composer and retracts it, so Enter queues it instead; **Delete** retracts one bubble so the model never sees it.
  Stop is what cancels the whole turn. The PC and phone Agents panels list
  unfinished workers under **Stopped**, separate from successful or failed
  workers. Reopening or reloading retains that state even when a worker's
  cancellation receipt arrives after cleanup or the manager stops. Cleanup
  alone never means a task completed; genuine success and failure remain
  distinct, and explicitly resuming a worker can run it again. If the manager
  hits its tool-round limit, the transcript asks whether to add another slice
  rather than dying with a graph error.
- **Automatic restart with saved progress.** Opening a newer/different build
  prepares the checkout frontend while the old engine keeps serving, then
  requests a handoff and automatically replaces the engine within a 30s
  handoff/listener budget. A frontend preparation error leaves the old engine serving.
  Active and background sub-agents retain their IDs, histories, unread
  messages and confirmed results. Summaries are best effort under one shared
  deadline; a task does not become completed because it was summarized.
  Questions and iteration-cap pauses still wait for you. A restored question
  reuses its verified call identity without rereading the complete turn's
  checkpoint bodies when waiting or stopping. The conversation
  shows saving/saved/restored notices, and its turn ID exposes the complete
  handoff in Trace. Checkpoint failures stop replacement with an explicit
  error. A failed handoff keeps execution frozen and its checkpoints retryable;
  readable history, Trace and restart status remain available while storage can
  be read. It does not automatically resume unknown external operations.
  An older engine without handoff support uses bounded event-based
  recovery; its unsaved messages cannot be reconstructed.
- **Local startup remains available while the phone hub connects.** A cold
  launch allows up to two minutes for local initialization after frontend
  preparation and reports progress
  every ten seconds. Phone registration runs in the background, so its network
  wait does not hold the window closed. Startup errors show the current
  attempt's log and actual process failure. The separate 30s restart budget
  still applies when replacing an existing engine.
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
  providers as rows (open one for URL, key, default). The switch on a row
  shows that endpoint in the composer; off hides it from the model list
  without dropping a conversation that already uses it. Hiding the default
  moves new conversations to an endpoint that is still listed. The last
  endpoint stays on. Discovering a catalog
  that fails toasts over the sheet (× to close) instead of a red line at
  the top of that page. The same toast is how Phone pairing, a failed
  Settings save, and a failed project save report — never a red line under
  the heading you already scrolled past. The composer picker
  groups models by provider, searches, refreshes the catalog, and jumps
  to Edit providers. A thinking-level menu (Default / Low / Medium / High)
  sets how hard the models reason. Both apply from the next turn. A
  thought that is still streaming is not cut off by the request timeout.
  If the model sends no first byte, open its provider under Settings → Models
  and raise **First-byte timeout (seconds)** (default 30 seconds). The error's
  **Configure first-byte timeout** button opens and focuses that provider's
  field. Changes save automatically; close Settings and resend when ready.
  **Request timeout (seconds)** separately controls silence after the stream starts.
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
  control, including paired and direct phone conversations in light and dark
  themes. Unknown or empty language tags stay plain; copying preserves the code
  text and whitespace while streaming. `$…$` / `$$…$$` and `math` fences paint as formulas. The manager
  is told to tag fences and to write mathematics as LaTeX, not as a code dump.
- **Links leave the app.** A markdown URL opens in a new browser tab (`zwai web`)
  or the system browser (`zwai desktop`). It does not replace the window.
- **One-id troubleshooting.** Copy a turn id from the UI and
  `zwai trace <id>` replays the whole run: timeline, tool calls, model calls
  with billed tokens. The Trace tab shows the same turn's status and cost; open
  **Full log** for the on-screen timeline.
  After a chat service fails, manager and subagents on that same service share
  a 5 → 10 → 20 → 30-second capped recovery backoff, including new turns and
  provider aliases. One probe tests recovery at a time; its first decoded nonempty
  body, reasoning or tool-call delta lets other requests proceed while it keeps
  streaming. Headers, empty deltas and usage alone do not release waiting calls.
  Only that probe's final result resets or advances the failure streak; concurrent
  calls during it cannot change that decision. Non-streaming requests wait for
  their complete result. A Chat Completions stream needs a protocol terminator or
  explicit finish reason; a truncated connection starts cooldown again even after
  partial content released other calls, without cancelling those calls. Stop
  cancels waiting and the active request. Read-only answer classification
  uses its own candidate recovery and cannot start or wait on this shared gate.
  Trace records `model_backoff` waits separately from actual calls and retries,
  distinguishing origin cooldowns from candidate checks. PC/phone chat and
  replay show one timed explanation per conversation/cooldown generation;
  unknown-duration probe waits add no extra row. Trace keeps each caller record.
  Stream errors and EOF reach the consumer after the call-record callback finishes,
  retaining received usage before the turn can finish or storage can shut down.
  Model API errors retain their HTTP status and concrete rejection or response
  decoding cause in desktop and phone chat. An empty 502 response does not imply
  invalid tool arguments. Empty/HTML errors without a more specific HTTP or parsed API category, and
  malformed successful streams, show a response-decoding retry, including
  incorrect JSON field types. Rate limits and 5xx keep their status category;
  actual tool argument failures retain their own diagnosis. The public
  error omits raw bodies and graph paths;
  inspect the original call and retry records through the same turn's Trace.
  An immediate completion stays Idle when its send acknowledgement arrives later.

## Quick start

The desktop confirms an engine presence connection before loading its window
and keeps it while the native app is open, including page reloads. Reopening
as the old engine becomes idle waits for that holder to leave or be replaced,
including a new process at the same address, and checks its identity again;
an engine that still holds the directory but fails to answer is reported.
If the held connection drops, the native app retries that engine within a
bounded recovery budget. If it cannot restore the connection, it records the
cause in the desktop log before closing instead of leaving a disconnected window
open. Native shutdown may terminate the process without returning to the CLI.

Requirements: Go 1.26+ and Node.js (for a missing/stale checkout UI bundle or
dependency repair; `npm install --include=dev` uses the public registry). The desktop window uses
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

When a checkout bundle needs rebuilding, startup checks its declared direct
dependencies and development build tools. An empty or partial `node_modules`
is repaired before building even when its directory timestamp is recent.
Installation is checked again before the build can be stamped as current.

First launch writes `~/.zwai-swarm/config.yaml` and shows a setup banner until a
model is configured. Open **Settings** (⌘,) — a full-page sheet that starts on
**General**, with sections in the
left rail (on the desktop window, **Back to app** sits below the traffic
lights). Each page is a list of compact rows (name and hint left, control
right). Edits write themselves; **Back to app** flushes the last keystroke.
Chrome language is **Settings → General**, the app menu at the bottom of
the icon rail, or ⌘K → Switch language. Agents still answer in the language you are using.
Theme is **Settings → General**: System / Light / Dark cards, then **Color theme**
ZWAI (default chrome) or FOFA (FOFA console tokens: cyan signal on navy / cool
gray, cyan→blue composer rim).
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

Released macOS apps check for updates at launch and every six hours while open.
Returning to the window or reconnecting the network checks again when due;
background failures stay quiet. A newer Mac installer shows **Update / 升级**
and **Not now / 暂不**. Update downloads and replaces this `zwai.app`, then
reopens it with the same data directory. The banner shows downloaded bytes and,
when the installer size is known, a percentage, followed by installation and
restart stages. Errors use a red alert. **Retry / 重试** after installation
failure retries the confirmed version directly; a failed check retries the
check. **Menu → Check for updates** checks immediately. A launch-command failure
restores the previous app and leaves its window open. Two engines using different
data directories still share one installer lock for this app; a competing install
reports busy. Separate app bundles can update independently. Installation requires
confirmation; Windows/Linux and unversioned development builds have no update
installer in this channel.

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
   APK is on the newest [GitHub Release containing an Android APK](https://github.com/LubyRuffy/eino-swarm/releases)
   (`zwai-<version>-android.apk`). A newer Android build offers **Update** on the
   phone: it downloads that APK from GitHub Releases and opens the system
   installer. iOS beta builds are installed and updated through TestFlight;
   the app does not show the GitHub update check there. **Not now** hides an
   Android offer from the automatic bar. On Android and in the browser
   walkthrough, **Menu → Check for updates** asks immediately and shows that
   it is fetching. The current build says so. A newer Android build asks
   before installing. A failed request shows the response's own error.
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
   the menu. Switching a chip opens that PC's inbox without automatically
   opening its live or last conversation; tap a row to enter it. In-progress rows show
   findings or **Waiting**, not `schedule_wake` / `report_schedule` JSON.
   The transcript opens on the
   last turn, already at the live edge; Earlier or pull up loads older rows.
   If the current turn contains only hidden background coordination, the phone
   explains the empty view; one Earlier request skips that activity and reaches
   the nearest visible conversation history. A completed turn has a dated
   divider on both the PC and the phone, so repeated scheduled results stay distinct.
   **Agents / 子 Agent** counts running workers and separates them from finished
   or failed history; each row shows status and opens its separate
   activity log. Back returns through the Agents list, then the conversation;
   Android's system Back and an iOS swipe right from the left screen edge follow
   the same order. An open model picker closes first; iOS root screens stay open.
   The phone shows only
   Pairlink-permitted, clipped events, not the worker's launch instruction.
   A tap paints the chrome immediately;
   **Outputs / 产出物** opens this conversation's PC workspace tree. Folders
   fold, search narrows the list, and a tap previews text, images or PDFs on
   the phone. HTML and SVG show their source text, not an executable page.
   Other files can be downloaded after a bounded read; large files show a
   limit rather than silently truncating.
   until that conversation arrives the body
   shows a loading status, not a blank screen. The transcript is a short tail, not the
   whole turn. The composer matches the PC: a model, a thinking level, and a
   file or image. Tap the model button for an in-app list grouped by provider;
   long model names wrap and Back closes the list before leaving the chat.
    In a PC conversation or New chat, **Refresh models / 刷新模型** asks that PC
    for the current upstream model lists. New models appear and removed models
    leave the choices; an explicit selection stays unchanged until you choose
    another model. Refresh shows progress, and a failure keeps the previous
    list with a retryable error. An older PC without this operation asks you
    to update the PC. Refresh does not change the PC's saved models or defaults.
   On that PC's inbox, **Menu → PC timeouts / 电脑超时** edits its worker
   lifetime and provider first-byte/stream-idle limits. A new worker defaults
   to one hour; an existing saved positive value stays unchanged until edited.
   First-byte waiting defaults to 30 seconds and is independent of stream idle,
   so raise the first-byte field when an endpoint takes longer to start.
   Save applies to subsequently created model clients and worker lifetimes;
   current deadlines and tasks paused after three watchdog failures keep their
   state. These controls are separate from phone-direct **Menu → Models**.
   Long drafts keep Send, Follow-up, and Steer inside the phone screen;
   closing the iOS keyboard preserves that width.
   An image is shown to the model; other files go into the
   workspace. **Menu → Models** (on the scan screen, **Connect a model**) saves
   an OpenAI-compatible endpoint on the phone: base URL, optional API key,
   **Chat completions** or **Responses**, Discover, a timeout, and a switch
   that hides it from that composer's model list. After one
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
   reconnect banner, not a frozen inbox. Full Settings,
   PTY and Trace stay on the PC. The composer can still attach a file or an image.
   `make mobile-ios` / `make mobile-android` open Xcode or Android Studio
   after copying the web bundle. `make mobile-android-release` writes the
   signed APK/AAB to `bin/` (set `ANDROID_KEYSTORE*` or a gitignored
   `mobile/android/keystore.properties`; `ANDROID_UNSIGNED=1` is a debug-signed
   sideload APK, not a Play bundle). Published sideload APKs also land on
   [GitHub Releases](https://github.com/LubyRuffy/eino-swarm/releases).
   `make release VERSION=x.y.z` on a Mac builds and publishes only the
   batch's affected platforms: a desktop change gets the `zwai.app` zip,
   whose extracted app shows the ZWAI mark in Finder as well as the Dock;
   a mobile change gets Android APK and iOS TestFlight through the existing
   command-line credentials. Mixed changes share one GitHub version.
   TestFlight updates reuse the last published iOS marketing version and increase
   the build number; Apple may still require review. Processing/review waits remain
   pending; see [release setup](docs/RELEASE.md). The installed Mac app shows its version and can install a newer
   zip from there. See [docs/CLI.md](docs/CLI.md).

Traffic starts on the hub as ciphertext and upgrades to UDP when punching
works. Conversations never enter the hub database.

## Issue resolution and release batches

Issue resolution and release are separate: a fix closes its Issue after
behavior acceptance, tests, documentation and review pass and the complete
change is integrated and pushed to main. The closing comment records published
and pending platforms; closed does not imply all platforms have shipped.
Pending deliveries remain in the release ledger, including closed Issues.
The reconciliation command counts pending deliveries on every required platform,
and separates already allocated batches from fixes awaiting their first version.
More than three main commits since the previous published source SHA, or an
oldest accepted pending change waiting over 24 hours, triggers one new release
batch. A current explicit urgent request can use the documented
`tools/release.py --urgent-reason <reason>` exception for aggregation and the
local TestFlight daily policy, retaining all acceptance gates. The trigger persists across runs and dates; before it fires, fixes stay
integrated without a new version. Only affected desktop/mobile platforms are
published. Remaining platforms of an allocated batch resume its same
version/source SHA without reuploading completed platforms. See `C-010` in
[CONTRACTS](docs/CONTRACTS.md) and the reconciliation command in
[TESTING](docs/TESTING.md).

## Local agent progress

Settings → Clients → **Show local agent tasks** lists Claude, Codex, and Cursor
sessions already on this machine. The leftmost navigation's Clients list groups them by tool. Each tool group folds. The first
page is at most five tasks from the last three days; More shows the next five. A breathing light
means that task is still running. Open a row and the session fills the main
chat. The opening request stays pinned at the top. The column follows the
latest lines. **Earlier** loads the page above that, the same way a
conversation does. Adjacent thinking and tool calls start collapsed, the same row as a
conversation. The composer stays where it is and cannot be used. A paired phone
opens the same chat with the same locked composer, the same **Earlier**
control, and the same follow of the live tail. The switch is off until
you turn it on. The background refresh reuses parsed rows for unchanged files,
checks file metadata for changes, and publishes one sorted snapshot. Idle polls
copy only the visible page; they do not reread every transcript or sort the
whole archive. Changed and deleted sessions appear on the next finished refresh.
On iOS, the phone's read-only client detail keeps its title and Back button
above the task log when the inbox is pulled down; Back returns to the inbox.
On the phone, Clients → More shows a loading spinner while the next page arrives.
If that request fails, the existing rows stay visible and More can be retried.
In a Clients task, Android Back returns to the list; Back from the root inbox
exits the app.

## Using it

The manager is instructed to use sub-agents whenever they would save time or
improve quality, and to reassess that choice as the task develops. You do not
have to request delegation. One worker can provide an independent review or
second approach even when the manager must wait for it. Work without a useful
delegation benefit stays on the manager; there is no target worker count.
Your explicit constraints and the configured concurrency cap still apply.
Past that cap, `spawn_agent` and `resume_agent` refuse and start nothing.
The manager waits for one to finish, then resumes that worker or starts
the next piece. It does not launch the whole batch and leave the rest hanging.
The manager checks the workers' evidence and combines their results before
reporting completion. The same policy applies to ordinary turns and `/goal`.
An ordinary turn without an executable goal or next conversation wake must
collect each worker's current result with `wait_agents`, or explicitly discard
that work with `close_agent`, before it can complete. A finished worker still
needs collection; a wait timeout is insufficient. If the manager ends early,
the engine withholds that answer and lets it correct once in the same turn.
A repeated early ending pauses for **Continue** or **Stop**, retaining the
original workers. Continue grants one further attempt; Stop cancels them.
Restart preserves this pause and the correction allowance. The turn's Trace
records `workers_uncollected` and the affected worker IDs.
A request with independent parts is one way to benefit:

> Go through the three files I just uploaded, pull out every deadline, and leave me
> a single summary.md with one table.

What you get:

1. **A plan, then sub-agents.** The manager spawns workers (`fork_context` when a
   new worker needs this conversation so far). This supplies historical conversation
   evidence, without copying the manager system role or personal preferences;
   the worker follows its own assignment. A later task for the same role
   stays on that agent — new description, same `agent_id` — whether it is still
   running or already finished. `resume_agent` targets a specific leftover
   sibling. Open one in the Agents tab to read the system prompt it was given;
   the roster shows that `agent_id`, the same string `resume_agent` takes, not a `#n` badge. The log opens at the latest line, not the first tool call. The tab lists
   every worker this conversation started, not only the ones whose tools are
   still in the live-edge viewport.
   Continuing a goal or scheduled check, compacting, retrying a model call, or
   restarting retains those IDs and their current states. Finished workers
   stay stopped and can still be inspected or explicitly resumed. The manager
   receives that history as runtime state; it does not count the old roster
   as workers created by the new turn. Use that turn's Trace to check actual
   creation and resume activity.
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
   current manager tool; the pencil pulls one unread bubble into the composer
   and retracts it, so Enter queues that draft; **Delete** retracts one unread bubble. Steering that
   arrives after the last model call becomes a follow-up turn.
4. **Quote the conversation or terminal.** Select a passage and **Add to chat** when you want
   the next message to point at it — including while a turn is still streaming.
   The composer shows a count chip at rest (hover to read, edit or drop);
   send with an empty box if the quote is the whole request. The model sees
   `<selected_text>` and `<user_request>` as separate blocks.
   Terminal selections use the same action and include their source tab,
   conversation or project, initial directory and shell. They go into the
   current composer even when the terminal belongs to another conversation.
   Only selected text is included: select the command too when it matters.
   The initial directory is where the shell started, not a claim about its
   directory after `cd` or `ssh`. References can be edited or removed and are
   saved with the existing composer draft across refreshes.
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
list beside the icon rail (the rail stays) · `⌘\` toggle the right panel · `⌘J` open a terminal in the
current project (or conversation) directory · `⌘,` settings · `Esc` close
the active preview, find bar or other local overlay. To stop a running turn,
click **Stop** in the composer; Escape never stops it. Drag the border
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
The leftmost rail opens **Projects** (the default) or **Conversations**.
**New conversation** is on **Conversations**, not on the project list. It
starts a thread with no project. Hover a project
for a new-conversation control on the row itself — it starts one in that folder
and stays on **Projects**.
Click a folder to collapse its topics — the directory icon is closed
when collapsed and open when expanded. On **Projects**, click **Pinned** or
**Projects** to fold that section. On **Conversations**, click **Conversations**
to fold its rows (the arrow after the name shows on hover while the section is
open). Pin a topic from the row menu to keep it in **Pinned** at the top of
**Projects**. Launch and reload show the home page and select no conversation. Restart leaves the rail where you left it.
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

In the desktop window, **Choose directory / 选择目录** opens the system folder
picker and fills the working-directory field with the chosen folder's absolute
path. Cancelling keeps the current value; a selection error appears below the
field. The browser keeps manual path entry and the empty managed-directory
option because it cannot provide a local folder's absolute path. Saving checks
that the selected directory still exists. Selecting a directory uses its files
in place. See [C-026](docs/CONTRACTS.md#c-026-project-working-directory-selection).

A non-empty working directory keeps its path whitespace, including a folder name ending in a space. Saving and later turns use the same directory. Paste the intended absolute path without decorative padding; empty or all-whitespace still uses the managed directory.

With memory and auto-review on, each successful turn is reviewed even if the
manager already wrote memory: one saved requirement does not prove that the
whole turn was covered. A failed project turn may retain explicit human
requirements as notes, without learning skills from failed work. Cancellation
does not start an automatic review. What is worth carrying forward is kept:
short notes whose absence would misdirect future work (preferences, facts, hard
constraints, or pointers to recorded skills), and *skills* whose methods make
work achievable (applicability, steps, verification and failure branches).
Requested project **Tidy memory** merges duplicate skills, reviews stale
candidates and corrects notes/skills classification. Moves preserve source text:
promote keeps the complete note unchanged except whitespace in the skill; demote keeps a short skill body
unchanged except whitespace and refuses list steps or budget excess. The target
is saved before deleting the source, so failure may leave duplicates. Retry
reuses an identical target and never overwrites different content. Post-turn
review and workers cannot move memory; the shared library has no notes.

The Memory panel shows agent view counts and the last view time. Settings →
Memory controls the stale-candidate threshold (`memory.skill_stale_days`,
default 90 days). Stale skills are candidates for review, never automatically
deleted. Usage is local to a skill copy and does not rewrite its content.
Notes are short declarations that stand alone; constraints needed only inside
one workflow belong in that skill. Skill descriptions say when to use them. The next conversation in
that project starts with the notes in its prompt and an index of the skills, and
opens a skill when it needs one. Sub-agents on that conversation get the same
snapshot and can open a skill; they cannot write the store. Their report back to
the manager is the task result, plus at most a short durable note if something
would change later work.

A conversation that belongs to no project does not grow those notes. A finished
turn can still record a procedure, and that file goes in the shared skill
library (`library/skills/` under the data directory, next to `projects/`). The
next conversation outside a project starts with that index and opens a skill
when it needs one. Open **Memory** on that conversation to read the files,
then **Copy** one into a project when that agent should follow it. **Update** on
the copy pulls a later change from the library the same way it pulls from
another project.

The sidebar lists each project's conversations under its name. A folder
(and Conversations) shows the five conversations active in the last seven days;
**Show more** reveals the rest, **Show less** folds them again. Click the
folder to collapse it. The open conversation is marked; the folder is not.
A conversation that is mid-turn keeps a progress mark in that same column
even if you are looking at another one; a conversation whose next turn is a
parked wait keeps a clock there instead, so it does not look Idle. The
title bar says **Waiting**, and the wait itself is the composer banner
(Run now / Cancel wait) — that is not a row in **Pinned**. A
collapsed folder that still has a live turn or a wait keeps the mark on the
directory. Click **Pinned**, **Projects**, or **Conversations** to fold
that section. Pin a topic from the row menu to keep it in **Pinned**
at the top. Conversations that belong to no project sit in **Conversations**;
**New conversation** lands there. The icon on the right of that header
starts one too. Skills are behind **View skills** on the
project menu, which opens the **Memory** tab. Notes are editable — **Save notes** appears only after
the draft differs from what is stored — skills can be read, copied, and deleted.
**Copy** on a skill (or **Copy all skills** when there are several) writes a
separate copy into another project. Editing either side leaves the other
alone. The copy remembers where it came from. **Update** pulls a later change
from that project when this copy was not edited; if both sides changed, it
asks before replacing the local steps. Nothing is written back. Keep one
project for the shared procedures and copy from that one. A copy that was
edited is what the next project receives, so do not copy a business project's
version when you wanted the shared procedure. An unmodified copy still tracks
the original project. **Review now** (the sparkles on the Memory tab) re-reads the last successfully completed turn.
Automatic review starts with current notes and effective source events, giving
human corrections priority over tool noise. Sources whose identity framing
cannot fit the per-source limit are omitted as a whole group. Skill extraction
separates normal work from recovery after an existing failure, without requiring
the failed work to be repeated first. Note changes stay in an in-memory
draft until the complete proposal passes validation and its original revision
still matches the notes shown in the first model request; a refusal or conflict leaves the stored notes intact. A changed input revision detected before batch execution stops the review without another repair call; a change racing the final commit may use one repair call before the fixed input revision stops further note edits. Skill
changes remain immediate and may survive a later refusal: this is not a
transaction across notes and skills. Successful submission finishes the review
without another model request just to announce the result. If review fails, the
transcript and Memory panel show its stopping reason, proposal count and concrete
refusal categories, including capacity usage and excess characters when present.
They distinguish notes that did not commit from any skill changes already saved.
The failure's **Trace** button opens the original turn's Full log even when a
newer turn exists or the right panel is closed. The button loads older source
history when needed and reports an unavailable Trace explicitly. Expand its review
row, or run `zwai trace <turn-id> --full`, for bounded proposal metadata and source
references. A historical focus does not show the latest turn's token usage as its
own. Returning to the review's conversation restores a completed result even
when it is older than the loaded transcript tail. Rejected proposal text and quotes are
not added to that diagnostic history. Older review events remain readable.
These checks verify source
identity, exact quotes and numeric spelling; they do not prove semantic truth.
Set **Settings → Memory → Review thinking** to choose a separate reasoning level for automatic review and **Review now**. The default **Inherit conversation** preserves the reviewed turn's choice; **Low** is an optional latency profile for models that support it. It does not change conversation work, catalog tidy, the model, or the output cap.
See [the review contract](docs/CONTRACTS.md#c-025-memory-review-evidence-and-commit).
The panel says when it is reading, and what it decided — including when it kept
nothing. A write that landed is also named in the transcript itself
(`Memory updated: …`), so you do not have to have the tab open to notice. If you were mid-edit when a review wrote, the panel says so
and lets you keep yours or take the new ones — the last save does not silently
win. Correcting a wrong note there matters: it would otherwise be repeated in
every future conversation. Notes are budgeted (`memory.char_limit`, default 2200
characters; one agent note is also capped at `memory.entry_max`, default 360)
because they ride in every prompt — once full, a write that would
grow the notes is refused, even a replace with a longer note. Review receives the
current usage, capacity gap and existing entries as structured repair feedback.
It can shorten or replace source-supported related notes before adding a new one
within the same review budget; it must not clear unrelated notes to make room.
If it cannot safely submit the complete proposal, it reports failure and leaves
pending notes unsaved. Creating a skill that already covers the same
subject is refused: the result names the existing skill so the next call is
a patch, not a second name. Skills that share a name stem (the same procedure
split into chapter-skills) are refused the same way, and leftover families are
folded into one skill after a successful turn — automatically, including when
auto-review is off. Failed-turn requirements-only reviews do not fold skills. The fold is reported
as a `memory_review` when it changed the catalog. Editing a skill file by hand
does **not** fold until the next turn; **Tidy memory** on the Memory
heading still folds leftover name-stem families, then asks the model to curate
the whole catalog by content. That is a real reviewer call — it takes as long
as the model takes. While it runs, the panel estimates progress (the bar stays
short of full) and scrolls the model's reply in a short box. Reload first if
you edited files outside the app. When it finishes, the card names what was
merged, deleted, created and patched, with counts (scanned / merged / deleted /
created / updated / remaining).

Session briefings and successful-turn reviews also receive worker lifecycle facts from
the stored event log, attributed to the turns they describe. A historical
worker being restored is different from a new worker, and a refused creation
call creates none. Earlier briefings and assistant claims are labelled as
model-written claims, so their worker counts cannot replace the recorded
evidence. This does not delete existing notes or skills; correct a wrong saved
note in Memory and inspect the relevant turn in Trace.
The compressed-context preview shows the briefing prose; internal lifecycle
records remain available to later turns and diagnostics.

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
is still running. A newer binary requests a durable task handoff, prints
progress every 5s, and automatically replaces the engine within one 30s
handoff/listener budget after checkout frontend preparation. Recovery follows
listener publication. The old engine
must acknowledge saved progress before it is stopped;
engines without this API use bounded compatibility recovery. See
[docs/CLI.md](docs/CLI.md).

## Where things live

```
~/.zwai-swarm/            $ZWAI_HOME overrides this
├── config.yaml           settings (0600; the API key is in here)
├── engine.lock           one engine process for this directory
├── engine.json           pid and URL of that process
├── zwai.db               conversations, transcripts, event timeline, model calls
├── remote/               Host Token and device identity; not in yaml
├── workspaces/<thread>/  one directory per standalone conversation, `uploads/` inside it
├── library/skills/       procedures recorded from conversations in no project
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

Native desktop tests run only in UTM `ci-macOS`, one isolated case at a time.
Offline recovery-proof preparation also refuses incomplete evidence: a failed
command or timeout remains the primary error if receipt storage fails.
Recovery predicate summaries retain first/last types, hashes and comparisons.
Each helper invocation also retains its complete streams and timing in a unique
private `native-actions/<id>/receipt.json`, including any fixture text emitted by
the helper. Automatic recovery needs the successor's own
fresh pixel-ready event before QA activation can inspect that window.
Saved-input acceptance reads back the durable draft and original attachment
bytes alongside the native composer value; a healthy save needs no status label.
Internal native QA callers may provide an admission callback under the campaign
driver lock before owner/exit receipts can change; it must raise an exception to
refuse, preserving prior records. Its return value is not an admission decision.
This opt-in API leaves the default full6 launch/resume policy unchanged.
Use the [native case runner](docs/TESTING.md#native-desktop-isolation-f-430--c-031):
it refuses a physical host and closes case-owned processes before removing
their private home/cache and test data. Keep the user's running desktop and normal data untouched.
For a daily offline native round, build/transfer the identified candidate once,
prepare its AX helper with `make native-smoke-prepare`,
then run `make native-smoke APP=/absolute/candidate/zwai.app EVIDENCE=/absolute/private/new-round`
inside the guest. Its 60-second budget includes preparation, native behavior
checks, evidence and cleanup; `cleanup.json` reports stage and total timings.
The timed scenario validates the prepared helper and performs no compilation.
Cold builds and long recovery scenarios have separate explicit budgets; see
the [daily round procedure](docs/TESTING.md#daily-native-round-60-seconds-including-cleanup).

Invalid delegation calls do not create workers. Within one manager reply, repeated spawn argument errors are reported once per error with their rejected count; the manager can supply both role and task and continue. See [the lifecycle tools](docs/LIBRARY.md).

### Coordination between conversations

Managers can discover other active tasks on this PC with `list_tasks`, compare
actual workspaces, and negotiate through `send_task_message` and
`read_task_messages`. Messages show their source in desktop and paired-phone
conversations and remain in the event log when a recipient stops or restarts.
Reading uses a saved cursor by default; explicit cursors recover earlier pages.
The capability is advisory: wait for a peer's explicit agreement before relying
on file division or execution order. It does not lock resources, wake idle tasks,
interrupt work, answer a human pause or grant new permissions (`F-135`, `C-021`).

### Unread task results

A task that finishes while you are elsewhere keeps a static completion mark.
This differs from the running pulse. Collapsed projects show a completion mark
at the right of their title; the paired phone keeps unread completions above its
folders. Open a task to read its rendered result and clear the mark. Automatic
reconnect and background polls leave it unread. A failed read acknowledgement
keeps the mark and shows the error. Quiet checks and user cancellation do not
create a completion mark. See [F-125 and C-023](docs/FEATURES.md#f-125-unread-completion-attention).
The phone hides an earlier unread completion while that conversation is running,
waiting or asking for an answer. The result stays unread and can reappear when the
conversation becomes idle; this does not send a read acknowledgement. Status is
matched by conversation ID, so separate tasks with the same title remain separate.
Older PCs that omit completion watermarks keep ordinary phone conversations
and scheduled checks usable. The phone does not send the optional read
acknowledgement to those hosts. A supported PC's initial watermark of zero
still allows the first visible result to be acknowledged.

### Cloud built-in tools

Settings → Models → open a provider, select **Responses**, and configure its cloud
tools JSON, for example `[{"type":"web_search"}]`. The endpoint and model must
support that API and tool. The default remains Chat Completions with no cloud
tools. Cloud calls run at the provider, coexist with local Agent functions, and
appear in the same turn's Trace; web sources remain clickable in answers.
See [configuration](docs/CONFIG.md#provider-cloud-tools) for supported tool types,
parameters, YAML and limits. Paired phone sessions using the PC inherit its
provider configuration; independent phone direct chat has its own configuration.

### Engine recovery at startup

The desktop/Web window can attach while unfinished work is restoring. PC and
phone show recovery progress and keep saved history available; new work waits
until recovery ends. Individual recovery failures leave a visible failure count
and the original Trace while other work stays usable. Stop remains available and is persisted before that
conversation can resume. Goal editing and its keyboard shortcuts pause during recovery. A refused goal
edit stays open with its text; readiness never submits it automatically, and
only an acknowledged explicit save closes it.
The scheduler starts after recovery. A replacement
that has already accepted handoff is not killed by the old 30-second launch
budget; an unusually slow live start reports its PID and log path for diagnosis.
