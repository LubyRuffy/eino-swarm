import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import type { Settings } from "@/lib/types"

import { MemorySettings } from "./memory-settings"

const settings: Settings = {
  server: { addr: "127.0.0.1:1", open_browser: false },
  models: { default: "default", providers: [] },
  swarm: {
    max_concurrent: 1, agent_timeout_seconds: 1, max_turns: 1,
    manager_max_iterations: 1, progress_interval_seconds: 1,
    delta_coalesce_ms: 1, auto_title: true,
  },
  tools: {
    disabled: [], enabled: [],
    proxy: { http: "", https: "", no_proxy: "" }, web_search_max_results: 8,
  },
  memory: {
    enabled: true, auto_review: true, char_limit: 6000, entry_max: 500,
    review_max_iterations: 8, skills_index_max: 50, notifications: "on",
  },
  log: { level: "info" },
}

describe("Memory review thinking", () => {
  it("inherits the conversation for older settings without the field", () => {
    const onChange = vi.fn()
    render(<MemorySettings settings={settings} onChange={onChange} />)
    expect(screen.getByLabelText("Review thinking")).toHaveTextContent("Inherit conversation")
    expect(onChange).not.toHaveBeenCalled()
  })

  it.each(["Low", "Medium", "High"])("saves %s only for memory review", (label) => {
    const onChange = vi.fn()
    render(<MemorySettings settings={settings} onChange={onChange} />)
    fireEvent.click(screen.getByLabelText("Review thinking"))
    fireEvent.click(screen.getByRole("option", { name: label }))
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.memory).toEqual({ ...settings.memory, review_reasoning_effort: label.toLowerCase() })
    expect(next.swarm).toEqual(settings.swarm)
    expect(next.models).toEqual(settings.models)
  })

  it("can clear an override to restore conversation inheritance", () => {
    const onChange = vi.fn()
    render(<MemorySettings
      settings={{ ...settings, memory: { ...settings.memory, review_reasoning_effort: "low" } }}
      onChange={onChange}
    />)
    expect(screen.getByLabelText("Review thinking")).toHaveTextContent("Low")
    fireEvent.click(screen.getByLabelText("Review thinking"))
    fireEvent.click(screen.getByRole("option", { name: "Inherit conversation" }))
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.memory).toEqual({ ...settings.memory, review_reasoning_effort: "" })
  })
})


describe("Skill age settings", () => {
  it("shows the configured threshold and saves an edited value", () => {
    const onChange = vi.fn()
    const configured = { ...settings, memory: { ...settings.memory, skill_stale_days: 37 } }
    render(<MemorySettings settings={configured} onChange={onChange} />)
    const threshold = screen.getByLabelText("Unviewed skill threshold (days)")
    expect(threshold).toHaveValue(37)
    fireEvent.change(threshold, { target: { value: "45" } })
    expect(onChange).toHaveBeenCalledWith({
      ...configured, memory: { ...configured.memory, skill_stale_days: 45 },
    })
    expect(screen.getByText(/candidates for review.*never deleted automatically/i)).toBeInTheDocument()
  })
})
