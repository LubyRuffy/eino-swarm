import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { Transcript } from "./transcript"
import { sessionPreview, splitSessionBlocks } from "./transcript-session"
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

describe("sessionPreview", () => {
  it("flattens an answer onto one line", () => {
    expect(sessionPreview([block({ kind: "answer", text: "foo\n\nbar   baz" })])).toBe(
      "foo bar baz",
    )
  })
})

describe("splitSessionBlocks", () => {
  it("keeps the user bubble out of the folded work", () => {
    const { leading, work } = splitSessionBlocks([
      block({ id: "u", kind: "user", text: "go", seq: 1 }),
      block({ id: "a", kind: "answer", text: "done", seq: 2 }),
    ])
    expect(leading.map((b) => b.kind)).toEqual(["user"])
    expect(work.map((b) => b.kind)).toEqual(["answer"])
  })
})

describe("goal session fold", () => {
  it("collapses a finished session behind Worked for", () => {
    render(<Transcript state={sessionState()} loaded onSelectAgent={() => {}} />)
    const row = screen.getByTestId("goal-session")
    expect(row).toHaveAttribute("aria-expanded", "false")
    expect(row.querySelector(".whitespace-nowrap")).toHaveTextContent("Worked for 9s")
    expect(row.querySelector(".truncate")).toHaveTextContent("progress so far")
    fireEvent.click(row)
    expect(row).toHaveAttribute("aria-expanded", "true")
    expect(row.querySelector(".truncate")).toBeNull()
    expect(screen.getByText("progress so far")).toBeInTheDocument()
  })

  it("does not mount folded work until the row is opened", () => {
    render(<Transcript state={sessionState()} loaded onSelectAgent={() => {}} />)
    expect(document.querySelector(".md")).toBeNull()
    fireEvent.click(screen.getByTestId("goal-session"))
    expect(document.querySelector(".md")).toBeTruthy()
  })

  it("leaves the user message visible when the work is folded", () => {
    render(
      <Transcript
        state={sessionState({ user: "keep going on the import" })}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByTestId("user-message")).toHaveTextContent("keep going on the import")
    expect(screen.getByTestId("goal-session")).toHaveAttribute("aria-expanded", "false")
    expect(screen.getByTestId("goal-session").querySelector(".truncate")).toHaveTextContent(
      "progress so far",
    )
  })

  it("keeps a long CJK preview on the same line as the duration", () => {
    const wall =
      "两个解包目录都是今天早上生成的且 sql 大小对不上旧记录需要继续核对用户表和附件路径".repeat(4)
    render(
      <Transcript
        state={sessionState({ answer: `XenForo\n\n${wall}` })}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    const row = screen.getByTestId("goal-session")
    expect(row.querySelector(".whitespace-nowrap")).toHaveTextContent("Worked for 9s")
    const preview = row.querySelector(".truncate")
    expect(preview).toHaveClass("truncate")
    expect(preview).toHaveTextContent(/^XenForo /)
    expect(preview?.textContent).not.toMatch(/\n/)
  })

  it("folds when a live session finishes", () => {
    const running: TranscriptState = {
      ...sessionState({ answer: "still working" }),
      running: true,
    }
    running.agents.manager = { ...running.agents.manager, status: "running" }
    running.turns = [{ ...running.turns[0], status: "running", endedAt: undefined }]
    const { rerender } = render(<Transcript state={running} loaded onSelectAgent={() => {}} />)
    expect(screen.queryByTestId("goal-session")).not.toBeInTheDocument()
    rerender(
      <Transcript
        state={sessionState({ answer: "still working" })}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByTestId("goal-session")).toHaveAttribute("aria-expanded", "false")
  })

  it("keeps a running session expanded", () => {
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
              kind: "notice",
              text: "Continuing the standing objective.",
            }),
          ],
        },
      },
      turns: [
        {
          id: "tn_s",
          userText: "",
          status: "running",
          session: true,
          agentIds: [],
        },
      ],
    }
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.queryByTestId("goal-session")).not.toBeInTheDocument()
    expect(screen.getByText("Continuing the standing objective.")).toBeInTheDocument()
  })
})
