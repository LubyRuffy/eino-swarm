import { describe, expect, it, vi } from "vitest"

import { openPhoneThread } from "./open-thread"
import type { RemoteLink } from "./link"
import { OpLog, OpOpen, OpReady, OpWatch, type RemoteEvent, type RemoteResponse } from "./rpc"
import { applyPush, emptyView, openView, prependOlder, type PhoneView } from "./session"
import { foldPhoneItems, managerBlocks } from "./transcript"

const detail = { id: "thread", title: "A conversation" }
const ready: RemoteResponse = {
  v: 1, id: "w", ok: true, op: OpReady, thread_id: detail.id, seq: 1,
  events: [{ thread_id: detail.id, seq: 1, kind: "user_message", text: "The request", created_at: "2026-01-01T00:00:00Z" }],
}

function harness(reply: (op: string, req: { op: string; before?: number }) => Promise<RemoteResponse>) {
  let view: PhoneView = openView(detail)
  const rpc = vi.fn((req: { op: string; before?: number }) => reply(req.op, req))
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

function historyEvent(seq: number, fields: Partial<RemoteEvent> = {}): RemoteEvent {
  return {
    thread_id: detail.id, seq, turn_id: "earlier-turn", agent_id: "manager",
    kind: "agent_message", text: "Earlier manager answer", created_at: "2026-01-01T00:00:00Z",
    ...fields,
  }
}

const hiddenReady: RemoteResponse = {
  ...ready, seq: 30, more: true,
  status: { running: true, turn_id: "latest-turn" },
  events: [
    historyEvent(28, { turn_id: "latest-turn", kind: "spawned", agent_id: "worker", role: "reader", text: "" }),
    historyEvent(29, { turn_id: "latest-turn", kind: "tool_call", tool_call_id: "spawn", text: "spawn_agent({})" }),
    historyEvent(30, { turn_id: "latest-turn", agent_id: "worker", text: "Worker-only progress" }),
  ],
}

function pendingReply() {
  let resolve!: (response: RemoteResponse) => void
  let reject!: (error: Error) => void
  const promise = new Promise<RemoteResponse>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
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

  it("finds visible manager history through filtered pages without draining history or moving the live edge", async () => {
    const h = harness(async (op, req) => {
      if (op === OpWatch) return { ...hiddenReady, detail }
      if (op === OpLog && req.before === 28) {
        return { v: 1, id: "filtered", ok: true, thread_id: detail.id, events: [], seq: 20, more: true }
      }
      if (op === OpLog && req.before === 20) {
        return { v: 1, id: "visible", ok: true, thread_id: detail.id, events: [historyEvent(18)], seq: 18, more: true }
      }
      throw new Error("Opening must stop once the manager history is visible")
    })
    expect(await openPhoneThread(h.opts)).toBe(true)
    expect(h.rpc.mock.calls.map(([req]) => [req.op, req.before])).toEqual([
      [OpWatch, undefined], [OpLog, 28], [OpLog, 20],
    ])
    expect(h.view().blocks.some((block) => block.text === "Earlier manager answer")).toBe(true)
    expect(h.view().blocks.some((block) => block.text === "Worker-only progress")).toBe(true)
    expect(h.view().oldestSeq).toBe(18)
    expect(h.view().hasMore).toBe(true)
    expect(h.view().lastSeq).toBe(30)
    expect(h.view().detail?.running?.turn_id).toBe("latest-turn")
    expect(h.opts.setError).not.toHaveBeenCalled()
  })

  it("stops opening history at exhaustion even when no manager message can be rendered", async () => {
    const h = harness(async (op) => op === OpWatch ? { ...hiddenReady, detail }
      : { v: 1, id: "end", ok: true, thread_id: detail.id, events: [], seq: 1, more: false })
    expect(await openPhoneThread(h.opts)).toBe(true)
    expect(h.rpc.mock.calls.map(([req]) => req.op)).toEqual([OpWatch, OpLog])
    expect(foldPhoneItems(managerBlocks(h.view().blocks))).toEqual([])
    expect(h.view().caughtUp).toBe(true)
    expect(h.view().hasMore).toBe(false)
    expect(h.view().lastSeq).toBe(30)
  })

  it("keeps a manually advanced history cursor when an older automatic read returns late", async () => {
    const page = pendingReply()
    const h = harness(async (op) => op === OpWatch ? { ...hiddenReady, detail } : page.promise)
    const opening = openPhoneThread(h.opts)
    await vi.waitFor(() => expect(h.rpc).toHaveBeenCalledWith({ op: OpLog, thread_id: detail.id, before: 28 }), { timeout: 250 })
    h.opts.commitView(prependOlder(h.view(), [historyEvent(18)], true, 18))
    const manuallyAdvanced = h.view()
    page.resolve({ v: 1, id: "late", ok: true, thread_id: detail.id, events: [], seq: 20, more: true })
    expect(await opening).toBe(true)
    expect(h.view()).toBe(manuallyAdvanced)
    expect(h.view().oldestSeq).toBe(18)
    expect(h.view().hasMore).toBe(true)
    expect(h.view().blocks.some((block) => block.text === "Earlier manager answer")).toBe(true)
    expect(h.rpc).toHaveBeenCalledTimes(2)
  })

  it("backfills an empty ready from its next-sequence cursor without moving the live turn", async () => {
    const h = harness(async (op) => op === OpWatch ? { ...hiddenReady, detail, events: [] }
      : { v: 1, id: "older", ok: true, thread_id: detail.id, events: [historyEvent(18)], seq: 18, more: true })
    expect(await openPhoneThread(h.opts)).toBe(true)
    expect(h.rpc.mock.calls.map(([req]) => [req.op, req.before])).toEqual([
      [OpWatch, undefined], [OpLog, 31],
    ])
    expect(h.view().events.map((event) => event.seq)).toEqual([18])
    expect(h.view().oldestSeq).toBe(18)
    expect(h.view().hasMore).toBe(true)
    expect(h.view().lastSeq).toBe(30)
    expect(h.view().detail?.running?.turn_id).toBe("latest-turn")
    expect(foldPhoneItems(managerBlocks(h.view().blocks))).toHaveLength(1)
  })

  it("stops when an empty older page cannot advance the existing history cursor", async () => {
    const h = harness(async (op) => op === OpWatch ? { ...hiddenReady, detail }
      : { v: 1, id: "no-progress", ok: true, thread_id: detail.id, events: [], seq: 28, more: true })
    expect(await openPhoneThread(h.opts)).toBe(true)
    expect(h.rpc.mock.calls.map(([req]) => [req.op, req.before])).toEqual([
      [OpWatch, undefined], [OpLog, 28],
    ])
    expect(h.view().oldestSeq).toBe(28)
    expect(h.view().hasMore).toBe(false)
    expect(h.opts.setError).not.toHaveBeenCalled()
  })

  it("merges opening history into live events and zero-sequence output that arrive while the page is pending", async () => {
    const page = pendingReply()
    const h = harness(async (op) => op === OpWatch ? { ...hiddenReady, detail } : page.promise)
    const opening = openPhoneThread(h.opts)
    await vi.waitFor(() => expect(h.rpc).toHaveBeenCalledWith({ op: OpLog, thread_id: detail.id, before: 28 }), { timeout: 250 })
    h.opts.commitView(applyPush(h.view(), { v: 1, id: "", ok: true, op: "event",
      event: historyEvent(31, { turn_id: "new-live-turn", kind: "user_message", text: "New live request" }),
      status: { running: true, turn_id: "new-live-turn" } }))
    h.opts.commitView(applyPush(h.view(), { v: 1, id: "", ok: true, op: "event",
      event: historyEvent(0, { turn_id: "new-live-turn", kind: "delta", message_id: "live-answer", text: "Live partial answer" }) }))
    page.resolve({ v: 1, id: "older", ok: true, thread_id: detail.id, events: [historyEvent(18)], seq: 18, more: true })
    expect(await opening).toBe(true)
    expect(h.view().events.map((event) => event.seq)).toEqual([18, 28, 29, 30, 31])
    expect(h.view().blocks.some((block) => block.text === "Earlier manager answer")).toBe(true)
    expect(h.view().blocks.some((block) => block.text === "New live request")).toBe(true)
    expect(h.view().blocks.some((block) => block.text === "Live partial answer" && block.streaming)).toBe(true)
    expect(h.view().lastSeq).toBe(31)
    expect(h.view().detail?.running?.turn_id).toBe("new-live-turn")
    expect(h.rpc).toHaveBeenCalledTimes(2)
  })

  it.each([
    ["Back", "response"], ["Back", "error"],
    ["another thread", "response"], ["another thread", "error"],
    ["a new opening of the same thread", "response"], ["a new opening of the same thread", "error"],
  ] as const)("keeps %s authoritative after a stale history %s", async (navigation, completion) => {
    const page = pendingReply()
    let watches = 0
    const h = harness(async (op) => {
      if (op === OpLog) return page.promise
      watches += 1
      return watches === 1 ? { ...hiddenReady, detail }
        : { ...ready, seq: 40, detail, events: [historyEvent(40, { kind: "user_message", text: "Current opening" })] }
    })
    const opening = openPhoneThread(h.opts)
    await vi.waitFor(() => expect(h.rpc).toHaveBeenCalledWith({ op: OpLog, thread_id: detail.id, before: 28 }), { timeout: 250 })
    if (navigation === "Back") h.opts.setView(emptyView())
    else if (navigation === "another thread") h.opts.setView(openView({ id: "other", title: "Other conversation" }))
    else {
      // The old and new calls share the real target and thread identity.
      // A thread-id-only guard must not resurrect the old request after A→Back→A.
      h.opts.setView(emptyView())
      h.opts.setView(openView(detail))
      expect(await openPhoneThread(h.opts)).toBe(true)
    }
    const authoritative = h.view()
    if (completion === "response") page.resolve({ v: 1, id: "stale", ok: true,
      thread_id: detail.id, seq: 18, events: [historyEvent(18)], more: true })
    else page.reject(new Error("Old history transport failed"))
    await opening
    expect(h.view()).toBe(authoritative)
    expect(h.opts.setError).not.toHaveBeenCalled()
  })

  it("does not request earlier history when the live tail already has a visible manager message", async () => {
    const h = harness(async () => ({ ...hiddenReady, detail,
      events: [...hiddenReady.events!, historyEvent(31, { turn_id: "latest-turn", text: "Visible current answer" })], seq: 31 }))
    expect(await openPhoneThread(h.opts)).toBe(true)
    expect(h.rpc.mock.calls.map(([req]) => req.op)).toEqual([OpWatch])
    expect(foldPhoneItems(managerBlocks(h.view().blocks))).toHaveLength(1)
  })

  it.each(["refused", "transport"] as const)("keeps the ready conversation without retrying a %s history read", async (failure) => {
    const h = harness(async (op) => {
      if (op === OpWatch) return { ...hiddenReady, detail }
      if (failure === "transport") throw new Error("History connection failed")
      return { v: 1, id: "refused", ok: false, thread_id: detail.id, error: "History unavailable" }
    })
    await openPhoneThread(h.opts)
    expect(h.rpc.mock.calls.map(([req]) => req.op)).toEqual([OpWatch, OpLog])
    expect(h.view().threadId).toBe(detail.id)
    expect(h.view().caughtUp).toBe(true)
    expect(h.view().lastSeq).toBe(30)
    expect(h.opts.setError).toHaveBeenCalledOnce()
  })
})
