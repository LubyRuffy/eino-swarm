import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { WorkFold, TurnBlockList } from "./work-fold"
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

describe("TurnBlockList live ticker", () => {
  it("only tickers the latest work group in a live turn", () => {
    render(
      <TurnBlockList
        running
        mode="user"
        renderBlock={paint}
        blocks={[
          block({
            id: "r1",
            kind: "reasoning",
            text: "first pass",
            seq: 1,
          }),
          block({
            id: "k1",
            kind: "tool",
            seq: 2,
            tool: {
              callId: "c1",
              name: "read",
              args: `{"file_path":"notes.md"}`,
              pending: false,
            },
          }),
          block({
            id: "n1",
            kind: "notice",
            text: "Memory updated.",
            seq: 3,
          }),
          block({
            id: "k2",
            kind: "tool",
            seq: 4,
            tool: {
              callId: "c2",
              name: "wait_agents",
              args: `{"agent_ids":["a-1"]}`,
              pending: true,
            },
          }),
        ]}
      />,
    )
    const folds = screen.getAllByTestId("work-fold")
    expect(folds).toHaveLength(2)
    expect(folds[0]).not.toHaveTextContent("Planning next moves")
    expect(folds[0]).toHaveTextContent("Thought · 1 tool")
    expect(folds[1]).toHaveTextContent("wait_agents")
    expect(folds[1].querySelector("[data-testid=swap-line]")).toBeTruthy()
  })

  it("keeps a mid-turn answer visible and splits the fold around it", () => {
    render(
      <TurnBlockList
        mode="user"
        renderBlock={paint}
        blocks={[
          block({ id: "r1", kind: "reasoning", text: "first pass", seq: 1 }),
          block({
            id: "k1",
            kind: "tool",
            seq: 2,
            tool: {
              callId: "c1",
              name: "read",
              args: `{"file_path":"notes.md"}`,
              pending: false,
            },
          }),
          block({ id: "a1", kind: "answer", text: "still working the layout", seq: 3 }),
          block({
            id: "k2",
            kind: "tool",
            seq: 4,
            tool: {
              callId: "c2",
              name: "grep",
              args: `{"pattern":"alpha"}`,
              pending: false,
            },
          }),
          block({ id: "a2", kind: "answer", text: "here is the result", seq: 5 }),
        ]}
      />,
    )
    expect(screen.getByTestId("row-a1")).toBeInTheDocument()
    expect(screen.getByTestId("row-a2")).toBeInTheDocument()
    expect(screen.getAllByTestId("work-fold")).toHaveLength(2)
    expect(screen.queryByTestId("row-r1")).toBeNull()
    expect(screen.queryByTestId("row-k1")).toBeNull()
    expect(screen.queryByTestId("row-k2")).toBeNull()
  })
})
