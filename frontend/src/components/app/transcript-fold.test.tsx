import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { Transcript } from "./transcript"
import type { TranscriptState } from "@/lib/transcript"
import { emptyTranscript } from "@/lib/transcript"
import { useApp } from "@/store/app"

function mixedWorkState(): TranscriptState {
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
            id: "r1",
            kind: "reasoning",
            agentId: "manager",
            text: "first pass\nthen a closer look",
            turnId: "t1",
            seq: 1,
            at: new Date().toISOString(),
          },
          {
            id: "k1",
            kind: "tool",
            agentId: "manager",
            text: "read",
            tool: {
              callId: "c1",
              name: "read",
              args: `{"file_path":"notes.md"}`,
              pending: false,
            },
            turnId: "t1",
            seq: 2,
            at: new Date().toISOString(),
          },
          {
            id: "a1",
            kind: "answer",
            agentId: "manager",
            text: "here is the result",
            turnId: "t1",
            seq: 3,
            at: new Date().toISOString(),
          },
        ],
      },
    },
    turns: [
      {
        id: "t1",
        userText: "ask",
        status: "done",
        agentIds: [],
      },
    ],
  }
}

function reasoningState(text: string, streaming: boolean): TranscriptState {
  return {
    ...emptyTranscript(),
    running: streaming,
    agentOrder: ["manager"],
    agents: {
      manager: {
        id: "manager",
        role: "manager",
        status: streaming ? "running" : "done",
        activity: streaming ? "thinking" : "",
        blocks: [
          {
            id: "r1",
            kind: "reasoning",
            agentId: "manager",
            text,
            streaming,
            turnId: "t1",
            seq: 1,
            at: new Date().toISOString(),
          },
        ],
      },
    },
    turns: [
      {
        id: "t1",
        userText: "go",
        status: streaming ? "running" : "done",
        agentIds: [],
      },
    ],
    lastSeq: 1,
  }
}

describe("user transcript mode", () => {
  afterEach(() => {
    cleanup()
    useApp.setState({ transcriptMode: "user" })
  })

  it("folds thinking and tools behind one row and keeps the answer", () => {
    render(
      <Transcript
        state={mixedWorkState()}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByTestId("work-fold")).toBeInTheDocument()
    expect(screen.queryByTestId("thought-toggle")).toBeNull()
    expect(screen.queryByText("read")).toBeNull()
    expect(screen.getByText("here is the result")).toBeInTheDocument()
  })

  it("tickers the latest thinking line while the turn is live", () => {
    render(
      <Transcript
        state={reasoningState("first pass\nthen a closer look", true)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByTestId("work-fold")).toBeInTheDocument()
    expect(screen.getByTestId("swap-line")).toHaveTextContent("then a closer look")
    expect(screen.queryByTestId("thought-scroll")).toBeNull()
  })

  it("expands into the existing thought and tool rows", () => {
    render(
      <Transcript
        state={mixedWorkState()}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    fireEvent.click(screen.getByTestId("work-fold"))
    expect(screen.getByTestId("thought-toggle")).toBeInTheDocument()
    expect(screen.getByText("read")).toBeInTheDocument()
  })

  it("keeps a mid-turn answer on screen without opening the fold", () => {
    const state = mixedWorkState()
    const manager = state.agents.manager
    manager.blocks = [
      manager.blocks[0]!,
      manager.blocks[1]!,
      {
        id: "mid",
        kind: "answer",
        agentId: "manager",
        text: "still working the layout",
        turnId: "t1",
        seq: 3,
        at: new Date().toISOString(),
      },
      {
        id: "k2",
        kind: "tool",
        agentId: "manager",
        text: "grep",
        tool: {
          callId: "c2",
          name: "grep",
          args: `{"pattern":"alpha"}`,
          pending: false,
        },
        turnId: "t1",
        seq: 4,
        at: new Date().toISOString(),
      },
      manager.blocks[2]!,
    ]
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.getByText("still working the layout")).toBeInTheDocument()
    expect(screen.getAllByTestId("work-fold")).toHaveLength(2)
    expect(screen.queryByText("read")).toBeNull()
    expect(screen.queryByText("grep")).toBeNull()
    expect(screen.getByText("here is the result")).toBeInTheDocument()
  })

  it("shows every thought and tool row in developer mode", () => {
    useApp.setState({ transcriptMode: "developer" })
    render(
      <Transcript
        state={mixedWorkState()}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.queryByTestId("work-fold")).toBeNull()
    expect(screen.getByTestId("thought-toggle")).toBeInTheDocument()
    expect(screen.getByText("read")).toBeInTheDocument()
  })
})
