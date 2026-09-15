import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { WaitProgress, waitAgentIds } from "./transcript"
import type { AgentState, Block } from "@/lib/transcript"

function agent(partial: Partial<AgentState> & { id: string }): AgentState {
  return {
    role: partial.id,
    status: "running",
    activity: "",
    blocks: [],
    ...partial,
  }
}

function waitBlock(args: string): Block {
  return {
    id: "b1",
    kind: "tool",
    agentId: "manager",
    text: "wait_agents",
    tool: { callId: "c1", name: "wait_agents", args, pending: true },
    turnId: "t1",
    seq: 1,
    at: new Date().toISOString(),
  }
}

describe("waitAgentIds", () => {
  it("pulls the ids out of a wait_agents call", () => {
    expect(waitAgentIds(`{"agent_ids":["a-1","b-2"],"timeout_s":60}`)).toEqual(["a-1", "b-2"])
  })

  it("is empty for half-streamed or malformed args", () => {
    expect(waitAgentIds(`{"agent_ids":["a-1"`)).toEqual([])
    expect(waitAgentIds("")).toEqual([])
    expect(waitAgentIds(`{"timeout_s":5}`)).toEqual([])
  })
})

describe("WaitProgress", () => {
  it("shows each waited-on sub-agent and what it is doing right now", () => {
    const agents = {
      "a-1": agent({ id: "a-1", role: "researcher", status: "running", activity: "web_fetch" }),
      "b-2": agent({ id: "b-2", role: "reviewer", status: "done" }),
    }
    render(
      <WaitProgress
        block={waitBlock(`{"agent_ids":["a-1","b-2"],"timeout_s":60}`)}
        agents={agents}
        onSelect={() => {}}
      />,
    )
    // the running one shows its live activity, the finished one its status —
    // the whole point is that the manager thread is no longer a blank "running…"
    expect(screen.getByText("researcher")).toBeInTheDocument()
    expect(screen.getByText("web_fetch")).toBeInTheDocument()
    expect(screen.getByText("reviewer")).toBeInTheDocument()
    expect(screen.getByText(/Waiting for 1 sub-agent/)).toBeInTheDocument()
  })

  it("selects the agent whose row is clicked", () => {
    const onSelect = vi.fn()
    const agents = {
      "a-1": agent({ id: "a-1", role: "researcher", status: "running", activity: "thinking" }),
    }
    render(
      <WaitProgress
        block={waitBlock(`{"agent_ids":["a-1"]}`)}
        agents={agents}
        onSelect={onSelect}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: /researcher/ }))
    expect(onSelect).toHaveBeenCalledWith("a-1")
  })
})
