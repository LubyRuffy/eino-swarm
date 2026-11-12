import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { RemoteTab } from "./settings-remote"
import { RemoteLoginSection } from "./settings-remote-login"
import { DesktopAccountMenu } from "./desktop-account-menu"
import { defaultRemoteSettings, type RemoteStatus, type Settings } from "@/lib/types"

vi.mock("@/lib/api", () => ({
  api: {
    remoteStatus: vi.fn(), remoteOffer: vi.fn(), remoteBindings: vi.fn(),
    revokeRemoteBinding: vi.fn(), startRemoteLogin: vi.fn(), cancelRemoteLogin: vi.fn(),
  },
}))
const { api } = await import("@/lib/api")
const settings = {
  remote: { ...defaultRemoteSettings(), enabled: true, hub_url: "https://hub.example", auth_mode: "account" },
} as Settings
const signedOut: RemoteStatus = {
  enabled: true, hub_url: "https://hub.example", auth_mode: "account", online: false,
  has_token: false, login_available: true, login: { state: "signed_out" },
}
const pending: RemoteStatus = {
  ...signedOut, login: { state: "authorization_pending", user_code: "ABCD-1234-EF56-7890", host_fp: "1122334455667788" },
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => { resolve = r })
  return { promise, resolve }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.remoteStatus).mockResolvedValue(signedOut)
  vi.mocked(api.remoteBindings).mockResolvedValue([])
  vi.mocked(api.startRemoteLogin).mockResolvedValue(pending)
  vi.mocked(api.cancelRemoteLogin).mockResolvedValue({ ...signedOut, login: { state: "cancelled" } })
})
afterEach(() => { vi.useRealTimers() })

describe("Account phone connection", () => {
  it("hides cached account and authorization details while status is unavailable", () => {
    render(<RemoteLoginSection accountEntry enabled status={{ ...pending,
      login: { ...pending.login!, username: "previous-account" } }} unavailable busy={false}
      onStart={vi.fn()} onCancel={vi.fn()} />)
    expect(screen.getByText("Cannot read connection status. Phone connection stays paused until it recovers.")).toBeVisible()
    expect(screen.queryByText(/previous-account/)).not.toBeInTheDocument()
    expect(screen.queryByText("ABCD-1234-EF56-7890")).not.toBeInTheDocument()
    expect(screen.queryByText("1122334455667788")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Cancel sign-in" })).toBeDisabled()
  })

  it("updates the rail avatar from Phone login through the same status owner", async () => {
    render(<><DesktopAccountMenu /><RemoteTab settings={settings} onChange={vi.fn()} /></>)
    const login = await screen.findByRole("button", { name: "Sign in" })
    await waitFor(() => expect(login).toBeEnabled())
    const avatar = screen.getByRole("button", { name: "Account" })
    expect(avatar).toHaveAttribute("title", "Account: Local use · Not signed in")
    expect(api.remoteStatus).toHaveBeenCalledTimes(1)
    fireEvent.click(login)
    await waitFor(() => expect(avatar).toHaveAttribute("title", "Account: Awaiting browser authorization"))
    expect(api.startRemoteLogin).toHaveBeenCalledTimes(1)
  })

  it("does not start another authorization from an already authorized account dialog", () => {
    render(<RemoteLoginSection accountEntry enabled status={{ ...signedOut, has_token: true,
      login: { state: "authorized", username: "alice" } }} unavailable={false} busy={false}
      onStart={vi.fn()} onCancel={vi.fn()} />)
    expect(screen.queryByRole("button", { name: "Sign in" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Reconnect as a new device" })).toBeInTheDocument()
  })

  it("shares status admission and mutation ownership between mounted account surfaces", async () => {
    const login = deferred<RemoteStatus>()
    vi.mocked(api.startRemoteLogin).mockReturnValue(login.promise)
    render(<><RemoteTab settings={settings} onChange={vi.fn()} /><RemoteTab settings={settings} onChange={vi.fn()} /></>)
    const buttons = await screen.findAllByRole("button", { name: "Sign in" })
    await waitFor(() => expect(buttons[0]).toBeEnabled())
    expect(api.remoteStatus).toHaveBeenCalledTimes(1)
    fireEvent.click(buttons[0])
    fireEvent.click(buttons[1])
    expect(api.startRemoteLogin).toHaveBeenCalledTimes(1)
    await act(async () => { login.resolve(pending); await login.promise })
    expect(await screen.findAllByText("ABCD-1234-EF56-7890")).toHaveLength(2)
  })

  it("requires an explicit login and keeps QR unavailable before a real online connection", async () => {
    render(<RemoteTab settings={settings} onChange={vi.fn()} />)
    const login = await screen.findByRole("button", { name: "Sign in" })
    await waitFor(() => expect(login).toBeEnabled())
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
    expect(screen.queryByRole("button", { name: "Show pairing QR" })).not.toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Pairing QR" })).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Host Token")).not.toBeInTheDocument()
    fireEvent.click(login)
    await screen.findByText("ABCD-1234-EF56-7890")
    expect(api.startRemoteLogin).toHaveBeenCalledTimes(1)
    expect(api.startRemoteLogin).toHaveBeenCalledWith(false)
    expect(screen.getByText("1122334455667788")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Cancel sign-in" }))
    await waitFor(() => expect(api.cancelRemoteLogin).toHaveBeenCalledOnce())
    await screen.findByText("Sign-in cancelled.")
  })

  it.each([
    ["account_pending", "Waiting for administrator approval. No device is connected yet."],
    ["account_rejected", "This account was rejected. Ask an administrator to review it."],
    ["expired", "Sign-in expired. Start again in your browser."],
    ["access_denied", "This account cannot connect devices. Contact an administrator."],
  ] as const)("shows %s without exposing pairing", async (state, text) => {
    vi.mocked(api.remoteStatus).mockResolvedValue({ ...pending, login: { ...pending.login!, state } })
    render(<RemoteTab settings={settings} onChange={vi.fn()} />)
    await screen.findByText(text)
    expect(screen.queryByRole("button", { name: "Show pairing QR" })).not.toBeInTheDocument()
    expect(api.remoteOffer).not.toHaveBeenCalled()
  })

  it("says a disabled authorized link is off and turns that saved connection on", async () => {
    const onChange = vi.fn()
    const off: RemoteStatus = { ...signedOut, enabled: false, has_token: true, login: { state: "authorized", username: "alice" } }
    vi.mocked(api.remoteStatus).mockResolvedValue(off)
    const draft = { ...settings, remote: { ...settings.remote!, enabled: false } }
    render(<RemoteTab settings={draft} onChange={onChange} />)
    expect(await screen.findByText("Signed in, but this PC's connection is off, so it is not connecting to the hub.")).toBeVisible()
    expect(screen.queryByText("Authorized. Waiting for this PC to connect to the hub.")).not.toBeInTheDocument()
    const toggle = screen.getByRole("switch", { name: "This PC's connection" })
    expect(toggle).not.toBeChecked()
    fireEvent.click(toggle)
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({
      remote: expect.objectContaining({ enabled: true, hub_url: "https://hub.example", auth_mode: "account" }),
    }))
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
  })

  it.each([
    ["remote: authorized host registration failed", "This PC could not connect to the hub. It keeps trying."],
    ["remote: authorized host connection failed", "This PC could not connect to the hub. It keeps trying."],
    ["remote needs hub_url", "No hub address is configured, so this PC is not connecting."],
    ["remote: gateway account login and approval are required", "Account authorization is not complete, so this PC is not connecting."],
    ["remote: connection credential origin changed", "The saved credential does not match this hub, so this PC is not connected."],
    ["dial failed at https://secret.example/token", "This PC could not connect to the hub."],
  ] as const)("describes %s without the raw host error", (error, text) => {
    render(<RemoteLoginSection accountEntry enabled status={{ ...signedOut, error, login: { state: "authorized", username: "alice" } }}
      unavailable={false} busy={false} onStart={vi.fn()} onCancel={vi.fn()} />)
    expect(screen.getByText(text)).toBeVisible()
    expect(screen.queryByText("Authorized. Waiting for this PC to connect to the hub.")).not.toBeInTheDocument()
    expect(screen.queryByText(error)).not.toBeInTheDocument()
  })

  it("distinguishes authorization from PC online and refreshes local status", async () => {
    vi.useFakeTimers()
    let state: RemoteStatus = { ...signedOut, login: { state: "authorized", username: "alice" } }
    vi.mocked(api.remoteStatus).mockImplementation(async () => state)
    const onChange = vi.fn()
    render(<RemoteTab settings={settings} onChange={onChange} />)
    await act(async () => { await Promise.resolve() })
    expect(screen.getByText("Authorized. Waiting for this PC to connect to the hub.")).toBeInTheDocument()
    const toggle = screen.getByRole("switch", { name: "This PC's connection" })
    expect(toggle).toBeChecked()
    fireEvent.click(toggle)
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({
      remote: expect.objectContaining({ enabled: false, hub_url: "https://hub.example", auth_mode: "account" }),
    }))
    expect(screen.queryByRole("heading", { name: "Pairing QR" })).not.toBeInTheDocument()
    state = { ...state, online: true, fingerprint: "1122334455667788" }
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(screen.getByText("This PC is authorized and connected to the hub.")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Show pairing QR" })).not.toBeInTheDocument()
    expect(api.remoteOffer).not.toHaveBeenCalled()
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
  })

  it("requires confirmation before replacing device credentials", async () => {
    vi.mocked(api.remoteStatus).mockResolvedValue({ ...signedOut, login: { state: "error", error_code: "device_conflict" } })
    render(<RemoteTab settings={settings} onChange={vi.fn()} />)
    fireEvent.click(await screen.findByRole("button", { name: "Reconnect as a new device" }))
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
    expect(screen.getByRole("dialog", { name: "Reconnect as a new device?" })).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Confirm reconnect" }))
    await waitFor(() => expect(api.startRemoteLogin).toHaveBeenCalledWith(true))
  })

  it("waits for saved Hub and mode to be reflected in local status", async () => {
    vi.mocked(api.remoteStatus).mockResolvedValue({ ...signedOut, hub_url: "https://old-hub.example" })
    render(<RemoteTab settings={settings} onChange={vi.fn()} />)
    expect(await screen.findByRole("button", { name: "Sign in" })).toBeDisabled()
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
  })

  it("discards a late login response after changing Hub and never retargets it", async () => {
    const late = deferred<RemoteStatus>()
    vi.mocked(api.startRemoteLogin).mockReturnValue(late.promise)
    const view = render(<RemoteTab settings={settings} onChange={vi.fn()} />)
    const login = await screen.findByRole("button", { name: "Sign in" })
    await waitFor(() => expect(login).toBeEnabled())
    fireEvent.click(login)
    const changed = { ...settings, remote: { ...settings.remote!, hub_url: "https://new-hub.example" } }
    vi.mocked(api.remoteStatus).mockResolvedValue({ ...signedOut, hub_url: changed.remote.hub_url })
    view.rerender(<RemoteTab settings={changed} onChange={vi.fn()} />)
    await act(async () => { late.resolve(pending); await late.promise })
    expect(screen.queryByText("ABCD-1234-EF56-7890")).not.toBeInTheDocument()
    expect(api.startRemoteLogin).toHaveBeenCalledTimes(1)
  })

  it("rejects an old status read that finishes after an explicit login starts", async () => {
    vi.useFakeTimers()
    const old = deferred<RemoteStatus>()
    vi.mocked(api.remoteStatus).mockResolvedValueOnce(signedOut).mockReturnValueOnce(old.promise)
    render(<RemoteTab settings={settings} onChange={vi.fn()} />)
    await act(async () => { await Promise.resolve() })
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }))
    await act(async () => { await Promise.resolve() })
    await act(async () => { old.resolve({ ...signedOut, online: true, login: { state: "authorized" } }); await old.promise })
    expect(screen.getByText("ABCD-1234-EF56-7890")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Show pairing QR" })).not.toBeInTheDocument()
  })

  it("keeps the pairing QR hidden while access changes and recovers a failed local status read", async () => {
    vi.useFakeTimers()
    let state: RemoteStatus = { ...signedOut, online: true, login: { state: "authorized" } }
    let failed = false
    vi.mocked(api.remoteStatus).mockImplementation(async () => {
      if (failed) throw new Error("offline")
      return state
    })
    const view = render(<RemoteTab settings={settings} onChange={vi.fn()} />)
    await act(async () => { await Promise.resolve() })
    expect(screen.queryByRole("heading", { name: "Pairing QR" })).not.toBeInTheDocument()
    state = { ...state, online: false, login: { state: "access_denied" } }
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(screen.queryByTestId("remote-qr")).not.toBeInTheDocument()
    expect(api.remoteOffer).not.toHaveBeenCalled()
    failed = true
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(screen.getByText("Cannot read connection status. Phone connection stays paused until it recovers.")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Show pairing QR" })).not.toBeInTheDocument()
    failed = false
    state = { ...signedOut, online: true, login: { state: "authorized" } }
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(screen.getByText("This PC is authorized and connected to the hub.")).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Pairing QR" })).not.toBeInTheDocument()
    view.unmount()
    const reads = vi.mocked(api.remoteStatus).mock.calls.length
    await act(async () => { await vi.advanceTimersByTimeAsync(3000) })
    expect(api.remoteStatus).toHaveBeenCalledTimes(reads)
  })
})
