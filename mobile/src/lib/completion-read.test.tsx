import { act, renderHook, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { mergeCompletion, primaryCompletion, readInbox, useCompletionRead } from "./completion-read"
import { emptyInbox, reduceInbox } from "./inbox-window"
import type { RemoteLink } from "./link"
import type { RemoteResponse } from "./rpc"
import { applyPush, openView, type PhoneView } from "./session"

const detail = { id: "t", title: "Task", unread: true, completed_seq: 12 }
const terminal = { thread_id: "t", seq: 12, kind: "done", agent_id: "manager", text: "", created_at: "" }
const view = () => ({ ...openView(detail), caughtUp: true, lastSeq: 12, events: [terminal] })
const event = (changes: object = {}): RemoteResponse => ({ v: 1, id: "", ok: true, op: "event", thread_id: "t",
  event: { thread_id: "t", seq: 13, kind: "done", agent_id: "manager", text: "", created_at: "", ...changes } })

describe("completion read watermarks", () => {
  it("accepts only new primary terminal events after ready, never workers or replay", () => {
    expect(primaryCompletion(event(), view())).toBe(13)
    expect(primaryCompletion(event({ kind: "error", agent_id: undefined }), view())).toBe(13)
    for (const changes of [{ kind: "delta" }, { seq: 0 }, { seq: 12 }, { agent_id: "worker" }, { role: "reader" }, { thread_id: "other" }]) {
      expect(primaryCompletion(event(changes), view())).toBe(0)
    }
    expect(primaryCompletion({ ...event(), thread_id: "other" }, view())).toBe(0)
    expect(primaryCompletion({ ...event(), op: "ready" }, view())).toBe(0)
    expect(primaryCompletion({ ...event(), event: undefined }, view())).toBe(0)
    expect(primaryCompletion(event(), { ...view(), caughtUp: false })).toBe(0)
    expect(primaryCompletion(event(), { ...view(), detail: null })).toBe(0)
  })

  it("keeps newer completion state against an older successful read reply", () => {
    const newer = { ...detail, completed_seq: 14 }
    expect(mergeCompletion(newer, { ...detail, unread: false })).toBe(newer)
    expect(mergeCompletion(detail, { ...detail, id: "other", unread: false })).toBe(detail)
    const read = { ...detail, unread: false }
    expect(mergeCompletion(read, detail)).toBe(read)
    expect(mergeCompletion(read, { ...detail, completed_seq: 14 })).toMatchObject({ completed_seq: 14, unread: true })
    expect(mergeCompletion({ ...read, completed_seq: 0 }, detail, 12)).toMatchObject({ completed_seq: 0, unread: false })
    const row = { ...detail, running: false, last_active_at: "" }
    const inbox = reduceInbox(emptyInbox(), { threads: [row], attention: [row] }, "replace")
    expect(readInbox(inbox, { ...detail, unread: true, completed_seq: 14 }).attention).toHaveLength(1)
    const updated = readInbox(inbox, { ...detail, unread: false })
    expect(updated.attention).toEqual([])
    expect(updated.head[0].unread).toBe(false)
  })

  it("retains a verified newer completion watermark while an older read is in flight", () => {
    const live = applyPush(view(), event())
    const current = { ...live, detail: mergeCompletion(live.detail!, { ...detail, completed_seq: 13 }) }
    expect(current.detail?.completed_seq).toBe(13)
    expect(mergeCompletion(current.detail!, { ...detail, unread: false }).unread).toBe(true)
    expect(applyPush(view(), event({ agent_id: "worker" })).detail?.completed_seq).toBe(12)
  })

  it("does not clear the inbox when a late reply predates a new visible completion", () => {
    const row = { ...detail, running: false, last_active_at: "" }
    const inbox = reduceInbox(emptyInbox(), { threads: [row], attention: [row] }, "replace")
    const pushed = applyPush(view(), event())
    const live = { ...pushed, detail: mergeCompletion(pushed.detail!, { ...detail, completed_seq: 13 }) }
    const applied = readInbox(inbox, { ...detail, unread: false }, live)
    expect(applied.attention).toHaveLength(1)
    const current = readInbox(inbox, { ...detail, unread: false }, view())
    expect(current.attention).toEqual([])
    expect(readInbox(inbox, { ...detail, unread: false }, { ...view(), threadId: "other", detail: { id: "other", title: "Other" } }).attention).toEqual([])
  })

  it("does not relight an acknowledged watermark when an older read returns after a newer one", async () => {
    const pending: Array<(reply: RemoteResponse) => void> = []
    const rpc = vi.fn(() => new Promise<RemoteResponse>((resolve) => pending.push(resolve)))
    const target = { rpc } as unknown as RemoteLink
    const current = { current: view() as PhoneView }
    let inbox = reduceInbox(emptyInbox(), { threads: [{ ...detail, running: false, last_active_at: "" }],
      attention: [{ ...detail, running: false, last_active_at: "" }] }, "replace")
    const hook = renderHook(() => useCompletionRead({ link: { current: target }, view: current, visible: true,
      onRead: (reply, baseline) => {
        inbox = readInbox(inbox, reply, current.current, baseline)
        current.current = { ...current.current, detail: mergeCompletion(current.current.detail!, reply, baseline) }
      }, onError: vi.fn() }))
    act(() => hook.result.current.request(target, "t", 12))
    await waitFor(() => expect(pending).toHaveLength(1))
    await act(async () => { hook.result.current.observe(target, event({ seq: 14 })); current.current = applyPush(current.current, event({ seq: 14 })) })
    await waitFor(() => expect(pending).toHaveLength(2))
    await act(async () => pending[1]({ v: 1, id: "", ok: true, detail: { ...detail, completed_seq: 14, unread: false } }))
    await act(async () => pending[0]({ v: 1, id: "", ok: true, detail: { ...detail, completed_seq: 14, unread: true } }))
    expect(current.current.detail).toMatchObject({ completed_seq: 14, unread: false })
    expect(inbox.attention).toEqual([])
    expect(inbox.head[0]).toMatchObject({ completed_seq: 14, unread: false })
  })

  it("does not restore a removed completion when a read returns after rewind", async () => {
    let resolve!: (reply: RemoteResponse) => void
    const target = { rpc: vi.fn(() => new Promise<RemoteResponse>((done) => { resolve = done })) } as unknown as RemoteLink
    const current = { current: view() as PhoneView }
    const hook = renderHook(() => useCompletionRead({ link: { current: target }, view: current, visible: true,
      onRead: (reply, baseline) => { current.current = { ...current.current, detail: mergeCompletion(current.current.detail!, reply, baseline) } }, onError: vi.fn() }))
    act(() => hook.result.current.request(target, "t", 12))
    await waitFor(() => expect(target.rpc).toHaveBeenCalled())
    current.current = { ...openView({ ...detail, completed_seq: 0, unread: false }), caughtUp: true }
    await act(async () => resolve({ v: 1, id: "", ok: true, detail }))
    expect(current.current.detail).toMatchObject({ completed_seq: 0, unread: false })
  })

  it("keeps authoritative completion watermarks separate from first live and quiet terminal observations", () => {
    const fresh = { ...openView({ ...detail, completed_seq: 0, unread: false }), caughtUp: true }
    expect(applyPush(fresh, event()).detail).toMatchObject({ completed_seq: 0, unread: false })
    expect(applyPush(view(), event()).detail?.completed_seq).toBe(12)
  })

  it.each([false, true])("accepts the authoritative reply for a first live or quiet no-op completion (quiet=%s)", async (quiet) => {
    const initial = quiet ? view() : { ...openView({ ...detail, completed_seq: 0, unread: false }), caughtUp: true }
    const current = { current: initial as PhoneView }
    const reply = quiet ? detail : { ...detail, completed_seq: 13, unread: false }
    const rpc = vi.fn().mockResolvedValue({ v: 1, id: "", ok: true, detail: reply })
    const target = { rpc } as unknown as RemoteLink
    const onRead = vi.fn((next, baseline) => { current.current = { ...current.current,
      detail: mergeCompletion(current.current.detail!, next, baseline) } })
    const hook = renderHook(() => useCompletionRead({ link: { current: target }, view: current, visible: true, onRead, onError: vi.fn() }))
    await act(async () => { hook.result.current.observe(target, event()); current.current = applyPush(current.current, event()) })
    await waitFor(() => expect(onRead).toHaveBeenCalledWith(reply, quiet ? 12 : 0))
    expect(rpc).toHaveBeenCalledWith({ op: "read", thread_id: "t", completed_seq: 13 })
    expect(current.current.detail).toMatchObject(reply)
  })

  it("holds an older response behind a new visible terminal without guessing whether that terminal is quiet", async () => {
    const pending: Array<(reply: RemoteResponse) => void> = []
    const rpc = vi.fn(() => new Promise<RemoteResponse>((resolve) => pending.push(resolve)))
    const target = { rpc } as unknown as RemoteLink, current = { current: view() as PhoneView }, onRead = vi.fn()
    const hook = renderHook(() => useCompletionRead({ link: { current: target }, view: current, visible: true, onRead, onError: vi.fn() }))
    act(() => hook.result.current.request(target, "t", 12))
    await waitFor(() => expect(pending).toHaveLength(1))
    await act(async () => { hook.result.current.observe(target, event()); current.current = applyPush(current.current, event()) })
    await waitFor(() => expect(pending).toHaveLength(2))
    await act(async () => pending[0]({ v: 1, id: "", ok: true, detail: { ...detail, unread: false } }))
    expect(onRead).not.toHaveBeenCalled()
    // Quiet/cancelled terminal observations may legitimately return the old watermark unchanged.
    await act(async () => pending[1]({ v: 1, id: "", ok: true, detail }))
    expect(onRead).toHaveBeenCalledWith(detail, 12)
  })

  it("reads once after render and applies only the returned authoritative detail", async () => {
    const rpc = vi.fn().mockResolvedValue({ ok: true, detail: { ...detail, unread: false } })
    const target = { rpc } as unknown as RemoteLink
    const onRead = vi.fn(), onError = vi.fn()
    const refs = { link: { current: target }, view: { current: view() } }
    const hook = renderHook(() => useCompletionRead({ ...refs, visible: true, onRead, onError }))
    act(() => hook.result.current.request(target, "t", 12))
    await waitFor(() => expect(onRead).toHaveBeenCalledWith({ ...detail, unread: false }, 12))
    hook.rerender()
    expect(rpc).toHaveBeenCalledTimes(1)
    expect(rpc).toHaveBeenCalledWith({ op: "read", thread_id: "t", completed_seq: 12 })
    expect(onError).not.toHaveBeenCalled()
  })

  it("does not read hidden, covered, navigated-away, unrecovered or unrendered content", async () => {
    for (const condition of ["hidden", "covered", "other-thread", "not-ready", "no-detail", "other-link", "worker-page"]) {
      const rpc = vi.fn().mockResolvedValue({ ok: true, detail }), target = { rpc } as unknown as RemoteLink
      const current = view()
      const refs = { link: { current: target }, view: { current }, contentVisible: { current: condition !== "worker-page" } }
      if (condition === "other-thread") current.threadId = "other"
      if (condition === "not-ready") current.caughtUp = false
      if (condition === "no-detail") refs.view.current = { ...current, detail: null }
      if (condition === "other-link") refs.link.current = {} as RemoteLink
      const visibility = vi.spyOn(document, "visibilityState", "get").mockReturnValue(condition === "hidden" ? "hidden" : "visible")
      const hook = renderHook(() => useCompletionRead({ ...refs, visible: condition !== "covered", onRead: vi.fn(), onError: vi.fn() }))
      await act(async () => hook.result.current.request(target, "t", 12))
      expect(rpc).not.toHaveBeenCalled()
      hook.unmount(); visibility.mockRestore()
    }
  })

  it("does not acknowledge a cursor without the matching rendered primary terminal", async () => {
    for (const events of [[], [{ ...terminal, kind: "roster", seq: 18 }], [{ ...terminal, agent_id: "worker" }],
      [{ ...terminal, role: "worker" }], [{ ...terminal, thread_id: "other" }], [{ ...terminal, seq: 11 }]]) {
      const rpc = vi.fn().mockResolvedValue({ ok: true, detail }), target = { rpc } as unknown as RemoteLink
      const hook = renderHook(() => useCompletionRead({ link: { current: target },
        view: { current: { ...view(), lastSeq: 18, events } }, visible: true, onRead: vi.fn(), onError: vi.fn() }))
      await act(async () => hook.result.current.request(target, "t", 12))
      expect(rpc).not.toHaveBeenCalled()
      hook.unmount()
    }
  })

  it("shows acknowledgement errors without a false local clear and supports an explicit retry", async () => {
    const rpc = vi.fn().mockResolvedValueOnce({ ok: false, error: "read unavailable" })
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce({ ok: true })
      .mockResolvedValueOnce({ ok: true, detail: { ...detail, unread: false } })
    const target = { rpc } as unknown as RemoteLink, onRead = vi.fn(), onError = vi.fn()
    const hook = renderHook(() => useCompletionRead({ link: { current: target }, view: { current: view() }, visible: true, onRead, onError }))
    for (let i = 1; i <= 3; i++) {
      act(() => hook.result.current.request(target, "t", 12))
      await waitFor(() => expect(onError).toHaveBeenCalledTimes(i))
      expect(onRead).not.toHaveBeenCalled()
    }
    act(() => hook.result.current.request(target, "t", 12))
    await waitFor(() => expect(onRead).toHaveBeenCalledTimes(1))
  })

  it("ignores a reply from a host that was replaced while the read was in flight", async () => {
    let resolve!: (response: RemoteResponse) => void
    const rpc = vi.fn(() => new Promise<RemoteResponse>((done) => { resolve = done }))
    const target = { rpc } as unknown as RemoteLink, onRead = vi.fn()
    const link = { current: target }
    const hook = renderHook(() => useCompletionRead({ link, view: { current: view() }, visible: true, onRead, onError: vi.fn() }))
    act(() => hook.result.current.request(target, "t", 12))
    await waitFor(() => expect(rpc).toHaveBeenCalledTimes(1))
    link.current = {} as RemoteLink
    await act(async () => resolve({ v: 1, id: "", ok: true, detail: { ...detail, unread: false } }))
    expect(onRead).not.toHaveBeenCalled()
  })

  it("reads visible new completion pushes but not hidden pushes or zero watermarks", async () => {
    const rpc = vi.fn().mockResolvedValue({ ok: true, detail }), target = { rpc } as unknown as RemoteLink
    let visible = false
    const currentView = { current: view() }
    const hook = renderHook(() => useCompletionRead({ link: { current: target }, view: currentView, visible, onRead: vi.fn(), onError: vi.fn() }))
    await act(async () => hook.result.current.observe(target, event()))
    expect(rpc).not.toHaveBeenCalled()
    visible = true; hook.rerender()
    await act(async () => hook.result.current.request(target, "t", 0))
    expect(rpc).not.toHaveBeenCalled()
    act(() => { hook.result.current.observe(target, event()); currentView.current = applyPush(currentView.current, event()) as ReturnType<typeof view> })
    await waitFor(() => expect(rpc).toHaveBeenCalledWith({ op: "read", thread_id: "t", completed_seq: 13 }))
  })
})
