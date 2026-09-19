import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ScheduleWaitActions } from "./schedule-wait-actions"

describe("ScheduleWaitActions", () => {
  it("labels both actions so Cancel wait is not an icon-only X", () => {
    const onRunNow = vi.fn()
    const onCancel = vi.fn()
    render(<ScheduleWaitActions onRunNow={onRunNow} onCancel={onCancel} />)
    fireEvent.click(screen.getByRole("button", { name: "Run now" }))
    fireEvent.click(screen.getByRole("button", { name: "Cancel wait" }))
    expect(onRunNow).toHaveBeenCalled()
    expect(onCancel).toHaveBeenCalled()
    expect(screen.getByRole("button", { name: "Cancel wait" }).textContent).toMatch(
      /Cancel wait/,
    )
  })

  it("hides Run now while the conversation is already working", () => {
    render(<ScheduleWaitActions running onRunNow={vi.fn()} onCancel={vi.fn()} />)
    expect(screen.queryByRole("button", { name: "Run now" })).toBeNull()
    expect(screen.getByRole("button", { name: "Cancel wait" })).toBeInTheDocument()
  })
})
