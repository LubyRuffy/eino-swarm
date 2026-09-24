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

  it("estimates progress and keeps the bar short of full while the model is still writing", () => {
    render(
      <SkillTidyCard
        tidying
        skillCount={3}
        live={{ text: "Reading the catalog and comparing procedures.", changes: [] }}
      />,
    )
    const bar = screen.getByRole("progressbar")
    expect(bar).toHaveAttribute("aria-valuemax", "100")
    expect(Number(bar.getAttribute("aria-valuenow"))).toBeLessThan(92)
    expect(screen.getByText(/Scanning 3 skills/)).toBeInTheDocument()
    const stream = screen.getByTestId("tidy-stream")
    expect(stream).toHaveClass("max-h-32")
    expect(stream).toHaveTextContent("Reading the catalog and comparing procedures.")

    act(() => {
      vi.advanceTimersByTime(TIDY_SCAN_MS)
    })
    expect(screen.getByText(/Asking the model to review the catalog/)).toBeInTheDocument()
    expect(Number(screen.getByRole("progressbar").getAttribute("aria-valuenow"))).toBeLessThan(92)

    act(() => {
      vi.advanceTimersByTime(30 * 60 * 1000)
    })
    expect(Number(screen.getByRole("progressbar").getAttribute("aria-valuenow"))).toBe(92)
  })

  it("lists what changed as soon as the model returns", () => {
    const { rerender } = render(<SkillTidyCard tidying skillCount={3} />)
    rerender(<SkillTidyCard tidying={false} skillCount={2} report={folded} />)
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument()
    expect(screen.getByText(/Skills curated/)).toBeInTheDocument()
    expect(screen.getByTestId("tidy-summary")).toHaveTextContent(
      "Deleted 2, created 1, merged 1, updated 0.",
    )
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

  it("turns a header timeout into a settings hint and does not say the catalog is empty", () => {
    render(
      <SkillTidyCard
        tidying={false}
        skillCount={8}
        report={{
          ...emptyTidyReport(8),
          err: '[NodeRunError] failed to create chat completion: Post "https://example.invalid/v1/chat/completions": http2: timeout awaiting response headers\nnode path: [node_a, ChatModel]',
        }}
      />,
    )
    const status = screen.getByTestId("tidy-status")
    expect(status).toHaveTextContent(/30s/)
    expect(status).not.toHaveTextContent("NodeRunError")
    expect(status).not.toHaveTextContent("example.invalid")
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/8 remaining/)
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
