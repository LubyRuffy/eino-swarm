import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { SkillTidyCard } from "./skill-tidy-card"
import { TIDY_STEP_MS, emptyTidyReport } from "@/lib/skill-tidy"
import type { SkillTidyReport } from "@/lib/types"

const folded: SkillTidyReport = {
  scanned: 3,
  before: 3,
  after: 2,
  families: 1,
  unchanged: 1,
  created: ["a-procedure"],
  deleted: ["a-procedure-notes", "a-procedure-send"],
  merged: [
    {
      keep: "a-procedure",
      dropped: ["a-procedure-notes", "a-procedure-send"],
      created: true,
    },
  ],
}

describe("Skill tidy card", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it("walks the scan, group and fold steps while a tidy is in flight", () => {
    render(<SkillTidyCard tidying skillCount={3} />)
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "1")
    expect(screen.getByText(/Scanning 3 skills/)).toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(TIDY_STEP_MS)
    })
    expect(screen.getByText(/Finding overlapping groups/)).toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(TIDY_STEP_MS)
    })
    expect(screen.getByText(/Collapsing overlapping groups/)).toBeInTheDocument()
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "3")
  })

  it("holds the progress until the steps have been shown, then lists what changed", () => {
    const { rerender } = render(<SkillTidyCard tidying skillCount={3} />)
    rerender(<SkillTidyCard tidying={false} skillCount={2} report={folded} />)
    expect(screen.getByRole("progressbar")).toBeInTheDocument()
    expect(screen.queryByTestId("tidy-stats")).not.toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(TIDY_STEP_MS * 2)
    })
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument()
    expect(screen.getByText(/Folded overlapping skills/)).toBeInTheDocument()
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/3 scanned/)
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/1 merged/)
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/2 deleted/)
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/1 created/)
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/2 remaining/)
    expect(screen.getByText("Merged")).toBeInTheDocument()
    expect(
      screen.getByText("a-procedure-notes, a-procedure-send → a-procedure"),
    ).toBeInTheDocument()
    expect(screen.getByText("Deleted")).toBeInTheDocument()
    expect(screen.getByText("a-procedure-notes")).toBeInTheDocument()
    expect(screen.getByText("Created")).toBeInTheDocument()
    expect(screen.getByText("a-procedure")).toBeInTheDocument()
  })

  it("says so when nothing overlapped, with the counts still visible", () => {
    render(
      <SkillTidyCard
        tidying={false}
        skillCount={2}
        report={emptyTidyReport(2)}
      />,
    )
    expect(screen.getByText(/already tidy/)).toBeInTheDocument()
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/2 scanned/)
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/0 merged/)
    expect(screen.queryByText("Merged")).not.toBeInTheDocument()
  })

  it("keeps a failed tidy visible", () => {
    render(<SkillTidyCard tidying={false} skillCount={1} error="cannot tidy skills" />)
    expect(screen.getByTestId("tidy-status")).toHaveTextContent("cannot tidy skills")
  })

  it("dismisses the result on request", () => {
    const onDismiss = vi.fn()
    render(
      <SkillTidyCard
        tidying={false}
        skillCount={2}
        report={emptyTidyReport(2)}
        onDismiss={onDismiss}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Dismiss tidy result" }))
    expect(onDismiss).toHaveBeenCalled()
  })
})
