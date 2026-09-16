import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { RightPanel } from "./panel"
import { MANAGER_ID, type AgentState, type TranscriptState } from "@/lib/transcript"

function agent(partial: Partial<AgentState> & { id: string }): AgentState {
  return {
    role: partial.id,
    status: "done",
    activity: "",
    blocks: [],
    ...partial,
  }
}

function transcript(workers: AgentState[]): TranscriptState {
  const agents: TranscriptState["agents"] = {
    [MANAGER_ID]: agent({ id: MANAGER_ID, role: "manager" }),
  }
  for (const w of workers) agents[w.id] = w
  return {
    agentOrder: [MANAGER_ID, ...workers.map((w) => w.id)],
    agents,
    turns: [],
    lastSeq: 1,
    running: false,
  }
}

const noop = {
  onTabChange: vi.fn(),
  onSelectAgent: vi.fn(),
  files: [],
  workspace: "",
  turns: [],
  onUpload: async () => {},
  onDeleteFile: () => {},
  onRefreshFiles: () => {},
  onReveal: () => {},
}

describe("Agents tab chrome", () => {
  // A sticky bar inside the scroller lets the transcript paint through the
  // back button the moment you drag-scroll. The chrome has to be a sibling.
  it("keeps the back row out of the scroller", () => {
    const worker = agent({
      id: "worker-1",
      role: "worker",
      blocks: [
        {
          id: "b1",
          kind: "answer",
          agentId: "worker-1",
          text: "a body long enough to scroll",
          turnId: "t1",
          seq: 1,
          at: new Date().toISOString(),
        },
      ],
    })
    render(
      <RightPanel
        tab="agents"
        transcript={transcript([worker])}
        selectedAgent="worker-1"
        {...noop}
      />,
    )
    const chrome = screen.getByTestId("agent-chrome")
    const scroller = screen.getByTestId("agent-scroller")
    expect(chrome).not.toHaveClass("sticky")
    expect(chrome.parentElement).toBe(scroller.parentElement)
    expect(scroller).toHaveClass("overflow-y-auto")
    expect(scroller).toHaveAttribute("data-quote-source")
    expect(scroller).toHaveTextContent("a body long enough to scroll")
    expect(chrome).not.toHaveTextContent("a body long enough to scroll")
  })

  it("returns to the roster from the labelled back control", () => {
    const onSelectAgent = vi.fn()
    render(
      <RightPanel
        tab="agents"
        transcript={transcript([agent({ id: "worker-1", role: "worker" })])}
        selectedAgent="worker-1"
        {...noop}
        onSelectAgent={onSelectAgent}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Back to agents" }))
    expect(onSelectAgent).toHaveBeenCalledWith(undefined)
  })

  it("opens the worker's system prompt from the chrome", () => {
    const prompt = "## Environment\n\ndo the assigned work"
    render(
      <RightPanel
        tab="agents"
        transcript={transcript([
          agent({ id: "worker-1", role: "worker", instruction: prompt }),
        ])}
        selectedAgent="worker-1"
        {...noop}
      />,
    )
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "View system prompt" }))
    const dialog = screen.getByRole("dialog")
    expect(dialog).toHaveTextContent("System prompt")
    expect(dialog).toHaveTextContent("## Environment")
    expect(dialog).toHaveTextContent("do the assigned work")
    fireEvent.click(screen.getByRole("button", { name: "Close" }))
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
  })

  it("hides the prompt control when this worker has none recorded", () => {
    render(
      <RightPanel
        tab="agents"
        transcript={transcript([agent({ id: "worker-1", role: "worker" })])}
        selectedAgent="worker-1"
        {...noop}
      />,
    )
    expect(
      screen.queryByRole("button", { name: "View system prompt" }),
    ).not.toBeInTheDocument()
  })

  it("opens a worker from the roster", () => {
    const onSelectAgent = vi.fn()
    render(
      <RightPanel
        tab="agents"
        transcript={transcript([agent({ id: "worker-1", role: "worker" })])}
        {...noop}
        onSelectAgent={onSelectAgent}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: /worker/ }))
    expect(onSelectAgent).toHaveBeenCalledWith("worker-1")
  })
})

describe("inactive tab panels", () => {
  // The Agents pane is a flex column so the worker log can scroll. Tailwind's
  // [hidden] preflight and .flex have equal specificity, so without !hidden
  // the empty Agents shell kept half the column when Memory was selected.
  it("hides an inactive agents pane hard enough to beat display:flex", () => {
    render(
      <RightPanel tab="agents" transcript={transcript([])} {...noop} />,
    )
    const panel = document.querySelector('[role="tabpanel"]')
    expect(panel?.className).toMatch(/\bflex\b/)
    expect(panel?.className).toMatch(/data-\[state=inactive\]:!hidden/)
  })

  // flex-1 panes default to min-height:auto (content size). Without min-h-0
  // they cannot shrink, overflow-y-auto never engages, and Skills get clipped
  // at the window.
  it("lets Files, Trace and Memory shrink so they can scroll", () => {
    render(<RightPanel tab="files" transcript={transcript([])} {...noop} />)
    const panel = document.querySelector('[role="tabpanel"][data-state="active"]')
    expect(panel?.className).toMatch(/\bmin-h-0\b/)
    expect(panel?.className).toMatch(/overflow-hidden/)
    expect(panel?.className).toMatch(/\bflex\b/)
  })

  it("gives Skills the leftover height in the Memory pane", () => {
    render(
      <RightPanel
        tab="memory"
        transcript={transcript([])}
        memory={{
          project: {
            id: "pj_1",
            name: "Anchored",
            system_prompt: "",
            workdir: "",
            resolved_workdir: "/tmp/ws",
            memory_enabled: true,
            memory_dir: "/tmp/mem",
            created_at: "",
            updated_at: "",
          },
          loading: false,
          onSave: async () => {},
          onDeleteSkill: () => {},
          onRefresh: () => {},
          onReview: () => {},
        }}
        {...noop}
      />,
    )
    const panel = document.querySelector('[role="tabpanel"][data-state="active"]')
    expect(panel?.className).toMatch(/\bflex\b/)
    expect(panel?.className).toMatch(/overflow-hidden/)
    expect(screen.getByTestId("skills-list")).toHaveClass("overflow-auto")
  })

  it("does not leave Files in the layout when Memory is selected", () => {
    render(
      <RightPanel
        tab="memory"
        transcript={transcript([])}
        memory={{
          project: {
            id: "pj_1",
            name: "Anchored",
            system_prompt: "",
            workdir: "",
            resolved_workdir: "/tmp/ws",
            memory_enabled: true,
            memory_dir: "/tmp/mem",
            created_at: "",
            updated_at: "",
          },
          loading: false,
          onSave: async () => {},
          onDeleteSkill: () => {},
          onRefresh: () => {},
          onReview: () => {},
        }}
        {...noop}
        files={[
          {
            path: "ARCHITECTURE.md",
            name: "ARCHITECTURE.md",
            size: 12,
            dir: false,
            modified: "",
          },
        ]}
      />,
    )
    const inactive = document.querySelectorAll('[role="tabpanel"][data-state="inactive"]')
    expect(inactive.length).toBeGreaterThan(0)
    inactive.forEach((el) => {
      expect(el).toHaveAttribute("hidden")
      expect(el).not.toBeVisible()
    })
    const memory = document.querySelector('[role="tabpanel"][data-state="active"]')
    expect(memory?.className).toMatch(/\bbg-card\b/)
    expect(memory?.parentElement?.className).toMatch(/\boverflow-hidden\b/)
  })
})

describe("Side panel resize", () => {
  // The handle sits on top of the agent chrome; a lower z-index used to lose
  // the drag to the back button in that strip.
  it("keeps the drag strip above the agent chrome", () => {
    render(
      <RightPanel
        tab="agents"
        transcript={transcript([])}
        {...noop}
      />,
    )
    const handle = screen.getByRole("separator", { name: "Resize the side panel" })
    expect(handle.className).toMatch(/\bz-20\b/)
  })

  it("widens and narrows from the keyboard", () => {
    const { container } = render(
      <RightPanel tab="agents" transcript={transcript([])} {...noop} />,
    )
    const panel = container.querySelector("aside")
    const handle = screen.getByRole("separator", { name: "Resize the side panel" })
    expect(panel).toHaveStyle({ width: "352px" })
    fireEvent.keyDown(handle, { key: "ArrowLeft" })
    expect(panel).toHaveStyle({ width: "376px" })
    fireEvent.keyDown(handle, { key: "ArrowRight" })
    expect(panel).toHaveStyle({ width: "352px" })
  })

  // The strip is 8px over the transcript. A drag without killing selection
  // paints a blue range through the conversation and the agent log.
  it("does not let a drag become a text selection", () => {
    render(<RightPanel tab="agents" transcript={transcript([])} {...noop} />)
    const handle = screen.getByRole("separator", { name: "Resize the side panel" })
    fireEvent.pointerDown(handle, { button: 0, clientX: 800, pointerId: 1 })
    expect(document.body.style.userSelect).toBe("none")
    const blocked = new Event("selectstart", { cancelable: true })
    document.dispatchEvent(blocked)
    expect(blocked.defaultPrevented).toBe(true)
    fireEvent.pointerUp(window, { button: 0, pointerId: 1 })
    expect(document.body.style.userSelect).toBe("")
  })
})

describe("Trace token usage", () => {
  it("names billed tokens under the turn id", () => {
    render(
      <RightPanel
        {...noop}
        tab="trace"
        transcript={transcript([])}
        turns={[
          {
            id: "tn_1",
            thread_id: "th_1",
            seq: 1,
            status: "done",
            user_text: "go",
            final: "done",
            provider_id: "default",
            model: "m",
            started_at: new Date().toISOString(),
            duration_ms: 12,
          },
        ]}
        usage={{
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
        }}
      />,
    )
    const box = screen.getByTestId("trace-usage")
    expect(box.textContent).toContain("28% context used")
    expect(box.textContent).toContain("71.3K / 256K tokens")
    expect(box.textContent).toContain("This turn billed 12K in · 3.1K out")
  })
})

