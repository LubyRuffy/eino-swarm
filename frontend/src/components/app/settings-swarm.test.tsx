import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { SwarmTab } from "./settings-swarm"
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
  it("labels the three schedule fields and shows repaired defaults", () => {
    render(<SwarmTab settings={base} onChange={() => undefined} />)
    expect(screen.getByLabelText("Shortest wait (seconds)")).toHaveValue(30)
    expect(screen.getByLabelText("Ticker interval (ms)")).toHaveValue(1000)
    expect(screen.getByLabelText("Active waits at once")).toHaveValue(32)
  })
})
