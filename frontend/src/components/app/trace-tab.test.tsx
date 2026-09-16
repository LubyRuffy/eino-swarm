import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { TraceTab } from "./trace-tab"
import { MANAGER_ID, type AgentState, type Block, type TranscriptState } from "@/lib/transcript"
import type { Turn, UsageSnapshot } from "@/lib/types"

function block(partial: Partial<Block> & Pick<Block, "id" | "kind">): Block {
  return {
    agentId: MANAGER_ID,
    text: "",
    turnId: "tn_1",
    seq: 1,
    at: "2026-01-01T12:00:00.000Z",
    ...partial,
  }
}

function agent(partial: Partial<AgentState> & { id: string }): AgentState {
  return {
    role: partial.id,
    status: "done",
    activity: "",
    blocks: [],
    ...partial,
  }
}

function state(workers: AgentState[], turn: TranscriptState["turns"][number]): TranscriptState {
  const agents: TranscriptState["agents"] = {
    [MANAGER_ID]: agent({ id: MANAGER_ID, role: "manager" }),
  }
  for (const w of workers) agents[w.id] = w
  return {
    agentOrder: [MANAGER_ID, ...workers.map((w) => w.id)],
    agents,
    turns: [turn],
    lastSeq: 1,
    running: false,
  }
}

function turn(partial: Partial<Turn> = {}): Turn {
  return {
    id: "tn_1",
    thread_id: "th_1",
    seq: 1,
    status: "done",
    user_text: "go",
    final: "ok",
    provider_id: "default",
    model: "some-model",
    started_at: "2026-01-01T12:00:00.000Z",
    duration_ms: 371000,
    ...partial,
  }
}

const usage: UsageSnapshot = {
  context_tokens: 71300,
  context_window: 256000,
  turn: {
    prompt_tokens: 12000,
    completion_tokens: 3100,
    cached_tokens: 0,
    reasoning_tokens: 0,
    total_tokens: 15100,
    calls: 2,
  },
  thread: {
    prompt_tokens: 71300,
    completion_tokens: 3100,
    cached_tokens: 0,
    reasoning_tokens: 0,
    total_tokens: 74400,
    calls: 2,
  },
}

const noisy = state(
  [
    agent({
      id: "worker-1",
      role: "worker",
      blocks: [
        block({
          id: "tool-1",
          kind: "tool",
          agentId: "worker-1",
          at: "2026-01-01T12:00:01.000Z",
          tool: {
            callId: "c1",
            name: "ls",
            args: '{"path":"/tmp"}',
            pending: false,
          },
        }),
      ],
    }),
  ],
  { id: "tn_1", userText: "go", status: "done", agentIds: ["worker-1"] },
)
noisy.agents[MANAGER_ID].blocks = [
  block({
    id: "user-1",
    kind: "user",
    text: "go",
    at: "2026-01-01T12:00:00.000Z",
  }),
  block({
    id: "spawn-1",
    kind: "spawn",
    text: "Started worker",
    at: "2026-01-01T12:00:00.500Z",
    spawn: { agentId: "worker-1", role: "worker" },
  }),
]

describe("Trace tab summary", () => {
  // A long turn used to dump every tool row into the sidebar. The tab is
  // now the bill and the failure, not a live syslog.
  it("names billed tokens without opening the event dump", () => {
    render(
      <TraceTab
        turns={[turn()]}
        transcript={noisy}
        usage={usage}
      />,
    )
    const box = screen.getByTestId("trace-usage")
    expect(box.textContent).toContain("28% context used")
    expect(box.textContent).toContain("71.3K / 256K tokens")
    expect(box.textContent).toContain("This turn billed 12K in · 3.1K out")
    expect(screen.queryByTestId("trace-log")).toBeNull()
    expect(screen.queryByText("ls")).toBeNull()
    expect(screen.queryByText("worker-1")).toBeNull()
  })

  it("shows a failed turn's error in the summary, still with the log folded", () => {
    render(
      <TraceTab
        turns={[turn({ status: "error", error: "provider timed out", duration_ms: 371000 })]}
        transcript={{
          ...noisy,
          turns: [
            {
              id: "tn_1",
              userText: "go",
              status: "error",
              error: "provider timed out",
              agentIds: ["worker-1"],
            },
          ],
        }}
      />,
    )
    expect(screen.getByTestId("trace-status")).toHaveTextContent("failed")
    expect(screen.getByTestId("trace-error")).toHaveTextContent("provider timed out")
    expect(screen.getByText("6m 11s")).toBeInTheDocument()
    expect(screen.getByTestId("trace-log-toggle")).toHaveAttribute("aria-invalid", "true")
    expect(screen.queryByTestId("trace-log")).toBeNull()
    expect(screen.queryByText("ls")).toBeNull()
  })

  it("opens the log on purpose and paints a failed tool in the error colour", () => {
    const withFail = structuredClone(noisy)
    withFail.agents["worker-1"].blocks[0].tool = {
      callId: "c1",
      name: "ls",
      args: '{"path":"/tmp"}',
      pending: false,
      failed: true,
    }
    render(<TraceTab turns={[turn()]} transcript={withFail} />)
    fireEvent.click(screen.getByTestId("trace-log-toggle"))
    const log = screen.getByTestId("trace-log")
    expect(log).toHaveTextContent("worker-1")
    expect(log).toHaveTextContent("ls")
    const failed = [...log.querySelectorAll("li")].find((row) =>
      row.textContent?.includes("ls"),
    )
    expect(failed?.className).toMatch(/text-destructive/)
  })

  it("folds the log again when a later turn starts", () => {
    const first = noisy
    const nextTurn = state(
      [],
      { id: "tn_2", userText: "again", status: "running", agentIds: [] },
    )
    nextTurn.agents[MANAGER_ID].blocks = [
      block({
        id: "user-2",
        kind: "user",
        text: "again",
        turnId: "tn_2",
        at: "2026-01-01T12:10:00.000Z",
      }),
    ]
    const { rerender } = render(<TraceTab turns={[turn()]} transcript={first} />)
    fireEvent.click(screen.getByTestId("trace-log-toggle"))
    expect(screen.getByTestId("trace-log")).toBeInTheDocument()
    rerender(
      <TraceTab
        turns={[turn({ id: "tn_2", status: "running", duration_ms: 0 })]}
        transcript={nextTurn}
      />,
    )
    expect(screen.queryByTestId("trace-log")).toBeNull()
  })
})
