import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ScheduleNotice } from "./schedule-notice"
import type { Block } from "@/lib/transcript"

function notice(partial: Partial<Block> = {}): Block {
  return {
    id: "n1",
    kind: "notice",
    agentId: "manager",
    text: "A wait is armed.",
    detail: "sch_ab12",
    turnId: "tn_1",
    seq: 1,
    at: "2026-01-01T00:00:00.000Z",
    ...partial,
  }
}

describe("ScheduleNotice", () => {
  it("cancels when detail is a schedule id", () => {
    const onCancel = vi.fn()
    render(<ScheduleNotice block={notice()} onCancel={onCancel} onRunNow={vi.fn()} />)
    expect(screen.getByTestId("schedule-notice").textContent).toMatch(/A wait is armed/)
    fireEvent.click(screen.getByRole("button", { name: "Cancel wait" }))
    expect(onCancel).toHaveBeenCalledWith("sch_ab12")
  })

  it("runs the wait now from the armed chip", () => {
    const onRunNow = vi.fn()
    render(<ScheduleNotice block={notice()} onCancel={vi.fn()} onRunNow={onRunNow} />)
    fireEvent.click(screen.getByRole("button", { name: "Run now" }))
    expect(onRunNow).toHaveBeenCalledWith("sch_ab12")
  })

  it("does not treat a cancelled notice as a briefing or a second cancel", () => {
    render(
      <ScheduleNotice
        block={notice({ text: "A wait was cancelled.", detail: "sch_ab12" })}
      />,
    )
    expect(screen.getByTestId("schedule-notice").textContent).toMatch(
      /A wait was cancelled/,
    )
    expect(screen.queryByRole("button", { name: "Cancel wait" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Run now" })).toBeNull()
    expect(screen.queryByTestId("compact-briefing-open")).toBeNull()
  })
})
