import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { SearchSettingsPanel } from "./settings-search"
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
        catalog: ["alpha", "named-embed"],
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

describe("SearchSettingsPanel", () => {
  it("hides the model picker while semantic search is off", () => {
    render(<SearchSettingsPanel settings={base} onChange={vi.fn()} />)
    expect(screen.getByLabelText("Semantic search")).toBeInTheDocument()
    expect(screen.queryByLabelText("Embedding model")).toBeNull()
  })

  it("turns semantic search on without inventing a model name", () => {
    const onChange = vi.fn()
    render(<SearchSettingsPanel settings={base} onChange={onChange} />)
    fireEvent.click(screen.getByLabelText("Semantic search"))
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.search?.embedding).toBe(true)
    expect(next.search?.embedding_model).toBe("")
  })

  it("pins the named embedding model once the switch is on", () => {
    const onChange = vi.fn()
    render(
      <SearchSettingsPanel
        settings={{ ...base, search: { embedding: true, embedding_provider: "", embedding_model: "" } }}
        onChange={onChange}
      />,
    )
    fireEvent.click(screen.getByLabelText("Embedding model"))
    fireEvent.click(screen.getByRole("option", { name: "named-embed" }))
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.search?.embedding_model).toBe("named-embed")
    expect(next.search?.embedding_provider).toBe("default")
  })

  it("lets the user type a model name when the catalog is empty", () => {
    const onChange = vi.fn()
    render(
      <SearchSettingsPanel
        settings={{
          ...base,
          models: {
            ...base.models,
            providers: [{ ...base.models.providers[0], catalog: [], model: "" }],
          },
          search: {
            embedding: true,
            embedding_provider: "",
            embedding_model: "",
          },
        }}
        onChange={onChange}
      />,
    )
    fireEvent.change(screen.getByLabelText("Embedding model"), {
      target: { value: "named-embed" },
    })
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.search?.embedding_model).toBe("named-embed")
  })
})
