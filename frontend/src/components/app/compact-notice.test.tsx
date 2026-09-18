import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { CompactNotice } from "./compact-notice"
import type { Block } from "@/lib/transcript"

function notice(partial: Partial<Block> = {}): Block {
  return {
    id: "n1",
    kind: "notice",
    agentId: "manager",
    text: "Context compressed (91200 → 1400 tokens). The transcript is unchanged.",
    turnId: "tn_1",
    seq: 1,
    at: "2026-01-01T00:00:00.000Z",
    ...partial,
  }
}

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

  it("keeps a one-liner when there is no briefing to open", () => {
    render(<CompactNotice block={notice()} />)
    expect(screen.getByTestId("memory-notice").textContent).toMatch(/Context compressed/)
    expect(screen.queryByTestId("compact-briefing-open")).toBeNull()
  })

  it("opens the briefing from the icon and leaves the row a one-liner", () => {
    const body = "Standing constraint still holds.\nNext: finish the remaining work."
    render(<CompactNotice block={notice({ detail: body })} />)
    expect(screen.getByTestId("memory-notice").textContent).not.toMatch(/Standing constraint/)
    expect(screen.queryByTestId("compact-briefing")).toBeNull()
    fireEvent.click(screen.getByTestId("compact-briefing-open"))
    expect(screen.getByTestId("compact-briefing").textContent).toBe(body)
    expect(screen.getByRole("dialog").textContent).not.toMatch(/through_seq/)
  })
})
