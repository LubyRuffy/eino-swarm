import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { Transcript } from "./transcript"
import type { TranscriptState } from "@/lib/transcript"
import { emptyTranscript } from "@/lib/transcript"
import { formatMessageTime } from "@/lib/utils"

describe("message clocks", () => {
  // Matching a transcript row to a log line needs the event time on the
  // message itself. The turn footer only has the end of the run.
  it("stamps a finished assistant answer with the event clock", () => {
    const at = "2026-09-21T04:40:00.000Z"
    render(<Transcript state={oneAnswer("done text", at)} loaded onSelectAgent={() => {}} />)
    const stamp = screen.getByTestId("assistant-message-time")
    expect(screen.getByTestId("assistant-message")).toContainElement(stamp)
    expect(stamp).toHaveAttribute("datetime", at)
    expect(stamp).toHaveAttribute("title", at)
    expect(stamp).toHaveTextContent(formatMessageTime(at))
  })

  it("does not invent a clock when the event has no stamp", () => {
    render(<Transcript state={oneAnswer("done text", "")} loaded onSelectAgent={() => {}} />)
    expect(screen.queryByTestId("assistant-message-time")).toBeNull()
  })

  it("does not mount an empty action row when there is nothing to stamp or copy", () => {
    render(
      <Transcript
        state={{
          ...emptyTranscript(),
          agentOrder: ["manager"],
          agents: {
            manager: {
              id: "manager",
              role: "manager",
              status: "done",
              activity: "",
              blocks: [
                {
                  id: "u1",
                  kind: "user",
                  agentId: "manager",
                  text: "",
                  turnId: "t1",
                  seq: 0,
                  at: "",
                },
              ],
            },
          },
        }}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.queryByTestId("message-meta")).toBeNull()
  })

  it("keeps the clock off a pointer until the message is hovered", () => {
    const at = "2026-09-21T04:41:00.000Z"
    render(
      <Transcript
        state={userAndAnswer("next step", "done text", at)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    const userStamp = screen.getByTestId("user-message-time")
    const userMeta = userStamp.closest("[data-testid=message-meta]")
    const answerMeta = screen
      .getByTestId("assistant-message-time")
      .closest("[data-testid=message-meta]")
    expect(userStamp).toHaveAttribute("title", at)
    expect(userMeta).toHaveClass("h-5")
    expect(userMeta).toHaveClass("mt-0.5")
    expect(userMeta).toHaveClass("opacity-0")
    expect(userMeta).toHaveClass("group-hover/msg:opacity-100")
    expect(userMeta).toHaveClass("pointer-events-none")
    expect(userMeta).not.toHaveClass("max-h-0")
    expect(answerMeta).toHaveClass("h-5")
    expect(answerMeta).toHaveClass("opacity-0")
    expect(answerMeta).toHaveClass("group-hover/msg:opacity-100")
    expect(answerMeta).not.toHaveClass("max-h-0")
    expect(screen.getByRole("button", { name: "Copy" })).toHaveClass("size-5")
    expect(screen.getByRole("button", { name: "Copy message" })).toHaveClass("size-5")
  })

  it("does not stamp an earlier answer while a later one is still the response", () => {
    const at = "2026-09-21T04:42:00.000Z"
    render(
      <Transcript
        state={{
          ...emptyTranscript(),
          agentOrder: ["manager"],
          agents: {
            manager: {
              id: "manager",
              role: "manager",
              status: "running",
              activity: "writing",
              blocks: [
                {
                  id: "u1",
                  kind: "user",
                  agentId: "manager",
                  text: "go",
                  turnId: "t1",
                  seq: 1,
                  at,
                },
                {
                  id: "a1",
                  kind: "answer",
                  agentId: "manager",
                  text: "first",
                  turnId: "t1",
                  seq: 2,
                  at,
                },
                {
                  id: "a2",
                  kind: "answer",
                  agentId: "manager",
                  text: "second",
                  turnId: "t1",
                  seq: 3,
                  at,
                },
              ],
            },
          },
          turns: [{ id: "t1", userText: "go", status: "running", agentIds: [] }],
          lastSeq: 3,
        }}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByTestId("user-message-time")).toBeInTheDocument()
    const answers = screen.getAllByTestId("assistant-message")
    expect(answers).toHaveLength(2)
    expect(answers[0]?.querySelector("[data-testid=message-meta]")).toBeNull()
    expect(answers[1]?.querySelector("[data-testid=assistant-message-time]")).not.toBeNull()
  })

  it("holds the response clock until that last answer finishes", () => {
    render(<Transcript state={oneAnswer("live text", "2026-09-21T04:43:00.000Z", true)} loaded onSelectAgent={() => {}} />)
    expect(screen.queryByTestId("assistant-message-time")).toBeNull()
  })
})

function oneAnswer(text: string, at: string, streaming = false): TranscriptState {
  return {
    ...emptyTranscript(),
    running: streaming,
    agentOrder: ["manager"],
    agents: {
      manager: {
        id: "manager",
        role: "manager",
        status: streaming ? "running" : "done",
        activity: streaming ? "writing" : "",
        blocks: [
          {
            id: "a1",
            kind: "answer",
            agentId: "manager",
            text,
            streaming,
            turnId: "t1",
            seq: 1,
            at,
          },
        ],
      },
    },
    turns: [{ id: "t1", userText: "go", status: streaming ? "running" : "done", agentIds: [] }],
    lastSeq: 1,
  }
}

function userAndAnswer(user: string, answer: string, at: string): TranscriptState {
  return {
    ...emptyTranscript(),
    agentOrder: ["manager"],
    agents: {
      manager: {
        id: "manager",
        role: "manager",
        status: "done",
        activity: "",
        blocks: [
          {
            id: "u1",
            kind: "user",
            agentId: "manager",
            text: user,
            turnId: "t1",
            seq: 1,
            at,
          },
          {
            id: "a1",
            kind: "answer",
            agentId: "manager",
            text: answer,
            turnId: "t1",
            seq: 2,
            at,
          },
        ],
      },
    },
    turns: [{ id: "t1", userText: user, status: "done", agentIds: [] }],
    lastSeq: 2,
  }
}
