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
const initial: RemoteStatus = { enabled: false, hub_url: origin, auth_mode: "independent",
  online: false, has_token: false, login_available: true }
const settings = { remote: defaultRemoteSettings({ hub_url: origin }) } as Settings

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.remoteStatus).mockResolvedValue(initial)
  vi.mocked(api.remoteBindings).mockResolvedValue([])
  vi.mocked(api.startRemoteLogin).mockResolvedValue({ ...initial, enabled: true, auth_mode: "account",
    login: { state: "authorization_pending", user_code: "CONFIRM-THIS-PC" } })
})

describe("Phone account defaults", () => {
  it("offers explicit sign-in without a server, token or authentication form and never starts automatically", async () => {
    const change = vi.fn()
    render(<RemoteTab settings={settings} onChange={change}
      onAccountLogin={(newDevice) => api.startRemoteLogin(newDevice, true)} />)
    await waitFor(() => expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled())
    expect(screen.getByText("Sign in to link this PC to your account. No server address or token needs to be entered.")).toBeVisible()
    expect(screen.queryByText(origin)).not.toBeInTheDocument()
    expect(screen.queryByText("Configured server")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Advanced / self-hosted server" })).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Hub URL")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Connection sign-in")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Host Token")).not.toBeInTheDocument()
    expect(screen.getByText("Bound phones")).toBeVisible()
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
    expect(change).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }))
    await waitFor(() => expect(api.startRemoteLogin).toHaveBeenCalledWith(false, true))
  })

  it("keeps a configured server address off the phone page", async () => {
    const custom = { ...settings, remote: { ...settings.remote!, enabled: true, hub_url: "https://self-hosted.example", display_name: "my-pc" } }
    vi.mocked(api.remoteStatus).mockResolvedValue({ ...initial, ...custom.remote! })
    const change = vi.fn()
    await act(async () => { render(<RemoteTab settings={custom} onChange={change} />) })
    expect(screen.queryByText(custom.remote.hub_url)).not.toBeInTheDocument()
    expect(screen.queryByText("Configured server")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Advanced / self-hosted server" })).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Hub URL")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Connection sign-in")).not.toBeInTheDocument()
    expect(change).not.toHaveBeenCalled()
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

  it("does not reveal server fields when settings search asks for them", async () => {
    const change = vi.fn()
    await act(async () => { render(<RemoteTab settings={settings} onChange={change} query="Hub URL" />) })
    expect(screen.queryByLabelText("Hub URL")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Advanced / self-hosted server" })).not.toBeInTheDocument()
    expect(change).not.toHaveBeenCalled()
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
  })
})
