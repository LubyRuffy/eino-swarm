import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

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

  it("marks a completed objective", () => {
    render(<GoalBanner goal="keep going" complete onClear={vi.fn()} />)
    expect(screen.getByTestId("goal-banner").textContent).toContain("Done")
    expect(screen.queryByTestId("goal-start")).toBeNull()
  })

  it("marks a capped objective", () => {
    render(<GoalBanner goal="keep going" capped onResume={vi.fn()} onClear={vi.fn()} />)
    expect(screen.getByTestId("goal-banner").textContent).toContain("Paused")
    expect(screen.getByTestId("goal-reason").textContent).toMatch(/not an error/)
    expect(screen.getByTestId("goal-reason").textContent).toMatch(/Press Start/)
    expect(screen.getByRole("button", { name: "Start goal" })).toHaveTextContent("Start goal")
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
    expect(screen.getByTestId("goal-banner").textContent).toContain("Blocked")
    expect(screen.getByTestId("goal-reason").textContent).toContain(
      "needs an external change",
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
  })

  it("shows start after a no-progress continuation", () => {
    const onResume = vi.fn()
    render(
      <GoalBanner goal="keep going" idle onResume={onResume} onClear={vi.fn()} />,
    )
    expect(screen.getByTestId("goal-banner").textContent).toContain("Paused")
    expect(screen.getByTestId("goal-reason").textContent).toMatch(/no progress/)
    expect(screen.getByTestId("goal-reason").textContent).toMatch(/Press Start/)
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

  it("edits the objective and saves on blur", () => {
    const onEdit = vi.fn()
    render(<GoalBanner goal="keep going" onEdit={onEdit} onClear={vi.fn()} />)
    fireEvent.click(screen.getByTestId("goal-text"))
    const box = screen.getByTestId("goal-edit")
    fireEvent.change(box, { target: { value: "keep going, tighter" } })
    fireEvent.blur(box)
    expect(onEdit).toHaveBeenCalledWith("keep going, tighter")
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

  it("clears when the edit is emptied", () => {
    const onClear = vi.fn()
    render(<GoalBanner goal="keep going" onEdit={vi.fn()} onClear={onClear} />)
    fireEvent.click(screen.getByTestId("goal-text"))
    const box = screen.getByTestId("goal-edit")
    fireEvent.change(box, { target: { value: "  " } })
    fireEvent.blur(box)
    expect(onClear).toHaveBeenCalled()
  })

  it("shows elapsed time from when the objective was set", () => {
    render(
      <GoalBanner
        goal="keep going"
        startedAt="2026-01-01T00:00:00.000Z"
        onClear={vi.fn()}
      />,
    )
    expect(screen.getByTestId("goal-age").textContent).toMatch(/\d+s/)
  })
})
