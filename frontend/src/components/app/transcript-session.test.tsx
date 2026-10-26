import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { Transcript } from "./transcript"
import { splitSessionBlocks, responseClockBlockId } from "./transcript-session"
import type { AgentState, Block, TranscriptState } from "@/lib/transcript"
import { emptyTranscript } from "@/lib/transcript"

function block(partial: Partial<Block> & { kind: Block["kind"]; text: string }): Block {
  return {
    id: partial.id ?? partial.kind,
    agentId: "manager",
    turnId: "tn_s",
    seq: partial.seq ?? 1,
    at: "2026-01-01T00:00:00.000Z",
    ...partial,
  }
}

function sessionState(opts?: { answer?: string; user?: string }): TranscriptState {
  const started = "2026-01-01T00:00:00.000Z"
  const ended = "2026-01-01T00:00:09.000Z"
  const blocks: Block[] = []
  if (opts?.user) blocks.push(block({ id: "u", kind: "user", text: opts.user, seq: 1 }))
  blocks.push(
    block({
      id: "a",
      kind: "answer",
      text: opts?.answer ?? "progress so far",
      seq: 2,
    }),
  )
  const manager: AgentState = {
    id: "manager",
    role: "manager",
    status: "done",
    activity: "",
    blocks,
  }
  return {
    ...emptyTranscript(),
    agentOrder: ["manager"],
    agents: { manager },
    turns: [
      {
        id: "tn_s",
        userText: opts?.user ?? "",
        status: "done",
        startedAt: started,
        endedAt: ended,
        session: true,
        agentIds: [],
      },
    ],
  }
}

describe("responseClockBlockId", () => {
  it("shows a visible completion divider with a timestamp for ordinary and goal sessions", () => {
    const ordinary = sessionState({ answer: "ordinary result" })
    ordinary.turns[0].session = false
    const { rerender } = render(<Transcript state={ordinary} loaded onSelectAgent={() => {}} />)
    expect(screen.getAllByTestId("turn-boundary")).toHaveLength(1)
    expect(screen.getByTestId("turn-boundary").querySelector("time")).toHaveAttribute("dateTime", ordinary.turns[0].endedAt)
    expect(screen.getByTestId("turn-boundary")).toHaveClass("border-border")
    rerender(<Transcript state={sessionState({ answer: "scheduled result" })} loaded onSelectAgent={() => {}} />)
    expect(screen.getAllByTestId("turn-boundary")).toHaveLength(1)
    expect(screen.getByTestId("turn-boundary").querySelector("time")).toHaveAttribute("dateTime", "2026-01-01T00:00:09.000Z")
  })

  it("stamps the last finished answer, not every answer in the turn", () => {
    expect(
      responseClockBlockId([
        block({ id: "u", kind: "user", text: "ask", seq: 1 }),
        block({ id: "a1", kind: "answer", text: "mid", seq: 2 }),
        block({ id: "a2", kind: "answer", text: "end", seq: 3 }),
      ]),
    ).toBe("a2")
  })

  it("waits until the last answer finishes streaming", () => {
    expect(
      responseClockBlockId([
        block({ id: "a1", kind: "answer", text: "mid", seq: 2 }),
        block({ id: "a2", kind: "answer", text: "live", seq: 3, streaming: true }),
      ]),
    ).toBeUndefined()
  })
})

describe("splitSessionBlocks", () => {
  it("keeps the user bubble out of the folded work", () => {
    const { leading, work, trailing } = splitSessionBlocks([
      block({ id: "u", kind: "user", text: "go", seq: 1 }),
      block({ id: "a", kind: "answer", text: "done", seq: 2 }),
    ])
    expect(leading.map((b) => b.kind)).toEqual(["user"])
    expect(work.map((b) => b.kind)).toEqual(["answer"])
    expect(trailing).toEqual([])
  })

  it("keeps a budget-cap notice outside the folded work", () => {
    const { leading, work, trailing } = splitSessionBlocks([
      block({ id: "u", kind: "user", text: "go", seq: 1 }),
      block({ id: "a", kind: "answer", text: "progress so far", seq: 2 }),
      block({
        id: "c",
        kind: "notice",
        text: "Stopped auto-continuing: the standing objective is still open. Press Start on the goal to keep going.",
        seq: 3,
      }),
    ])
    expect(leading.map((b) => b.kind)).toEqual(["user"])
    expect(work.map((b) => b.kind)).toEqual(["answer"])
    expect(trailing.map((b) => b.kind)).toEqual(["notice"])
  })

  it("peels an older idle notice that lacks the resume sentence", () => {
    const { work, trailing } = splitSessionBlocks([
      block({ id: "a", kind: "answer", text: "progress so far", seq: 1 }),
      block({
        id: "n",
        kind: "notice",
        text: "Stopped auto-continuing: the last continuation made no progress.",
        seq: 2,
      }),
    ])
    expect(work.map((b) => b.kind)).toEqual(["answer"])
    expect(trailing.map((b) => b.kind)).toEqual(["notice"])
  })

  it("pins an armed wait after the work so it is not buried in the report", () => {
    const { work, trailing } = splitSessionBlocks([
      block({ id: "u", kind: "user", text: "go", seq: 1 }),
      block({ id: "w", kind: "notice", text: "A wait is armed.", seq: 2 }),
      block({ id: "a", kind: "answer", text: "progress so far", seq: 3 }),
    ])
    expect(work.map((b) => b.kind)).toEqual(["answer"])
    expect(trailing.map((b) => b.text)).toEqual(["A wait is armed."])
  })

  it("leaves a hold notice in the middle of the work", () => {
    const hold =
      "Stopped auto-continuing: the standing objective is still open. Press Start on the goal to keep going."
    const { work, trailing } = splitSessionBlocks([
      block({ id: "n", kind: "notice", text: hold, seq: 1 }),
      block({ id: "a", kind: "answer", text: "progress so far", seq: 2 }),
    ])
    expect(work.map((b) => b.kind)).toEqual(["notice", "answer"])
    expect(trailing).toEqual([])
  })
})

describe("goal session answers", () => {
  it.each([false, true])("keeps completion and the answer visible (before answer=%s)", (beforeAnswer) => {
    const state = sessionState()
    const blocks = state.agents.manager.blocks
    blocks.splice(beforeAnswer ? 0 : blocks.length, 0,
      block({ id: "complete", kind: "notice", text: "Standing objective completed.", seq: beforeAnswer ? 1 : 3 }),
    )
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.queryByTestId("goal-session")).not.toBeInTheDocument()
    expect(screen.getByText("progress so far")).toBeVisible()
    expect(screen.getAllByText("Standing objective completed.")).toHaveLength(1)
  })

  it("shows a finished session answer without opening a session row", () => {
    render(<Transcript state={sessionState()} loaded onSelectAgent={() => {}} />)
    expect(screen.queryByTestId("goal-session")).not.toBeInTheDocument()
    expect(screen.getByText("progress so far")).toBeVisible()
    expect(screen.getByTestId("turn-boundary")).toHaveTextContent("Worked for 9s")
  })

  it("keeps the user request and its whole answer visible together", () => {
    render(
      <Transcript
        state={sessionState({ user: "continue checking the pending changes" })}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByTestId("user-message")).toHaveTextContent("continue checking the pending changes")
    expect(screen.getByText("progress so far")).toBeVisible()
    expect(document.querySelector(".md")).not.toBeNull()
  })

  it("renders a long answer as paragraphs instead of a truncated session preview", () => {
    const paragraph = "完成的结果需要完整保留，下一轮仍然能够直接阅读。".repeat(20)
    render(
      <Transcript
        state={sessionState({ answer: `Current result\n\n${paragraph}` })}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByText("Current result")).toBeVisible()
    expect(screen.getByText(paragraph)).toBeVisible()
    expect(screen.queryByTestId("goal-session")).not.toBeInTheDocument()
  })

  it("keeps every answer visible while mounting process bodies only on demand", () => {
    const state = sessionState({ answer: "first result" })
    state.agents.manager.blocks = [
      block({ id: "r1", kind: "reasoning", text: "first private reasoning", seq: 1 }),
      block({ id: "k1", kind: "tool", text: "read", seq: 2,
        tool: { callId: "c1", name: "read", args: '{"file_path":"first.txt"}', result: "first tool output", pending: false } }),
      block({ id: "a1", kind: "answer", text: "first result", seq: 3 }),
      block({ id: "r2", kind: "reasoning", text: "second private reasoning", seq: 4 }),
      block({ id: "k2", kind: "tool", text: "read", seq: 5,
        tool: { callId: "c2", name: "read", args: '{"file_path":"second.txt"}', result: "second tool output", pending: false } }),
      block({ id: "a2", kind: "answer", text: "second result", seq: 6 }),
    ]
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.getByText("first result")).toBeVisible()
    expect(screen.getByText("second result")).toBeVisible()
    const folds = screen.getAllByTestId("work-fold")
    expect(folds).toHaveLength(2)
    for (const fold of folds) expect(fold).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByTestId("thought-toggle")).not.toBeInTheDocument()
    expect(screen.queryByText("first tool output")).not.toBeInTheDocument()
    fireEvent.click(folds[0])
    expect(screen.getByTestId("thought-toggle")).toBeInTheDocument()
    expect(screen.queryByText("first tool output")).not.toBeInTheDocument()
    expect(screen.getByText("second result")).toBeVisible()
  })

  it("keeps earlier session answers visible as the next session progresses", () => {
    const state = sessionState({ answer: "earlier session result" })
    state.running = true
    state.turns.push({ id: "tn_next", userText: "", status: "running", session: true, agentIds: [] })
    state.agents.manager.blocks.push(
      block({ id: "next-answer", kind: "answer", text: "latest session progress", seq: 3, turnId: "tn_next", streaming: true }),
    )
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.getByText("earlier session result")).toBeVisible()
    expect(screen.getByText("latest session progress")).toBeVisible()
  })

  it("does not hide the answer when a live session finishes", () => {
    const running: TranscriptState = {
      ...sessionState({ answer: "still working" }),
      running: true,
    }
    running.agents.manager = { ...running.agents.manager, status: "running" }
    running.turns = [{ ...running.turns[0], status: "running", endedAt: undefined }]
    const { rerender } = render(<Transcript state={running} loaded onSelectAgent={() => {}} />)
    expect(screen.getByText("still working")).toBeVisible()
    rerender(<Transcript state={sessionState({ answer: "still working" })} loaded onSelectAgent={() => {}} />)
    expect(screen.getByText("still working")).toBeVisible()
    expect(screen.queryByTestId("goal-session")).not.toBeInTheDocument()
  })

  it("keeps both a failed session's partial answer and its actionable error visible", () => {
    const state = sessionState({ answer: "partial result" })
    state.turns = [{ ...state.turns[0], status: "error", error: "the endpoint refused the connection" }]
    state.agents.manager = {
      ...state.agents.manager,
      status: "failed",
      blocks: [...state.agents.manager.blocks,
        block({ id: "e", kind: "error", text: "the endpoint refused the connection", seq: 3 }),
      ],
    }
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.getByText("partial result")).toBeVisible()
    expect(screen.getByText("the endpoint refused the connection")).toBeVisible()
    expect(screen.getByTestId("turn-boundary")).toHaveTextContent("Stopped after 9s")
  })

  it("keeps an armed wait visible after the full answer", () => {
    const state = sessionState()
    state.agents.manager.blocks.unshift(
      block({ id: "w", kind: "notice", text: "A wait is armed.", detail: "sch_ab12", seq: 1 }),
    )
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    const answer = screen.getByText("progress so far")
    const wait = screen.getByTestId("schedule-notice")
    expect(answer).toBeVisible()
    expect(wait).toHaveTextContent("A wait is armed")
    expect(answer.compareDocumentPosition(wait) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it("keeps the budget-cap notice visible beside the full answer", () => {
    const state = sessionState()
    state.agents.manager.blocks.push(
      block({ id: "c", kind: "notice", text: "Stopped auto-continuing: the standing objective is still open. Press Start on the goal to keep going.", seq: 3 }),
    )
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.getByText("progress so far")).toBeVisible()
    expect(screen.getByTestId("memory-notice")).toHaveTextContent("Press Start on the goal")
  })

  it("keeps a running session's notice visible", () => {
    const state = sessionState()
    state.running = true
    state.turns[0] = { ...state.turns[0], status: "running", endedAt: undefined }
    state.agents.manager.blocks = [block({ kind: "notice", text: "Continuing the standing objective." })]
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.queryByTestId("goal-session")).not.toBeInTheDocument()
    expect(screen.getByText("Continuing the standing objective.")).toBeVisible()
  })

  it("does not leave Planning next moves on a turn that is no longer the live one", () => {
    const started = "2026-01-01T00:00:00.000Z"
    const ended = "2026-01-01T00:00:09.000Z"
    const state: TranscriptState = {
      ...emptyTranscript(),
      running: true,
      agentOrder: ["manager"],
      agents: {
        manager: {
          id: "manager",
          role: "manager",
          status: "running",
          activity: "",
          blocks: [
            block({
              id: "r1",
              kind: "reasoning",
              text: "first pass",
              turnId: "tn_old",
              seq: 1,
            }),
            block({
              id: "k1",
              kind: "tool",
              text: "read",
              turnId: "tn_old",
              seq: 2,
              tool: {
                callId: "c1",
                name: "read",
                args: `{"file_path":"notes.md"}`,
                pending: false,
              },
            }),
            block({
              id: "n2",
              kind: "notice",
              text: "Continuing the standing objective.",
              turnId: "tn_live",
              seq: 3,
            }),
            block({
              id: "k2",
              kind: "tool",
              text: "wait_agents",
              turnId: "tn_live",
              seq: 4,
              tool: {
                callId: "c2",
                name: "wait_agents",
                args: `{"agent_ids":["a-1"]}`,
                pending: true,
              },
            }),
          ],
        },
      },
      turns: [
        {
          id: "tn_old",
          userText: "",
          status: "running",
          startedAt: started,
          endedAt: ended,
          session: true,
          agentIds: [],
        },
        {
          id: "tn_live",
          userText: "",
          status: "running",
          session: true,
          agentIds: [],
        },
      ],
    }
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    const folds = screen.getAllByTestId("work-fold")
    expect(folds).toHaveLength(1)
    expect(folds[0]).not.toHaveTextContent("Planning next moves")
    expect(folds[0]).toHaveTextContent("Thought · 1 tool")
    expect(screen.getByText("Collecting sub-agent results")).toBeInTheDocument()
  })
})
