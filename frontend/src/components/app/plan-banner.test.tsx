import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { PlanBanner } from "./plan-banner"

describe("PlanBanner", () => {
  it("hides when not planning", () => {
    const { container } = render(<PlanBanner />)
    expect(container.firstChild).toBeNull()
  })

  it("edits, implements, and leaves", () => {
    const onEdit = vi.fn()
    const onImplement = vi.fn()
    const onLeave = vi.fn()
    render(
      <PlanBanner
        mode
        markdown="# Plan\n\nDo the work."
        onEdit={onEdit}
        onImplement={onImplement}
        onLeave={onLeave}
      />,
    )
    expect(screen.getByTestId("plan-banner").textContent).toContain("Planning")
    expect(screen.getByTestId("plan-text").textContent).toContain("Do the work")
    fireEvent.click(screen.getByTestId("plan-implement"))
    expect(onImplement).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole("button", { name: "Leave planning" }))
    expect(onLeave).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByTestId("plan-text"))
    const box = screen.getByTestId("plan-edit")
    fireEvent.change(box, { target: { value: "# Plan\n\nRevised." } })
    fireEvent.blur(box)
    expect(onEdit).toHaveBeenCalledWith("# Plan\n\nRevised.")
  })

  it("hides implement while a turn is running", () => {
    render(<PlanBanner mode markdown="# Plan" running />)
    expect(screen.queryByTestId("plan-implement")).toBeNull()
  })
})
