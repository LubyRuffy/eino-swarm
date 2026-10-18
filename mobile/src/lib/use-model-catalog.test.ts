import { act, renderHook, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { setLocale } from "./i18n"
import type { RemoteLink } from "./link"
import { OpCatalog, OpRefreshCatalog, type ModelChoice, type RemoteResponse } from "./rpc"
import { useModelCatalog } from "./use-model-catalog"

const old = [{ provider_id: "p", model: "old-model", default: true }]
const fresh = [{ provider_id: "p", model: "new-model", default: true }]
const ok = (models: ModelChoice[]): RemoteResponse => ({ v: 1, id: "r", ok: true, models, reasoning_levels: ["low"] })
function deferred() {
  let resolve!: (v: RemoteResponse) => void
  let reject!: (e: Error) => void
  const promise = new Promise<RemoteResponse>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
function link(rpc = vi.fn(async () => ok(old))): RemoteLink {
  return { rpc, path: "direct", alive: () => true, close: vi.fn(), announceDevice: vi.fn() }
}

describe("model catalog ownership", () => {
  it("boots cached then explicitly replaces names with a nondropping bounded refresh", async () => {
    const rpc = vi.fn(async (r) => ok(r.op === OpCatalog ? old : fresh))
    const target = link(rpc)
    const { result } = renderHook(() => useModelCatalog(target))
    await waitFor(() => expect(result.current.models).toEqual(old))
    await act(() => result.current.refreshModels())
    expect(result.current.models).toEqual(fresh)
    expect(result.current.levels).toEqual(["low"])
    expect(rpc.mock.calls[1]).toEqual([{ op: OpRefreshCatalog }, { dropOnTimeout: false, timeoutMs: 30_000 }])
    expect(result.current.refreshError).toBe("")
  })

  it("keeps the old list on unsupported/failure and permits a successful retry", async () => {
    setLocale("en")
    const rpc = vi.fn().mockResolvedValueOnce(ok(old))
      .mockResolvedValueOnce({ v: 1, id: "r", ok: false, code: "unknown_op", error: "private detail" })
      .mockRejectedValueOnce(new Error("private transport detail"))
      .mockResolvedValueOnce(ok(fresh))
    const target = link(rpc)
    const { result } = renderHook(() => useModelCatalog(target))
    await waitFor(() => expect(result.current.models).toEqual(old))
    await act(() => result.current.refreshModels())
    expect(result.current.models).toEqual(old)
    expect(result.current.refreshError).toBe("Update the PC app to refresh its models.")
    await act(() => result.current.refreshModels())
    expect(result.current.refreshError).toBe("Could not refresh models. Try again.")
    expect(result.current.models).toEqual(old)
    await act(() => result.current.refreshModels())
    expect(result.current.models).toEqual(fresh)
    expect(result.current.refreshError).toBe("")
  })

  it("does not duplicate an in-flight request or let old boot catalog overwrite refresh", async () => {
    const boot = deferred(), refresh = deferred()
    const rpc = vi.fn().mockReturnValueOnce(boot.promise).mockReturnValueOnce(refresh.promise)
    const target = link(rpc)
    const { result } = renderHook(() => useModelCatalog(target))
    let pending!: Promise<void>
    act(() => { pending = result.current.refreshModels() })
    await act(() => result.current.refreshModels())
    expect(rpc).toHaveBeenCalledTimes(2)
    expect(result.current.refreshBusy).toBe(true)
    await act(async () => { refresh.resolve(ok(fresh)); await pending })
    await act(async () => { boot.resolve(ok(old)); await boot.promise })
    expect(result.current.models).toEqual(fresh)
    expect(result.current.catalogBusy).toBe(false)
    expect(result.current.refreshBusy).toBe(false)
  })

  it.each(["success", "failure", "rejection"])("ignores old-link %s and finally after reconnect, including a new in-flight job", async (kind) => {
    const stale = deferred(), nextRefresh = deferred()
    const first = link(vi.fn().mockResolvedValueOnce(ok(old)).mockReturnValueOnce(stale.promise))
    const second = link(vi.fn().mockResolvedValueOnce(ok(fresh)).mockReturnValueOnce(nextRefresh.promise))
    const { result, rerender } = renderHook(({ target }) => useModelCatalog(target), { initialProps: { target: first } })
    await waitFor(() => expect(result.current.models).toEqual(old))
    let firstJob!: Promise<void>, secondJob!: Promise<void>
    act(() => { firstJob = result.current.refreshModels() })
    rerender({ target: second })
    await waitFor(() => expect(result.current.models).toEqual(fresh))
    act(() => { secondJob = result.current.refreshModels() })
    await act(async () => {
      if (kind === "rejection") stale.reject(new Error("old link"))
      else stale.resolve(kind === "success" ? ok(old) : { v: 1, id: "old", ok: false, code: "unknown_op" })
      await firstJob
    })
    expect(result.current.models).toEqual(fresh)
    expect(result.current.refreshError).toBe("")
    expect(result.current.refreshBusy).toBe(true)
    await act(async () => { nextRefresh.resolve(ok([])); await secondJob })
    expect(result.current.models).toEqual([])
    expect(result.current.refreshBusy).toBe(false)
  })

  it("can refresh after an initial optional catalog failure and displays a successful empty catalog", async () => {
    const rpc = vi.fn().mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce(ok([]))
    const target = link(rpc)
    const { result, unmount } = renderHook(() => useModelCatalog(target))
    await waitFor(() => expect(result.current.catalogBusy).toBe(false))
    await act(() => result.current.refreshModels())
    expect(result.current.models).toEqual([])
    expect(result.current.refreshError).toBe("")
    unmount()
  })
})
