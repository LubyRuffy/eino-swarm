import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

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
    event_chars: 4000,
    watch_events: 80,
    keep_awake: true,
    display_name: "desk-one",
  },
}

const boundPhone = {
  id: "b1",
  device_fp: "aa11bb22cc33dd44",
  device: "Phone 1.0 Device",
  created_at: "2026-09-20T16:00:00Z",
  last_seen: "2026-09-20T16:03:32Z",
  session_id: "s1",
}

describe("RemoteTab", () => {
  afterEach(() => {
    vi.useRealTimers()
  })

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

  it("does not offer a pairing QR for binding", async () => {
    const onChange = vi.fn()
    await act(async () => { render(<RemoteTab settings={base} onChange={onChange} />) })
    expect(screen.queryByText(base.remote!.hub_url)).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Advanced / self-hosted server" })).not.toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Pairing QR" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Show pairing QR" })).not.toBeInTheDocument()
    expect(screen.queryByText("This PC is not connected to the hub.")).not.toBeInTheDocument()
    expect(screen.queryByText("Show a QR when this PC is online at the hub.")).not.toBeInTheDocument()
    expect(screen.queryByTestId("remote-qr")).not.toBeInTheDocument()
    expect(api.remoteOffer).not.toHaveBeenCalled()
    expect(onChange).not.toHaveBeenCalled()
  })

  it("does not show a host token field", async () => {
    await act(async () => { render(<RemoteTab settings={base} onChange={vi.fn()} />) })
    expect(screen.queryByText(base.remote!.hub_url)).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Host Token")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Advanced / self-hosted server" })).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Hub URL")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Connection sign-in")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("This computer's name")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Event text on the phone")).not.toBeInTheDocument()
    expect(screen.getByLabelText("Keep this computer awake")).toBeChecked()
  })

  it("describes account PC selection without making a QR or manual Hub setup a prerequisite", async () => {
    await act(async () => { render(<RemoteTab settings={{ ...base, remote: { ...base.remote!, auth_mode: "account" } }} onChange={vi.fn()} />) })
    expect(screen.getByText("Sign in with the same account on your phone, then select this PC without scanning. The hub forwards ciphertext; PC conversations stay here.")).toBeVisible()
    expect(screen.queryByText("Configured server")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Hub URL")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Advanced / self-hosted server" })).not.toBeInTheDocument()
    expect(screen.queryByText("The official Gateway is set by default. Change this address only for another server.")).not.toBeInTheDocument()
    expect(screen.queryByText("Scan the QR from the phone app. The hub only forwards ciphertext; conversations stay on this PC.")).not.toBeInTheDocument()
  })

  it("paints the reported phone model instead of a bare fingerprint", async () => {
    vi.mocked(api.remoteBindings).mockResolvedValue([boundPhone])
    render(<RemoteTab settings={base} onChange={vi.fn()} />)
    await waitFor(() =>
      expect(screen.getByText("Phone 1.0 Device")).toBeInTheDocument(),
    )
    expect(screen.getByText(/aa11bb22cc33dd44/)).toBeInTheDocument()
    expect(screen.getByText(/Last connected 2026-09-20T16:03:32Z/)).toBeInTheDocument()
  })

  it("hides keep-awake while phone connection is off", async () => {
    const onChange = vi.fn()
    const off = { ...base, remote: { ...base.remote!, enabled: false, keep_awake: true } }
    await act(async () => { render(<RemoteTab settings={off} onChange={onChange} />) })
    expect(screen.queryByRole("heading", { name: "Other settings" })).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Keep this computer awake")).not.toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Bound phones" })).not.toBeInTheDocument()
    expect(onChange).not.toHaveBeenCalled()
  })

  it("keeps a saved phone listed so it can be revoked while connection is off", async () => {
    vi.mocked(api.remoteBindings).mockResolvedValue([boundPhone])
    const off = { ...base, remote: { ...base.remote!, enabled: false } }
    render(<RemoteTab settings={off} onChange={vi.fn()} />)
    await waitFor(() =>
      expect(screen.getByText("Phone 1.0 Device")).toBeInTheDocument(),
    )
    expect(screen.getByRole("button", { name: "Revoke" })).toBeInTheDocument()
  })

  it("does not offer a computer name or server address to edit", async () => {
    const onChange = vi.fn()
    await act(async () => { render(<RemoteTab settings={base} onChange={onChange} />) })
    expect(screen.queryByLabelText("This computer's name")).not.toBeInTheDocument()
    expect(screen.queryByText("Configured server")).not.toBeInTheDocument()
    expect(onChange).not.toHaveBeenCalled()
  })

  it("toggles keep-awake without inventing a sample phrase", async () => {
    const onChange = vi.fn()
    render(<RemoteTab settings={base} onChange={onChange} />)
    await waitFor(() =>
      expect(screen.getByLabelText("Keep this computer awake")).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByLabelText("Keep this computer awake"))
    const next = onChange.mock.calls[0][0] as Settings
    expect(next.remote?.keep_awake).toBe(false)
  })

})
