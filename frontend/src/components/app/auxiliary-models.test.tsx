import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { AuxiliaryModels } from "./auxiliary-models"
import type { Settings } from "@/lib/types"

const base: Settings = {
  server: { addr: "127.0.0.1:1", open_browser: false },
  models: {
    default: "default",
    providers: [
      {
        id: "default",
        label: "Main",
        base_url: "http://endpoint.invalid/v1",
        model: "alpha",
        catalog: ["alpha", "beta"],
        timeout_seconds: 300,
        has_api_key: false,
        ready: true,
      },
    ],
  },
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

describe("AuxiliaryModels", () => {
  it("hides the picker when there is only one named model", () => {
    const one: Settings = {
      ...base,
      models: {
        ...base.models,
        providers: [{ ...base.models.providers[0], catalog: ["alpha"], model: "alpha" }],
      },
    }
    render(<AuxiliaryModels settings={one} onChange={vi.fn()} />)
    expect(screen.getByText("Title generation")).toBeTruthy()
    expect(screen.getByText("Compact summary")).toBeTruthy()
    expect(screen.queryByLabelText("Title generation model")).toBeNull()
    expect(screen.queryByLabelText("Compact summary model")).toBeNull()
  })

  it("pins a catalog name instead of following the conversation", () => {
    const onChange = vi.fn()
    render(<AuxiliaryModels settings={base} onChange={onChange} />)
    fireEvent.click(screen.getByLabelText("Title generation model"))
    fireEvent.click(screen.getByRole("option", { name: "beta" }))
    expect(onChange).toHaveBeenCalled()
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.swarm.title_provider).toBe("default")
    expect(next.swarm.title_model).toBe("beta")
  })

  it("clears a pin back to automatic", () => {
    const onChange = vi.fn()
    const pinned: Settings = {
      ...base,
      swarm: { ...base.swarm, title_provider: "default", title_model: "beta" },
    }
    render(<AuxiliaryModels settings={pinned} onChange={onChange} />)
    expect(screen.getByLabelText("Title generation model").textContent).toContain("beta")
    fireEvent.click(screen.getByLabelText("Title generation model"))
    fireEvent.click(
      screen.getByRole("option", { name: "Automatic · this conversation's model" }),
    )
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.swarm.title_provider).toBe("")
    expect(next.swarm.title_model).toBe("")
  })

  it("pins a compact summarizer independently of the namer", () => {
    const onChange = vi.fn()
    render(<AuxiliaryModels settings={base} onChange={onChange} />)
    fireEvent.click(screen.getByLabelText("Compact summary model"))
    fireEvent.click(screen.getByRole("option", { name: "beta" }))
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.swarm.compact_provider).toBe("default")
    expect(next.swarm.compact_model).toBe("beta")
  })
})
