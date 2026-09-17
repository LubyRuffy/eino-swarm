import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { AgentTranscript, Heartbeat, Transcript, WaitProgress, waitAgentIds } from "./transcript"
import type { AgentState, Block, Pulse, TranscriptState } from "@/lib/transcript"
import { emptyTranscript } from "@/lib/transcript"
import { useApp } from "@/store/app"

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

describe("queued steering", () => {
  const pulse: Pulse = { at: new Date().toISOString(), elapsedMs: 65000, agents: [] }

  function runningWith(blocks: Block[]): TranscriptState {
    return {
      ...emptyTranscript(),
      running: true,
      pulse,
      agentOrder: ["manager"],
      agents: {
        manager: agent({ id: "manager", role: "manager", status: "running", blocks }),
      },
      turns: [{ id: "t1", userText: "go", status: "running", agentIds: [] }],
      lastSeq: blocks.length,
    }
  }

  function row(
    kind: Block["kind"],
    text: string,
    extra: Partial<Block> = {},
  ): Block {
    return {
      id: extra.id ?? `${kind}-${text}`,
      kind,
      agentId: "manager",
      text,
      turnId: "t1",
      seq: extra.seq ?? 1,
      at: extra.at ?? new Date().toISOString(),
      ...extra,
    }
  }

  it("pins unread steering below the working line", () => {
    render(
      <Transcript
        state={runningWith([
          row("user", "look into this", { seq: 1 }),
          row("tool", "exec", {
            seq: 2,
            tool: { callId: "c1", name: "exec", args: "{}", pending: true },
          }),
          row("steer", "focus on the second part", { seq: 3 }),
        ])}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    const heartbeat = screen.getByTestId("heartbeat")
    const queued = screen.getByTestId("queued-steers")
    expect(queued).toHaveTextContent("focus on the second part")
    expect(heartbeat.compareDocumentPosition(queued) & Node.DOCUMENT_POSITION_FOLLOWING).not.toBe(0)
    expect(
      screen.getByTestId("user-message").compareDocumentPosition(heartbeat) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).not.toBe(0)
  })

  it("leaves consumed steering above the answer that followed it", () => {
    render(
      <Transcript
        state={runningWith([
          row("user", "look into this", { seq: 1 }),
          row("tool", "exec", {
            seq: 2,
            tool: { callId: "c1", name: "exec", args: "{}", pending: false, result: "ok" },
          }),
          row("steer", "focus on the second part", { seq: 3 }),
          row("answer", "adjusted", { seq: 4 }),
        ])}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.queryByTestId("queued-steers")).not.toBeInTheDocument()
    const steer = screen.getByTestId("steer")
    expect(
      steer.compareDocumentPosition(screen.getByText("adjusted")) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).not.toBe(0)
  })
})

describe("Transcript follow", () => {
  afterEach(() => {
    useApp.setState({ activeId: undefined })
  })

  function unpinScroller(el: HTMLElement) {
    el.scrollTop = 120
    fireEvent.wheel(el, { deltaY: -40 })
    fireEvent.scroll(el)
  }

  // Replay fills the transcript while the scroller is still the skeleton.
  // Following only blockCount/lastText misses the remount when loaded flips,
  // so a switch used to paint a long history at the top.
  it("lands at the latest turn once a conversation finishes loading", async () => {
    const { rerender } = render(
      <Transcript state={twoTurns()} loaded={false} onSelectAgent={() => {}} />,
    )
    expect(screen.queryByTestId("transcript")).not.toBeInTheDocument()
    rerender(<Transcript state={twoTurns()} loaded onSelectAgent={() => {}} />)
    const el = screen.getByTestId("transcript")
    expect(el.querySelector(".content-column")).not.toBeNull()
    expect(el).toHaveClass("content-gutter")
    mockScrollBox(el, { scrollHeight: 2000, clientHeight: 400 })
    await flushFollow()
    expect(el.scrollTop).toBe(2000)
  })

  // Switching conversations must re-pin even if the previous one had been
  // scrolled up. A reader looking at history in A is not looking at history
  // in B.
  it("re-pins to the bottom when switching conversations", async () => {
    act(() => {
      useApp.setState({ activeId: "th_a" })
    })
    const { rerender } = render(
      <Transcript state={twoTurns()} loaded onSelectAgent={() => {}} />,
    )
    const el = screen.getByTestId("transcript")
    mockScrollBox(el, { scrollHeight: 2000, clientHeight: 400 })
    unpinScroller(el)
    act(() => {
      useApp.setState({ activeId: "th_b" })
      rerender(<Transcript state={emptyTranscript()} loaded={false} onSelectAgent={() => {}} />)
    })
    rerender(<Transcript state={twoTurns()} loaded onSelectAgent={() => {}} />)
    const next = screen.getByTestId("transcript")
    mockScrollBox(next, { scrollHeight: 2000, clientHeight: 400 })
    await flushFollow()
    expect(next.scrollTop).toBe(2000)
  })

  // WKWebView/Chrome fire scroll at 0 when an overflow box first lays out.
  // That used to unpin with no unread, so a switch painted the top and hid
  // Jump to latest.
  it("ignores a layout scroll at the top while the conversation is opening", async () => {
    const { rerender } = render(
      <Transcript state={twoTurns()} loaded={false} onSelectAgent={() => {}} />,
    )
    rerender(<Transcript state={twoTurns()} loaded onSelectAgent={() => {}} />)
    const el = screen.getByTestId("transcript")
    mockScrollBox(el, { scrollHeight: 2000, clientHeight: 400 })
    fireEvent.scroll(el)
    await flushFollow()
    expect(el.scrollTop).toBe(2000)
    expect(screen.queryByTestId("jump-to-latest")).toBeNull()
  })

  // The old 80px slack re-pinned on every token: a wheel-up of 40px never
  // escaped before the next delta yanked the viewport back to the bottom.
  it("does not yank the transcript when the reader wheels up during a stream", async () => {
    const { rerender } = render(
      <Transcript
        state={streamingAnswer("first")}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    const el = screen.getByTestId("transcript")
    mockScrollBox(el, { scrollHeight: 400, clientHeight: 240 })
    unpinScroller(el)
    rerender(
      <Transcript
        state={streamingAnswer("first line grew")}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    await flushFollow()
    expect(el.scrollTop).toBe(120)
  })

  it("hides the jump until new content arrives while unpinned", () => {
    render(
      <Transcript
        state={streamingAnswer("first")}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    const el = screen.getByTestId("transcript")
    mockScrollBox(el, { scrollHeight: 400, clientHeight: 240 })
    unpinScroller(el)
    expect(screen.queryByTestId("jump-to-latest")).toBeNull()
  })

  it("offers a jump to latest after content grows while the reader is up", async () => {
    const { rerender } = render(
      <Transcript
        state={streamingAnswer("first")}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    const el = screen.getByTestId("transcript")
    mockScrollBox(el, { scrollHeight: 400, clientHeight: 240 })
    unpinScroller(el)
    rerender(
      <Transcript
        state={streamingAnswer("first line grew")}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    await flushFollow()
    expect(screen.getByTestId("jump-to-latest")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Jump to latest" }))
    expect(el.scrollTop).toBe(400)
    expect(screen.queryByTestId("jump-to-latest")).toBeNull()
  })

  // Opening a conversation paints a skeleton first; the scroller does not
  // exist on that mount. The listener has to attach when `loaded` flips,
  // not in an empty-deps effect that already ran against a null ref.
  it("unpins after the skeleton leaves", async () => {
    const { rerender } = render(
      <Transcript
        state={streamingAnswer("first")}
        loaded={false}
        onSelectAgent={() => {}}
      />,
    )
    rerender(
      <Transcript
        state={streamingAnswer("first")}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    const el = screen.getByTestId("transcript")
    mockScrollBox(el, { scrollHeight: 400, clientHeight: 240 })
    unpinScroller(el)
    rerender(
      <Transcript
        state={streamingAnswer("first line grew")}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    await flushFollow()
    expect(el.scrollTop).toBe(120)
  })

  it("follows new tokens while the reader stays at the live edge", async () => {
    const { rerender } = render(
      <Transcript
        state={streamingAnswer("first")}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    const el = screen.getByTestId("transcript")
    mockScrollBox(el, { scrollHeight: 400, clientHeight: 240 })
    el.scrollTop = 160
    rerender(
      <Transcript
        state={streamingAnswer("first line grew")}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    await flushFollow()
    expect(el.scrollTop).toBe(400)
  })
})

describe("Transcript turn nav", () => {
  it("anchors each user turn so the rail can jump to it", () => {
    render(<Transcript state={twoTurns()} loaded onSelectAgent={() => {}} />)
    expect(screen.getByText("alpha").closest("[data-turn-nav]")).toHaveAttribute(
      "data-turn-nav",
      "tn_a",
    )
    expect(screen.getByText("beta").closest("[data-turn-nav]")).toHaveAttribute(
      "data-turn-nav",
      "tn_b",
    )
    expect(screen.getByRole("navigation", { name: "Jump to a message" })).toBeInTheDocument()
  })

  it("hides the rail on a single turn", () => {
    const state = twoTurns()
    state.agents.manager = {
      ...state.agents.manager,
      blocks: state.agents.manager.blocks.slice(0, 1),
    }
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.queryByTestId("turn-nav")).not.toBeInTheDocument()
  })
})

describe("live thinking", () => {
  it("sweeps the Thinking label while the thought is still streaming", () => {
    render(
      <Transcript
        state={reasoningState("still working it out", true)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByTestId("marquee")).toHaveTextContent("Thinking")
    expect(screen.getByTestId("marquee")).toHaveAttribute("data-marquee", "shimmer")
  })

  it("drops the sweep once the thought is finished", () => {
    render(
      <Transcript
        state={reasoningState("that is the plan", false)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByText("Thought")).toBeInTheDocument()
    expect(screen.queryByTestId("marquee")).toBeNull()
    expect(screen.queryByTestId("thought-scroll")).toBeNull()
  })

  it("keeps a live thought inside a scrolling box", () => {
    render(
      <Transcript
        state={reasoningState("still working it out", true)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByTestId("thought-scroll")).toBeInTheDocument()
    expect(screen.getByTestId("thought-scroll")).toHaveClass("thought-scroll")
  })

  // The bug: streaming forced the row open, so the chevron was a no-op
  // until the model finished. A wall of thinking you cannot hide is worse
  // than a collapsed live thought.
  it("lets the reader collapse a live thought", () => {
    render(
      <Transcript
        state={reasoningState("still working it out", true)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    fireEvent.click(screen.getByTestId("thought-toggle"))
    expect(screen.queryByTestId("thought-scroll")).toBeNull()
    expect(screen.getByTestId("thought-toggle")).toHaveAttribute("aria-expanded", "false")
  })

  it("lets the reader open a collapsed live thought again", () => {
    render(
      <Transcript
        state={reasoningState("still working it out", true)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    fireEvent.click(screen.getByTestId("thought-toggle"))
    fireEvent.click(screen.getByTestId("thought-toggle"))
    expect(screen.getByTestId("thought-scroll")).toBeInTheDocument()
  })

  it("still auto-collapses when the thought finishes if the reader never clicked", () => {
    const { rerender } = render(
      <Transcript
        state={reasoningState("still working it out", true)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByTestId("thought-scroll")).toBeInTheDocument()
    rerender(
      <Transcript
        state={reasoningState("still working it out", false)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.queryByTestId("thought-scroll")).toBeNull()
    expect(screen.getByText("Thought")).toBeInTheDocument()
  })

  it("keeps a live thought collapsed while more tokens arrive", () => {
    const { rerender } = render(
      <Transcript
        state={reasoningState("first line", true)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    fireEvent.click(screen.getByTestId("thought-toggle"))
    rerender(
      <Transcript
        state={reasoningState("first line\nsecond line", true)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.queryByTestId("thought-scroll")).toBeNull()
  })

  it("opens a finished thought into the same scrolling box", () => {
    render(
      <Transcript
        state={reasoningState("that is the plan", false)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    fireEvent.click(screen.getByText("Thought"))
    expect(screen.getByTestId("thought-scroll")).toBeInTheDocument()
  })

  // Auto-follow lands at the newest tokens; the fade is how you can tell
  // the first lines are still above, not that the thought starts here.
  it("fades the top once earlier lines have scrolled away", () => {
    render(
      <Transcript
        state={reasoningState("still working it out", true)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    const el = screen.getByTestId("thought-scroll")
    mockScrollBox(el, { scrollHeight: 400, clientHeight: 240 })
    el.scrollTop = 80
    fireEvent.scroll(el)
    expect(el).toHaveAttribute("data-overflow-top", "true")
    expect(screen.getByTestId("thought-fade")).toBeInTheDocument()
    expect(screen.getByTestId("thought-fade").parentElement).toHaveClass("isolate")
    el.scrollTop = 0
    fireEvent.scroll(el)
    expect(el).not.toHaveAttribute("data-overflow-top")
    expect(screen.queryByTestId("thought-fade")).toBeNull()
  })

  it("follows new tokens to the bottom of a long thought", async () => {
    const { rerender } = render(
      <Transcript
        state={reasoningState("first line", true)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    const el = screen.getByTestId("thought-scroll")
    mockScrollBox(el, { scrollHeight: 400, clientHeight: 240 })
    rerender(
      <Transcript
        state={reasoningState("first line\nsecond line", true)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    await flushFollow()
    expect(el.scrollTop).toBe(400)
  })

  // Same contract as the transcript scroller: a reader who scrolled up
  // is reading, not waiting to be yanked back to the live edge.
  it("does not yank the thought back when the reader has scrolled up", async () => {
    const { rerender } = render(
      <Transcript
        state={reasoningState("first line", true)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    const el = screen.getByTestId("thought-scroll")
    mockScrollBox(el, { scrollHeight: 400, clientHeight: 240 })
    el.scrollTop = 12
    fireEvent.scroll(el)
    rerender(
      <Transcript
        state={reasoningState("first line\nsecond line", true)}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    await flushFollow()
    expect(el.scrollTop).toBe(12)
  })
})

describe("streaming answers", () => {
  // The bug this pins: a heading used to sit as "## Result" until the turn
  // finished, then snap into a real heading. Streaming has to look like the
  // finished document, just still growing.
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
})

describe("conversation find", () => {
  // Finished thoughts start collapsed. A query that only lives in the body
  // would otherwise report 0 hits because the nodes are not in the DOM.
  it("opens a finished thought when the query is in its body", () => {
    render(
      <Transcript
        state={reasoningState("the hidden needle sits here", false)}
        loaded
        findQuery="needle"
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByTestId("thought-scroll")).toHaveTextContent("hidden needle")
  })

  it("leaves a finished thought collapsed when the query is not in it", () => {
    render(
      <Transcript
        state={reasoningState("the hidden needle sits here", false)}
        loaded
        findQuery="zzz"
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.queryByTestId("thought-scroll")).toBeNull()
  })

  it("does not open a finished thought for a one-character query", () => {
    render(
      <Transcript
        state={reasoningState("the hidden needle sits here", false)}
        loaded
        findQuery="e"
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.queryByTestId("thought-scroll")).toBeNull()
  })

  it("opens a tool payload that holds the query", () => {
    render(
      <Transcript
        state={{
          ...emptyTranscript(),
          agentOrder: ["manager"],
          agents: {
            manager: agent({
              id: "manager",
              role: "manager",
              status: "done",
              blocks: [
                {
                  id: "tool-1",
                  kind: "tool",
                  agentId: "manager",
                  text: "read",
                  tool: {
                    callId: "c1",
                    name: "read",
                    args: `{"file_path":"alpha.txt"}`,
                    result: "payload-unique",
                    pending: false,
                  },
                  turnId: "t1",
                  seq: 1,
                  at: new Date().toISOString(),
                },
              ],
            }),
          },
        }}
        loaded
        findQuery="payload-unique"
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.getByText("payload-unique")).toBeInTheDocument()
  })

  it("reports how many hits are on screen", () => {
    const onFindCount = vi.fn()
    render(
      <Transcript
        state={twoTurns()}
        loaded
        findQuery="alpha"
        onFindCount={onFindCount}
        onSelectAgent={() => {}}
      />,
    )
    expect(onFindCount).toHaveBeenCalledWith(1)
  })
})

function mockScrollBox(
  el: HTMLElement,
  size: { scrollHeight: number; clientHeight: number },
) {
  Object.defineProperty(el, "scrollHeight", {
    configurable: true,
    get: () => size.scrollHeight,
  })
  Object.defineProperty(el, "clientHeight", {
    configurable: true,
    get: () => size.clientHeight,
  })
}

async function flushFollow() {
  await act(async () => {
    await new Promise<void>((resolve) => {
      requestAnimationFrame(() => resolve())
    })
  })
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

function twoTurns(at = new Date().toISOString()): TranscriptState {
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
            id: "b1",
            kind: "user",
            agentId: "manager",
            text: "alpha",
            turnId: "tn_a",
            seq: 1,
            at,
          },
          {
            id: "b2",
            kind: "user",
            agentId: "manager",
            text: "beta",
            turnId: "tn_b",
            seq: 2,
            at,
          },
        ],
      },
    },
  }
}
