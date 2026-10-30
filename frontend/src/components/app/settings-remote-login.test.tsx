import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { RemoteTab } from "./settings-remote"
import { defaultRemoteSettings, type RemoteStatus, type Settings } from "@/lib/types"

vi.mock("@/lib/api", () => ({
  api: {
    remoteStatus: vi.fn(), remoteOffer: vi.fn(), remoteBindings: vi.fn(),
    revokeRemoteBinding: vi.fn(), startRemoteLogin: vi.fn(), cancelRemoteLogin: vi.fn(),
  },
}))
const { api } = await import("@/lib/api")
const settings = {
  remote: { ...defaultRemoteSettings(), enabled: true, hub_url: "https://hub.example", auth_mode: "nosec" },
} as Settings
const signedOut: RemoteStatus = {
  enabled: true, hub_url: "https://hub.example", auth_mode: "nosec", online: false,
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

describe("NOSEC phone connection", () => {
  it("requires an explicit login and keeps QR unavailable before a real online connection", async () => {
    render(<RemoteTab settings={settings} onChange={vi.fn()} />)
    const login = await screen.findByRole("button", { name: "Sign in with NOSEC" })
    await waitFor(() => expect(login).toBeEnabled())
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
    expect(screen.getByRole("button", { name: "Show pairing QR" })).toBeDisabled()
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
    expect(screen.getByRole("button", { name: "Show pairing QR" })).toBeDisabled()
    expect(api.remoteOffer).not.toHaveBeenCalled()
  })

  it("distinguishes authorization from PC online and refreshes local status", async () => {
    vi.useFakeTimers()
    let state: RemoteStatus = { ...signedOut, login: { state: "authorized", username: "alice" } }
    vi.mocked(api.remoteStatus).mockImplementation(async () => state)
    render(<RemoteTab settings={settings} onChange={vi.fn()} />)
    await act(async () => { await Promise.resolve() })
    expect(screen.getByText("Authorized. Waiting for this PC to connect to the hub.")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Show pairing QR" })).toBeDisabled()
    state = { ...state, online: true, fingerprint: "1122334455667788" }
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(screen.getByRole("button", { name: "Show pairing QR" })).toBeEnabled()
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
    expect(await screen.findByRole("button", { name: "Sign in with NOSEC" })).toBeDisabled()
    expect(api.startRemoteLogin).not.toHaveBeenCalled()
  })

  it("discards a late login response after changing Hub and never retargets it", async () => {
    const late = deferred<RemoteStatus>()
    vi.mocked(api.startRemoteLogin).mockReturnValue(late.promise)
    const view = render(<RemoteTab settings={settings} onChange={vi.fn()} />)
    const login = await screen.findByRole("button", { name: "Sign in with NOSEC" })
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
    fireEvent.click(screen.getByRole("button", { name: "Sign in with NOSEC" }))
    await act(async () => { await Promise.resolve() })
    await act(async () => { old.resolve({ ...signedOut, online: true, login: { state: "authorized" } }); await old.promise })
    expect(screen.getByText("ABCD-1234-EF56-7890")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Show pairing QR" })).toBeDisabled()
  })

  it("hides late QR offers after access is revoked and recovers a failed local status read", async () => {
    vi.useFakeTimers()
    let state: RemoteStatus = { ...signedOut, online: true, login: { state: "authorized" } }
    let failed = false
    vi.mocked(api.remoteStatus).mockImplementation(async () => {
      if (failed) throw new Error("offline")
      return state
    })
    const offer = deferred<Awaited<ReturnType<typeof api.remoteOffer>>>()
    vi.mocked(api.remoteOffer).mockReturnValue(offer.promise)
    const view = render(<RemoteTab settings={settings} onChange={vi.fn()} />)
    await act(async () => { await Promise.resolve() })
    fireEvent.click(screen.getByRole("button", { name: "Show pairing QR" }))
    state = { ...state, online: false, login: { state: "access_denied" } }
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    await act(async () => { offer.resolve({ png: "data:image/png;base64,AAAA", uri: "pairlink:v1:late" }); await offer.promise })
    state = { ...state, online: true, login: { state: "authorized" } }
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(screen.queryByTestId("remote-qr")).not.toBeInTheDocument()
    failed = true
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(screen.getByText("Cannot read connection status. Pairing is paused until it recovers.")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Show pairing QR" })).toBeDisabled()
    failed = false
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(screen.getByRole("button", { name: "Show pairing QR" })).toBeEnabled()
    view.unmount()
    const reads = vi.mocked(api.remoteStatus).mock.calls.length
    await act(async () => { await vi.advanceTimersByTimeAsync(3000) })
    expect(api.remoteStatus).toHaveBeenCalledTimes(reads)
  })
})
