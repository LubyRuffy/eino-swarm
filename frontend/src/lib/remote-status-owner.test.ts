import { afterEach, describe, expect, it, vi } from "vitest"

import { RemoteStatusOwner, remoteAuthorityKey } from "./remote-status-owner"
import type { RemoteStatus } from "./types"

const signedOut: RemoteStatus = {
  enabled: true, hub_url: "https://hub.example", auth_mode: "account",
  online: false, has_token: false, login_available: true, login: { state: "signed_out" },
}
const authorized: RemoteStatus = {
  ...signedOut, online: true, has_token: true, login: { state: "authorized", username: "alice" },
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
async function settle() { await Promise.resolve(); await Promise.resolve() }
afterEach(() => vi.useRealTimers())

describe("shared desktop remote status owner", () => {
  it("fences an initial unknown-scope read when Phone first declares a different authority", async () => {
    const old = deferred<RemoteStatus>()
    const next = { ...signedOut, hub_url: "https://next.example" }
    const read = vi.fn().mockReturnValueOnce(old.promise).mockResolvedValue(next)
    const owner = new RemoteStatusOwner(read)
    const leave = owner.subscribe(vi.fn())
    await settle()
    owner.configurationChanged(remoteAuthorityKey(next))
    old.resolve(authorized)
    await settle()
    expect(owner.getSnapshot().status?.hub_url).toBe(next.hub_url)
    expect(owner.getSnapshot().status?.login?.state).toBe("signed_out")
    leave()
  })

  it("rejects an old-authority receipt read after a new Phone declaration", async () => {
    const stale = deferred<RemoteStatus>()
    const next = { ...signedOut, hub_url: "https://next.example" }
    const read = vi.fn().mockResolvedValueOnce(authorized).mockReturnValueOnce(stale.promise).mockResolvedValue(next)
    const owner = new RemoteStatusOwner(read)
    const leave = owner.subscribe(vi.fn())
    await settle()
    owner.configurationChanged(remoteAuthorityKey(next))
    // The newly issued GET took its snapshot before the settings PUT committed.
    stale.resolve(authorized)
    await settle()
    expect(owner.getSnapshot().status).toBeUndefined()
    await owner.refresh()
    expect(owner.getSnapshot().status).toEqual(next)
    leave()
  })

  it("accepts an explicit account-enable mutation as the new authority", async () => {
    const anonymous = { ...signedOut, enabled: false, auth_mode: "independent" as const, login: undefined }
    const pending = { ...signedOut, login: { state: "authorization_pending" as const } }
    const owner = new RemoteStatusOwner(async () => anonymous)
    const leave = owner.subscribe(vi.fn())
    await settle()
    await owner.perform(async () => pending)
    expect(owner.getSnapshot().status).toEqual(pending)
    // A stale status response must not undo the successful explicit mutation.
    await owner.refresh()
    expect(owner.getSnapshot().status).toEqual(pending)
    leave()
  })

  it("resumes observing external configuration changes after a declared scope is confirmed", async () => {
    let current = authorized
    const owner = new RemoteStatusOwner(async () => current)
    const leave = owner.subscribe(vi.fn())
    await settle()
    current = { ...signedOut, hub_url: "https://next.example" }
    owner.configurationChanged(remoteAuthorityKey(current))
    await settle()
    expect(owner.getSnapshot().status).toEqual(current)
    current = { ...signedOut, hub_url: "https://external.example" }
    await owner.refresh()
    expect(owner.getSnapshot().status).toEqual(current)
    leave()
  })

  it("shares one poll and never notifies unchanged status", async () => {
    vi.useFakeTimers()
    const read = vi.fn(async () => signedOut)
    const owner = new RemoteStatusOwner(read)
    const first = vi.fn(), second = vi.fn()
    const leaveFirst = owner.subscribe(first), leaveSecond = owner.subscribe(second)
    await settle()
    expect(read).toHaveBeenCalledTimes(1)
    first.mockClear(); second.mockClear()
    await vi.advanceTimersByTimeAsync(1000)
    expect(read).toHaveBeenCalledTimes(2)
    expect(first).not.toHaveBeenCalled()
    expect(second).not.toHaveBeenCalled()
    leaveFirst()
    await vi.advanceTimersByTimeAsync(1000)
    expect(read).toHaveBeenCalledTimes(3)
    leaveSecond()
    await vi.advanceTimersByTimeAsync(2000)
    expect(read).toHaveBeenCalledTimes(3)
  })

  it("fences old GETs and serializes start, cancel and logout across subscribers", async () => {
    const old = deferred<RemoteStatus>(), login = deferred<RemoteStatus>()
    const read = vi.fn().mockReturnValue(old.promise)
    const owner = new RemoteStatusOwner(read)
    const leaveA = owner.subscribe(vi.fn()), leaveB = owner.subscribe(vi.fn())
    const start = vi.fn(() => login.promise), cancel = vi.fn(async () => signedOut)
    const action = owner.perform(start)
    await owner.perform(cancel)
    expect(cancel).not.toHaveBeenCalled()
    old.resolve(authorized); await settle()
    expect(owner.getSnapshot().status).toBeUndefined()
    login.resolve({ ...signedOut, login: { state: "authorization_pending" } })
    await action
    expect(owner.getSnapshot().status?.login?.state).toBe("authorization_pending")
    expect(owner.getSnapshot().busy).toBe(false)
    leaveA(); leaveB()
  })

  it("invalidates a late action when the configuration changes and reads the new scope", async () => {
    const login = deferred<RemoteStatus>()
    let current = signedOut
    const owner = new RemoteStatusOwner(async () => current)
    const leave = owner.subscribe(vi.fn())
    await settle()
    const action = owner.perform(() => login.promise)
    current = { ...signedOut, hub_url: "https://next.example" }
    owner.configurationChanged(remoteAuthorityKey(current))
    login.resolve(authorized)
    await action; await settle()
    expect(owner.getSnapshot().status?.hub_url).toBe(current.hub_url)
    expect(owner.getSnapshot().status?.login?.state).toBe("signed_out")
    leave()
  })

  it("marks failed reads unknown, recovers, and does not revive state after final unmount", async () => {
    const late = deferred<RemoteStatus>()
    const read = vi.fn().mockResolvedValueOnce(authorized).mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce(signedOut).mockReturnValue(late.promise)
    const owner = new RemoteStatusOwner(read)
    const leave = owner.subscribe(vi.fn())
    await settle(); await owner.refresh()
    expect(owner.getSnapshot().unavailable).toBe(true)
    await owner.refresh()
    expect(owner.getSnapshot()).toMatchObject({ status: signedOut, unavailable: false })
    const pending = owner.refresh()
    leave()
    late.resolve(authorized); await pending
    expect(owner.getSnapshot().status).toBeUndefined()
  })

  it("keeps the known account when logout fails and releases the mutation fence", async () => {
    const owner = new RemoteStatusOwner(async () => authorized)
    const leave = owner.subscribe(vi.fn())
    await settle()
    await expect(owner.perform(async () => { throw new Error("failed") })).rejects.toThrow("failed")
    expect(owner.getSnapshot()).toMatchObject({ status: authorized, busy: false })
    await owner.perform(async () => signedOut)
    expect(owner.getSnapshot().status).toEqual(signedOut)
    leave()
  })

  it("keeps a pending mutation fenced across remount and rejects its old-session reply", async () => {
    const login = deferred<RemoteStatus>()
    const owner = new RemoteStatusOwner(async () => signedOut)
    const leave = owner.subscribe(vi.fn())
    await settle()
    const oldAction = owner.perform(() => login.promise)
    leave()
    const leaveAgain = owner.subscribe(vi.fn())
    const logout = vi.fn(async () => signedOut)
    await owner.perform(logout)
    expect(logout).not.toHaveBeenCalled()
    expect(owner.getSnapshot().busy).toBe(true)
    login.resolve(authorized)
    await oldAction; await settle()
    expect(owner.getSnapshot()).toMatchObject({ busy: false, status: signedOut })
    leaveAgain()
  })

  it("pauses the shared watch while hidden and recovers on the next visible tick", async () => {
    vi.useFakeTimers()
    const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(true)
    const read = vi.fn(async () => signedOut)
    const owner = new RemoteStatusOwner(read)
    const leave = owner.subscribe(vi.fn())
    await settle()
    await vi.advanceTimersByTimeAsync(3000)
    expect(read).toHaveBeenCalledTimes(1)
    hidden.mockReturnValue(false)
    await vi.advanceTimersByTimeAsync(1000)
    expect(read).toHaveBeenCalledTimes(2)
    leave()
    hidden.mockRestore()
  })
})
