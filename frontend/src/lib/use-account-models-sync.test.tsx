import { act, render } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { AccountModels, Meta, RemoteStatus } from "@/lib/types"
import { useApp } from "@/store/app"

import {
  ACCOUNT_MODELS_POLL_MAX, ACCOUNT_MODELS_POLL_MS, useAccountModelsSync,
} from "./use-account-models-sync"

const fake = vi.hoisted(() => ({
  status: undefined as RemoteStatus | undefined,
  unavailable: false,
  meta: vi.fn(),
  models: vi.fn(),
}))

vi.mock("@/lib/use-remote-status", () => ({
  useDesktopAccount: () => ({ status: fake.status, unavailable: fake.unavailable }),
}))
vi.mock("@/lib/api", () => ({ api: { meta: fake.meta, models: fake.models } }))

const meta = (account: AccountModels): Meta => ({
  version: "test", mode: "desktop", mock: false, configured: account.state === "ready", default_provider: "",
  reasoning_levels: [], data_dir: "", capabilities: {}, swarm: {} as Meta["swarm"], account_models: account,
})
const signedIn = (state: NonNullable<RemoteStatus["login"]>["state"]): RemoteStatus => ({
  enabled: true, hub_url: "https://server.example", auth_mode: "account", has_token: true, online: false,
  login_available: true, login: { state },
})

function Probe() {
  useAccountModelsSync()
  return null
}

beforeEach(() => {
  vi.useFakeTimers()
  fake.unavailable = false
  fake.meta.mockReset()
  fake.models.mockReset()
  fake.models.mockResolvedValue({ models: [], default: "", mock: false })
})
afterEach(() => vi.useRealTimers())

describe("account model sync", () => {
  it("looks again after sign-in until the server's models arrive, then stops", async () => {
    fake.status = signedIn("authorized")
    useApp.setState({ meta: meta({ state: "signed_out", models: 0 }) })
    fake.meta
      .mockResolvedValueOnce(meta({ state: "pending", models: 0 }))
      .mockResolvedValue(meta({ state: "ready", models: 2 }))
    render(<Probe />)
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(fake.meta).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(ACCOUNT_MODELS_POLL_MS) })
    expect(useApp.getState().meta?.account_models?.state).toBe("ready")
    const settled = fake.meta.mock.calls.length
    await act(async () => { await vi.advanceTimersByTimeAsync(ACCOUNT_MODELS_POLL_MS * 5) })
    expect(fake.meta.mock.calls.length).toBe(settled)
  })

  it("drops the server's models after sign-out instead of showing a stale list", async () => {
    fake.status = signedIn("signed_out")
    useApp.setState({ meta: meta({ state: "ready", models: 2 }) })
    fake.meta.mockResolvedValue(meta({ state: "signed_out", models: 0 }))
    render(<Probe />)
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(useApp.getState().meta?.account_models?.state).toBe("signed_out")
  })

  it("stops looking after a bounded number of tries when the server never answers", async () => {
    fake.status = signedIn("authorized")
    useApp.setState({ meta: meta({ state: "pending", models: 0 }) })
    fake.meta.mockResolvedValue(meta({ state: "pending", models: 0 }))
    render(<Probe />)
    await act(async () => { await vi.advanceTimersByTimeAsync(ACCOUNT_MODELS_POLL_MS * (ACCOUNT_MODELS_POLL_MAX + 10)) })
    expect(fake.meta.mock.calls.length).toBe(ACCOUNT_MODELS_POLL_MAX)
  })

  it("does nothing while sign-in is mid-flight or the status is unreadable", async () => {
    fake.status = signedIn("authorization_pending")
    useApp.setState({ meta: meta({ state: "signed_out", models: 0 }) })
    const view = render(<Probe />)
    await act(async () => { await vi.advanceTimersByTimeAsync(ACCOUNT_MODELS_POLL_MS * 3) })
    fake.status = signedIn("authorized")
    fake.unavailable = true
    view.rerender(<Probe />)
    await act(async () => { await vi.advanceTimersByTimeAsync(ACCOUNT_MODELS_POLL_MS * 3) })
    expect(fake.meta).not.toHaveBeenCalled()
  })

  it("keeps the latest answer when an older request finishes last", async () => {
    fake.status = signedIn("signed_out")
    useApp.setState({ meta: meta({ state: "ready", models: 2 }) })
    let release: (m: Meta) => void = () => undefined
    fake.meta
      .mockImplementationOnce(() => new Promise<Meta>((resolve) => { release = resolve }))
      .mockResolvedValue(meta({ state: "signed_out", models: 0 }))
    render(<Probe />)
    await act(async () => { await vi.advanceTimersByTimeAsync(ACCOUNT_MODELS_POLL_MS) })
    expect(useApp.getState().meta?.account_models?.state).toBe("signed_out")
    await act(async () => { release(meta({ state: "ready", models: 2 })) })
    expect(useApp.getState().meta?.account_models?.state).toBe("signed_out")
  })
})
