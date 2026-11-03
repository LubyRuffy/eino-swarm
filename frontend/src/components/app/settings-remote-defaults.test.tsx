import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { RemoteTab } from "./settings-remote"
import { defaultRemoteSettings, type RemoteStatus, type Settings } from "@/lib/types"

vi.mock("@/lib/api", () => ({ api: {
  remoteStatus: vi.fn(), remoteBindings: vi.fn(), remoteOffer: vi.fn(),
  startRemoteLogin: vi.fn(), cancelRemoteLogin: vi.fn(), revokeRemoteBinding: vi.fn(),
} }))
const { api } = await import("@/lib/api")
// The Go settings response supplies its effective default origin. The renderer
// must not need a compiled-in official hostname to offer the account entry.
const origin = "https://configured-gateway.example"
const initial: RemoteStatus = { enabled: false, hub_url: origin, auth_mode: "legacy",
  online: false, has_token: false, login_available: true }
const settings = { remote: defaultRemoteSettings({ hub_url: origin }) } as Settings

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.remoteStatus).mockResolvedValue(initial)
  vi.mocked(api.remoteBindings).mockResolvedValue([])
  vi.mocked(api.startRemoteLogin).mockResolvedValue({ ...initial, enabled: true, auth_mode: "nosec",
    login: { state: "authorization_pending", user_code: "CONFIRM-THIS-PC" } })
})

describe("Phone account defaults", () => {
  it("offers explicit sign-in without a server, token or authentication form and never starts automatically", async () => {
    const change = vi.fn()
    render(<RemoteTab settings={settings} onChange={change}
      onAccountLogin={(newDevice) => api.startRemoteLogin(newDevice, true)} />)
    await waitFor(() => expect(screen.getByRole("button", { name: "Sign in with NOSEC" })).toBeEnabled())
    expect(screen.getByText("Sign in to link this PC to your account. No server address or token needs to be entered.")).toBeVisible()
    expect(screen.getByText(origin)).toBeVisible()
    expect(screen.queryByLabelText("Hub URL")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Connection sign-in")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Host Token")).not.toBeInTheDocument()
    expect(screen.getByText("Bound phones")).toBeVisible()
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
    expect(change).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Sign in with NOSEC" }))
    await waitFor(() => expect(api.startRemoteLogin).toHaveBeenCalledWith(false, true))
  })

  it("reveals existing self-hosted settings only through an explicit advanced action", async () => {
    const custom = { ...settings, remote: { ...settings.remote!, enabled: true, hub_url: "https://self-hosted.example", display_name: "my-pc" } }
    vi.mocked(api.remoteStatus).mockResolvedValue({ ...initial, ...custom.remote! })
    const change = vi.fn()
    await act(async () => { render(<RemoteTab settings={custom} onChange={change} />) })
    expect(screen.getByText(custom.remote.hub_url)).toBeVisible()
    expect(screen.queryByLabelText("Hub URL")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Advanced / self-hosted server" }))
    expect(screen.getByLabelText("Hub URL")).toHaveValue(custom.remote.hub_url)
    expect(screen.getByLabelText("Connection sign-in")).toHaveTextContent("Independent hub (legacy)")
    fireEvent.change(screen.getByLabelText("Hub URL"), { target: { value: "https://replacement.example" } })
    expect(change.mock.calls[0][0].remote).toMatchObject({ enabled: true, auth_mode: "legacy", hub_url: "https://replacement.example", display_name: "my-pc" })
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
  })

  it("keeps anonymous QR pairing and existing legacy configuration without automatically claiming an account", async () => {
    const legacy = { ...settings, remote: { ...settings.remote!, enabled: true } }
    vi.mocked(api.remoteStatus).mockResolvedValue({ ...initial, enabled: true, has_token: true, online: true })
    vi.mocked(api.remoteOffer).mockResolvedValue({ uri: "pairlink:v1:fixture", png: "data:image/png;base64,fixture" })
    const change = vi.fn()
    render(<RemoteTab settings={legacy} onChange={change} />)
    await act(async () => { await Promise.resolve() })
    fireEvent.click(screen.getByRole("button", { name: "Show pairing QR" }))
    await screen.findByTestId("remote-qr")
    expect(screen.queryByRole("button", { name: "Reconnect as a new device" })).not.toBeInTheDocument()
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
    expect(change).not.toHaveBeenCalled()
  })

  it("reveals advanced fields for a settings search without changing the saved connection", async () => {
    await act(async () => { render(<RemoteTab settings={settings} onChange={vi.fn()} query="Hub URL" />) })
    expect(screen.getByLabelText("Hub URL")).toHaveValue(origin)
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
  })

  it("keeps the advanced editor open when a saved Hub or authentication mode changes", async () => {
    const change = vi.fn()
    const view = render(<RemoteTab settings={settings} onChange={change} />)
    fireEvent.click(screen.getByRole("button", { name: "Advanced / self-hosted server" }))
    const updated = { ...settings, remote: { ...settings.remote!, hub_url: "https://saved-server.example", auth_mode: "nosec" as const } }
    vi.mocked(api.remoteStatus).mockResolvedValue({ ...initial, ...updated.remote! })
    await act(async () => { view.rerender(<RemoteTab settings={updated} onChange={change} />) })
    expect(screen.getByRole("button", { name: "Advanced / self-hosted server" })).toHaveAttribute("aria-expanded", "true")
    expect(screen.getByLabelText("Hub URL")).toHaveValue("https://saved-server.example")
    expect(screen.getByLabelText("Connection sign-in")).toHaveTextContent("NOSEC account")
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
  })
})
