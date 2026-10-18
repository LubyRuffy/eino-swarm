import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { setLocale } from "@/lib/i18n"
import { ModelPicker } from "./model-picker"

describe("model refresh controls", () => {
  it("offers a local refresh even when no model is listed", () => {
    setLocale("en")
    const refresh = vi.fn()
    render(<ModelPicker {...{ models: [], providerId: "", model: "", onSelect: vi.fn(), onClose: vi.fn(), onRefresh: refresh }} />)
    fireEvent.click(screen.getByRole("button", { name: "Refresh models" }))
    expect(refresh).toHaveBeenCalledOnce()
    expect(screen.getByText("No models available.")).toBeVisible()
  })

  it("keeps close available while refresh is busy and shows retry errors locally", () => {
    setLocale("en")
    const close = vi.fn()
    render(<ModelPicker {...{ models: [], providerId: "", model: "", onSelect: vi.fn(), onClose: close, onRefresh: vi.fn(), refreshBusy: true, refreshError: "Try again" }} />)
    expect(screen.getByRole("button", { name: "Refresh models" })).toBeDisabled()
    expect(screen.getByRole("alert")).toHaveTextContent("Try again")
    fireEvent.keyDown(window, { key: "Escape" })
    expect(close).toHaveBeenCalledOnce()
  })
})
