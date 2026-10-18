import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { useCompletionRead } from "./completion-read"
import { remoteError } from "./client"
import type { RemoteLink } from "./link"
import type { RemoteResponse, ThreadDetail } from "./rpc"
import { applyPush, openView, type PhoneView } from "./session"

afterEach(cleanup)

const legacyDetail: ThreadDetail = { id: "task", title: "Scheduled check", waiting: true }
const modernDetail: ThreadDetail = { ...legacyDetail, completed_seq: 0, unread: false }
const view = (detail: ThreadDetail): PhoneView => ({ ...openView(detail), caughtUp: true, lastSeq: 12 })
const terminal = (seq: number, kind = "done"): RemoteResponse => ({
  v: 1, id: "", ok: true, op: "event", thread_id: "task",
  event: { thread_id: "task", seq, kind, agent_id: "manager", text: "", created_at: "" },
  status: { running: false, waiting: true, wake: { id: "next-check" } },
})

describe("completion reads across host versions", () => {
  it.each(["done", "error"])("keeps repeated legacy %s checks parked without unsupported reads or link errors", async (kind) => {
    const rpc = vi.fn().mockResolvedValue({ ok: false, code: "unknown_op", error: "unknown op" })
    const target = { rpc } as unknown as RemoteLink
    const current = { current: view(legacyDetail) }, onRead = vi.fn(), onError = vi.fn()
    const hook = renderHook(() => useCompletionRead({ link: { current: target }, view: current,
      visible: true, onRead, onError }))
    for (const seq of [13, 14, 15]) {
      await act(async () => {
        hook.result.current.observe(target, terminal(seq, kind))
        current.current = applyPush(current.current, terminal(seq, kind))
      })
    }
    expect(current.current.detail).toMatchObject({ waiting: true, wake: { id: "next-check" } })
    expect(current.current.detail).not.toHaveProperty("completed_seq")
    expect(rpc).not.toHaveBeenCalled()
    expect(onError).not.toHaveBeenCalled()
    expect(onRead).not.toHaveBeenCalled()
  })

  it("acknowledges the first visible completion when a modern host starts at zero", async () => {
    const reply = { ...modernDetail, completed_seq: 13, unread: false }
    const rpc = vi.fn().mockResolvedValue({ ok: true, detail: reply })
    const target = { rpc } as unknown as RemoteLink
    const current = { current: view(modernDetail) }, onRead = vi.fn(), onError = vi.fn()
    const hook = renderHook(() => useCompletionRead({ link: { current: target }, view: current,
      visible: true, onRead, onError }))
    await act(async () => {
      hook.result.current.observe(target, terminal(13))
      current.current = applyPush(current.current, terminal(13))
    })
    await waitFor(() => expect(onRead).toHaveBeenCalledWith(reply, 0))
    expect(rpc).toHaveBeenCalledTimes(1)
    expect(rpc).toHaveBeenCalledWith({ op: "read", thread_id: "task", completed_seq: 13 })
    expect(onError).not.toHaveBeenCalled()
  })

  it("does not mistake a listing detail's explicit undefined field for completion support", async () => {
    const target = { rpc: vi.fn() } as unknown as RemoteLink
    const current = { current: view({ ...legacyDetail, completed_seq: undefined }) }
    const onRead = vi.fn(), onError = vi.fn()
    const hook = renderHook(() => useCompletionRead({ link: { current: target }, view: current,
      visible: true, onRead, onError }))
    await act(async () => {
      hook.result.current.observe(target, terminal(13))
      current.current = applyPush(current.current, terminal(13))
    })
    expect(target.rpc).not.toHaveBeenCalled()
    expect(onRead).not.toHaveBeenCalled()
    expect(onError).not.toHaveBeenCalled()
  })

  it("takes completion support from the selected host rather than carrying it across host switches", async () => {
    const legacyRPC = vi.fn().mockResolvedValue({ ok: false, code: "unknown_op", error: "unknown op" })
    const legacy = { rpc: legacyRPC } as unknown as RemoteLink
    const reply = { ...modernDetail, completed_seq: 13, unread: false }
    const modernRPC = vi.fn().mockResolvedValue({ ok: true, detail: reply })
    const modern = { rpc: modernRPC } as unknown as RemoteLink
    const link = { current: legacy }, current = { current: view(legacyDetail) }, onRead = vi.fn(), onError = vi.fn()
    const hook = renderHook(() => useCompletionRead({ link, view: current, visible: true, onRead, onError }))
    const push = async () => act(async () => {
      hook.result.current.observe(link.current, terminal(13))
      current.current = applyPush(current.current, terminal(13))
    })
    await push()
    link.current = modern; current.current = view(modernDetail); hook.rerender()
    await push()
    await waitFor(() => expect(onRead).toHaveBeenCalledWith(reply, 0))
    link.current = legacy; current.current = view(legacyDetail); hook.rerender()
    await push()
    expect(legacyRPC).not.toHaveBeenCalled()
    expect(modernRPC).toHaveBeenCalledTimes(1)
    expect(onError).not.toHaveBeenCalled()
  })

  // An advertised completion protocol that refuses reads is an actual failure,
  // not evidence that the legacy wire shape should be silently acknowledged.
  it.each(["read unavailable", "unknown op"])("keeps a supported host's %s refusal visible without clearing the result", async (error) => {
    const rpc = vi.fn().mockResolvedValue({ ok: false, error, code: error === "unknown op" ? "unknown_op" : "" })
    const target = { rpc } as unknown as RemoteLink
    const current = { current: view({ ...modernDetail, completed_seq: 12, unread: true }) }
    current.current.events = [{ thread_id: "task", seq: 12, kind: "done", text: "", created_at: "" }]
    const onRead = vi.fn(), onError = vi.fn()
    const hook = renderHook(() => useCompletionRead({ link: { current: target }, view: current,
      visible: true, onRead, onError }))
    await act(async () => hook.result.current.request(target, "task", 12))
    await waitFor(() => expect(onError).toHaveBeenCalledWith(remoteError(error)))
    expect(onRead).not.toHaveBeenCalled()
    expect(current.current.detail).toMatchObject({ completed_seq: 12, unread: true })
  })
})
