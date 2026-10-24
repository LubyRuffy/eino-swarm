import { describe, expect, it, vi } from "vitest"
import type { RemoteLink } from "./link"
import { OpCancelWait, OpResumeGoal, OpRunNow, type RemoteResponse } from "./rpc"
import { emptyView, type PhoneView } from "./session"
import { runPhoneAction } from "./phone-action"

function fixture(rpc = vi.fn(async (): Promise<RemoteResponse> => ({ v: 1, id: "r", ok: true }))) {
  const target: RemoteLink = { rpc, alive: () => true, path: "relay", close: vi.fn(), announceDevice: vi.fn() }
  let view: PhoneView = { ...emptyView(), threadId: "t1", detail: { id: "t1", title: "A task" } }
  return { rpc, target, deps: { link: () => target, view: () => view, commit: vi.fn((next) => { view = next }), fail: vi.fn(), setError: vi.fn() } }
}

describe("phone lifecycle acknowledgements", () => {
  it.each([OpRunNow, OpResumeGoal, OpCancelWait])("retains idle state after a restoring %s refusal", async (op) => {
    const f = fixture(vi.fn(async () => ({ v: 1, id: "r", ok: false, code: "restoring", error: "restoring" })))
    await runPhoneAction(f.deps, op, "t1")
    expect(f.deps.commit).not.toHaveBeenCalled()
    expect(f.deps.setError).toHaveBeenCalledOnce()
    expect(f.rpc).toHaveBeenCalledOnce()
  })
  it.each([OpRunNow, OpResumeGoal])("paints accepted %s work only on its original thread", async (op) => {
    const f = fixture()
    await runPhoneAction(f.deps, op, "t1")
    expect(f.deps.view().detail?.running).toBeTruthy()
  })
  it("refreshes the acknowledged cancelled wait", async () => {
    const f = fixture(vi.fn().mockResolvedValueOnce({ ok: true }).mockResolvedValueOnce({ ok: true, detail: { id: "t1", title: "After cancel" } }))
    await runPhoneAction(f.deps, OpCancelWait, "t1")
    expect(f.deps.view().detail?.title).toBe("After cancel")
    expect(f.rpc).toHaveBeenCalledTimes(2)
  })
  it("does not apply a late accepted action to another thread", async () => {
    const f = fixture()
    await runPhoneAction(f.deps, OpRunNow, "other-thread")
    expect(f.deps.commit).not.toHaveBeenCalled()
  })
  it("reports a transport failure without replaying the action", async () => {
    const f = fixture(vi.fn(async () => { throw new Error("dropped") }))
    await runPhoneAction(f.deps, OpRunNow, "t1")
    expect(f.deps.fail).toHaveBeenCalledOnce()
    expect(f.deps.commit).not.toHaveBeenCalled()
    expect(f.rpc).toHaveBeenCalledOnce()
  })
  it("does not send through a disconnected PC", async () => {
    const f = fixture()
    f.target.alive = () => false
    await runPhoneAction(f.deps, OpRunNow, "t1")
    expect(f.rpc).not.toHaveBeenCalled()
  })
})
