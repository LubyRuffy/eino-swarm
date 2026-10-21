import { fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { AgentFailure } from "./agent-failure"
import { RightPanel } from "./panel"
import { emptyTranscript, type AgentState } from "@/lib/transcript"
import { OUTPUT_BUDGET_ERROR } from "@/lib/output-budget"
import { useApp } from "@/store/app"
import { useSettingsSheet } from "@/store/settings-sheet"

const timeout = "swarm: agent writer-1 exceeded timeout 10m0s (1 consecutive watchdog failures): context deadline exceeded"

function worker(error?: string): AgentState {
  return { id: "writer-1", role: "writer", status: "failed", activity: "failed", blocks: [], error }
}

const noop = { onTabChange: vi.fn(), onSelectAgent: vi.fn(), files: [], workspace: "", turns: [],
  onUpload: async () => {}, onDeleteFile: vi.fn(), onRefreshFiles: vi.fn(), onReveal: vi.fn() }

beforeEach(() => {
  useApp.setState({ locale: "en" })
  useSettingsSheet.setState({ open: false, section: "general", focus: "", budgetReturn: null })
})

describe("agent failure details", () => {
  it("explains the recorded watchdog limit and opens that exact setting", () => {
    render(<AgentFailure agent={worker(timeout)} />)
    const card = screen.getByRole("alert")
    expect(card).toHaveTextContent("Overall runtime limit reached (10m0s).")
    expect(card).toHaveTextContent(timeout)
    expect(card).toHaveTextContent("Changing the limit does not resume a stopped agent.")
    fireEvent.click(screen.getByRole("button", { name: "Sub-agent timeout" }))
    expect(useSettingsSheet.getState()).toMatchObject({ open: true, section: "swarm", focus: "agent_timeout_seconds" })
  })

  it("opens the existing completion setting without arming a manager retry", () => {
    render(<AgentFailure agent={worker(OUTPUT_BUDGET_ERROR)} />)
    fireEvent.click(screen.getByRole("button", { name: "Max completion tokens" }))
    expect(useSettingsSheet.getState()).toMatchObject({ open: true, section: "swarm", focus: "max_completion_tokens", budgetReturn: null })
  })

  it("links an ADK iteration stop to the worker's tool-round setting", () => {
    render(<AgentFailure agent={worker("exceeds max iterations")} />)
    expect(screen.getByRole("alert")).toHaveTextContent("Sub-agent tool-round limit reached.")
    fireEvent.click(screen.getByRole("button", { name: "Sub-agent tool rounds" }))
    expect(useSettingsSheet.getState()).toMatchObject({ open: true, section: "swarm", focus: "max_turns" })
  })

  it("opens Models for a provider stream-idle timeout without guessing the provider field", () => {
    render(<AgentFailure agent={worker("provider: stream idle timeout after 2m0s")} />)
    expect(screen.getByRole("alert")).toHaveTextContent("The model service stopped sending data for 2m0s.")
    expect(screen.getByRole("alert")).toHaveTextContent("the provider used by this task")
    fireEvent.click(screen.getByRole("button", { name: "Model timeout settings" }))
    expect(useSettingsSheet.getState()).toMatchObject({ open: true, section: "models", focus: "" })
  })

  it("identifies a parent turn failure without attributing its setting to the worker", () => {
    render(<AgentFailure agent={{ ...worker(OUTPUT_BUDGET_ERROR), errorSource: "turn" }} />)
    expect(screen.getByRole("alert")).toHaveTextContent("Stopped because this turn failed.")
    expect(screen.queryByRole("button")).not.toBeInTheDocument()
  })

  it.each(["provider unavailable", "context deadline exceeded"])("keeps an unknown error visible without a speculative settings link: %s", (error) => {
    render(<AgentFailure agent={worker(error)} />)
    expect(screen.getByRole("alert")).toHaveTextContent(error)
    expect(screen.queryByRole("button")).not.toBeInTheDocument()
  })

  it.each(["en", "zh"] as const)("makes a missing recorded reason explicit in %s", (locale) => {
    useApp.setState({ locale })
    render(<AgentFailure agent={worker()} />)
    expect(screen.getByRole("alert")).toHaveTextContent(locale === "en"
      ? "No failure reason was recorded for this agent."
      : "未记录这个子代理的失败原因。")
    expect(screen.queryByRole("button")).not.toBeInTheDocument()
  })

  it.each(["running", "done", "cancelled"] as const)("does not claim a %s worker failed", (status) => {
    render(<AgentFailure agent={{ ...worker(), status }} />)
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
  })

  it("offers a visible cause entry on the existing failed row", () => {
    const agent = worker(timeout)
    const transcript = emptyTranscript()
    transcript.agents[agent.id] = agent
    transcript.agentOrder.push(agent.id)
    const onSelectAgent = vi.fn()
    render(<RightPanel tab="agents" transcript={transcript} {...noop} onSelectAgent={onSelectAgent} />)
    const row = screen.getByRole("button", { name: /writer-1/ })
    expect(row).toHaveTextContent("View failure reason")
    fireEvent.click(row)
    expect(onSelectAgent).toHaveBeenCalledWith("writer-1")
    expect(row.querySelector("button")).toBeNull()
  })

  it("keeps failure details outside the following log, with bounded raw-error height", () => {
    const agent = worker(timeout)
    const transcript = emptyTranscript()
    transcript.agents[agent.id] = agent
    transcript.agentOrder.push(agent.id)
    render(<RightPanel tab="agents" transcript={transcript} selectedAgent={agent.id} {...noop} />)
    const card = screen.getByRole("alert")
    expect(screen.getByTestId("agent-scroller").contains(card)).toBe(false)
    expect(screen.getByTestId("agent-failure-error")).toHaveClass("max-h-32", "overflow-y-auto")
  })
})
