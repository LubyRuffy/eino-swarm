import { act, fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it } from "vitest"

import { CompactNotice } from "./compact-notice"
import type { Block } from "@/lib/transcript"
import { useApp } from "@/store/app"

function notice(partial: Partial<Block> = {}): Block {
  return {
    id: "n1",
    kind: "notice",
    agentId: "manager",
    text: "Context compressed (91200 → 1400 tokens). The transcript is unchanged.",
    compacted: true,
    turnId: "tn_1",
    seq: 1,
    at: "2026-01-01T00:00:00.000Z",
    ...partial,
  }
}

beforeEach(() => useApp.setState({ locale: "en" }))

describe("CompactNotice", () => {
  it("hides a quiet or empty row", () => {
    const { container: quiet } = render(
      <CompactNotice block={notice({ quiet: true })} />,
    )
    expect(quiet).toBeEmptyDOMElement()
    const { container: empty } = render(
      <CompactNotice block={notice({ text: "" })} />,
    )
    expect(empty).toBeEmptyDOMElement()
  })

  it("keeps compression secondary while retaining token counts without a briefing", () => {
    render(<CompactNotice block={notice()} />)
    const row = screen.getByTestId("memory-notice")
    expect(row).toHaveTextContent("Context compressed (91200 → 1400 tokens)")
    expect(row).not.toHaveTextContent("The transcript is unchanged")
    expect(row).not.toHaveClass("border", "bg-muted/50")
    expect(screen.queryByTestId("compact-briefing-open")).toBeNull()
  })

  it("exposes the complete briefing inline through a visible accessible toggle", () => {
    const body = "  Standing constraint still holds.\nNext: finish the remaining work.\n"
    render(<CompactNotice block={notice({ detail: body })} />)
    expect(screen.getByTestId("memory-notice").textContent).not.toMatch(/Standing constraint/)
    expect(screen.queryByTestId("compact-briefing")).toBeNull()
    const toggle = screen.getByRole("button", { name: "View compressed briefing" })
    expect(toggle).toHaveTextContent("View compressed briefing")
    expect(toggle).toHaveAttribute("aria-expanded", "false")
    expect(document.getElementById(toggle.getAttribute("aria-controls")!)).not.toBeVisible()
    fireEvent.click(toggle)
    const briefing = screen.getByTestId("compact-briefing")
    expect(briefing.textContent).toBe(body)
    expect(screen.getByTestId("memory-notice")).toContainElement(briefing)
    expect(toggle).toHaveAttribute("aria-expanded", "true")
    expect(toggle).toHaveAttribute("aria-controls", screen.getByRole("region", { name: "Compressed briefing" }).id)
    expect(screen.getByRole("button", { name: "Hide briefing" })).toBe(toggle)
    expect(screen.queryByRole("dialog")).toBeNull()
    fireEvent.click(toggle)
    expect(screen.queryByTestId("compact-briefing")).toBeNull()
    expect(toggle).toHaveAttribute("aria-expanded", "false")
  })

  it("keeps every briefing attached to its own disclosure", () => {
    render(<>
      <CompactNotice block={notice({ detail: "First briefing" })} />
      <CompactNotice block={notice({ id: "n2", detail: "Second briefing" })} />
    </>)
    const [first, second] = screen.getAllByRole("button", { name: "View compressed briefing" })
    expect(first.getAttribute("aria-controls")).not.toBe(second.getAttribute("aria-controls"))
    fireEvent.click(second)
    expect(screen.getByTestId("compact-briefing")).toHaveTextContent("Second briefing")
    expect(first).toHaveAttribute("aria-expanded", "false")
    expect(second).toHaveAttribute("aria-expanded", "true")
  })

  it.each([
    "Earlier turns were folded into a briefing. The transcript is unchanged.",
    "Context compressed. The transcript is unchanged.",
  ])("uses a concise localized state for %s and ignores whitespace-only briefings", (text) => {
    render(<CompactNotice block={notice({
      text,
      detail: " \n ",
    })} />)
    expect(screen.getByTestId("memory-notice")).toHaveTextContent("Context compressed")
    expect(screen.queryByTestId("compact-briefing-open")).toBeNull()
    act(() => useApp.setState({ locale: "zh" }))
    expect(screen.getByTestId("memory-notice")).toHaveTextContent("上下文已压缩")
  })

  it("localizes the token state and disclosure without changing the briefing", () => {
    render(<CompactNotice block={notice({ detail: "Full briefing body" })} />)
    act(() => useApp.setState({ locale: "zh" }))
    expect(screen.getByTestId("memory-notice")).toHaveTextContent("上下文已压缩（91200 → 1400 tokens）")
    fireEvent.click(screen.getByRole("button", { name: "查看压缩后的简报" }))
    expect(screen.getByRole("button", { name: "收起简报" })).toHaveAttribute("aria-expanded", "true")
    expect(screen.getByTestId("compact-briefing")).toHaveTextContent("Full briefing body")
  })

  it.each([
    "Standing objective completed.",
    "Memory updated.",
    "could not store auto-compact briefing",
    "Earlier turns were folded into a briefing. The transcript is unchanged.",
  ])("does not downgrade an unmarked notice: %s", (text) => {
    render(<CompactNotice block={notice({ text, compacted: undefined })} />)
    const row = screen.getByTestId("memory-notice")
    expect(row).toHaveTextContent(text)
    expect(row).toHaveClass("border", "bg-muted/50")
  })

  it("keeps ongoing compression explicit", () => {
    render(<CompactNotice block={notice({ text: "Compressing conversation context…" })} />)
    expect(screen.getByTestId("memory-notice")).toHaveTextContent("Compressing conversation context…")
  })

  it("keeps a failed compression prominent and preserves its actual error", () => {
    render(<CompactNotice block={notice({ compacted: false, text: "the summarizer stopped sending data" })} />)
    expect(screen.getByTestId("memory-notice")).toHaveTextContent("the summarizer stopped sending data")
    expect(screen.getByTestId("memory-notice")).toHaveClass("border", "bg-muted/50")
  })

  it("retains structured review diagnostics instead of rendering a compact state", () => {
    render(<CompactNotice block={notice({
      review: { changed: false, stop_reason: "iteration_limit", attempt_count: 2 },
    })} />)
    expect(screen.getByTestId("memory-notice")).toHaveTextContent("2 attempts")
    expect(screen.getByRole("button", { name: "Trace tn_1" })).toBeVisible()
    expect(screen.queryByTestId("compact-briefing-open")).toBeNull()
  })
})
