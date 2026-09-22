import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { SkillTidyCard } from "./skill-tidy-card"
import { TIDY_SCAN_MS, emptyTidyReport } from "@/lib/skill-tidy"
import type { SkillTidyReport } from "@/lib/types"

const folded: SkillTidyReport = {
  scanned: 3,
  before: 3,
  after: 2,
  families: 1,
  unchanged: 1,
  created: ["a-procedure"],
  deleted: ["a-procedure-notes", "a-procedure-send"],
  patched: [],
  merged: [
    {
      keep: "a-procedure",
      dropped: ["a-procedure-notes", "a-procedure-send"],
      created: true,
    },
  ],
  reviewed: true,
}

describe("Skill tidy card", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it("scans then parks on the model step while a tidy is in flight", () => {
    render(<SkillTidyCard tidying skillCount={3} />)
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "1")
    expect(screen.getByText(/Scanning 3 skills/)).toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(TIDY_SCAN_MS)
    })
    expect(screen.getByText(/Asking the model to review the catalog/)).toBeInTheDocument()
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "2")
  })

  it("lists what changed as soon as the model returns", () => {
    const { rerender } = render(<SkillTidyCard tidying skillCount={3} />)
    rerender(<SkillTidyCard tidying={false} skillCount={2} report={folded} />)
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument()
    expect(screen.getByText(/Skills curated/)).toBeInTheDocument()
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/3 scanned/)
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/1 merged/)
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/2 deleted/)
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/1 created/)
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/0 updated/)
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

  it("says so when the model looked and kept the catalog, with the counts still visible", () => {
    render(
      <SkillTidyCard
        tidying={false}
        skillCount={2}
        report={{ ...emptyTidyReport(2), reviewed: true }}
      />,
    )
    expect(screen.getByText(/nothing to merge/i)).toBeInTheDocument()
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/2 scanned/)
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/0 merged/)
    expect(screen.queryByText("Merged")).not.toBeInTheDocument()
  })

  it("does not pretend the model ran on an empty catalog", () => {
    render(
      <SkillTidyCard tidying={false} skillCount={0} report={emptyTidyReport(0)} />,
    )
    expect(screen.getByText(/No skills to curate/)).toBeInTheDocument()
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
