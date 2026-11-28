import { act, renderHook, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { getPendingHostUpdate } from "./host-update-storage"
import type { RemoteLink } from "./link"
import { OpHostUpdateCheck, OpHostUpdateStart, OpHostUpdateStatus, type EngineStartup, type RemoteResponse } from "./rpc"
import type { SavedLink } from "./store"
import { useHostUpdate } from "./use-host-update"

const ready: EngineStartup = { startup: "ready", host_update_supported: true, host_version: "1.2.3" }
const paired: SavedLink = { fingerprint: "same-fingerprint", hostPub: "public-key", sessionID: "session", ticket: "private-ticket", hubURL: "https://hub.example.test/team" }
const acceptsLink = () => true

function connected(rpc: RemoteLink["rpc"]): RemoteLink {
  return { path: "relay", alive: () => true, rpc, close: vi.fn(), announceDevice: vi.fn() }
}

function reply(part: Partial<RemoteResponse>): RemoteResponse {
  return { v: 1, id: "metadata", ok: true, ...part }
}

function offer(version = "1.2.4") {
  return reply({ host_update_offer: { status: "available", current: "1.2.3", offer: { version } } })
}

afterEach(() => {
  vi.useRealTimers()
  localStorage.clear()
})

describe("paired host update", () => {
  it("opens no mutation on discovery and fences offers from an earlier binding of the same PC", async () => {
    let finish!: (reply: RemoteResponse) => void
    const old = vi.fn<RemoteLink["rpc"]>(() => new Promise(resolve => { finish = resolve }))
    const latest = vi.fn<RemoteLink["rpc"]>().mockResolvedValue(offer("1.2.5"))
    const target = connected(latest)
    const { result, rerender } = renderHook(({ host, link }) => useHostUpdate(host, link, ready, acceptsLink), {
      initialProps: { host: paired, link: connected(old) },
    })
    expect(old.mock.calls[0]?.[0].op).toBe(OpHostUpdateCheck)
    rerender({ host: { ...paired, ticket: "rotated" }, link: target })
    await waitFor(() => expect(result.current.offer?.version).toBe("1.2.5"))
    await act(async () => { finish(offer()) })
    expect(result.current.offer?.version).toBe("1.2.5")
    expect(latest.mock.calls.map(([req]) => req.op)).toEqual([OpHostUpdateCheck])
  })

  it("fences a confirmation after switching PCs and admits only one click for the selected offer", async () => {
    const first = vi.fn<RemoteLink["rpc"]>().mockResolvedValue(offer())
    const next = vi.fn<RemoteLink["rpc"]>(async req => req.op === OpHostUpdateCheck ? offer("1.2.5") : new Promise(() => {}))
    const { result, rerender } = renderHook(({ host, link }) => useHostUpdate(host, link, ready, acceptsLink), {
      initialProps: { host: paired, link: connected(first) },
    })
    await waitFor(() => expect(result.current.offer).toBeDefined())
    const previous = result.current.offer!
    const confirmPrevious = result.current.confirm
    rerender({ host: { ...paired, fingerprint: "other-pc" }, link: connected(next) })
    await waitFor(() => expect(result.current.offer?.version).toBe("1.2.5"))
    act(() => { confirmPrevious(previous); result.current.confirm(previous) })
    expect(first.mock.calls.map(([req]) => req.op)).toEqual([OpHostUpdateCheck])
    expect(next.mock.calls.map(([req]) => req.op)).toEqual([OpHostUpdateCheck])
    const current = result.current.offer!
    act(() => { result.current.confirm(current); result.current.confirm(current) })
    expect(next.mock.calls.map(([req]) => req.op)).toEqual([OpHostUpdateCheck, OpHostUpdateStart])
    expect(next.mock.calls[1]?.[0]).toEqual({
      op: OpHostUpdateStart, version: "1.2.5", operation_id: expect.stringMatching(/^[0-9a-f-]{36}$/), host_update_verify: true,
    })
  })

  it("keeps an unchanged confirmation when a foreground refresh sees the same offer", async () => {
    const rpc = vi.fn<RemoteLink["rpc"]>().mockImplementation(async () => offer())
    const link = connected(rpc)
    const { result } = renderHook(() => useHostUpdate(paired, link, ready, acceptsLink))
    await waitFor(() => expect(result.current.offer).toBeDefined())
    const candidate = result.current.offer!
    await act(async () => { document.dispatchEvent(new Event("visibilitychange")) })
    expect(rpc).toHaveBeenCalledTimes(2)
    expect(result.current.offer).toBe(candidate)
  })

  it("restores read-only status after a lost acknowledgement and requires the target version and ready", async () => {
    const original = vi.fn<RemoteLink["rpc"]>(async req => {
      if (req.op === OpHostUpdateCheck) return offer()
      if (req.op === OpHostUpdateStart) throw new Error("lost acknowledgement")
      throw new Error("disconnected")
    })
    const link = connected(original)
    const { result, unmount } = renderHook(() => useHostUpdate(paired, link, ready, acceptsLink))
    await waitFor(() => expect(result.current.offer).toBeDefined())
    const candidate = result.current.offer!
    act(() => result.current.confirm(candidate))
    await waitFor(() => expect(result.current.attemptStage).toBe("unconfirmed"))
    const operationID = result.current.attempt!.operation_id
    expect(getPendingHostUpdate(paired)).toEqual({ current: "1.2.3", version: "1.2.4", operation_id: operationID })
    expect(getPendingHostUpdate({ ...paired, hubURL: "https://hub.example.test/other" })).toBeUndefined()
    expect(getPendingHostUpdate({ ...paired, ticket: "different" })).toBeUndefined()
    expect(JSON.stringify(localStorage)).not.toContain(paired.ticket)
    unmount()

    const status = vi.fn<RemoteLink["rpc"]>(async req => req.op === OpHostUpdateStatus
      ? reply({ host_update: { operation_id: operationID, version: "1.2.4", stage: "completed" } })
      : reply({ host_update_offer: { status: "current", current: "1.2.4" } }))
    const replacement = connected(status)
    const reopened = renderHook(({ metadata }) => useHostUpdate(paired, replacement, metadata, acceptsLink), {
      initialProps: { metadata: { ...ready, host_version: "1.2.4", startup: "restoring" } as EngineStartup },
    })
    await waitFor(() => expect(reopened.result.current.attempt?.stage).toBe("completed"))
    expect(reopened.result.current.attemptStage).toBe("restoring")
    await act(async () => { reopened.rerender({ metadata: ready }) })
    expect(reopened.result.current.attemptStage).toBe("restoring")
    await act(async () => { reopened.rerender({ metadata: { ...ready, host_version: "1.2.4" } }) })
    expect(reopened.result.current.attemptStage).toBe("completed")
    expect(status.mock.calls.filter(([req]) => req.op === OpHostUpdateStatus).map(([req]) => req)).toEqual([{ op: OpHostUpdateStatus, operation_id: operationID }])
    expect(status.mock.calls.some(([req]) => req.op === OpHostUpdateStart)).toBe(false)
    act(() => reopened.result.current.dismiss())
    expect(getPendingHostUpdate(paired)).toBeUndefined()
  })

  it("echoes only the target candidate challenge from the same live pairing without starting another install", async () => {
    vi.useFakeTimers()
    const oldChallenge = "a".repeat(64)
    const candidateChallenge = "b".repeat(64)
    const old = vi.fn<RemoteLink["rpc"]>(async req => {
      if (req.op === OpHostUpdateCheck) return offer()
      return reply({
        host_version: "1.2.3",
        host_update_challenge: oldChallenge,
        host_update: { operation_id: req.operation_id!, version: "1.2.4", stage: "restarting" },
      })
    })
    const candidate = vi.fn<RemoteLink["rpc"]>(async req => reply({
      host_version: "1.2.4",
      host_update_challenge: candidateChallenge,
      host_update: {
        operation_id: req.operation_id!, version: "1.2.4",
        stage: req.host_update_challenge === candidateChallenge ? "completed" : "restoring",
      },
    }))
    const original = connected(old)
    const replacement = connected(candidate)
    const { result, rerender } = renderHook(({ link, startup }) => useHostUpdate(paired, link, startup, acceptsLink), {
      initialProps: { link: original, startup: ready },
    })
    await act(async () => {})
    await act(async () => result.current.confirm(result.current.offer!))
    const id = result.current.attempt!.operation_id
    expect(old.mock.calls.find(([req]) => req.op === OpHostUpdateStart)?.[0]).toEqual({
      op: OpHostUpdateStart, operation_id: id, version: "1.2.4", host_update_verify: true,
    })
    await act(() => vi.advanceTimersByTimeAsync(2000))
    expect(old.mock.calls.some(([req]) => req.host_update_challenge)).toBe(false)

    const restoring: EngineStartup = { ...ready, startup: "restoring", host_version: "1.2.4" }
    await act(async () => rerender({ link: replacement, startup: restoring }))
    expect(result.current.attemptStage).toBe("restoring")
    expect(candidate.mock.calls.map(([req]) => req)).toEqual([{ op: OpHostUpdateStatus, operation_id: id }])
    await act(() => vi.advanceTimersByTimeAsync(1000))
    expect(candidate.mock.calls.map(([req]) => req)).toEqual([
      { op: OpHostUpdateStatus, operation_id: id },
      { op: OpHostUpdateStatus, operation_id: id, host_update_challenge: candidateChallenge },
    ])
    expect(result.current.attemptStage).toBe("restoring")
    await act(async () => rerender({ link: replacement, startup: { ...restoring, startup: "ready" } }))
    expect(result.current.attemptStage).toBe("completed")
    expect(old.mock.calls.filter(([req]) => req.op === OpHostUpdateStart)).toHaveLength(1)
    expect(candidate.mock.calls.some(([req]) => req.op === OpHostUpdateStart)).toBe(false)
  })

  it("discards a late candidate challenge after switching authority and does not transfer it onto a fresh link", async () => {
    vi.useFakeTimers()
    let finish!: (reply: RemoteResponse) => void
    const challenge = "c".repeat(64)
    const old = vi.fn<RemoteLink["rpc"]>(async req => {
      if (req.op === OpHostUpdateCheck) return offer()
      if (req.op === OpHostUpdateStatus) return new Promise(resolve => { finish = resolve })
      return reply({ host_update: { operation_id: req.operation_id!, version: "1.2.4", stage: "restarting" } })
    })
    const wrong = vi.fn<RemoteLink["rpc"]>(async () => offer("1.2.5"))
    const fresh = vi.fn<RemoteLink["rpc"]>(async req => reply({
      host_version: "1.2.4",
      host_update: { operation_id: req.operation_id!, version: "1.2.4", stage: "restoring" },
    }))
    const wrongOwner = { ...paired, ticket: "superseding pairing", sessionID: "other session" }
    const newer: EngineStartup = { ...ready, startup: "restoring", host_version: "1.2.4" }
    const { result, rerender } = renderHook(({ owner, link, startup }) => useHostUpdate(owner, link, startup, acceptsLink), {
      initialProps: { owner: paired, link: connected(old), startup: ready },
    })
    await act(async () => {})
    await act(async () => result.current.confirm(result.current.offer!))
    const id = result.current.attempt!.operation_id
    await act(() => vi.advanceTimersByTimeAsync(1000))
    expect(finish).toBeDefined()
    await act(async () => rerender({ owner: wrongOwner, link: connected(wrong), startup: ready }))
    await act(async () => finish(reply({
      host_version: "1.2.4", host_update_challenge: challenge,
      host_update: { operation_id: id, version: "1.2.4", stage: "restoring" },
    })))
    expect(result.current.attempt).toBeUndefined()
    await act(async () => rerender({ owner: paired, link: connected(fresh), startup: newer }))
    await act(() => vi.advanceTimersByTimeAsync(2000))
    expect(fresh.mock.calls.length).toBeGreaterThan(1)
    expect(fresh.mock.calls.every(([req]) => req.op === OpHostUpdateStatus && req.operation_id === id && !req.host_update_challenge)).toBe(true)
    expect(wrong.mock.calls.every(([req]) => req.op === OpHostUpdateCheck)).toBe(true)
    expect(old.mock.calls.filter(([req]) => req.op === OpHostUpdateStart)).toHaveLength(1)
  })

  it("lets the user hide an uncertain result without dropping readback or replaying start", async () => {
    vi.useFakeTimers()
    let reads = 0
    const rpc = vi.fn<RemoteLink["rpc"]>(async req => {
      if (req.op === OpHostUpdateCheck) return offer()
      if (req.op === OpHostUpdateStatus) reads++
      return reply({ host_update: { operation_id: req.operation_id!, version: "1.2.4", stage: "pending", error: "New process identity unconfirmed" } })
    })
    const link = connected(rpc)
    const { result } = renderHook(() => useHostUpdate(paired, link, ready, acceptsLink))
    await act(async () => {})
    await act(async () => result.current.confirm(result.current.offer!))
    expect(result.current.attempt?.stage).toBe("pending")
    act(() => result.current.dismiss())
    expect(result.current.attempt).toBeUndefined()
    const previousReads = reads
    await act(() => vi.advanceTimersByTimeAsync(2000))
    expect(reads).toBeGreaterThan(previousReads)
    expect(getPendingHostUpdate(paired)?.version).toBe("1.2.4")
    expect(result.current.attempt).toBeUndefined()
    act(() => result.current.checkNow())
    expect(result.current.attempt?.error).toBe("New process identity unconfirmed")
    expect(rpc.mock.calls.filter(([request]) => request.op === OpHostUpdateStart)).toHaveLength(1)
  })

  it("reuses the operation ID only on explicit retry after missing readback", async () => {
    vi.useFakeTimers()
    const starts: string[] = []
    const rpc = vi.fn<RemoteLink["rpc"]>(async req => {
      if (req.op === OpHostUpdateCheck) return offer()
      if (req.op === OpHostUpdateStatus) return reply({ ok: false, code: "not_found" })
      starts.push(req.operation_id!)
      if (starts.length === 1) throw new Error("lost acknowledgement")
      return reply({ host_update: { operation_id: req.operation_id!, version: "1.2.4", stage: "failed", error: "digest mismatch" } })
    })
    const link = connected(rpc)
    const { result } = renderHook(() => useHostUpdate(paired, link, ready, acceptsLink))
    await act(async () => {})
    await act(async () => result.current.confirm(result.current.offer!))
    await act(() => vi.advanceTimersByTimeAsync(2000))
    expect(result.current.attemptStage).toBe("missing")
    expect(starts).toHaveLength(1)
    await act(async () => result.current.retry())
    expect(starts).toEqual([starts[0], starts[0]])
    expect(result.current.attempt?.error).toBe("digest mismatch")
    await act(async () => result.current.retry())
    expect(starts).toHaveLength(3)
    expect(starts[2]).not.toBe(starts[0])
  })
})
