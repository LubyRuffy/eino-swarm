import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { WorkFold } from "./work-fold"
import type { Block } from "@/lib/transcript"

function block(partial: Partial<Block> & { kind: Block["kind"] }): Block {
  return {
    id: partial.id ?? partial.kind,
    agentId: "manager",
    text: partial.text ?? "",
    turnId: "t1",
    seq: partial.seq ?? 1,
    at: "2026-01-01T00:00:00.000Z",
    ...partial,
  }
}

function paint(b: Block) {
  return <div data-testid={`row-${b.id}`}>{b.kind}</div>
}

describe("WorkFold live ticker", () => {
  it("marquees the latest thinking line while a thought is streaming", () => {
    render(
      <WorkFold
        running
        renderBlock={paint}
        blocks={[
          block({
            id: "r",
            kind: "reasoning",
            text: "first pass\nthen a closer look",
            streaming: true,
          }),
        ]}
      />,
    )
    expect(screen.getByTestId("swap-line")).toHaveTextContent("then a closer look")
    expect(screen.getByTestId("marquee")).toHaveAttribute("data-marquee", "shimmer")
  })

  it("falls back to Thinking when the thought has no line yet", () => {
    render(
      <WorkFold
        running
        renderBlock={paint}
        blocks={[block({ id: "r", kind: "reasoning", text: "", streaming: true })]}
      />,
    )
    expect(screen.getByTestId("swap-line")).toHaveTextContent("Thinking")
  })

  it("says Planning next moves in the gap between model turns", () => {
    render(
      <WorkFold
        running
        renderBlock={paint}
        blocks={[
          block({ id: "r", kind: "reasoning", text: "first pass" }),
          block({
            id: "k",
            kind: "tool",
            tool: {
              callId: "c",
              name: "read",
              args: `{"file_path":"notes.md"}`,
              pending: false,
            },
          }),
        ]}
      />,
    )
    expect(screen.getByTestId("swap-line")).toHaveTextContent("Planning next moves")
    expect(screen.getByTestId("marquee")).toBeInTheDocument()
  })

  it("reads a live file tool as Editing / Reading / Exec", () => {
    const { rerender } = render(
      <WorkFold
        running
        renderBlock={paint}
        blocks={[
          block({
            id: "ed",
            kind: "tool",
            tool: {
              callId: "c",
              name: "edit",
              args: `{"file_path":"src/lib/appearance.ts"}`,
              pending: true,
            },
          }),
        ]}
      />,
    )
    expect(screen.getByTestId("swap-line")).toHaveTextContent("Editing appearance.ts")

    rerender(
      <WorkFold
        running
        renderBlock={paint}
        blocks={[
          block({
            id: "rd",
            kind: "tool",
            tool: {
              callId: "c",
              name: "read",
              args: `{"file_path":"frontend/src/index.css"}`,
              pending: true,
            },
          }),
        ]}
      />,
    )
    expect(screen.getByTestId("swap-line")).toHaveTextContent("Reading index.css")

    rerender(
      <WorkFold
        running
        renderBlock={paint}
        blocks={[
          block({
            id: "ex",
            kind: "tool",
            tool: {
              callId: "c",
              name: "exec",
              args: `{"command":"printf x"}`,
              pending: true,
            },
          }),
        ]}
      />,
    )
    expect(screen.getByTestId("swap-line")).toHaveTextContent("Exec printf x")
    expect(screen.getByTestId("marquee")).toHaveAttribute("data-marquee", "shimmer")
  })

  it("keeps the finished fold as a count, not a live ticker", () => {
    render(
      <WorkFold
        renderBlock={paint}
        blocks={[
          block({ id: "r", kind: "reasoning", text: "first pass" }),
          block({
            id: "k",
            kind: "tool",
            tool: {
              callId: "c",
              name: "read",
              args: `{"file_path":"notes.md"}`,
              pending: false,
            },
          }),
        ]}
      />,
    )
    expect(screen.queryByTestId("swap-line")).toBeNull()
    expect(screen.getByTestId("work-fold")).toHaveTextContent("Thought · 1 tool")
  })

  it("does not keep spinning after the turn stops, even if a tool is still pending", () => {
    render(
      <WorkFold
        renderBlock={paint}
        blocks={[
          block({
            id: "ex",
            kind: "tool",
            tool: {
              callId: "c",
              name: "exec",
              args: `{"command":"sleep 30"}`,
              pending: true,
            },
          }),
        ]}
      />,
    )
    expect(screen.queryByTestId("swap-line")).toBeNull()
    expect(screen.getByTestId("work-fold").querySelector(".animate-spin")).toBeNull()
  })
})
