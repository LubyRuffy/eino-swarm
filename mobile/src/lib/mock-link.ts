import {
  OpAnswer,
  OpCancelWait,
  OpCatalog,
  OpRefreshCatalog,
  OpEvent,
  OpFiles,
  OpFileChunk,
  OpFollowupSteer,
  OpHello,
  OpClientRead,
  OpClients,
  OpList,
  OpLog,
  OpMore,
  OpOpen,
  OpRead,
  OpPreempt,
  OpReady,
  OpResumeGoal,
  OpRunNow,
  OpSend,
  OpStart,
  OpSteer,
  OpStop,
  OpPut,
  OpTune,
  GROUP_RECENT,
  GROUP_ATTENTION,
  OpUnwatch,
  OpWatch,
  PROTOCOL_V,
  type ProjectView,
  type FollowupView,
  type RemoteEvent,
  type RemoteRequest,
  type RemoteResponse,
  type RunningView,
  type ThreadDetail,
  type ThreadView,
} from "./rpc"
import { stopReplayEnd, stopReplayStart } from "./mock-stop-replay"

/** A phone with no PC in reach. The walkthrough host answers the same ops
 *  with the same shapes, so the inbox and a conversation can be driven in a
 *  browser and in Playwright. No network, no model, no storage.
 *
 *  Conversations live on the host, not on a link, because one PC seen down
 *  two sockets is still one PC — and React's strict mode opens two. */

const MOCK_FLAG = "mock"
const MOCK_HOST = "Walkthrough PC"
const OUTPUT_FILES: Record<string, { text: string; mime: string }> = {
  "outputs/report.md": { text: "# Verified output\n\n> A finding\n\n- First result\n", mime: "text/markdown" },
  "outputs/sample.go": { text: 'package main\n\nconst value = "hello"\n', mime: "text/plain" },
}
/** Playwright drives the script; a human wants to watch it arrive. */
const DEFAULT_TICK_MS = 420

export function wantsMock(search = location.search): boolean {
  return new URLSearchParams(search).get(MOCK_FLAG) === "1"
}

export function mockTickMs(search = location.search): number {
  const raw = new URLSearchParams(search).get("tick")
  const n = raw ? Number(raw) : NaN
  return Number.isFinite(n) && n >= 0 ? n : DEFAULT_TICK_MS
}

/** Holds open long enough for a loading status to be visible. The default
 *  walkthrough does not wait. */
export function mockPauseOpenMs(search = location.search): number {
  return new URLSearchParams(search).get("pause") === "open" ? 1200 : 0
}

function mockPauseClientsMs(search = location.search): number {
  return new URLSearchParams(search).get("pause") === "clients" ? 1200 : 0
}

type MockThread = {
  id: string
  title: string
  projectID: string
  summary: string
  running: boolean
  waiting: boolean
  action: string
  askUser: boolean
  minutesAgo: number
  goal?: string
  providerID?: string
  model?: string
  reasoning?: string
  events: RemoteEvent[]
  completedSeq?: number
  readSeq?: number
  followups?: FollowupView[]
}

type Step = { kind: string; text?: string; callID?: string; err?: string; agentID?: string; role?: string; at?: string; sourceThreadID?: string; sourceTurnID?: string }

const projects: ProjectView[] = [
  { id: "p-platform", name: "Platform" },
  { id: "p-notes", name: "Field notes" },
]

/** A turn the phone can replay: a thought, work, then an answer. Generic on
 *  purpose — this is a rendering fixture, not a task the product knows. */
function scriptedTurn(prompt: string): Step[] {
  return [
    { kind: "user_message", text: prompt },
    { kind: "reasoning_delta", text: "Reading the current shape before touching it." },
    { kind: "reasoning", text: "Reading the current shape before touching it." },
    { kind: "tool_call", text: `read({"file_path":"src/lib/layout.ts"})`, callID: "" },
    { kind: "tool_result", text: "42 lines", callID: "" },
    { kind: "tool_call", text: `exec({"command":"npm test -- layout"})`, callID: "" },
    { kind: "tool_result", text: "12 passed", callID: "" },
    { kind: "delta", text: "Here is what changed" },
    {
      kind: "agent_message",
      text: [
        "Here is what changed:",
        "",
        "- the column keeps its width on a narrow screen",
        "- the tail stays pinned while older rows page in",
        "",
        "```chart",
        '{"type":"bar","title":"Counts","x":"item","y":"n","unit":"n","data":[{"item":"a","n":1},{"item":"b","n":3}]}',
        "```",
        "",
        "Run `npm test` to see it green.",
      ].join("\n"),
    },
    { kind: "done" },
  ]
}

function threadSeed(): MockThread[] {
  return [
    {
      id: "t-live",
      title: "Trim the layout pass",
      projectID: "p-platform",
      summary: "",
      running: true,
      waiting: false,
      action: "Reading src/lib/layout.ts",
      askUser: false,
      minutesAgo: 0,
      events: [],
    },
    {
      id: "t-wait",
      title: "Watch the nightly export",
      projectID: "p-platform",
      summary: "Checking again after the next run",
      running: false,
      waiting: true,
      action: "",
      askUser: false,
      minutesAgo: 6,
      goal: "Keep the export green and report anything that breaks",
      events: [],
    },
    {
      id: "t-recent",
      title: "Sweep the unused exports",
      projectID: "p-platform",
      summary: "Removed four dead helpers and kept the tests green",
      running: false,
      waiting: false,
      action: "",
      askUser: false,
      minutesAgo: 52,
      events: [],
    },
    {
      id: "t-notes",
      title: "Summarise this week",
      projectID: "p-notes",
      summary: "Wrote the digest into the notes folder",
      running: false,
      waiting: false,
      action: "",
      askUser: false,
      minutesAgo: 20 * 60,
      events: [],
    },
    {
      id: "t-loose",
      title: "Name the release",
      projectID: "",
      summary: "Three candidates, one of them survives a trademark check",
      running: false,
      waiting: false,
      action: "",
      askUser: false,
      minutesAgo: 3 * 24 * 60,
      events: [],
    },
  ]
}

type Watcher = { threadID: string; deliver: (resp: RemoteResponse) => void }

export class MockHost {
  private createdAt = Date.now()
  private threads = threadSeed()
  private watchers = new Set<Watcher>()
  private timers = new Set<number>()
  private seq = 0
  private calls = 0
  private streamedAgentsStarted = false
  private completionChecksStarted = false
  private activeUnreadEnded = false
  private readonly activeUnread = new URLSearchParams(location.search).get("history") === "active-unread"
  readonly legacyCompletion = new URLSearchParams(location.search).get("completion") === "legacy"
  private readFailures = new URLSearchParams(location.search).get("readfail") === "1" ? 1 : 0
  private catalogFailures = new URLSearchParams(location.search).get("catalog") === "retry" ? 1 : 0
  takeCatalogFailure() {
    if (this.catalogFailures <= 0) return false
    this.catalogFailures -= 1
    return true
  }
  private askFailures = new URLSearchParams(location.search).get("askfail") === "1" ? 1 : 0

  constructor(readonly tickMs = mockTickMs()) {
    const live = this.find("t-live")
    if (live) {
      if (new URLSearchParams(location.search).get("history") === "stopped-workers") {
        this.seed(live, stopReplayStart(), true)
      } else if (new URLSearchParams(location.search).get("history") === "streamed-agents") {
        this.seed(live, [
          { kind: "agent_message", agentID: "manager", text: "Older manager answer" },
          { kind: "user_message", agentID: "manager", text: "Check the current workspace" },
        ], true)
      } else if (new URLSearchParams(location.search).get("history") === "hidden") {
        this.seed(live, [
          { kind: "agent_message", agentID: "manager", text: "Earlier answer" },
          { kind: "tool_call", agentID: "manager", callID: "hidden-spawn", text: "spawn_agent({})" },
          { kind: "tool_result", agentID: "manager", callID: "hidden-spawn", text: "started" },
        ], true)
      } else if (new URLSearchParams(location.search).get("ask") === "1") {
        live.askUser = true
        this.seed(live, [{
          kind: "tool_call",
          callID: "ask-fixture",
          text: `ask_user({"questions":[{"id":"test-window","prompt":"When should this run?","options":[{"id":"now","label":"Now"},{"id":"later","label":"Later"}]}]})`,
        }], true)
      } else {
        this.seed(live, scriptedTurn(live.title).slice(0, 4), true)
      }
      if (new URLSearchParams(location.search).get("queue") === "1") {
        live.followups = [
          { id: "first", seq: 1, text: "first waiting message" },
          { id: "second", seq: 2, text: "second waiting message" },
        ]
      }
    }
    const done = this.find("t-recent")
    if (done) {
      if (new URLSearchParams(location.search).get("history") === "model-response-error") {
        this.seed(done, [
          { kind: "model_retry", agentID: "manager", text: JSON.stringify({ attempt: 1, cap: 2, reason: "model_response_invalid" }) },
          { kind: "model_retry", agentID: "manager", text: JSON.stringify({ attempt: 2, cap: 2, reason: "model_response_invalid" }) },
          { kind: "error", agentID: "manager", err: "the model service returned a response that could not be decoded" },
          { kind: "done" },
        ], false)
      } else if (new URLSearchParams(location.search).get("history") === "api-error") {
        this.seed(done, [
          { kind: "error", agentID: "manager", err: "model API request failed: HTTP 502 Bad Gateway: unexpected end of JSON input" },
          { kind: "done" },
        ], false)
      } else if (new URLSearchParams(location.search).get("history") === "turns") {
        this.seed(done, [
          { kind: "user_message", text: "This turn is a scheduled check." },
          { kind: "agent_message", text: "First completed run" },
          { kind: "done", at: "2026-10-08T01:01:02Z" },
          { kind: "user_message", text: "This turn is a scheduled check." },
          { kind: "agent_message", text: "Second completed run" },
          { kind: "done", at: "2026-10-08T02:03:04Z" },
        ], false)
      } else this.seed(done, scriptedTurn(done.title), false)
      if (new URLSearchParams(location.search).get("history") === "timeout") {
        this.seed(done, [{ kind: "error", agentID: "manager", err: "swarm: worker worker-1 stopped after 3 consecutive watchdog timeouts; automatic recovery is exhausted. This recurring check is paused." }], false)
      }
      if (new URLSearchParams(location.search).get("history") === "peers") {
        this.seed(done, [{ kind: "task_message", sourceThreadID: "peer-task", sourceTurnID: "peer-turn", text: "Peer coordination request." }], false)
      }
      if (new URLSearchParams(location.search).get("agents") === "1") {
        this.seed(done, [
          { kind: "spawned", agentID: "worker-1", role: "reader" },
          { kind: "reasoning", agentID: "worker-1", text: "Checking the source." },
          { kind: "tool_call", agentID: "worker-1", text: "read({})", callID: "worker-call" },
          { kind: "tool_result", agentID: "worker-1", text: "Evidence found.", callID: "worker-call" },
          { kind: "agent_message", agentID: "worker-1", text: "worker answer" },
          { kind: "finished", agentID: "worker-1", role: "reader", text: "worker answer" },
        ], false)
      }
    }
    // Ordinary walkthrough rows start read; the attention fixture isolates this flow.
    for (const th of this.threads) th.readSeq = th.completedSeq ?? 0
    const completionHistory = new URLSearchParams(location.search).get("history")
    if (completionHistory === "unread-completion" || completionHistory === "unread-completion-pages") {
      const th = this.find("t-recent")
      if (th) th.readSeq = 0
      if (th && completionHistory === "unread-completion-pages") {
        for (let i = 1; i <= 7; i++) {
          const completed = { ...th, id: `done-${i}`, title: `Completed task ${i}`, minutesAgo: th.minutesAgo + i, events: [] }
          this.seed(completed, [{ kind: "user_message", text: "Check the result." },
            { kind: "agent_message", agentID: "manager", text: "The result is complete." }, { kind: "done", agentID: "manager" }], false)
          this.threads.push(completed)
        }
      }
    }
    if (this.activeUnread) this.seedActiveUnread()
  }

  private seedActiveUnread() {
    this.threads = []
    const rows = [
      { id: "active-run", title: "Shared task", running: true },
      { id: "active-ask", title: "Pending answer", running: true, askUser: true },
      { id: "completed-shared", title: "Shared task" },
      { id: "active-wait", title: "Scheduled task", waiting: true },
      ...Array.from({ length: 4 }, (_, i) => ({ id: `completed-${i}`, title: `Completed item ${i + 1}` })),
    ]
    for (const [i, row] of rows.entries()) {
      const th: MockThread = {
        id: row.id, title: row.title, projectID: "", summary: "Earlier result", events: [],
        running: false, waiting: false, action: "", askUser: false, minutesAgo: i,
      }
      this.seed(th, [{ kind: "agent_message", text: "Earlier result" }, { kind: "done" }], false)
      th.readSeq = 0
      th.running = "running" in row && Boolean(row.running)
      th.waiting = "waiting" in row && Boolean(row.waiting)
      th.askUser = "askUser" in row && Boolean(row.askUser)
      th.action = th.askUser ? "Waiting for an answer" : th.running ? "Checking current work" : ""
      this.threads.push(th)
    }
  }

  finishActiveUnreadAfterMore() {
    if (!this.activeUnread || this.activeUnreadEnded) return
    this.activeUnreadEnded = true
    // Another client ends the activities while this phone stays on Home.
    // No new completion or read watermark is created by that status change.
    // Only the final More page advances this fixture, never a wall-clock timer.
    for (const th of this.threads) {
      th.running = false
      th.waiting = false
      th.askUser = false
      th.action = ""
    }
  }

  read(th: MockThread, seq: number): boolean {
    if (this.readFailures > 0 && th.id === "t-recent") { this.readFailures -= 1; return false }
    th.readSeq = Math.max(th.readSeq ?? 0, Math.min(seq, th.completedSeq ?? 0))
    return true
  }

  stop() {
    for (const id of this.timers) clearTimeout(id)
    this.timers.clear()
    this.watchers.clear()
  }

  watch(w: Watcher) {
    this.watchers.add(w)
    if (w.threadID === "t-wait" && new URLSearchParams(location.search).get("history") === "completion-compatibility"
      && !this.completionChecksStarted) {
      this.completionChecksStarted = true
      const th = this.find(w.threadID)!
      // Separate checks reach the rendered live path after ready, rather than
      // arriving together in a replay that must not acknowledge results.
      for (const check of [1, 2]) this.later(() => {
        const completed = th.completedSeq
        this.push(th, { kind: "schedule_report", text: JSON.stringify({ quiet: true }) })
        this.push(th, { kind: "done", agentID: "manager" })
        th.completedSeq = completed // A quiet terminal does not create attention.
      }, check * 600)
    }
    if (w.threadID === "t-live" && new URLSearchParams(location.search).get("history") === "streamed-agents"
      && !this.streamedAgentsStarted) {
      this.streamedAgentsStarted = true
      const th = this.find(w.threadID)!
      const steps: Step[] = []
      for (let i = 0; i < 3; i++) {
        steps.push(
          { kind: "delta", agentID: "manager", text: `Manager update ${i}` },
          { kind: "agent_message", agentID: "manager", text: `Manager update ${i}` },
          { kind: "tool_call", agentID: "manager", text: "read_file({})", callID: `roster-read-${i}` },
          { kind: "tool_result", agentID: "manager", text: "Read completed", callID: `roster-read-${i}` },
        )
      }
      for (let i = 1; i <= 3; i++) steps.push({ kind: "spawned", agentID: `worker-${i}`, role: "worker" })
      steps.push({ kind: "agent_message", agentID: "worker-1", text: "Worker evidence" })
      // Separate browser frames are essential: a seeded replay cannot expose
      // React retaining paragraphs when multiple live deltas reuse one key.
      steps.forEach((step, i) => this.later(() => this.push(th, step), (i + 1) * Math.max(16, this.tickMs)))
    }
  }

  unwatch(w: Watcher) {
    this.watchers.delete(w)
  }

  find(id: string): MockThread | undefined {
    return this.threads.find((th) => th.id === id)
  }

  listing(first = false) {
    const running: RunningView[] = this.threads
      .filter((th) => th.running || th.waiting)
      .map((th) => ({
        thread_id: th.id,
        title: th.title,
        project_id: th.projectID || undefined,
        // A parked wait has no live turn, so the host sends its summary.
        action: th.action || (th.waiting ? th.summary : ""),
        ask_user: th.askUser,
        waiting: th.waiting,
        last_active_at: this.at(th),
      }))
    const idle = this.threads.filter((th) => !th.running && !th.waiting).map((th) => this.view(th))
    const section = (id: string, rows: ThreadView[], more: boolean) => ({
      id,
      threads: rows,
      more,
      next: more ? "older" : "",
    })
    return {
      projects,
      threads: idle,
      ...(this.legacyCompletion ? {} : this.attentionPage()),
      // Start on Home rather than auto-opening an active task and reading it.
      running: this.activeUnread && first ? [] : running,
      // One idle row lives past this page so More is a real page, not a
      // button that appends nothing. The first page never includes it.
      // Each folder pages itself; the loose row is the recent section.
      more: true,
      next: "older",
      groups: [
        ...projects.map((p) => section(p.id, idle.filter((th) => th.project_id === p.id), false)),
        section(GROUP_RECENT, idle.filter((th) => !th.project_id), true),
      ],
      clients: this.clientCatalog(),
    }
  }

  attentionPage(cursor?: string) {
    const all = this.threads.filter((th) => (th.completedSeq ?? 0) > (th.readSeq ?? 0))
      .sort((a, b) => a.minutesAgo - b.minutesAgo || b.id.localeCompare(a.id))
    const anchor = cursor ? JSON.parse(cursor) as [number, string] : undefined
    const rows = anchor ? all.filter((th) => th.minutesAgo > anchor[0]
      || (th.minutesAgo === anchor[0] && th.id < anchor[1])) : all
    const page = rows.slice(0, 3)
    const last = page.at(-1)
    return { attention: page.map((th) => this.activeUnread
      ? { ...this.view(th), running: false, waiting: false }
      : this.view(th)), attention_more: rows.length > page.length,
      attention_cursor: rows.length > page.length && last ? JSON.stringify([last.minutesAgo, last.id]) : "" }
  }

  // The walkthrough stands in for a PC that has the clients switch on.
  // Rows stay read-only; More only pages the scripted older session.
  clientCatalog(before?: number, group?: string) {
    const stamp = new Date().toISOString()
    let tools = before
      ? [
          {
            id: "claude",
            more: false,
            tasks: [{ id: "c-old", title: "older session", status: "done", updated_at: "2026-01-01T00:00:00Z" }],
          },
        ]
      : [
          {
            id: "claude",
            more: true,
            next: "1",
            tasks: [{ id: "c-open", title: "open session", status: "running", updated_at: stamp }],
          },
          {
            id: "codex",
            more: false,
            tasks: [{ id: "c-live", title: "live session", status: "running", updated_at: stamp }],
          },
          {
            id: "cursor",
            more: false,
            tasks: [{ id: "c-shut", title: "finished session", status: "done", updated_at: stamp }],
          },
        ]
    if (group) tools = tools.filter((tool) => tool.id === group)
    return { enabled: true, tools }
  }

  older(): ThreadView {
    return this.view({
      id: "t-older",
      title: "Page past the first",
      projectID: "",
      summary: "",
      running: false,
      waiting: false,
      action: "",
      askUser: false,
      minutesAgo: 90,
      events: [],
    })
  }

  at(th: MockThread): string {
    return new Date(this.createdAt - th.minutesAgo * 60_000).toISOString()
  }

  view(th: MockThread): ThreadView {
    return {
      id: th.id,
      title: th.title,
      ...(this.legacyCompletion ? {} : { unread: (th.completedSeq ?? 0) > (th.readSeq ?? 0), completed_seq: th.completedSeq ?? 0 }),
      project_id: th.projectID || undefined,
      running: th.running,
      waiting: th.waiting,
      summary: th.summary,
      last_active_at: this.at(th),
    }
  }

  detail(th: MockThread): ThreadDetail {
    return {
      id: th.id,
      title: th.title,
      ...(this.legacyCompletion ? {} : { unread: (th.completedSeq ?? 0) > (th.readSeq ?? 0), completed_seq: th.completedSeq ?? 0 }),
      goal: th.goal,
      goal_on: Boolean(th.goal),
      waiting: th.waiting,
      wake: th.waiting
        ? { id: "w-" + th.id, next_run_at: new Date(Date.now() + 9 * 60_000).toISOString() }
        : undefined,
      running: th.running
        ? { thread_id: th.id, title: th.title, action: th.action }
        : undefined,
      provider_id: th.providerID,
      model: th.model,
      reasoning: th.reasoning,
    }
  }

  snapshot(th: MockThread) {
    const hiddenHistory = new URLSearchParams(location.search).get("history") === "hidden" && th.id === "t-live"
    const streamedAgents = new URLSearchParams(location.search).get("history") === "streamed-agents" && th.id === "t-live"
    return {
      events: hiddenHistory ? th.events.slice(-2) : streamedAgents ? th.events.slice(1).filter((event) => event.seq > 0) : th.events,
      seq: th.events[th.events.length - 1]?.seq ?? 0,
      status: { running: th.running, waiting: th.waiting, followups: th.followups },
    }
  }

  begin(text: string): MockThread {
    const th: MockThread = {
      id: "t-" + Date.now().toString(36),
      title: text.slice(0, 40) || "New conversation",
      projectID: "",
      summary: "",
      running: false,
      waiting: false,
      action: "",
      askUser: false,
      minutesAgo: 0,
      events: [],
    }
    this.threads.unshift(th)
    this.run(th, text)
    return th
  }

  /** Play a turn one frame at a time so the live tail, the work fold and the
   *  answer are all exercised the way a real turn drives them. */
  run(th: MockThread, prompt: string) {
    th.running = true
    th.waiting = false
    th.minutesAgo = 0
    const steps = scriptedTurn(prompt)
    steps.forEach((step, i) => {
      this.later(() => {
        // The turn is already over when `done` goes out, and the frame
        // carries that status. Settling after it would re-arm the header.
        if (i === steps.length - 1) this.settle(th)
        this.push(th, step)
      }, i * this.tickMs)
    })
  }

  settle(th: MockThread) {
    th.running = false
    th.action = ""
    const last = [...th.events].reverse().find((ev) => ev.kind === "agent_message")
    if (last) th.summary = last.text.split("\n")[0]
  }

  stopTurn(th: MockThread) {
    this.settle(th)
    const params = new URLSearchParams(location.search)
    if (params.get("history") === "stopped-workers" && th.id === "t-live") {
      for (const step of stopReplayEnd(params.get("stop_order"))) this.push(th, step)
    }
  }

  answer(th: MockThread, req: Pick<RemoteRequest, "answers">): string | null {
    if (!th.askUser) return null
    const answers = req.answers as Record<string, { answers?: string[] }> | undefined
    if (!answers?.test_window?.answers?.[0]) return 'ask_user: missing an answer for "test_window"'
    if (this.askFailures > 0) {
      this.askFailures -= 1
      return "PC rejected the answer"
    }
    th.askUser = false
    this.push(th, { kind: "tool_result", callID: "ask-fixture", text: JSON.stringify({ answers }) })
    this.push(th, { kind: "agent_message", text: "The answer was received." })
    this.settle(th)
    this.push(th, { kind: "done" })
    return null
  }

  cancelWait(th: MockThread) {
    th.waiting = false
    th.summary = "The wait was cancelled"
    this.push(th, { kind: "schedule_cancelled" })
  }

  push(th: MockThread, step: Step) {
    const ev = this.event(th, step)
    th.events.push(ev)
    for (const w of this.watchers) {
      if (w.threadID !== th.id) continue
      w.deliver({
        v: PROTOCOL_V,
        id: "",
        ok: true,
        op: OpEvent,
        thread_id: th.id,
        event: ev,
        status: { running: th.running, waiting: th.waiting, followups: th.followups },
      })
    }
  }

  private seed(th: MockThread, steps: Step[], leaveRunning: boolean) {
    for (const step of steps) th.events.push(this.event(th, step))
    th.running = leaveRunning
    if (!leaveRunning) this.settle(th)
  }

  private event(th: MockThread, step: Step): RemoteEvent {
    // A delta is live-only on the wire, so it never takes a sequence number.
    const streamed = step.kind === "delta" || step.kind === "reasoning_delta"
    if (!streamed) this.seq += 1
    if (["done", "error"].includes(step.kind) && (!step.agentID || step.agentID === "manager")) th.completedSeq = this.seq
    // A tool result has to find its call, and ids must not collide across
    // replays of the same script.
    let callID = step.callID
    if (callID === "") {
      if (step.kind === "tool_call") {
        this.calls += 1
        callID = "call-" + this.calls
      } else {
        callID = "call-" + this.calls
      }
    }
    return {
      thread_id: th.id,
      seq: streamed ? 0 : this.seq,
      kind: step.kind,
      agent_id: step.agentID,
      source_thread_id: step.sourceThreadID,
      source_turn_id: step.sourceTurnID,
      role: step.role,
      text: step.text ?? "",
      tool_call_id: callID,
      err: step.err,
      created_at: step.at ?? new Date().toISOString(),
    }
  }

  private later(fn: () => void, delay: number) {
    const id = window.setTimeout(() => {
      this.timers.delete(id)
      fn()
    }, delay)
    this.timers.add(id)
  }
}

let host: MockHost | null = null

export function mockHost(): MockHost {
  if (!host) host = new MockHost()
  return host
}

export function resetMockHost() {
  host?.stop()
  host = null
}

/** One socket onto the walkthrough host. */
export class MockLink {
  path = "direct"
  onPush?: (resp: RemoteResponse) => void
  onDisconnect?: (err: Error) => void

  private closed = false
  private listed = false
  private watcher: Watcher | null = null

  constructor(private readonly host: MockHost = mockHost()) {}

  alive(): boolean {
    return !this.closed
  }

  close() {
    this.closed = true
    if (this.watcher) this.host.unwatch(this.watcher)
    this.watcher = null
  }

  async announceDevice(): Promise<RemoteResponse> {
    return this.ok({})
  }

  async rpc(
    req: Omit<RemoteRequest, "v" | "id"> & { id?: string },
    _opts?: { dropOnTimeout?: boolean; timeoutMs?: number },
  ): Promise<RemoteResponse> {
    if (this.closed) throw new Error("offline")
    const id = req.id ?? ""
    const thread = () => this.host.find(req.thread_id ?? "")
    switch (req.op) {
      case OpHello:
        return this.ok({ id })
      case OpUnwatch:
        this.stopWatching()
        return this.ok({ id })
      case OpList: {
        const listing = this.host.listing(!this.listed)
        this.listed = true
        return this.ok({ id, ...listing })
      }
      case OpClientRead:
        return this.ok({
          id,
          client_view: {
            id: req.task_id ?? "",
            title: "open session",
            status: "running",
            entries: [{ role: "user", text: "open session" }],
          },
        })
      case OpClients:
        if (req.group && mockPauseClientsMs()) {
          await new Promise((resolve) => setTimeout(resolve, mockPauseClientsMs()))
        }
        return this.ok({
          id,
          clients: this.host.clientCatalog(req.before, req.group),
        })
      case OpMore:
        if (req.group === GROUP_ATTENTION) {
          const page = this.host.attentionPage(req.cursor)
          if (!page.attention_more) this.host.finishActiveUnreadAfterMore()
          return this.ok({ id, ...page })
        }
        return this.ok({ id, threads: [this.host.older()], more: false, next: "" })
      case OpFiles:
        if (!thread()) return this.missing(id)
        return this.ok({ id, files: [
          { path: "outputs", name: "outputs", size: 0, dir: true, modified: "", uploaded: false },
          ...Object.entries(OUTPUT_FILES).map(([path, file]) => ({ path, name: path.split("/").at(-1)!, size: new TextEncoder().encode(file.text).length, dir: false, modified: "", uploaded: false })),
        ], more: false })
      case OpFileChunk: {
        if (!thread()) return this.missing(id)
        const file = OUTPUT_FILES[req.file_path ?? ""]
        if (!file) return this.missing(id)
        const bytes = new TextEncoder().encode(file.text)
        const offset = req.before ?? 0
        const data = Array.from(bytes.slice(offset)).map((byte) => String.fromCharCode(byte)).join("")
        return this.ok({ id, file_chunk: {
          path: req.file_path!, name: req.file_path!.split("/").at(-1)!, size: bytes.length, mime: file.mime,
          offset, next_offset: bytes.length, data: btoa(data), more: false,
        } })
      }
      case OpRefreshCatalog: {
        const scenario = new URLSearchParams(location.search).get("catalog")
        if (scenario === "legacy") return this.ok({ id, ok: false, code: "unknown_op" })
        if (scenario === "slow") await new Promise((resolve) => setTimeout(resolve, 1200))
        if (this.host.takeCatalogFailure()) return this.ok({ id, ok: false, code: "refresh_failed" })
        return this.ok({ id, models: scenario === "empty" ? [] : [{ provider_id: "walkthrough", provider_label: "Walkthrough", model: "scripted-next", default: true }], reasoning_levels: ["low", "medium", "high"] })
      }
      case OpCatalog:
        return this.ok({
          id,
          models: [
            {
              provider_id: "walkthrough",
              provider_label: "Walkthrough",
              model: "scripted",
              default: true,
            },
          ],
          reasoning_levels: ["low", "medium", "high"],
        })
      case OpTune: {
        const th = thread()
        if (!th) return this.missing(id)
        if (req.provider_id) th.providerID = req.provider_id
        if (req.model) th.model = req.model
        if (req.reasoning !== undefined) th.reasoning = req.reasoning
        return this.ok({ id })
      }
      case OpPut:
        return this.ok({
          id,
          put: { id: req.put_id ?? "", ready: (req.part ?? 0) > 0 && req.part === req.parts },
        })
      case OpOpen: {
        const pause = mockPauseOpenMs()
        if (pause > 0) await new Promise((resolve) => window.setTimeout(resolve, pause))
        const th = thread()
        if (!th) return this.missing(id)
        return this.ok({ id, detail: this.host.detail(th) })
      }
      case OpRead: {
        if (this.host.legacyCompletion) return this.ok({ id, ok: false, code: "unknown_op", error: "unknown op" })
        const th = thread()
        if (!th) return this.missing(id)
        if (!this.host.read(th, req.completed_seq ?? 0)) return this.ok({ id, ok: false, error: "Read acknowledgement unavailable" })
        return this.ok({ id, detail: this.host.detail(th) })
      }
      case OpWatch: {
        const th = thread()
        if (!th) return this.missing(id)
        this.stopWatching()
        this.watcher = { threadID: th.id, deliver: (r) => this.onPush?.(r) }
        this.host.watch(this.watcher)
        return this.ok({ id, op: OpReady, thread_id: th.id,
          more: ["hidden", "streamed-agents"].includes(new URLSearchParams(location.search).get("history") ?? "") && th.id === "t-live",
          ...this.host.snapshot(th) })
      }
      case OpLog: {
        const th = thread()
        const hiddenHistory = ["hidden", "streamed-agents"].includes(new URLSearchParams(location.search).get("history") ?? "") && th?.id === "t-live"
        return this.ok({ id, thread_id: th?.id, events: hiddenHistory ? th.events.slice(0, 1) : [], more: false })
      }
      case OpStart:
        return this.ok({ id, threads: [this.host.view(this.host.begin(req.text ?? ""))] })
      case OpSend:
      case OpAnswer:
      case OpRunNow:
      case OpResumeGoal: {
        const th = thread()
        if (!th) return this.missing(id)
        if (req.op === OpAnswer && th.askUser) {
          const error = this.host.answer(th, req)
          return error ? { v: PROTOCOL_V, id, ok: false, error } : this.ok({ id })
        }
        this.host.run(th, req.text ?? th.title)
        return this.ok({ id })
      }
      case OpSteer: {
        const th = thread()
        if (th && req.text) this.host.push(th, { kind: "steer", text: req.text })
        return this.ok({ id })
      }
      case OpFollowupSteer: {
        const th = thread()
        const item = th?.followups?.find((row) => row.id === req.followup_id)
        if (!th || !item) return this.missing(id)
        th.followups = th.followups?.filter((row) => row.id !== item.id)
        if (th.running) this.host.push(th, { kind: "steer", text: item.text })
        else this.host.run(th, item.text)
        return this.ok({ id, followups: th.followups })
      }
      case OpPreempt:
        return this.ok({ id })
      case OpStop: {
        const th = thread()
        if (th) this.host.stopTurn(th)
        return this.ok({ id })
      }
      case OpCancelWait: {
        const th = thread()
        if (th) this.host.cancelWait(th)
        return this.ok({ id })
      }
      default:
        return { ...this.ok({ id }), ok: false, code: "unknown_op" }
    }
  }

  private stopWatching() {
    if (this.watcher) this.host.unwatch(this.watcher)
    this.watcher = null
  }

  private missing(id: string): RemoteResponse {
    return { ...this.ok({ id }), ok: false, error: "no such thread" }
  }

  private ok(part: Partial<RemoteResponse>): RemoteResponse {
    return { v: PROTOCOL_V, id: "", ok: true, path: this.path, host: MOCK_HOST, ...part }
  }
}

export function mockLink(): MockLink {
  return new MockLink()
}

/** A saved link the boot path will accept, so the walkthrough starts on the
 *  inbox rather than the scan screen. */
export function mockSavedLink() {
  return {
    hubURL: "http://127.0.0.1:0",
    ticket: MOCK_FLAG,
    hostPub: "A".repeat(43),
    sessionID: "ab".repeat(16),
    fingerprint: "walkthrough",
    label: MOCK_HOST,
  }
}
