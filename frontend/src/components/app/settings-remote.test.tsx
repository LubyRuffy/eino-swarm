import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { RemoteTab } from "./settings-remote"
import type { Settings } from "@/lib/types"

vi.mock("@/lib/api", () => ({
  api: {
    remoteStatus: vi.fn(),
    remoteOffer: vi.fn(),
    saveRemoteToken: vi.fn(),
    remoteBindings: vi.fn(),
    revokeRemoteBinding: vi.fn(),
  },
}))

const { api } = await import("@/lib/api")

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
  remote: {
    enabled: true,
    hub_url: "http://127.0.0.1:7780",
    thread_limit: 5,
    summary_chars: 280,
    open_turns: 6,
  },
}

describe("RemoteTab", () => {
  beforeEach(() => {
    vi.mocked(api.remoteStatus).mockResolvedValue({
      enabled: true,
      hub_url: "http://127.0.0.1:7780",
      has_token: true,
      online: true,
      fingerprint: "abcd1234efgh5678",
    })
    vi.mocked(api.remoteBindings).mockResolvedValue([])
    vi.mocked(api.remoteOffer).mockResolvedValue({
      uri: "pairlink:v1:http://127.0.0.1:7780:Ab3xYz9Qmn:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
      pairing_id: "p1",
      png: "data:image/png;base64,aaaa",
      expires_at: new Date(Date.now() + 60_000).toISOString(),
    })
    vi.mocked(api.saveRemoteToken).mockResolvedValue({
      enabled: true,
      hub_url: "http://127.0.0.1:7780",
      has_token: true,
      online: true,
    })
  })

  it("paints a scannable QR from the pairing offer", async () => {
    const onChange = vi.fn()
    render(<RemoteTab settings={base} onChange={onChange} />)
    await waitFor(() =>
      expect(screen.getByLabelText("Hub URL")).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole("button", { name: "Show pairing QR" }))
    await waitFor(() => expect(screen.getByTestId("remote-qr")).toBeInTheDocument())
    const img = screen.getByTestId("remote-qr")
    expect(img).toHaveAttribute("src", "data:image/png;base64,aaaa")
    expect(
      (screen.getByLabelText("Pairing URI") as HTMLInputElement).value,
    ).toMatch(/^pairlink:v1:/)
  })

  it("does not echo a stored host token", async () => {
    render(<RemoteTab settings={base} onChange={vi.fn()} />)
    await waitFor(() =>
      expect(screen.getByLabelText("Host Token")).toBeInTheDocument(),
    )
    expect(screen.getByLabelText("Host Token")).toHaveValue("")
    expect(screen.getByText(/already stored/i)).toBeInTheDocument()
  })
})
