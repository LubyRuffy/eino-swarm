// @vitest-environment jsdom

import { fireEvent, render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { KnowledgePageView } from "./knowledge-page"
import type { KnowledgePage } from "@/lib/knowledge"

const page: KnowledgePage = {
  id: "entry",
  title: "Entry",
  type: "item",
  body: "Read [[another|Related page]].",
  updated_at: "2026-11-23T08:00:00Z",
  updated_thread: "thread-one",
  updated_turn: "turn-one",
  sources: [
    { classification: "primary", thread_id: "thread-two", turn_id: "turn-two", kind: "message", seq: 11, available: true },
    { classification: "analysis", thread_id: "thread-three", kind: "event", seq: 23, available: false },
    { uri: "https://example.invalid/source" },
  ],
}

describe("Knowledge page provenance", () => {
  it("only links still-available history and preserves the recorded source classification", () => {
    const onTrace = vi.fn(async () => {})
    const onOpenThread = vi.fn(async () => {})
    render(<KnowledgePageView page={page} onPage={vi.fn()} onTrace={onTrace} onOpenThread={onOpenThread} />)
    expect(screen.getByText("Primary material")).toBeTruthy()
    expect(screen.getByText("Unverified analysis")).toBeTruthy()
    expect(screen.getByText(/Event #23.*Source unavailable/)).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Event #23/ })).toBeNull()
    expect(screen.getByRole("link", { name: "https://example.invalid/source" }).getAttribute("href"))
      .toBe("https://example.invalid/source")

    fireEvent.click(screen.getByRole("button", { name: "Message #11" }))
    fireEvent.click(screen.getByRole("button", { name: "Open Trace" }))
    fireEvent.click(screen.getByRole("button", { name: "Open conversation" }))
    expect(onTrace.mock.calls).toEqual([["thread-two", "turn-two"], ["thread-one", "turn-one"]])
    expect(onOpenThread.mock.calls).toEqual([["thread-one"]])
  })

  it("opens the body in the wide preview and sends wikilink navigation back to the panel", () => {
    const onPage = vi.fn()
    render(<KnowledgePageView page={page} onPage={onPage} onTrace={vi.fn()} onOpenThread={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: "Expand preview" }))
    const dialog = screen.getByRole("dialog", { name: page.title })
    fireEvent.click(within(dialog).getByRole("button", { name: "Related page" }))
    expect(onPage.mock.calls).toEqual([["another"]])
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("opens the cited excerpt inline before the article is written", () => {
    const onTrace = vi.fn()
    const indexed = {
      ...page,
      body: "",
      wiki: { kind: "entity" as const, category: "topic", document_pending: true },
      wiki_observations: [
        { source: page.sources![0], quote: "An exact excerpt from the available source." },
        { source: page.sources![1], quote: "This former source has been removed." },
      ],
    }
    render(<KnowledgePageView page={indexed} onPage={vi.fn()} onTrace={onTrace} onOpenThread={vi.fn()} />)
    const excerpt = screen.getByText("View cited excerpt")
    const details = excerpt.closest("details")
    expect(details?.hasAttribute("open")).toBe(false)
    fireEvent.click(excerpt)
    expect(details?.hasAttribute("open")).toBe(true)
    expect(within(details!).getByText("An exact excerpt from the available source.")).toBeTruthy()
    expect(screen.queryByText("This former source has been removed.")).toBeNull()
    expect(onTrace).not.toHaveBeenCalled()
  })
})
