import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { SwarmTab } from "./settings-swarm"
import { ToolsTab } from "./settings-tools"
import type { Settings } from "@/lib/types"

const base: Settings = {
  server: { addr: "127.0.0.1:1", open_browser: false },
  models: { default: "default", providers: [] },
  swarm: {
    max_concurrent: 1,
    agent_timeout_seconds: 1,
    max_turns: 1,
    manager_max_iterations: 1,
    progress_interval_seconds: 1,
    delta_coalesce_ms: 1,
    auto_title: true,
  },
  tools: {
    disabled: [],
    enabled: [],
    proxy: { http: "", https: "", no_proxy: "" },
    web_search_max_results: 8,
  },
  memory: {
    enabled: true,
    auto_review: true,
    char_limit: 1,
    entry_max: 1,
    review_max_iterations: 1,
    skills_index_max: 1,
    notifications: "on",
  },
  log: { level: "info" },
}

describe("Swarm schedule caps", () => {
  it("exposes a stable target for the agent timeout failure link", () => {
    render(<SwarmTab settings={base} onChange={() => undefined} />)
    expect(screen.getByLabelText("Sub-agent timeout (seconds)").closest("[data-settings-key]"))
      .toHaveAttribute("data-settings-key", "agent_timeout_seconds")
  })

  it("exposes a stable target for the agent tool-round failure link", () => {
    render(<SwarmTab settings={base} onChange={() => undefined} />)
    expect(screen.getByLabelText("Sub-agent tool rounds").closest("[data-settings-key]"))
      .toHaveAttribute("data-settings-key", "max_turns")
  })

  it("labels the three schedule fields and shows repaired defaults", () => {
    render(<SwarmTab settings={base} onChange={() => undefined} />)
    expect(screen.getByLabelText("Shortest wait (seconds)")).toHaveValue(30)
    expect(screen.getByLabelText("Ticker interval (ms)")).toHaveValue(1000)
    expect(screen.getByLabelText("Active waits at once")).toHaveValue(32)
  })
})

// The classifiers hold a finished answer until they decide, so their thinking
// level is a separate setting that defaults to Low and can follow the
// conversation only when chosen.
describe("Policy check thinking", () => {
  it("shows Low for settings written before the field and saves inherit explicitly", () => {
    const onChange = vi.fn()
    render(<SwarmTab settings={base} onChange={onChange} />)
    const policy = screen.getByLabelText("Policy check thinking")
    expect(policy).toHaveTextContent("Default (Low)")
    fireEvent.click(policy)
    fireEvent.click(screen.getByRole("option", { name: "Inherit conversation" }))
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.swarm.policy_reasoning_effort).toBe("inherit")
    expect(next.memory).toEqual(base.memory)
  })
})

describe("Tool result limit", () => {
  it("shows the server default when absent and saves an edited cap", () => {
    const onChange = vi.fn()
    render(<ToolsTab settings={base} catalog={[]} onChange={onChange} />)
    const limit = screen.getByLabelText("Tool result limit (characters)")
    expect(limit).toHaveValue(40000)
    fireEvent.change(limit, { target: { value: "12000" } })
    expect(onChange).toHaveBeenCalledWith({ ...base, tools: { ...base.tools, result_max_chars: 12000 } })
  })
})
