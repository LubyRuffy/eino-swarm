import { describe, expect, it, vi } from "vitest"

import { openPhoneThread } from "./open-thread"
import type { RemoteLink } from "./link"
import { OpOpen, OpReady, OpWatch, type RemoteResponse } from "./rpc"
import { applyPush, emptyView, openView, type PhoneView } from "./session"

const detail = { id: "thread", title: "A conversation" }
const ready: RemoteResponse = {
  v: 1, id: "w", ok: true, op: OpReady, thread_id: detail.id, seq: 1,
  events: [{ thread_id: detail.id, seq: 1, kind: "user_message", text: "The request", created_at: "2026-01-01T00:00:00Z" }],
}

function harness(reply: (op: string) => Promise<RemoteResponse>) {
  let view: PhoneView = openView(detail)
  const rpc = vi.fn((req: { op: string }) => reply(req.op))
  const opts = {
    target: { rpc } as unknown as RemoteLink,
    id: detail.id,
    view: () => view,
    setView: (next: PhoneView) => { view = next },
    commitView: (next: PhoneView) => { view = next },
    setError: vi.fn(), bumpLoad: vi.fn(), clearOlder: vi.fn(),
  }
  return { opts, rpc, view: () => view }
}

describe("opening a phone conversation", () => {
  it("paints the header and conversation after one watch response", async () => {
    const h = harness(async () => ({ ...ready, detail }))
    expect(await openPhoneThread(h.opts)).toBe(true)
    expect(h.rpc).toHaveBeenCalledTimes(1)
    expect(h.rpc).toHaveBeenCalledWith({ op: OpWatch, thread_id: detail.id, include_detail: true })
    expect(h.view().caughtUp).toBe(true)
    expect(h.view().blocks.some((block) => block.text === "The request")).toBe(true)
  })

  it("keeps old PCs usable when watch does not include details", async () => {
    const h = harness(async (op) => op === OpOpen ? { v: 1, id: "o", ok: true, detail } : ready)
    expect(await openPhoneThread(h.opts)).toBe(true)
    expect(h.rpc.mock.calls.map(([req]) => req.op)).toEqual([OpWatch, OpOpen])
    expect(h.view().caughtUp).toBe(true)
    expect(h.view().detail?.id).toBe(detail.id)
  })

  it("keeps live events which arrive before the opening response", async () => {
    const h = harness(async () => {
      h.opts.commitView(applyPush(h.view(), { v: 1, id: "", ok: true, op: "event",
        event: { thread_id: detail.id, seq: 2, kind: "agent_message", text: "New answer", created_at: "2026-01-01T00:00:01Z" } }))
      return { ...ready, detail }
    })
    expect(await openPhoneThread(h.opts)).toBe(true)
    expect(h.view().events.map((event) => event.seq)).toEqual([1, 2])
    expect(h.view().blocks.some((block) => block.text === "New answer")).toBe(true)
  })

  it("does not reopen a conversation after Back while waiting", async () => {
    const h = harness(async () => {
      h.opts.setView(emptyView())
      return { ...ready, detail }
    })
    expect(await openPhoneThread(h.opts)).toBe(false)
    expect(h.view().threadId).toBeNull()
    expect(h.opts.bumpLoad).not.toHaveBeenCalled()
  })

  it("shows a refused watch without making another RPC", async () => {
    const h = harness(async () => ({ v: 1, id: "w", ok: false, code: "too_large", error: "Cannot open" }))
    expect(await openPhoneThread(h.opts)).toBe(false)
    expect(h.rpc).toHaveBeenCalledTimes(1)
    expect(h.opts.setError).toHaveBeenCalled()
  })

  it("keeps navigation authoritative during a legacy header request", async () => {
    const h = harness(async (op) => {
      if (op !== OpOpen) return ready
      h.opts.setView(emptyView())
      return { v: 1, id: "o", ok: true, detail }
    })
    expect(await openPhoneThread(h.opts)).toBe(false)
    expect(h.view().threadId).toBeNull()
  })

  it("reports a missing legacy header without painting another conversation", async () => {
    const h = harness(async (op) => op === OpOpen
      ? { v: 1, id: "o", ok: false, error: "task not found" } : ready)
    h.opts.setView({ ...emptyView(), threadId: detail.id })
    expect(await openPhoneThread(h.opts)).toBe(false)
    expect(h.view().threadId).toBeNull()
    expect(h.opts.setError).toHaveBeenCalled()
  })

  it("reports a transport failure while the same conversation is still open", async () => {
    const h = harness(async () => { throw new Error("network failed") })
    expect(await openPhoneThread(h.opts)).toBe(false)
    expect(h.opts.setError).toHaveBeenCalled()
  })

  it("does not restore an old running status over a live completion during legacy header loading", async () => {
    const h = harness(async (op) => {
      if (op !== OpOpen) return { ...ready, status: { running: true, turn_id: "turn" } }
      h.opts.commitView(applyPush(h.view(), { v: 1, id: "", ok: true, op: "event",
        event: { thread_id: detail.id, turn_id: "turn", seq: 2, kind: "done", text: "", created_at: "2026-01-01T00:00:02Z" },
        status: { running: false } }))
      return { v: 1, id: "o", ok: true, detail }
    })
    expect(await openPhoneThread(h.opts)).toBe(true)
    expect(h.view().events.map((event) => event.seq)).toEqual([1, 2])
    expect(h.view().detail?.running).toBeUndefined()
  })

  it("retains a live zero-sequence answer while the legacy header is pending", async () => {
    const h = harness(async (op) => {
      if (op !== OpOpen) return ready
      h.opts.commitView(applyPush(h.view(), { v: 1, id: "", ok: true, op: "event",
        event: { thread_id: detail.id, agent_id: "manager", message_id: "answer", seq: 0, kind: "delta", text: "Live partial answer", created_at: "2026-01-01T00:00:02Z" } }))
      return { v: 1, id: "o", ok: true, detail: { ...detail, title: "Full title" } }
    })
    h.opts.commitView(applyPush(h.view(), ready))
    expect(await openPhoneThread(h.opts)).toBe(true)
    expect(h.view().blocks.some((block) => block.text === "Live partial answer")).toBe(true)
    expect(h.view().detail?.title).toBe("Full title")
  })

  it("keeps an observed title even when live updates return to the original value", async () => {
    const h = harness(async (op) => {
      if (op !== OpOpen) return ready
      for (const [seq, title] of [[2, "Intermediate title"], [3, detail.title]] as const) {
        h.opts.commitView(applyPush(h.view(), { v: 1, id: "", ok: true, op: "event",
          event: { thread_id: detail.id, seq, kind: "title", text: title, created_at: "2026-01-01T00:00:02Z" } }))
      }
      return { v: 1, id: "o", ok: true, detail: { ...detail, title: "Intermediate title" } }
    })
    expect(await openPhoneThread(h.opts)).toBe(true)
    expect(h.view().detail?.title).toBe(detail.title)
  })

  it("keeps the actual newer turn status attached to a previous turn's completion", async () => {
    const h = harness(async (op) => {
      if (op !== OpOpen) return { ...ready, status: { running: true, turn_id: "old" } }
      h.opts.commitView(applyPush(h.view(), { v: 1, id: "", ok: true, op: "event",
        event: { thread_id: detail.id, turn_id: "old", seq: 2, kind: "done", text: "", created_at: "2026-01-01T00:00:02Z" },
        status: { running: true, turn_id: "new" } }))
      return { v: 1, id: "o", ok: true, detail: { ...detail,
        running: { thread_id: detail.id, title: detail.title, turn_id: "new" } } }
    })
    expect(await openPhoneThread(h.opts)).toBe(true)
    expect(h.view().detail?.running?.turn_id).toBe("new")
  })
})
