import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { startGatewayLogin } from "./gateway-account"

const native = vi.hoisted(() => ({
  platform: "android",
  launch: vi.fn<(options: { url: string }) => Promise<{ completed: boolean }>>(),
  browser: vi.fn<(options: { url: string }) => Promise<void>>(),
}))
vi.mock("@capacitor/core", () => ({
  Capacitor: { getPlatform: () => native.platform, isNativePlatform: () => native.platform !== "web" },
  registerPlugin: () => ({}),
}))
vi.mock("@capacitor/app", () => ({ App: { getInfo: async () => ({ version: "0.1.49" }) } }))
vi.mock("@capacitor/app-launcher", () => ({ AppLauncher: { openUrl: native.launch } }))
vi.mock("@capacitor/browser", () => ({ Browser: { open: native.browser } }))

const origin = "https://gateway.example.test"
const verification = origin + "/client/api/v1/auth/sso/nosec/start?mobile_code=abcd-1234-abcd-1234"
const fetcher = (uri = verification) => vi.fn<typeof fetch>(async () => new Response(JSON.stringify({ data: {
  device_code: "D".repeat(43), user_code: "abcd-1234-abcd-1234", interval: 5,
  verification_uri: uri, expires_at: new Date(Date.now() + 600_000).toISOString(),
} })))

beforeEach(() => {
  localStorage.clear()
  native.platform = "android"
  native.launch.mockReset().mockResolvedValue({ completed: true })
  native.browser.mockReset().mockResolvedValue(undefined)
})
afterEach(() => { vi.useRealTimers(); localStorage.clear() })

describe("NOSEC system browser selection", () => {
  it("opens the validated Android verification URL with the default external browser", async () => {
    await startGatewayLogin(origin, fetcher())
    expect(native.launch).toHaveBeenCalledOnce()
    expect(native.launch).toHaveBeenCalledWith({ url: verification })
    expect(native.browser).not.toHaveBeenCalled()
  })

  it.each(["ios", "web"])("preserves the existing Browser on %s", async (platform) => {
    native.platform = platform
    await startGatewayLogin(origin, fetcher())
    expect(native.browser).toHaveBeenCalledOnce()
    expect(native.browser).toHaveBeenCalledWith({ url: verification })
    expect(native.launch).not.toHaveBeenCalled()
  })

  it("fails when Android did not complete launching without falling back or repeating", async () => {
    native.launch.mockResolvedValue({ completed: false })
    await expect(startGatewayLogin(origin, fetcher())).rejects.toMatchObject({ code: "browser_unavailable" })
    expect(native.launch).toHaveBeenCalledOnce()
    expect(native.browser).not.toHaveBeenCalled()
  })

  it("normalizes a native launcher rejection without another open attempt", async () => {
    native.launch.mockRejectedValue(new Error("native launcher unavailable"))
    await expect(startGatewayLogin(origin, fetcher())).rejects.toMatchObject({ code: "browser_unavailable", message: "browser_unavailable" })
    expect(native.launch).toHaveBeenCalledOnce()
    expect(native.browser).not.toHaveBeenCalled()
  })

  it("rejects a forged URL before either native opener", async () => {
    await expect(startGatewayLogin(origin, fetcher("https://foreign.example.test/"))).rejects.toThrow("invalid_verification_url")
    expect(native.launch).not.toHaveBeenCalled()
    expect(native.browser).not.toHaveBeenCalled()
  })

  it("does not dispatch either opener for a pre-cancelled operation", async () => {
    const controller = new AbortController()
    controller.abort()
    await expect(startGatewayLogin(origin, fetcher(), controller.signal)).rejects.toMatchObject({ name: "AbortError" })
    expect(native.launch).not.toHaveBeenCalled()
    expect(native.browser).not.toHaveBeenCalled()
  })

  it("settles cancellation while an Android launch acknowledgement is pending and consumes its late result", async () => {
    vi.useFakeTimers()
    let release!: (value: { completed: boolean }) => void
    native.launch.mockReturnValue(new Promise((resolve) => { release = resolve }))
    const controller = new AbortController()
    let outcome = "pending"
    const login = startGatewayLogin(origin, fetcher(), controller.signal).then(
      () => { outcome = "resolved" },
      (error: unknown) => { outcome = error instanceof Error || error instanceof DOMException ? error.name : "unknown" },
    )
    try {
      await vi.advanceTimersByTimeAsync(0)
      expect(native.launch).toHaveBeenCalledOnce()
      controller.abort()
      await vi.advanceTimersByTimeAsync(1)
      expect(outcome).toBe("AbortError")
      release({ completed: true })
      await login
      expect(outcome).toBe("AbortError")
      expect(native.launch).toHaveBeenCalledOnce()
      expect(native.browser).not.toHaveBeenCalled()
    } finally {
      controller.abort()
      release?.({ completed: true })
      await login
    }
  })
})
