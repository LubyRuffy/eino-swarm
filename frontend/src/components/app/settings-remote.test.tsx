import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { RemoteTab } from "./settings-remote"
import { ToastStack } from "./toast-stack"
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

  it("does not show a host token field", async () => {
    render(<RemoteTab settings={base} onChange={vi.fn()} />)
    await waitFor(() =>
      expect(screen.getByLabelText("Hub URL")).toBeInTheDocument(),
    )
    expect(screen.queryByLabelText("Host Token")).not.toBeInTheDocument()
    expect(screen.getByLabelText("Event text on the phone")).toHaveValue(4000)
    expect(screen.getByLabelText("Events on the phone")).toHaveValue(80)
    expect(screen.getByLabelText("Keep this computer awake")).toBeChecked()
  })

  it("paints the reported phone model instead of a bare fingerprint", async () => {
    vi.mocked(api.remoteBindings).mockResolvedValue([
      {
        id: "b1",
        device_fp: "aa11bb22cc33dd44",
        device: "Phone 1.0 Device",
        created_at: "2026-09-20T16:00:00Z",
        last_seen: "2026-09-20T16:03:32Z",
        session_id: "s1",
      },
    ])
    render(<RemoteTab settings={base} onChange={vi.fn()} />)
    await waitFor(() =>
      expect(screen.getByText("Phone 1.0 Device")).toBeInTheDocument(),
    )
    expect(screen.getByText(/aa11bb22cc33dd44/)).toBeInTheDocument()
    expect(screen.getByText(/Last connected/)).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Bound phones" })?.closest("section")?.textContent ?? "").not.toMatch(
      /^aa11bb22cc33dd44$/,
    )
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

  it("toasts a pairing failure instead of a red line under the Phone heading", async () => {
    vi.mocked(api.remoteOffer).mockRejectedValueOnce(new Error("hub refused"))
    render(
      <>
        <ToastStack />
        <RemoteTab settings={base} onChange={vi.fn()} />
      </>,
    )
    await waitFor(() =>
      expect(screen.getByLabelText("Hub URL")).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole("button", { name: "Show pairing QR" }))
    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("Couldn't set up the phone")
    expect(alert).toHaveTextContent("hub refused")
    const page =
      screen.getByRole("heading", { name: "Phone" }).parentElement
        ?.parentElement
    expect(page?.textContent ?? "").not.toMatch(/hub refused/)
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }))
    expect(screen.queryByRole("alert")).toBeNull()
  })
})
