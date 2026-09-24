import { describe, expect, it, vi } from "vitest"

import type { RemoteLink } from "./link"
import { interruptPhoneFollowup } from "./phone-turn"
import { OpFollowupSteer, OpPreempt, type RemoteRequest, type RemoteResponse } from "./rpc"
import { emptyView } from "./session"

describe("interruptPhoneFollowup", () => {
  it("moves the chosen waiting message into this turn before preempting and keeps the rest queued", async () => {
    const calls: RemoteRequest[] = []
    const rpc = vi.fn(async (req: RemoteRequest): Promise<RemoteResponse> => {
      calls.push(req)
      return req.op === OpFollowupSteer
        ? { v: 1, id: "1", ok: true, followups: [{ id: "second", seq: 2, text: "later" }] }
        : { v: 1, id: "2", ok: true }
    })
    let view = {
      ...emptyView(),
      followups: [
        { id: "first", seq: 1, text: "now" },
        { id: "second", seq: 2, text: "later" },
      ],
    }
    const setError = vi.fn()

    await interruptPhoneFollowup({
      link: () => ({ alive: () => true, rpc }) as unknown as RemoteLink,
      view: () => view,
      commit: (next) => { view = next },
      fail: vi.fn(),
      setError,
      setPending: vi.fn(),
    }, "thread", "first")

    expect(calls.map((call) => call.op)).toEqual([OpFollowupSteer, OpPreempt])
    expect(calls[0].followup_id).toBe("first")
    expect(view.followups.map((item) => item.id)).toEqual(["second"])
    expect(setError).not.toHaveBeenCalled()
  })

  it("does not preempt when the waiting message could not be promoted", async () => {
    const rpc = vi.fn(async (): Promise<RemoteResponse> => ({
      v: 1, id: "1", ok: false, code: "not_found", error: "missing follow-up",
    }))
    const setError = vi.fn()
    await interruptPhoneFollowup({
      link: () => ({ alive: () => true, rpc }) as unknown as RemoteLink,
      view: emptyView,
      commit: vi.fn(),
      fail: vi.fn(),
      setError,
      setPending: vi.fn(),
    }, "thread", "missing")
    expect(rpc).toHaveBeenCalledTimes(1)
    expect(setError).toHaveBeenCalledOnce()
  })

  it("does not show a false error when the manager has already consumed the promoted steer", async () => {
    const rpc = vi.fn()
      .mockResolvedValueOnce({ v: 1, id: "1", ok: true, followups: [] })
      .mockResolvedValueOnce({
        v: 1, id: "2", ok: false, error: "engine: there is no unread steering to inject",
      })
    const setError = vi.fn()
    await interruptPhoneFollowup({
      link: () => ({ alive: () => true, rpc }) as unknown as RemoteLink,
      view: emptyView,
      commit: vi.fn(),
      fail: vi.fn(),
      setError,
      setPending: vi.fn(),
    }, "thread", "first")
    expect(rpc).toHaveBeenCalledTimes(2)
    expect(setError).not.toHaveBeenCalled()
  })

  it("shows a genuine preempt rejection after the message was promoted", async () => {
    const rpc = vi.fn()
      .mockResolvedValueOnce({ v: 1, id: "1", ok: true, followups: [] })
      .mockResolvedValueOnce({ v: 1, id: "2", ok: false, error: "preempt failed" })
    const setError = vi.fn()
    await interruptPhoneFollowup({
      link: () => ({ alive: () => true, rpc }) as unknown as RemoteLink,
      view: emptyView,
      commit: vi.fn(),
      fail: vi.fn(),
      setError,
      setPending: vi.fn(),
    }, "thread", "first")
    expect(setError).toHaveBeenCalledWith(expect.stringContaining("preempt failed"))
  })

  it("surfaces a lost connection between promotion and preemption", async () => {
    const rpc = vi.fn()
      .mockResolvedValueOnce({ v: 1, id: "1", ok: true, followups: [] })
      .mockRejectedValueOnce(new Error("offline"))
    const fail = vi.fn()
    await interruptPhoneFollowup({
      link: () => ({ alive: () => true, rpc }) as unknown as RemoteLink,
      view: emptyView,
      commit: vi.fn(),
      fail,
      setError: vi.fn(),
      setPending: vi.fn(),
    }, "thread", "first")
    expect(fail).toHaveBeenCalledWith(expect.objectContaining({ message: "offline" }))
  })

  it("leaves the queued message untouched while the phone is offline", async () => {
    const commit = vi.fn()
    const setError = vi.fn()
    await interruptPhoneFollowup({
      link: () => null,
      view: emptyView,
      commit,
      fail: vi.fn(),
      setError,
      setPending: vi.fn(),
    }, "thread", "first")
    expect(commit).not.toHaveBeenCalled()
    expect(setError).toHaveBeenCalledOnce()
  })
})
