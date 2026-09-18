import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ScheduleBanner } from "./schedule-banner"
import type { Schedule } from "@/lib/types"

function wake(partial: Partial<Schedule> = {}): Schedule {
  return {
    id: "sch_1",
    kind: "thread",
    origin_thread_id: "th_1",
    thread_id: "th_1",
    project_id: "",
    provider_id: "",
    model: "",
    title: "Periodic check",
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
    ...partial,
  }
}

describe("ScheduleBanner", () => {
  it("renders for an active wake and cancel calls delete", () => {
    const onCancel = vi.fn()
    render(<ScheduleBanner wake={wake()} onCancel={onCancel} />)
    expect(screen.getByTestId("schedule-banner")).toBeInTheDocument()
    expect(screen.getByTestId("schedule-next").textContent?.length).toBeGreaterThan(0)
    fireEvent.click(screen.getByRole("button", { name: "Cancel wait" }))
    expect(onCancel).toHaveBeenCalled()
  })

  it("hides when there is no active thread wake", () => {
    const { container: empty } = render(<ScheduleBanner onCancel={vi.fn()} />)
    expect(empty).toBeEmptyDOMElement()
    const { container: paused } = render(
      <ScheduleBanner wake={wake({ status: "paused" })} onCancel={vi.fn()} />,
    )
    expect(paused).toBeEmptyDOMElement()
    const { container: job } = render(
      <ScheduleBanner wake={wake({ kind: "standalone" })} onCancel={vi.fn()} />,
    )
    expect(job).toBeEmptyDOMElement()
  })
})
