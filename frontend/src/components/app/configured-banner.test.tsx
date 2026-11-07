import { fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import type { Meta, RemoteStatus } from "@/lib/types"
import { useApp } from "@/store/app"

import { ConfiguredBanner } from "./app-chrome"

const fake = vi.hoisted(() => ({
  status: undefined as RemoteStatus | undefined,
  perform: vi.fn(async (action: () => Promise<unknown>) => { await action() }),
  start: vi.fn(),
}))

vi.mock("@/lib/use-remote-status", () => ({
  useDesktopAccount: () => ({
    status: fake.status, unavailable: false, busy: false,
    refresh: vi.fn(async () => {}), perform: fake.perform,
  }),
}))
vi.mock("@/lib/api", () => ({ api: { startRemoteLogin: fake.start, cancelRemoteLogin: vi.fn() } }))

const signedOut: RemoteStatus = {
  enabled: false, hub_url: "https://server.example", auth_mode: "account", has_token: false,
  online: false, login_available: true, login: { state: "signed_out" },
}
const meta = (configured: boolean): Meta => ({
  version: "test", mode: "desktop", mock: false, configured, default_provider: "",
  reasoning_levels: [], data_dir: "", capabilities: {}, swarm: {} as Meta["swarm"],
})

beforeEach(() => {
  vi.clearAllMocks()
  fake.status = signedOut
  fake.start.mockResolvedValue(signedOut)
  useApp.setState({ meta: meta(false) })
})

describe("unconfigured banner", () => {
  it("signs in to the server first and offers the own-model path second", () => {
    const configure = vi.fn()
    render(<ConfiguredBanner onConfigure={configure} />)
    fireEvent.click(screen.getByRole("button", { name: "Sign in to the official server" }))
    expect(fake.start).toHaveBeenCalledWith(false, true)
    fireEvent.click(screen.getByRole("button", { name: "Set up my own models" }))
    expect(configure).toHaveBeenCalledTimes(1)
  })

  it("has no dead sign-in button in a browser, only the own-model path", () => {
    fake.status = { ...signedOut, login_available: false }
    render(<ConfiguredBanner onConfigure={vi.fn()} />)
    expect(screen.queryByRole("button", { name: "Sign in to the official server" })).toBeNull()
    expect(screen.getByRole("button", { name: "Set up my own models" })).toBeTruthy()
  })

  it("disappears once models can run", () => {
    useApp.setState({ meta: meta(true) })
    const { container } = render(<ConfiguredBanner onConfigure={vi.fn()} />)
    expect(container.firstChild).toBeNull()
  })
})
