import { act, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { notifyEngineRestoring } from "@/lib/engine-startup"
import type { Meta } from "@/lib/types"
import { useApp } from "@/store/app"
import { EngineStartupBanner } from "./engine-startup"

const progress = { startup: "restoring", restore: { completed: 1, total: 3 } } as Meta
vi.mock("@/lib/api", () => ({ ApiError: Error, api: { meta: vi.fn() } }))
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

describe("engine startup status", () => {
  it("shows progress, retains it on read failure, and disappears only after readiness", async () => {
    vi.useFakeTimers()
    useApp.setState({ meta: progress, locale: "zh" })
    vi.mocked(api.meta).mockRejectedValueOnce(new Error("dropped")).mockResolvedValueOnce({ ...progress, startup: "ready" })
    render(<EngineStartupBanner />)
    expect(screen.getByTestId("engine-startup")).toHaveTextContent("正在恢复未完成任务 1/3")
    await act(() => vi.advanceTimersByTimeAsync(1000))
    expect(screen.getByTestId("engine-startup")).toBeVisible()
    await act(() => vi.advanceTimersByTimeAsync(1000))
    expect(screen.queryByTestId("engine-startup")).toBeNull()
  })

  it("refreshes only metadata after a restoring refusal and keeps zero progress visible", async () => {
    vi.useFakeTimers()
    useApp.setState({ meta: { startup: "ready" } as Meta, locale: "en" })
    vi.mocked(api.meta).mockResolvedValue({ startup: "restoring" } as Meta)
    render(<EngineStartupBanner />)
    expect(screen.queryByTestId("engine-startup")).toBeNull()
    act(() => notifyEngineRestoring("unrelated"))
    expect(screen.queryByTestId("engine-startup")).toBeNull()
    act(() => notifyEngineRestoring("restoring"))
    expect(screen.getByTestId("engine-startup")).toHaveTextContent("Restoring unfinished tasks 0/0")
    await act(() => vi.advanceTimersByTimeAsync(1000))
    expect(api.meta).toHaveBeenCalled()
  })

  it("ignores a late readiness read after unmount", async () => {
    vi.useFakeTimers()
    let finish!: (meta: Meta) => void
    vi.mocked(api.meta).mockReturnValue(new Promise((done) => { finish = done }))
    useApp.setState({ meta: progress })
    const view = render(<EngineStartupBanner />)
    await act(() => vi.advanceTimersByTimeAsync(1000))
    view.unmount()
    await act(async () => { finish({ startup: "ready" } as Meta) })
    expect(useApp.getState().meta?.startup).toBe("restoring")
  })
})


it("shows a static partial-failure warning after readiness without blocking other work", () => {
  useApp.setState({ meta: { startup: "ready", restore: { completed: 3, total: 3, failed: 1 } } as Meta, locale: "zh" })
  render(<EngineStartupBanner />)
  expect(screen.getByRole("alert")).toHaveTextContent("有 1 个会话恢复失败，请查看 Trace 或日志。")
  expect(screen.getByTestId("engine-startup").querySelector('[class*="animate-spin"]')).toBeNull()
  expect(useApp.getState().meta?.startup).toBe("ready")
})
