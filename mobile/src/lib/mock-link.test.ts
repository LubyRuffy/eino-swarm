import { afterEach, describe, expect, it, vi } from "vitest"

import { MockHost, MockLink, mockTickMs, resetMockHost, wantsMock } from "./mock-link"
import {
  OpEvent,
  OpList,
  OpOpen,
  OpReady,
  OpSend,
  OpStart,
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
