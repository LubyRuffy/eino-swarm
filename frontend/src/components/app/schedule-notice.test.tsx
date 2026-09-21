import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ScheduleNotice } from "./schedule-notice"
import type { Block } from "@/lib/transcript"
import { useApp } from "@/store/app"

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
    expect(screen.getByTestId("wait-mark")).toHaveAttribute("aria-label", "waiting")
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
    expect(screen.queryByTestId("wait-mark")).toBeNull()
    expect(screen.queryByTestId("compact-briefing-open")).toBeNull()
  })

  it("drops Run now on a historical chip whose wait is no longer active", () => {
    useApp.setState({
      schedules: [
        {
          id: "sch_ab12",
          kind: "thread",
          origin_thread_id: "th_1",
          thread_id: "th_1",
          project_id: "",
          provider_id: "",
          model: "",
          title: "",
          prompt: "Continue the wait.",
          delay_s: 0,
          every_s: 60,
          cron: "",
          status: "done",
          next_run_at: "2026-09-19T12:00:00.000Z",
          run_count: 1,
          max_runs: 0,
          created_by: "manager",
          created_at: "2026-09-19T00:00:00.000Z",
          updated_at: "2026-09-19T00:00:00.000Z",
        },
      ],
    })
    render(<ScheduleNotice block={notice()} />)
    expect(screen.queryByRole("button", { name: "Run now" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Cancel wait" })).toBeNull()
  })

  it("shows the stored prompt and next check on an armed chip", () => {
    useApp.setState({
      schedules: [
        {
          id: "sch_ab12",
          kind: "thread",
          origin_thread_id: "th_1",
          thread_id: "th_1",
          project_id: "",
          provider_id: "",
          model: "",
          title: "",
          prompt: "Continue the wait.",
          delay_s: 0,
          every_s: 60,
          cron: "",
          status: "active",
          next_run_at: "2026-09-19T12:00:00.000Z",
          run_count: 0,
          max_runs: 0,
          created_by: "manager",
          created_at: "2026-09-19T00:00:00.000Z",
          updated_at: "2026-09-19T00:00:00.000Z",
        },
      ],
    })
    render(<ScheduleNotice block={notice()} />)
    expect(screen.getByTestId("schedule-prompt")).toHaveTextContent("Continue the wait.")
    expect(screen.getByTestId("schedule-next").textContent?.length).toBeGreaterThan(0)
  })

  it("matches a padded schedule id to the stored wait", () => {
    useApp.setState({
      schedules: [
        {
          id: "sch_ab12",
          kind: "thread",
          origin_thread_id: "th_1",
          thread_id: "th_1",
          project_id: "",
          provider_id: "",
          model: "",
          title: "",
          prompt: "Continue the wait.",
          delay_s: 0,
          every_s: 60,
          cron: "",
          status: "active",
          next_run_at: "2026-09-19T12:00:00.000Z",
          run_count: 0,
          max_runs: 0,
          created_by: "manager",
          created_at: "2026-09-19T00:00:00.000Z",
          updated_at: "2026-09-19T00:00:00.000Z",
        },
      ],
    })
    render(<ScheduleNotice block={notice({ detail: "sch_ab12 " })} />)
    expect(screen.getByTestId("schedule-prompt")).toHaveTextContent("Continue the wait.")
  })
})
