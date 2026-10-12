import { afterEach, describe, expect, it, vi } from "vitest"

import { MockHost, MockLink, mockTickMs, resetMockHost, wantsMock } from "./mock-link"
import {
  OpEvent,
  OpFollowupSteer,
  OpList,
  OpLog,
  OpClients,
  OpOpen,
  OpReady,
  OpSend,
  OpStart,
  OpStop,
  OpWatch,
  type RemoteResponse,
} from "./rpc"

afterEach(() => {
  resetMockHost()
  vi.useRealTimers()
})

function host() {
  return new MockHost(0)
}

describe("the walkthrough host", () => {
  it("starts the selected waiting message after Stop without consuming its sibling", async () => {
    const prior = location.href
    history.replaceState(null, "", "/?mock=1&tick=400&queue=1")
    vi.useFakeTimers()
    const h = new MockHost(400)
    const link = new MockLink(h)
    try {
      const frames: RemoteResponse[] = []
      link.onPush = (frame) => frames.push(frame)
      const ready = await link.rpc({ op: OpWatch, thread_id: "t-live" })
      await link.rpc({ op: OpStop, thread_id: "t-live" })
      const stopped = await link.rpc({ op: OpOpen, thread_id: "t-live" })
      expect(stopped.detail?.running).toBeUndefined()
      expect(h.find("t-live")?.followups?.map((row) => row.id)).toEqual(["first", "second"])

      const accepted = await link.rpc({ op: OpFollowupSteer, thread_id: "t-live", followup_id: "first" })
      expect(accepted.ok).toBe(true)
      expect(accepted.followups?.map((row) => row.id)).toEqual(["second"])
      await vi.advanceTimersByTimeAsync(0)
      const started = frames.find((frame) => frame.event?.kind === "user_message")
      expect(started?.event?.text).toBe("first waiting message")
      expect(started?.event?.seq).toBeGreaterThan(ready.seq!)
      expect(started?.status?.running).toBe(true)
      expect(frames.some((frame) => frame.event?.kind === "steer")).toBe(false)
      expect(started?.status?.followups?.map((row) => row.id)).toEqual(["second"])

      expect(frames.filter((frame) => frame.event?.kind === "user_message")).toHaveLength(1)
      await link.rpc({ op: OpStop, thread_id: "t-live" })
      expect(h.find("t-live")?.running).toBe(false)
      expect(h.find("t-live")?.followups?.map((row) => row.id)).toEqual(["second"])
    } finally { link.close(); h.stop(); history.replaceState(null, "", prior) }
  })

  it("injects a waiting message into a live turn without starting another turn", async () => {
    const prior = location.href
    history.replaceState(null, "", "/?mock=1&tick=400&queue=1")
    vi.useFakeTimers()
    const h = new MockHost(400)
    const link = new MockLink(h)
    try {
      const frames: RemoteResponse[] = []
      link.onPush = (frame) => frames.push(frame)
      await link.rpc({ op: OpWatch, thread_id: "t-live" })
      const accepted = await link.rpc({ op: OpFollowupSteer, thread_id: "t-live", followup_id: "first" })
      expect(accepted.ok).toBe(true)
      expect(accepted.followups?.map((row) => row.id)).toEqual(["second"])
      await vi.runAllTimersAsync()
      expect(frames).toHaveLength(1)
      expect(frames[0].event).toMatchObject({ kind: "steer", text: "first waiting message" })
      expect(frames[0].status?.running).toBe(true)
    } finally { link.close(); h.stop(); history.replaceState(null, "", prior) }
  })

  it("rejects a missing waiting message without changing the turn or queue", async () => {
    const prior = location.href
    history.replaceState(null, "", "/?mock=1&tick=400&queue=1")
    const h = new MockHost(400)
    const link = new MockLink(h)
    try {
      const frames: RemoteResponse[] = []
      link.onPush = (frame) => frames.push(frame)
      await link.rpc({ op: OpWatch, thread_id: "t-live" })
      await link.rpc({ op: OpStop, thread_id: "t-live" })
      const rejected = await link.rpc({ op: OpFollowupSteer, thread_id: "t-live", followup_id: "gone" })
      expect(rejected.ok).toBe(false)
      expect(rejected.error).toBe("no such thread")
      expect(frames).toHaveLength(0)
      expect(h.find("t-live")?.running).toBe(false)
      expect(h.find("t-live")?.followups?.map((row) => row.id)).toEqual(["first", "second"])
    } finally { link.close(); h.stop(); history.replaceState(null, "", prior) }
  })

  it("replays model response decoding retries and a terminal error without inventing tools", async () => {
    const prior = location.href
    history.replaceState(null, "", "/?mock=1&tick=0&history=model-response-error")
    const h = host()
    try {
      const link = new MockLink(h)
      const watch = await link.rpc({ op: OpWatch, thread_id: "t-recent" })
      const events = watch.events ?? []
      expect(events.map((event) => event.kind)).toEqual(["model_retry", "model_retry", "error", "done"])
      expect(events.slice(0, 2).map((event) => JSON.parse(event.text))).toEqual([
        { attempt: 1, cap: 2, reason: "model_response_invalid" },
        { attempt: 2, cap: 2, reason: "model_response_invalid" },
      ])
      expect(events[2]).toMatchObject({ thread_id: "t-recent", agent_id: "manager", text: "", err: "the model service returned a response that could not be decoded" })
      expect(events.every((event) => event.seq > 0)).toBe(true)
      expect(new Set(events.map((event) => event.seq)).size).toBe(events.length)
      expect(watch.status?.running).toBe(false)
      expect(events.some((event) => event.kind === "tool_call" || event.kind === "tool_result")).toBe(false)
      link.close()
      const replay = await new MockLink(h).rpc({ op: OpWatch, thread_id: "t-recent" })
      expect(replay.events).toEqual(events)
      expect(replay.status?.running).toBe(false)
    } finally { h.stop(); history.replaceState(null, "", prior) }
  })

  it("replays a terminal model API failure as an error instead of a tool result", async () => {
    const prior = location.href
    history.replaceState(null, "", "/?mock=1&tick=0&history=api-error")
    const h = host()
    try {
      const link = new MockLink(h)
      const watch = await link.rpc({ op: OpWatch, thread_id: "t-recent" })
      const error = watch.events?.find((ev) => ev.kind === "error")
      expect(watch.status?.running).toBe(false)
      expect(error).toMatchObject({
        thread_id: "t-recent", agent_id: "manager", text: "",
        err: "model API request failed: HTTP 502 Bad Gateway: unexpected end of JSON input",
      })
      expect(error?.seq).toBeGreaterThan(0)
      expect(watch.events?.at(-1)?.kind).toBe("done")
      expect(watch.events?.some((ev) => ev.kind === "tool_call" || ev.kind === "tool_result")).toBe(false)
      link.close()
      const replay = await new MockLink(h).rpc({ op: OpWatch, thread_id: "t-recent" })
      expect(replay.events?.find((ev) => ev.kind === "error")).toEqual(error)
      expect(replay.status?.running).toBe(false)
    } finally { h.stop(); history.replaceState(null, "", prior) }
  })

  it("streams separate live manager blocks before older history and the worker roster", async () => {
    const prior = location.href
    history.replaceState(null, "", "/?mock=1&tick=0&history=streamed-agents")
    vi.useFakeTimers()
    const h = host()
    try {
      const link = new MockLink(h)
      const frames: RemoteResponse[] = []
      link.onPush = (frame) => frames.push(frame)
      const ready = await link.rpc({ op: OpWatch, thread_id: "t-live" })
      expect(ready.more).toBe(true)
      expect(ready.events?.map((event) => event.kind)).toEqual(["user_message"])
      expect(frames).toHaveLength(0)
      await vi.runAllTimersAsync()
      const manager = frames.map((frame) => frame.event!).filter((event) => event.agent_id === "manager")
      expect(manager.filter((event) => event.kind === "delta").map((event) => event.seq)).toEqual([0, 0, 0])
      expect(manager.filter((event) => event.kind === "agent_message").map((event) => event.text))
        .toEqual(["Manager update 0", "Manager update 1", "Manager update 2"])
      expect(manager.filter((event) => event.kind === "agent_message").every((event) => event.seq > ready.seq!)).toBe(true)
      expect(manager.filter((event) => event.kind === "tool_call")).toHaveLength(3)
      expect(frames.filter((frame) => frame.event?.kind === "spawned")).toHaveLength(3)
      expect(frames.some((frame) => frame.event?.text === "Worker evidence")).toBe(true)
      const older = await link.rpc({ op: OpLog, thread_id: "t-live", before: ready.seq })
      expect(older.events?.map((event) => event.text)).toEqual(["Older manager answer"])
      expect(older.events![0].seq).toBeLessThan(ready.events![0].seq)
      expect(older.more).toBe(false)
      const count = frames.length
      const replay = await link.rpc({ op: OpWatch, thread_id: "t-live" })
      await vi.runAllTimersAsync()
      expect(frames).toHaveLength(count)
      expect(replay.events?.every((event) => event.seq > 0)).toBe(true)
    } finally {
      h.stop()
      history.replaceState(null, "", prior)
    }
  })

  it("keeps a watchdog-limit failure visible while the phone is idle", async () => {
    const prior = location.href
    history.replaceState(null, "", "/?mock=1&tick=0&history=timeout")
    const h = host()
    try {
      const watch = await new MockLink(h).rpc({ op: OpWatch, thread_id: "t-recent" })
      expect(watch.status?.running).toBe(false)
      expect(watch.events?.find((ev) => ev.kind === "error")?.err).toContain("3 consecutive watchdog timeouts")
    } finally { h.stop(); history.replaceState(null, "", prior) }
  })

  it("carries peer provenance through an idle phone history", async () => {
    const prior = location.href
    history.replaceState(null, "", "/?mock=1&tick=0&history=peers")
    const h = host()
    try {
      const link = new MockLink(h)
      const watch = await link.rpc({ op: OpWatch, thread_id: "t-recent" })
      expect(watch.status?.running).toBe(false)
      expect(watch.events?.find((ev) => ev.kind === "task_message")).toMatchObject({
        source_thread_id: "peer-task", source_turn_id: "peer-turn", text: "Peer coordination request.",
      })
    } finally {
      h.stop()
      history.replaceState(null, "", prior)
    }
  })

  it("only stands in when the URL asks for it", () => {
    expect(wantsMock("?mock=1")).toBe(true)
    expect(wantsMock("")).toBe(false)
    expect(wantsMock("?mock=0")).toBe(false)
  })

  it("lets a test drive the turn without waiting on the animation", () => {
    expect(mockTickMs("?tick=0")).toBe(0)
    expect(mockTickMs("?tick=nope")).toBeGreaterThan(0)
    expect(mockTickMs("")).toBeGreaterThan(0)
  })

  it("answers list with live rows separate from idle ones", async () => {
    const link = new MockLink(host())
    const r = await link.rpc({ op: OpList })
    expect(r.ok).toBe(true)
    expect(r.projects?.length).toBeGreaterThan(0)
    expect(r.running?.length).toBeGreaterThan(0)
    const liveIDs = new Set((r.running ?? []).map((x) => x.thread_id))
    // A live row must not also eat a slot in the idle recents page.
    expect((r.threads ?? []).some((t) => liveIDs.has(t.id))).toBe(false)
    expect((r.threads ?? []).every((t) => t.last_active_at)).toBe(true)
    expect(r.running?.find((x) => x.thread_id === "t-live")?.project_id).toBe("p-platform")
    expect(r.clients?.enabled).toBe(true)
    expect(r.clients?.tools?.map((tool) => tool.id)).toEqual(["claude", "codex", "cursor"])
  })

  it("pages an older client session without opening it", async () => {
    const link = new MockLink(host())
    const older = await link.rpc({ op: OpClients, group: "claude", before: 1 })
    const tasks = older.clients?.tools?.[0]?.tasks ?? []
    expect(tasks.map((task) => task.title)).toEqual(["older session"])
    expect(tasks.some((task) => task.title.includes("notes.md"))).toBe(false)
  })

  it("opens a thread and hands watch a snapshot already at the tail", async () => {
    const link = new MockLink(host())
    const list = await link.rpc({ op: OpList })
    const id = list.running![0].thread_id
    const open = await link.rpc({ op: OpOpen, thread_id: id })
    expect(open.detail?.id).toBe(id)
    const watch = await link.rpc({ op: OpWatch, thread_id: id })
    expect(watch.op).toBe(OpReady)
    expect(watch.events?.length).toBeGreaterThan(0)
    expect(watch.seq).toBe(watch.events![watch.events!.length - 1].seq)
  })

  it("streams a turn to whoever is watching that thread", async () => {
    const h = host()
    const link = new MockLink(h)
    const seen: RemoteResponse[] = []
    link.onPush = (r) => seen.push(r)
    await link.rpc({ op: OpWatch, thread_id: "t-live" })
    await link.rpc({ op: OpSend, thread_id: "t-live", text: "go" })
    await vi.waitFor(() => expect(seen.some((r) => r.event?.kind === "done")).toBe(true))
    expect(seen.every((r) => r.op === OpEvent)).toBe(true)
    const kinds = seen.map((r) => r.event?.kind)
    expect(kinds).toContain("user_message")
    expect(kinds).toContain("tool_call")
    expect(kinds).toContain("agent_message")
  })

  // Strict mode opens two sockets onto one PC. A turn started on either has
  // to reach the one the screen is rendering from.
  it("is one PC seen down two sockets, not two PCs", async () => {
    const h = host()
    const a = new MockLink(h)
    const b = new MockLink(h)
    const seen: string[] = []
    b.onPush = (r) => seen.push(r.event?.kind ?? "")
    await b.rpc({ op: OpWatch, thread_id: "t-live" })
    await a.rpc({ op: OpSend, thread_id: "t-live", text: "go" })
    await vi.waitFor(() => expect(seen).toContain("done"))
  })

  it("stops pushing to a socket that closed", async () => {
    const h = host()
    const link = new MockLink(h)
    const seen: string[] = []
    link.onPush = (r) => seen.push(r.event?.kind ?? "")
    await link.rpc({ op: OpWatch, thread_id: "t-live" })
    link.close()
    expect(link.alive()).toBe(false)
    await expect(link.rpc({ op: OpList })).rejects.toThrow()
    const th = h.find("t-live")!
    h.push(th, { kind: "agent_message", text: "after close" })
    expect(seen).toHaveLength(0)
  })

  it("says the turn is over on the same frame that ends it", async () => {
    const h = host()
    const link = new MockLink(h)
    const frames: RemoteResponse[] = []
    link.onPush = (r) => frames.push(r)
    await link.rpc({ op: OpWatch, thread_id: "t-live" })
    await link.rpc({ op: OpSend, thread_id: "t-live", text: "go" })
    await vi.waitFor(() => expect(frames.some((r) => r.event?.kind === "done")).toBe(true))
    const done = frames.find((r) => r.event?.kind === "done")!
    expect(done.status?.running).toBe(false)
  })

  it("pairs a tool result with its own call across replays", async () => {
    const h = host()
    const link = new MockLink(h)
    const calls: string[] = []
    link.onPush = (r) => {
      if (r.event?.kind === "tool_call") calls.push(r.event.tool_call_id ?? "")
    }
    await link.rpc({ op: OpWatch, thread_id: "t-live" })
    await link.rpc({ op: OpSend, thread_id: "t-live", text: "one" })
    await vi.waitFor(() => expect(calls.length).toBe(2))
    const before = [...calls]
    await link.rpc({ op: OpSend, thread_id: "t-live", text: "two" })
    await vi.waitFor(() => expect(calls.length).toBe(4))
    expect(new Set(calls).size).toBe(4)
    expect(calls.slice(0, 2)).toEqual(before)
  })

  it("starts a new conversation and puts it at the top of the list", async () => {
    const h = host()
    const link = new MockLink(h)
    const started = await link.rpc({ op: OpStart, text: "a fresh one" })
    const id = started.threads![0].id
    expect(id).toBeTruthy()
    const open = await link.rpc({ op: OpOpen, thread_id: id })
    expect(open.detail?.title).toBe("a fresh one")
  })

  it("refuses a thread it does not have rather than inventing one", async () => {
    const link = new MockLink(host())
    const r = await link.rpc({ op: OpOpen, thread_id: "nope" })
    expect(r.ok).toBe(false)
    expect(r.error).toBeTruthy()
  })

  it("answers an unknown op the way an older host would", async () => {
    const link = new MockLink(host())
    const r = await link.rpc({ op: "not_an_op" })
    expect(r.ok).toBe(false)
    expect(r.code).toBe("unknown_op")
  })
})


it("retains completed attention through open/watch and clears only the exact read watermark", async () => {
  const prior = location.href
  history.replaceState(null, "", "/?mock=1&tick=0&history=unread-completion")
  const h = host(), link = new MockLink(h)
  try {
    const first = await link.rpc({ op: OpList })
    const completed = first.attention?.find((row) => row.id === "t-recent")
    expect(completed).toMatchObject({ unread: true, running: false })
    const open = await link.rpc({ op: OpOpen, thread_id: "t-recent" })
    await link.rpc({ op: OpWatch, thread_id: "t-recent" })
    expect((await link.rpc({ op: OpList })).attention).toEqual(first.attention)
    const read = await link.rpc({ op: "read", thread_id: "t-recent", completed_seq: open.detail!.completed_seq })
    expect(read.detail).toMatchObject({ unread: false, completed_seq: completed!.completed_seq })
    expect((await link.rpc({ op: OpList })).attention).toEqual([])
    const th = h.find("t-recent")!
    h.push(th, { kind: "done", agentID: "manager" })
    expect((await link.rpc({ op: "read", thread_id: "t-recent", completed_seq: completed!.completed_seq })).detail?.unread).toBe(true)
    expect((await link.rpc({ op: OpList })).attention).toHaveLength(1)
  } finally { link.close(); h.stop(); history.replaceState(null, "", prior) }
})


it("pages unread completion independently and continues after its keyset anchor is read", async () => {
  const prior = location.href
  history.replaceState(null, "", "/?mock=1&tick=0&history=unread-completion-pages")
  const h = host(), link = new MockLink(h)
  try {
    const first = await link.rpc({ op: OpList })
    expect(first.attention?.map((row) => row.id)).toEqual(["t-recent", "done-1", "done-2"])
    expect(first.attention_more).toBe(true)
    const second = await link.rpc({ op: "more", group: "attention", cursor: first.attention_cursor })
    expect(second.attention?.map((row) => row.id)).toEqual(["done-3", "done-4", "done-5"])
    expect(second.threads).toBeUndefined()
    expect(second.running).toBeUndefined()
    expect(second.groups).toBeUndefined()
    const anchor = await link.rpc({ op: OpOpen, thread_id: "done-5" })
    await link.rpc({ op: "read", thread_id: "done-5", completed_seq: anchor.detail!.completed_seq })
    const last = await link.rpc({ op: "more", group: "attention", cursor: second.attention_cursor })
    expect(last.attention?.map((row) => row.id)).toEqual(["done-6", "done-7"])
    expect(last.attention_more).toBe(false)
    expect(last.attention_cursor).toBe("")
    expect((await link.rpc({ op: OpList })).attention?.map((row) => row.id)).toEqual(first.attention?.map((row) => row.id))
  } finally { link.close(); h.stop(); history.replaceState(null, "", prior) }
})


it("keeps persisted completion ordering timestamps stable across walkthrough list polls", () => {
  const now = vi.spyOn(Date, "now").mockReturnValue(1700000000000)
  try {
    const host = new MockHost(0)
    const before = host.listing().threads.map((row) => [row.id, row.last_active_at])
    now.mockReturnValue(1700000002000)
    expect(host.listing().threads.map((row) => [row.id, row.last_active_at])).toEqual(before)
    host.stop()
  } finally { now.mockRestore() }
})


it("pages equal-time completions in the host's descending id order", () => {
  const host = new MockHost(0)
  for (const id of ["t-live", "t-wait", "t-recent", "t-notes", "t-loose"]) {
    const th = host.find(id)!
    th.completedSeq = 12; th.readSeq = 0; th.minutesAgo = 1
  }
  const first = host.attentionPage()
  expect(first.attention.map((row) => row.id)).toEqual(["t-wait", "t-recent", "t-notes"])
  expect(host.attentionPage(first.attention_cursor).attention.map((row) => row.id)).toEqual(["t-loose", "t-live"])
  host.stop()
})
