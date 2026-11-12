import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import type { Settings } from "@/lib/types"
import { ToolsTab } from "./settings-tools"

const base: Settings = {
  server: { addr: "127.0.0.1:1", open_browser: false },
  models: { default: "", providers: [] },
  swarm: {
    max_concurrent: 1, agent_timeout_seconds: 30, max_turns: 1,
    manager_max_iterations: 1, progress_interval_seconds: 1,
    delta_coalesce_ms: 1, auto_title: false,
  },
  tools: {
    disabled: ["exec"], enabled: [],
    proxy: { http: "", https: "", no_proxy: "" },
    web_search_max_results: 8,
  },
  memory: {
    enabled: false, auto_review: false, char_limit: 1, entry_max: 1,
    review_max_iterations: 1, skills_index_max: 1, notifications: "off",
  },
  log: { level: "info" },
}

describe("account Web tool execution settings", () => {
  it("shows automatic cloud routing for a saved config with no new field", () => {
    render(<ToolsTab settings={base} catalog={[]} onChange={vi.fn()} />)
    expect(screen.getByLabelText("Web tools run on")).toHaveTextContent("Automatic")
    expect(screen.getByText(/Cloud after server sign-in/)).toBeVisible()
  })

  it("lets the user keep Web tools local without changing tool switches", () => {
    const onChange = vi.fn()
    render(<ToolsTab settings={base} catalog={[]} onChange={onChange} />)
    fireEvent.click(screen.getByLabelText("Web tools run on"))
    fireEvent.click(screen.getByRole("option", { name: "This device" }))
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({
      tools: expect.objectContaining({ web_execution: "local", disabled: ["exec"] }),
    }))
  })

  it("retains an explicit cloud selection and exposes the field through search", () => {
    const settings = structuredClone(base)
    Object.assign(settings.tools, { web_execution: "cloud" })
    render(<ToolsTab settings={settings} catalog={[]} onChange={vi.fn()} query="execution" />)
    expect(screen.getByLabelText("Web tools run on")).toHaveTextContent("Server cloud")
  })

  it("hides the local proxy while Web tools are forced onto the server", () => {
    const settings = structuredClone(base)
    settings.tools.proxy = { http: "http://127.0.0.1:9", https: "", no_proxy: "localhost" }
    settings.tools.web_execution = "cloud"
    render(<ToolsTab settings={settings} catalog={[]} onChange={vi.fn()} />)
    expect(screen.queryByRole("heading", { name: "Proxy" })).toBeNull()
    expect(screen.queryByLabelText("HTTP")).toBeNull()
  })

  it("keeps the proxy while routing can still use this device", () => {
    render(<ToolsTab settings={base} catalog={[]} onChange={vi.fn()} />)
    expect(screen.getByRole("heading", { name: "Proxy" })).toBeVisible()
    expect(screen.getByLabelText("HTTP")).toBeInTheDocument()
  })
})
