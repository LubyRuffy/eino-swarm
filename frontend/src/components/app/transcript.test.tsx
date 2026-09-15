import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { Heartbeat, WaitProgress, waitAgentIds } from "./transcript"
import type { AgentState, Block, Pulse } from "@/lib/transcript"

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

  // A silent row is only reassuring if it says how long it has been silent, and
  // the age has to come from the server rather than the browser's clock.
  it("ages each row from the latest pulse", () => {
    const agents = {
      "a-1": agent({ id: "a-1", role: "researcher", status: "running", activity: "web_fetch" }),
    }
    render(
      <WaitProgress
        block={waitBlock(`{"agent_ids":["a-1"]}`)}
        agents={agents}
        pulse={{
          at: new Date().toISOString(),
          elapsedMs: 90000,
          agents: [{ agentId: "a-1", status: "running", elapsedMs: 42000 }],
        }}
        onSelect={() => {}}
      />,
    )
    expect(screen.getByText("42s")).toBeInTheDocument()
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

describe("Heartbeat", () => {
  afterEach(() => vi.useRealTimers())

  function pulse(partial: Partial<Pulse> = {}): Pulse {
    return { at: new Date().toISOString(), elapsedMs: 65000, agents: [], ...partial }
  }

  // The user's complaint that started this: a swarm that streams nothing looks
  // dead. The pulse is the proof it is not.
  it("says how long the run has been going and how many agents are on it", () => {
    render(<Heartbeat pulse={pulse()} running workers={2} />)
    expect(screen.getByText(/Working for 1m 5s/)).toBeInTheDocument()
    expect(screen.getByText(/2 sub-agents running/)).toBeInTheDocument()
  })

  // A number that only moves every few seconds reads as a frozen screen, which
  // is the thing being fixed, so the clock ticks locally between pulses.
  it("keeps ticking between pulses", () => {
    vi.useFakeTimers()
    render(<Heartbeat pulse={pulse({ elapsedMs: 5000 })} running workers={0} />)
    expect(screen.getByText(/Working for 5s/)).toBeInTheDocument()
    act(() => void vi.advanceTimersByTime(3000))
    expect(screen.getByText(/Working for 8s/)).toBeInTheDocument()
  })

  // The manager alone in one long model call has no sub-agents to count, and
  // that is exactly when the line matters most.
  it("drops the agent count when the manager is working alone", () => {
    render(<Heartbeat pulse={pulse()} running workers={0} />)
    expect(screen.queryByText(/sub-agent/)).not.toBeInTheDocument()
  })

  it("shows nothing once the turn is over", () => {
    const { container } = render(<Heartbeat pulse={pulse()} running={false} workers={2} />)
    expect(container).toBeEmptyDOMElement()
  })

  // Before the first pulse arrives there is nothing honest to show.
  it("shows nothing until a pulse has arrived", () => {
    const { container } = render(<Heartbeat running workers={0} />)
    expect(container).toBeEmptyDOMElement()
  })
})
