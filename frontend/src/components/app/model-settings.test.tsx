import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ModelsTab } from "./model-settings"
import type { Settings } from "@/lib/types"

vi.mock("@/lib/api", () => ({
  api: {
    discoverModels: vi.fn(async () => ({
      models: ["alpha", "beta"],
      context_windows: { alpha: 128000 },
    })),
  },
}))

const base: Settings = {
  server: { addr: "127.0.0.1:1", open_browser: false },
  models: {
    default: "default",
    providers: [
      {
        id: "default",
        label: "Endpoint",
        base_url: "http://endpoint.invalid/v1",
        model: "",
        catalog: [],
        timeout_seconds: 300,
        has_api_key: false,
        ready: false,
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
    review_max_iterations: 1,
    skills_index_max: 1,
    notifications: "on",
  },
  log: { level: "info" },
}

function openDetails(heading: string) {
  fireEvent.click(screen.getByRole("button", { name: `${heading} details` }))
}

describe("ModelsTab", () => {
  it("lists providers collapsed so a second endpoint is not a wall of fields", () => {
    const two: Settings = {
      ...base,
      models: {
        default: "default",
        providers: [
          base.models.providers[0],
          {
            ...base.models.providers[0],
            id: "other",
            label: "Other",
            model: "beta",
            base_url: "http://other.invalid/v1",
          },
        ],
      },
    }
    render(<ModelsTab settings={two} onChange={vi.fn()} />)
    expect(screen.getByRole("button", { name: "Endpoint details" })).toHaveAttribute(
      "aria-expanded",
      "false",
    )
    expect(screen.getByRole("button", { name: "Other details" })).toHaveAttribute(
      "aria-expanded",
      "false",
    )
    expect(screen.getByText("beta")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Add a provider" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Make default" })).toBeTruthy()
    expect(screen.queryByLabelText("Provider")).toBeNull()
    expect(screen.queryByLabelText("Base URL")).toBeNull()
    expect(screen.queryByLabelText("Default model")).toBeNull()
  })

  it("opens one row to edit URL, key, and default", () => {
    render(<ModelsTab settings={base} onChange={vi.fn()} />)
    openDetails("Endpoint")
    expect(screen.getByRole("button", { name: "Endpoint details" })).toHaveAttribute(
      "aria-expanded",
      "true",
    )
    expect(screen.getByLabelText("Provider")).toBeTruthy()
    expect(screen.getByLabelText("Base URL")).toBeTruthy()
    expect(screen.getByLabelText("Default model")).toBeTruthy()
  })

  it("opens a newly added provider so the blank row can be filled", () => {
    const onChange = vi.fn()
    const { rerender } = render(<ModelsTab settings={base} onChange={onChange} />)
    openDetails("Endpoint")
    fireEvent.click(screen.getByRole("button", { name: "Add a provider" }))
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.models.providers).toHaveLength(2)
    expect(next.models.providers[1].id).toBe("provider-2")
    rerender(<ModelsTab settings={next} onChange={onChange} />)
    expect(screen.getByRole("button", { name: "provider-2 details" })).toHaveAttribute(
      "aria-expanded",
      "true",
    )
    expect(screen.getByLabelText("Provider")).toBeTruthy()
    expect(
      screen.getByRole("button", { name: "Endpoint details" }),
    ).toHaveAttribute("aria-expanded", "false")
  })

  it("opens matching rows when search hits a field name", () => {
    render(<ModelsTab settings={base} onChange={vi.fn()} query="timeout" />)
    expect(screen.getByRole("button", { name: "Endpoint details" })).toHaveAttribute(
      "aria-expanded",
      "true",
    )
    expect(screen.getByLabelText("Request timeout (seconds)")).toBeTruthy()
    expect(screen.queryByLabelText("Base URL")).toBeNull()
  })

  it("says a live stream is not cut off", () => {
    render(<ModelsTab settings={base} onChange={vi.fn()} />)
    openDetails("Endpoint")
    expect(screen.getByText(/still streaming is not cut off/i)).toBeTruthy()
  })

  it("fills the default-model dropdown from a discovered catalog", async () => {
    const onChange = vi.fn()
    render(<ModelsTab settings={base} onChange={onChange} />)
    openDetails("Endpoint")
    expect(screen.getByLabelText("Provider")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Add a provider" })).toBeTruthy()
    expect(screen.getByLabelText("Default model")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Discover models" }))
    await waitFor(() => expect(onChange).toHaveBeenCalled())
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.models.providers[0].catalog).toEqual(["alpha", "beta"])
    expect(next.models.providers[0].model).toBe("alpha")
    expect(next.models.providers[0].model_context).toEqual({ alpha: 128000 })
  })

  it("lets a context window be typed when no catalog exists yet", () => {
    const onChange = vi.fn()
    render(<ModelsTab settings={base} onChange={onChange} />)
    openDetails("Endpoint")
    fireEvent.change(screen.getByLabelText("Context window (tokens)"), {
      target: { value: "256000" },
    })
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.models.providers[0].context_window).toBe(256000)
  })

  it("edits each catalog name's window instead of one number for the endpoint", () => {
    const onChange = vi.fn()
    const listed: Settings = {
      ...base,
      models: {
        ...base.models,
        providers: [
          {
            ...base.models.providers[0],
            model: "alpha",
            catalog: ["alpha", "beta"],
            model_context: { alpha: 128000 },
          },
        ],
      },
    }
    render(<ModelsTab settings={listed} onChange={onChange} />)
    openDetails("Endpoint")
    expect(screen.getByLabelText("alpha context window")).toHaveValue(128000)
    expect(screen.getByLabelText("beta context window")).toHaveValue(null)
    fireEvent.change(screen.getByLabelText("beta context window"), {
      target: { value: "32768" },
    })
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.models.providers[0].model_context).toEqual({
      alpha: 128000,
      beta: 32768,
    })
    expect(next.models.providers[0].context_window ?? 0).toBe(0)
    fireEvent.change(screen.getByLabelText("Fallback for other names"), {
      target: { value: "8000" },
    })
    expect(
      (onChange.mock.calls[1][0] as Settings).models.providers[0].context_window,
    ).toBe(8000)
  })

  it("asks before removing a provider", () => {
    const onChange = vi.fn()
    const two: Settings = {
      ...base,
      models: {
        default: "default",
        providers: [
          base.models.providers[0],
          {
            ...base.models.providers[0],
            id: "other",
            label: "Other",
            model: "beta",
            base_url: "http://other.invalid/v1",
          },
        ],
      },
    }
    render(<ModelsTab settings={two} onChange={onChange} />)
    fireEvent.click(screen.getByRole("button", { name: "Remove Other" }))
    expect(onChange).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(onChange).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Remove Other" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove provider" }))
    expect(onChange).toHaveBeenCalled()
    expect((onChange.mock.calls[0][0] as Settings).models.providers.map((p) => p.id)).toEqual([
      "default",
    ])
  })
})
