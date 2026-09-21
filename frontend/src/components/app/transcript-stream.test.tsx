import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { AgentTranscript } from "./agent-transcript"
import { Transcript } from "./transcript"
import type { AgentState, TranscriptState } from "@/lib/transcript"
import { emptyTranscript } from "@/lib/transcript"

function agent(partial: Partial<AgentState> & { id: string }): AgentState {
  return {
    role: partial.id,
    status: "running",
    activity: "",
    blocks: [],
    ...partial,
  }
}

function streamingAnswer(text: string): TranscriptState {
  return {
    ...emptyTranscript(),
    running: true,
    agentOrder: ["manager"],
    agents: {
      manager: {
        id: "manager",
        role: "manager",
        status: "running",
        activity: "writing",
        blocks: [
          {
            id: "a1",
            kind: "answer",
            agentId: "manager",
            text,
            streaming: true,
            turnId: "t1",
            seq: 1,
            at: new Date().toISOString(),
          },
        ],
      },
    },
    turns: [{ id: "t1", userText: "go", status: "running", agentIds: [] }],
    lastSeq: 1,
  }
}

describe("streaming answers", () => {
  // A heading used to sit as "## Result" until the turn finished. Streaming
  // has to look like the finished document, just still growing.
  it("renders a streaming answer as markdown, not as raw hashes", () => {
    render(
      <Transcript
        state={streamingAnswer("## Result\n\nstill writing")}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByRole("heading", { name: "Result" })).toBeInTheDocument()
    expect(screen.queryByText("## Result")).not.toBeInTheDocument()
  })

  it("renders a worker's streaming answer the same way", () => {
    render(
      <AgentTranscript
        agent={agent({
          id: "researcher-1",
          role: "researcher",
          blocks: [
            {
              id: "a1",
              kind: "answer",
              agentId: "researcher-1",
              text: "## Notes\n\nstill writing",
              streaming: true,
              turnId: "t1",
              seq: 1,
              at: new Date().toISOString(),
            },
          ],
        })}
      />,
    )
    expect(screen.getByRole("heading", { name: "Notes" })).toBeInTheDocument()
    expect(screen.queryByText("## Notes")).not.toBeInTheDocument()
  })

  it("shows a finished worker's result when its tool log is not on this page", () => {
    render(
      <AgentTranscript
        agent={agent({
          id: "worker-1",
          role: "worker",
          status: "done",
          result: "the assigned work is done",
          blocks: [],
        })}
      />,
    )
    expect(screen.getByText("the assigned work is done")).toBeInTheDocument()
    expect(
      screen.queryByText("This agent has not produced anything yet."),
    ).not.toBeInTheDocument()
  })
})
