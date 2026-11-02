import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it } from "vitest"

import { Transcript } from "./transcript"
import { emptyTranscript, type Block, type TranscriptState } from "@/lib/transcript"
import { useApp } from "@/store/app"

function transcript(extra: Block[] = []): TranscriptState {
  const blocks: Block[] = [
    { ...answer("first"), kind: "user" as const, text: "Earlier request", turnId: "early" },
    { ...answer("last"), kind: "user" as const, text: "Current request" },
    ...extra,
  ].map((block, i) => ({ ...block, seq: i + 1 }))
  return {
    ...emptyTranscript(),
    agents: { manager: { id: "manager", role: "manager", status: "running", activity: "", blocks } },
    agentOrder: ["manager"],
  }
}

function answer(id: string): Block {
  return { id, kind: "answer", text: id, turnId: "current", agentId: "manager", seq: 3, at: "2026-11-02T00:00:00Z" }
}

async function paint() {
  await act(async () => {
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()))
  })
}

describe("Transcript returning to the live edge", () => {
  beforeEach(() => {
    act(() => useApp.setState({ activeId: "scroll-test", turns: [], historyHasMore: false, historyLoading: false }))
  })

  afterEach(() => {
    act(() => useApp.setState({ activeId: undefined, historyLoading: false }))
  })

  // The real hooks must agree about who owns the viewport. A rail target
  // used to remain active after following resumed and reclaim it on growth.
  it.each(["button", "scroll", "send"] as const)(
    "keeps following after returning by %s, even when an old navigation target exists",
    async (mode) => {
      const view = render(<Transcript state={transcript()} loaded onSelectAgent={() => {}} />)
      const root = screen.getByTestId("transcript")
      Object.defineProperty(root, "scrollHeight", { configurable: true, value: 2400 })
      Object.defineProperty(root, "clientHeight", { configurable: true, value: 400 })
      await paint()
      fireEvent.click(screen.getByRole("button", { name: "Earlier request" }))
      expect(root.scrollTop).toBe(0)
      const blocks = [answer("First update")]
      view.rerender(<Transcript state={transcript(blocks)} loaded onSelectAgent={() => {}} />)
      await paint()
      expect(screen.getByTestId("jump-to-latest")).toBeInTheDocument()

      if (mode === "button") {
        fireEvent.click(screen.getByTestId("jump-to-latest"))
      } else if (mode === "scroll") {
        root.scrollTop = 2000
        fireEvent.scroll(root)
      } else {
        blocks.push({ ...answer("New request"), kind: "user", turnId: "next" })
        view.rerender(<Transcript state={transcript(blocks)} loaded onSelectAgent={() => {}} />)
      }
      await paint()
      expect(screen.queryByTestId("jump-to-latest")).not.toBeInTheDocument()

      // Metadata arriving later also retries rail navigation, even without
      // a new token to trigger the transcript's follow effect.
      act(() => useApp.setState({ historyLoading: true }))
      await paint()
      expect(root.scrollTop).toBeGreaterThanOrEqual(2000)
      blocks.push(answer("Later update"))
      view.rerender(<Transcript state={transcript(blocks)} loaded onSelectAgent={() => {}} />)
      await paint()
      expect(root.scrollTop).toBeGreaterThanOrEqual(2000)
      expect(screen.queryByTestId("jump-to-latest")).not.toBeInTheDocument()
    },
  )
})
