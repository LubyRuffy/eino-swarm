import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { setLocale } from "@/lib/i18n"
import { EngineStartupBanner } from "./engine-startup"

describe("phone engine startup notice", () => {
  it("shows progress and clears after complete recovery", () => {
    setLocale("zh")
    const view = render(<EngineStartupBanner startup={{ startup: "restoring", restore: { completed: 1, total: 3 } }} />)
    expect(screen.getByRole("status")).toHaveTextContent("正在恢复未完成任务 1/3")
    view.rerender(<EngineStartupBanner startup={{ startup: "ready", restore: { completed: 3, total: 3 } }} />)
    expect(screen.queryByTestId("engine-startup")).toBeNull()
  })
  it("retains a static partial failure warning after readiness", () => {
    setLocale("zh")
    render(<EngineStartupBanner startup={{ startup: "ready", restore: { completed: 3, total: 3, failed: 1 } }} />)
    expect(screen.getByRole("alert")).toHaveTextContent("有 1 个会话恢复失败，请查看 Trace 或日志。")
    expect(screen.getByTestId("engine-startup").querySelector('[class*="animate-spin"]')).toBeNull()
  })
})
