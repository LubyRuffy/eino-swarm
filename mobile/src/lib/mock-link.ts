import {
  OpAnswer,
  OpCancelWait,
  OpCatalog,
  OpEvent,
  OpHello,
  OpList,
  OpLog,
  OpMore,
  OpOpen,
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
  OpUnwatch,
  OpWatch,
  PROTOCOL_V,
  type ProjectView,
  type RemoteEvent,
  type RemoteRequest,
  type RemoteResponse,
  type RunningView,
  type ThreadDetail,
  type ThreadView,
} from "./rpc"

/** A phone with no PC in reach. The walkthrough host answers the same ops
 *  with the same shapes, so the inbox and a conversation can be driven in a
 *  browser and in Playwright. No network, no model, no storage.
 *
 *  Conversations live on the host, not on a link, because one PC seen down
 *  two sockets is still one PC — and React's strict mode opens two. */

const MOCK_FLAG = "mock"
const MOCK_HOST = "Walkthrough PC"
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
}

type Step = { kind: string; text?: string; callID?: string; err?: string }

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
  private threads = threadSeed()
  private watchers = new Set<Watcher>()
  private timers = new Set<number>()
  private seq = 0
  private calls = 0

  constructor(readonly tickMs = mockTickMs()) {
    const live = this.find("t-live")
    if (live) this.seed(live, scriptedTurn(live.title).slice(0, 4), true)
    const done = this.find("t-recent")
    if (done) this.seed(done, scriptedTurn(done.title), false)
  }

  stop() {
    for (const id of this.timers) clearTimeout(id)
    this.timers.clear()
    this.watchers.clear()
  }

  watch(w: Watcher) {
    this.watchers.add(w)
  }

  unwatch(w: Watcher) {
    this.watchers.delete(w)
  }

  find(id: string): MockThread | undefined {
    return this.threads.find((th) => th.id === id)
  }

  listing() {
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
      running,
      // One idle row lives past this page so More is a real page, not a
      // button that appends nothing. The first page never includes it.
      // Each folder pages itself; the loose row is the recent section.
      more: true,
      next: "older",
      groups: [
        ...projects.map((p) => section(p.id, idle.filter((th) => th.project_id === p.id), false)),
        section(GROUP_RECENT, idle.filter((th) => !th.project_id), true),
      ],
    }
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
    return new Date(Date.now() - th.minutesAgo * 60_000).toISOString()
  }

  view(th: MockThread): ThreadView {
    return {
      id: th.id,
      title: th.title,
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
    return {
      events: th.events,
      seq: th.events[th.events.length - 1]?.seq ?? 0,
      status: { running: th.running, waiting: th.waiting },
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
        status: { running: th.running, waiting: th.waiting },
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
      text: step.text ?? "",
      tool_call_id: callID,
      err: step.err,
      created_at: new Date().toISOString(),
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
      case OpList:
        return this.ok({ id, ...this.host.listing() })
      case OpMore:
        return this.ok({ id, threads: [this.host.older()], more: false, next: "" })
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
      case OpWatch: {
        const th = thread()
        if (!th) return this.missing(id)
        this.stopWatching()
        this.watcher = { threadID: th.id, deliver: (r) => this.onPush?.(r) }
        this.host.watch(this.watcher)
        return this.ok({ id, op: OpReady, thread_id: th.id, more: false, ...this.host.snapshot(th) })
      }
      case OpLog:
        return this.ok({ id, events: [], more: false })
      case OpStart:
        return this.ok({ id, threads: [this.host.view(this.host.begin(req.text ?? ""))] })
      case OpSend:
      case OpAnswer:
      case OpRunNow:
      case OpResumeGoal: {
        const th = thread()
        if (!th) return this.missing(id)
        this.host.run(th, req.text ?? th.title)
        return this.ok({ id })
      }
      case OpSteer: {
        const th = thread()
        if (th && req.text) this.host.push(th, { kind: "steer", text: req.text })
        return this.ok({ id })
      }
      case OpStop: {
        const th = thread()
        if (th) this.host.settle(th)
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
