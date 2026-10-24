import { act, renderHook, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { RemoteLink } from "./link"
import type { RemoteResponse } from "./rpc"
import { useEngineStartup } from "./use-engine-startup"

function link(rpc: RemoteLink["rpc"]): RemoteLink {
  return { rpc, path: "direct", alive: () => true, close: vi.fn(), announceDevice: vi.fn() }
}
const restoring = { v: 1, id: "m", ok: true, startup: "restoring", restore: { completed: 1, total: 3 } } as RemoteResponse
const ready = { ...restoring, startup: "ready", restore: { completed: 3, total: 3 } } as RemoteResponse

afterEach(() => { vi.useRealTimers() })
describe("phone engine restoration", () => {
  it("reads progress without mutating work, retains it on a dropped read, and clears it when ready", async () => {
    vi.useFakeTimers()
    const rpc = vi.fn().mockResolvedValueOnce(restoring).mockRejectedValueOnce(new Error("dropped"))
      .mockResolvedValue(ready)
    const target = link(rpc)
    const { result } = renderHook(() => useEngineStartup(target))
    await act(async () => {})
    expect(result.current?.startup).toBe("restoring")
    expect(result.current?.restore).toEqual({ completed: 1, total: 3 })
    await act(() => vi.advanceTimersByTimeAsync(1000))
    expect(result.current?.startup).toBe("restoring")
    await act(() => vi.advanceTimersByTimeAsync(1000))
    expect(result.current?.startup).toBe("ready")
    expect(rpc.mock.calls.every(([request]) => request.op === "meta")).toBe(true)
  })

  it("does not carry another PC's late progress across a host switch", async () => {
    let finish!: (r: RemoteResponse) => void
    const old = link(vi.fn(() => new Promise<RemoteResponse>((done) => { finish = done })))
    const current = link(vi.fn(async () => ready))
    const { result, rerender } = renderHook(({ target }) => useEngineStartup(target), { initialProps: { target: old } })
    rerender({ target: current })
    await waitFor(() => expect(result.current?.startup).toBe("ready"))
    await act(async () => { finish(restoring) })
    expect(result.current?.startup).toBe("ready")
    rerender({ target: null as unknown as RemoteLink })
    expect(result.current).toBeUndefined()
  })

  it("stops probing an older PC that does not support metadata", async () => {
    const rpc = vi.fn(async () => ({ v: 1, id: "m", ok: false, code: "unknown_op" }))
    const target = link(rpc)
    const { result } = renderHook(() => useEngineStartup(target))
    await waitFor(() => expect(rpc).toHaveBeenCalledOnce())
    vi.useFakeTimers()
    await act(() => vi.advanceTimersByTimeAsync(3000))
    expect(result.current).toBeUndefined()
    expect(rpc).toHaveBeenCalledOnce()
  })
})
