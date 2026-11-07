import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import type { AccountModels, Meta, RemoteStatus } from "@/lib/types"
import { useApp } from "@/store/app"
import { useSettingsSheet } from "@/store/settings-sheet"

import { WelcomeGate } from "./welcome-gate"

const fake = vi.hoisted(() => ({
  status: undefined as RemoteStatus | undefined,
  unavailable: false,
  busy: false,
  perform: vi.fn(async (action: () => Promise<unknown>) => { await action() }),
  start: vi.fn(),
  cancel: vi.fn(),
  save: vi.fn(),
}))

vi.mock("@/lib/use-remote-status", () => ({
  useDesktopAccount: () => ({
    status: fake.status, unavailable: fake.unavailable, busy: fake.busy,
    refresh: vi.fn(async () => {}), perform: fake.perform,
  }),
}))
vi.mock("@/lib/api", () => ({
  api: { startRemoteLogin: fake.start, cancelRemoteLogin: fake.cancel, saveSettings: fake.save },
}))

const signedOut: RemoteStatus = {
  enabled: false, hub_url: "https://server.example", auth_mode: "account", has_token: false,
  online: false, login_available: true, login: { state: "signed_out" },
}
const meta = (patch: Partial<Meta> = {}): Meta => ({
  version: "test", mode: "desktop", mock: false, configured: false, default_provider: "",
  reasoning_levels: [], data_dir: "", capabilities: {}, swarm: {} as Meta["swarm"],
  account_models: { state: "signed_out", models: 0 }, ...patch,
})

beforeEach(() => {
  vi.clearAllMocks()
  fake.status = signedOut
  fake.unavailable = false
  fake.busy = false
  fake.start.mockResolvedValue({ ...signedOut, login: { state: "authorization_pending" } })
  fake.cancel.mockResolvedValue(signedOut)
  fake.save.mockResolvedValue({})
  useApp.setState({ meta: meta() })
  useSettingsSheet.setState({ open: false, section: "general", focus: "" })
})

describe("first-run welcome", () => {
  it("leads with the server account and keeps the own-service path beside it", () => {
    render(<WelcomeGate />)
    const dialog = screen.getByRole("dialog", { name: "Welcome to zwai" })
    expect(dialog).toBeTruthy()
    expect(screen.getByRole("button", { name: "Sign in to the official server" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Use my own model service" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Not now" })).toBeTruthy()
  })

  it("signs in through the account link and shows the browser confirmation code", async () => {
    render(<WelcomeGate />)
    fireEvent.click(screen.getByRole("button", { name: "Sign in to the official server" }))
    await waitFor(() => expect(fake.start).toHaveBeenCalledWith(false, true))
    act(() => {
      fake.status = { ...signedOut, login: { state: "authorization_pending", user_code: "ABCD-EFGH" } }
      useApp.setState({ meta: meta() })
    })
    expect(await screen.findByText("ABCD-EFGH")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Cancel sign-in" }))
    await waitFor(() => expect(fake.cancel).toHaveBeenCalled())
  })

  it("remembers 'Not now' without touching any other setting", async () => {
    render(<WelcomeGate />)
    fireEvent.click(screen.getByRole("button", { name: "Not now" }))
    await waitFor(() => expect(fake.save).toHaveBeenCalledWith({ ui: { welcome_dismissed: true } }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(useApp.getState().meta?.ui?.welcome_dismissed).toBe(true)
    expect(useSettingsSheet.getState().open).toBe(false)
  })

  it("sends 'my own model service' to the Models page and settles the question", async () => {
    render(<WelcomeGate />)
    fireEvent.click(screen.getByRole("button", { name: "Use my own model service" }))
    await waitFor(() => expect(useSettingsSheet.getState()).toMatchObject({ open: true, section: "models" }))
    expect(fake.save).toHaveBeenCalledWith({ ui: { welcome_dismissed: true } })
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("does not hide when the answer could not be saved", async () => {
    fake.save.mockRejectedValue(new Error("disk full"))
    render(<WelcomeGate />)
    fireEvent.click(screen.getByRole("button", { name: "Not now" }))
    await waitFor(() => expect(fake.save).toHaveBeenCalled())
    expect(screen.getByRole("dialog", { name: "Welcome to zwai" })).toBeTruthy()
    expect(useApp.getState().meta?.ui?.welcome_dismissed).toBeFalsy()
  })

  it("explains a signed-in account that has no models and still offers the own-service path", () => {
    const account: AccountModels = { state: "no_access", models: 0 }
    fake.status = { ...signedOut, has_token: true, login: { state: "authorized" } }
    useApp.setState({ meta: meta({ account_models: account }) })
    render(<WelcomeGate />)
    expect(screen.getByText(/cannot use server models/)).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Sign in to the official server" })).toBeNull()
    expect(screen.getByRole("button", { name: "Use my own model service" })).toBeTruthy()
  })

  it("tells a browser build that signing in needs the desktop app, without a dead button", () => {
    fake.status = { ...signedOut, login_available: false }
    render(<WelcomeGate />)
    expect((screen.getByRole("button", { name: "Sign in to the official server" }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText(/requires the desktop app/)).toBeTruthy()
    expect((screen.getByRole("button", { name: "Use my own model service" }) as HTMLButtonElement).disabled).toBe(false)
  })

  it("stays out of the way once models exist, was answered, or the engine is restoring", () => {
    for (const patch of [
      { configured: true },
      { ui: { welcome_dismissed: true } as Meta["ui"] },
      { startup: "restoring" as const },
    ]) {
      useApp.setState({ meta: meta(patch) })
      const { unmount } = render(<WelcomeGate />)
      expect(screen.queryByRole("dialog")).toBeNull()
      unmount()
    }
  })
})
