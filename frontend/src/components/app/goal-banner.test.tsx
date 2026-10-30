import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { useApp } from "@/store/app"

import { formatGoalAge, GoalBanner } from "./goal-banner"

describe("formatGoalAge", () => {
  it("names days hours minutes and seconds", () => {
    const from = new Date("2026-01-01T00:00:00Z")
    const now = new Date("2026-01-02T10:39:16Z")
    expect(formatGoalAge(from, now)).toBe("1d 10h 39m 16s")
  })

  it("drops leading zero units", () => {
    const from = new Date("2026-01-01T00:00:00Z")
    expect(formatGoalAge(from, new Date("2026-01-01T00:00:09Z"))).toBe("9s")
    expect(formatGoalAge(from, new Date("2026-01-01T00:02:03Z"))).toBe("2m 3s")
  })
})

describe("GoalBanner", () => {
  it("sits as a one-line pin, not a card", () => {
    render(<GoalBanner goal="keep going" onClear={vi.fn()} />)
    expect(screen.getByTestId("goal-banner").className).toContain("bg-background")
    expect(screen.getByTestId("goal-banner").className).not.toMatch(/bg-muted/)
  })
  it("shows the objective and can clear it", () => {
    const onClear = vi.fn()
    render(<GoalBanner goal="keep going" onClear={onClear} />)
    expect(screen.getByTestId("goal-banner").textContent).toContain("keep going")
    expect(screen.getByTestId("goal-banner").textContent).toContain("Pursuing")
    fireEvent.click(screen.getByRole("button", { name: "Clear goal" }))
    expect(onClear).toHaveBeenCalled()
  })

  it("hides when there is no goal", () => {
    const { container } = render(<GoalBanner goal="  " onClear={vi.fn()} />)
    expect(container).toBeEmptyDOMElement()
  })

  it.each([false, true])("hides a completed objective even while running=%s", (running) => {
    const onClear = vi.fn()
    const { container } = render(
      <GoalBanner goal="keep going" complete running={running} onResume={vi.fn()} onClear={onClear} />,
    )
    expect(container).toBeEmptyDOMElement()
    expect(onClear).not.toHaveBeenCalled()
  })

  it("removes the pin on completion and shows it for a later open goal", () => {
    const onClear = vi.fn()
    const { rerender } = render(<GoalBanner goal="keep going" onClear={onClear} />)
    expect(screen.getByTestId("goal-banner")).toBeInTheDocument()
    rerender(<GoalBanner goal="keep going" complete onClear={onClear} />)
    expect(screen.queryByTestId("goal-banner")).toBeNull()
    rerender(<GoalBanner goal="another objective" onClear={onClear} />)
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("another objective")
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("Pursuing")
    expect(onClear).not.toHaveBeenCalled()
  })

  it("discards an unfinished edit on completion before a later goal opens", () => {
    const onEdit = vi.fn()
    const onClear = vi.fn()
    const { rerender } = render(<GoalBanner goal="keep going" onEdit={onEdit} onClear={onClear} />)
    fireEvent.click(screen.getByTestId("goal-text"))
    fireEvent.change(screen.getByTestId("goal-edit"), { target: { value: "unsaved edit" } })
    rerender(<GoalBanner goal="keep going" complete onEdit={onEdit} onClear={onClear} />)
    rerender(<GoalBanner goal="another objective" onEdit={onEdit} onClear={onClear} />)
    expect(screen.queryByTestId("goal-edit")).toBeNull()
    expect(screen.getByTestId("goal-text")).toHaveTextContent("another objective")
    expect(onEdit).not.toHaveBeenCalled()
    expect(onClear).not.toHaveBeenCalled()
  })

  it("marks a capped objective", () => {
    render(<GoalBanner goal="keep going" capped onResume={vi.fn()} onClear={vi.fn()} />)
    expect(screen.getByTestId("goal-banner").textContent).toContain("Auto-run paused")
    expect(screen.getByTestId("goal-reason")).toHaveTextContent("Auto-run pause reason: Automatic continuation reached its current limit.")
    expect(screen.getByTestId("goal-reason").textContent).not.toMatch(/not an error/)
    expect(screen.getByText("You can keep chatting manually. Start goal resumes automatic pursuit.")).toBeVisible()
    expect(screen.getByRole("button", { name: "Start goal" })).toBeInTheDocument()
  })

  it.each([
    { pause: { blocked: true, blockReason: "needs an external change" }, reason: "needs an external change" },
    { pause: { capped: true }, reason: "Automatic continuation reached its current limit." },
    { pause: { idle: true }, reason: "The last automatic continuation made no progress." },
  ])("retains the pause cause without directing to an unavailable Start: %j", ({ pause, reason }) => {
    render(<GoalBanner goal="keep going" {...pause} onClear={vi.fn()} />)
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("Auto-run paused")
    expect(screen.getByTestId("goal-reason")).toHaveTextContent("Auto-run pause reason: " + reason)
    expect(screen.queryByTestId("goal-start")).toBeNull()
    expect(screen.queryByText("You can keep chatting manually. Start goal resumes automatic pursuit.")).toBeNull()
    expect(screen.getByTestId("goal-banner")).not.toHaveTextContent("Press Start")
  })

  it("marks a blocked objective and can resume", () => {
    const onResume = vi.fn()
    render(
      <GoalBanner
        goal="keep going"
        blocked
        blockReason="needs an external change"
        onResume={onResume}
        onClear={vi.fn()}
      />,
    )
    expect(screen.getByTestId("goal-banner").textContent).toContain("Auto-run paused")
    expect(screen.getByTestId("goal-reason").textContent).toContain(
      "Auto-run pause reason: needs an external change",
    )
    fireEvent.click(screen.getByRole("button", { name: "Start goal" }))
    expect(onResume).toHaveBeenCalled()
  })

  it("translates the failed-turn sentinel instead of showing the protocol string", () => {
    render(
      <GoalBanner
        goal="keep going"
        blocked
        blockReason="the last turn failed"
        onClear={vi.fn()}
      />,
    )
    expect(screen.getByTestId("goal-reason")).toHaveTextContent("The last turn failed.")
    expect(screen.getByTestId("goal-reason").textContent).not.toBe("the last turn failed")
  })

  it("prefers the turn error over the failed-turn sentinel", () => {
    render(
      <GoalBanner
        goal="keep going"
        blocked
        blockReason="the last turn failed"
        turnError="the endpoint refused the connection"
        onClear={vi.fn()}
      />,
    )
    expect(screen.getByTestId("goal-reason")).toHaveTextContent(
      "the endpoint refused the connection",
    )
  })

  it("hides start while a turn is running", () => {
    render(
      <GoalBanner
        goal="keep going"
        running
        capped
        onResume={vi.fn()}
        onClear={vi.fn()}
      />,
    )
    expect(screen.queryByTestId("goal-start")).toBeNull()
    expect(screen.queryByText("You can keep chatting manually. Start goal resumes automatic pursuit.")).toBeNull()
  })

  it("shows start after a no-progress continuation", () => {
    const onResume = vi.fn()
    render(
      <GoalBanner goal="keep going" idle onResume={onResume} onClear={vi.fn()} />,
    )
    expect(screen.getByTestId("goal-banner").textContent).toContain("Auto-run paused")
    expect(screen.getByTestId("goal-reason").textContent).toMatch(/no progress/)
    expect(screen.getByText("You can keep chatting manually. Start goal resumes automatic pursuit.")).toBeVisible()
    fireEvent.click(screen.getByTestId("goal-start"))
    expect(onResume).toHaveBeenCalled()
  })

  it("hides start while a pursuing goal is idle between turns", () => {
    // Codex: Play is resume. An active goal waiting for auto-continue is
    // still Pursuing, not a start control.
    render(
      <GoalBanner goal="keep going" onResume={vi.fn()} onClear={vi.fn()} />,
    )
    expect(screen.getByTestId("goal-banner").textContent).toContain("Pursuing")
    expect(screen.queryByTestId("goal-start")).toBeNull()
  })

  it("says the objective is parked on a wait, not crashed", () => {
    render(
      <GoalBanner goal="keep going" waiting onResume={vi.fn()} onClear={vi.fn()} />,
    )
    expect(screen.getByTestId("goal-banner")).toHaveAttribute("data-waiting", "true")
    expect(screen.getByTestId("goal-banner").textContent).toContain("Pursuing")
    expect(screen.queryByTestId("goal-reason")).toBeNull()
    expect(screen.queryByTestId("goal-start")).toBeNull()
  })

  it("edits the objective and saves on blur", async () => {
    const onEdit = vi.fn().mockResolvedValue(true)
    render(<GoalBanner goal="keep going" onEdit={onEdit} onClear={vi.fn()} />)
    fireEvent.click(screen.getByTestId("goal-text"))
    const box = screen.getByTestId("goal-edit")
    fireEvent.change(box, { target: { value: "keep going, tighter" } })
    fireEvent.blur(box)
    expect(onEdit).toHaveBeenCalledWith("keep going, tighter")
    await waitFor(() => expect(screen.queryByTestId("goal-edit")).toBeNull())
  })

  it("cancels an edit on Escape", () => {
    const onEdit = vi.fn()
    render(<GoalBanner goal="keep going" onEdit={onEdit} onClear={vi.fn()} />)
    fireEvent.click(screen.getByTestId("goal-text"))
    const box = screen.getByTestId("goal-edit")
    fireEvent.change(box, { target: { value: "do not keep this" } })
    fireEvent.keyDown(box, { key: "Escape" })
    fireEvent.blur(box)
    expect(onEdit).not.toHaveBeenCalled()
    expect(screen.getByTestId("goal-text").textContent).toContain("keep going")
  })

  it("clears when the edit is emptied", async () => {
    const onClear = vi.fn().mockResolvedValue(true)
    render(<GoalBanner goal="keep going" onEdit={vi.fn()} onClear={onClear} />)
    fireEvent.click(screen.getByTestId("goal-text"))
    const box = screen.getByTestId("goal-edit")
    fireEvent.change(box, { target: { value: "  " } })
    fireEvent.blur(box)
    expect(onClear).toHaveBeenCalled()
    await waitFor(() => expect(screen.queryByTestId("goal-edit")).toBeNull())
  })

  it("shows elapsed time from when the objective was set", () => {
    render(
      <GoalBanner
        goal="keep going"
        startedAt="2026-01-01T00:00:00.000Z"
        onClear={vi.fn()}
      />,
    )
    expect(screen.getByTestId("goal-age").textContent).toMatch(/^Goal age: .*\d+s$/)
  })

  it.each([
    { blocked: true, blockReason: "needs an external change" },
    { capped: true },
    { idle: true },
  ])("identifies the paused activity without completing or resuming it: %j", (pause) => {
    const onResume = vi.fn()
    const onClear = vi.fn()
    const props = { goal: "keep going", startedAt: "2026-01-01T00:00:00.000Z", onResume, onClear, ...pause }
    const { rerender } = render(<GoalBanner {...props} running />)
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("Auto-run paused")

    // Ending a manual reply does not change the durable goal's completion or pause.
    rerender(<GoalBanner {...props} running={false} />)
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("Auto-run paused")
    expect(screen.getByText("You can keep chatting manually. Start goal resumes automatic pursuit.")).toBeVisible()
    expect(screen.getByTestId("goal-age")).toHaveTextContent(/^Goal age: /)
    expect(screen.getByRole("button", { name: "Start goal" })).toBeVisible()
    expect(onResume).not.toHaveBeenCalled()
    expect(onClear).not.toHaveBeenCalled()

    rerender(<GoalBanner {...props} complete />)
    expect(screen.queryByTestId("goal-banner")).toBeNull()
    expect(onResume).not.toHaveBeenCalled()
    expect(onClear).not.toHaveBeenCalled()
  })

  it("distinguishes automatic pause and goal age in Chinese", () => {
    const previousLocale = useApp.getState().locale
    useApp.setState({ locale: "zh" })
    const view = render(
      <GoalBanner goal="继续核验目标" blocked blockReason="等待外部条件" startedAt="2026-01-01T00:00:00.000Z" onResume={vi.fn()} onClear={vi.fn()} />,
    )
    try {
      expect(screen.getByTestId("goal-banner")).toHaveTextContent("自动执行已暂停")
      expect(screen.getByTestId("goal-reason")).toHaveTextContent("自动执行暂停原因：等待外部条件")
      expect(screen.getByTestId("goal-age")).toHaveTextContent(/^目标设立至今 /)
      expect(screen.getByText("可手动继续对话；点「开始目标」恢复目标自动执行。")).toBeVisible()
    } finally {
      view.unmount()
      useApp.setState({ locale: previousLocale })
    }
  })
})
