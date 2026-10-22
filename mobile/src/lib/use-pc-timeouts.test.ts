import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { usePCTimeouts } from "./use-pc-timeouts"
import { setLocale } from "./i18n"
import type { RemoteLink } from "./link"
import type { RemoteResponse, TimeoutSettings } from "./rpc"

const settings: TimeoutSettings = { agent_timeout_seconds: 600, providers: [{ id: "p", label: "Primary", timeout_seconds: 300, first_byte_timeout_seconds: 30, effective_first_byte_timeout_seconds: 30 }] }
const ok = (value = settings): RemoteResponse => ({ v: 1, id: "r", ok: true, timeout_settings: value })
function link(rpc = vi.fn(async () => ok())): RemoteLink { return { rpc, path: "relay", alive: () => true, close: vi.fn(), announceDevice: vi.fn() } }
function deferred() { let resolve!: (r: RemoteResponse) => void; let reject!: (e: Error) => void; const promise = new Promise<RemoteResponse>((yes, no) => { resolve = yes; reject = no }); return { promise, resolve, reject } }
beforeEach(() => setLocale("en"))

describe("bound PC timeout ownership", () => {
  it("loads real values and saves only edited fields, never labels or effective values", async () => {
    const saved = { ...settings, agent_timeout_seconds: 3600, providers: [{ ...settings.providers[0], timeout_seconds: 20, effective_first_byte_timeout_seconds: 30 }] }
    const rpc = vi.fn().mockResolvedValueOnce(ok()).mockResolvedValueOnce(ok(saved))
    const target = link(rpc)
    const { result } = renderHook(() => usePCTimeouts(target))
    await waitFor(() => expect(result.current.draft?.agent).toBe("600"))
    act(() => { result.current.editAgent("3600"); result.current.editProvider("p", "idle", "20") })
    await act(() => result.current.save())
    expect(rpc.mock.calls).toEqual([
      [{ op: "get_timeouts" }, { dropOnTimeout: false }],
      [{ op: "set_timeouts", timeout_settings: { agent_timeout_seconds: 3600, providers: [{ id: "p", timeout_seconds: 20 }] } }, { dropOnTimeout: false }],
    ])
    expect(result.current.settings).toEqual(saved)
    expect(result.current.changed).toBe(false)
    expect(result.current.saved).toBe(true)
  })

  it.each(["0", "-1", "1.5", "", "Infinity", "9223372037", "9999999999999999999999999"])("refuses invalid whole seconds %s without sending", async (invalid) => {
    const target = link(); const { result } = renderHook(() => usePCTimeouts(target))
    await waitFor(() => expect(result.current.draft).not.toBeNull())
    act(() => result.current.editAgent(invalid))
    await act(() => result.current.save())
    expect(result.current.invalid).toBe(true)
    expect(target.rpc).toHaveBeenCalledTimes(1)
  })

  it("keeps failed-save edits and accepts an explicit retry without exposing host errors", async () => {
    const rpc = vi.fn().mockResolvedValueOnce(ok()).mockResolvedValueOnce({ v: 1, id: "r", ok: false, code: "save_failed", error: "private path/key" }).mockResolvedValueOnce(ok({ ...settings, agent_timeout_seconds: 3600 }))
    const target = link(rpc); const { result } = renderHook(() => usePCTimeouts(target))
    await waitFor(() => expect(result.current.draft).not.toBeNull())
    act(() => result.current.editAgent("3600")); await act(() => result.current.save())
    expect(result.current.error).toBe("Could not save PC timeouts. Try again.")
    expect(result.current.draft?.agent).toBe("3600")
    expect(result.current.saved).toBe(false)
    await act(() => result.current.save())
    expect(result.current.saved).toBe(true)
    expect(result.current.error).toBe("")
  })

  it("never substitutes defaults after a failed or unsupported load", async () => {
    const rpc = vi.fn().mockRejectedValueOnce(new Error("private URL")).mockResolvedValueOnce({ v: 1, id: "r", ok: false, code: "unknown_op" })
    const target = link(rpc); const { result } = renderHook(() => usePCTimeouts(target))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.draft).toBeNull(); expect(result.current.error).toBe("Could not load PC timeouts. Try again.")
    await act(() => result.current.reload())
    expect(result.current.unsupported).toBe(true); expect(result.current.draft).toBeNull()
    await act(() => result.current.save()); expect(rpc).toHaveBeenCalledTimes(2)
  })

  it("loads an empty provider list and can still change only worker lifetime", async () => {
    const target = link(vi.fn(async () => ok({ ...settings, providers: [] })))
    const { result } = renderHook(() => usePCTimeouts(target))
    await waitFor(() => expect(result.current.draft?.providers).toEqual([]))
    expect(result.current.draft?.agent).toBe("600")
  })

  it.each(["load-success", "load-error", "save-success", "save-error"])("discards stale %s and finally after a new link to the same PC", async (kind) => {
    const old = deferred(), next = deferred()
    const firstRPC = kind.startsWith("load") ? vi.fn().mockReturnValueOnce(old.promise) : vi.fn().mockResolvedValueOnce(ok()).mockReturnValueOnce(old.promise)
    const first = link(firstRPC), second = link(vi.fn().mockReturnValueOnce(next.promise))
    const { result, rerender } = renderHook(({ target }) => usePCTimeouts(target), { initialProps: { target: first } })
    let job: Promise<void> | undefined
    if (kind.startsWith("save")) {
      await waitFor(() => expect(result.current.draft).not.toBeNull())
      act(() => result.current.editAgent("900"))
      act(() => { job = result.current.save() })
    }
    rerender({ target: second })
    expect(result.current.loading).toBe(true)
    await act(async () => { if (kind.endsWith("error")) old.reject(new Error("stale")); else old.resolve(ok()); await job })
    expect(result.current.draft).toBeNull(); expect(result.current.loading).toBe(true); expect(result.current.error).toBe("")
    await act(async () => { next.resolve(ok({ ...settings, agent_timeout_seconds: 7200 })); await next.promise })
    expect(result.current.draft?.agent).toBe("7200"); expect(result.current.loading).toBe(false)
  })

  it("does not duplicate saves while pending, and classifies bad replies locally", async () => {
    const pending = deferred(); const rpc = vi.fn().mockResolvedValueOnce(ok()).mockReturnValueOnce(pending.promise)
    const target = link(rpc); const { result } = renderHook(() => usePCTimeouts(target))
    await waitFor(() => expect(result.current.draft).not.toBeNull())
    act(() => result.current.editProvider("p", "first", "45"))
    let job!: Promise<void>; act(() => { job = result.current.save() }); await act(() => result.current.save())
    expect(rpc).toHaveBeenCalledTimes(2)
    await act(async () => { pending.resolve({ v: 1, id: "r", ok: true }); await job })
    expect(result.current.error).toBe("Could not confirm the save. Reopen to check the PC’s saved values.")
    expect(result.current.draft?.providers[0].first).toBe("45")
  })
})
