import { act, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { loadGatewayPending, loadGatewaySession, startGatewayLogin } from "./gateway-account"
import { useGatewayAccount } from "./use-gateway-account"

const bridge = vi.hoisted(() => ({
  info: vi.fn<() => Promise<{ version: string }>>(),
  open: vi.fn<(options: { url: string }) => Promise<void>>(),
}))
vi.mock("@capacitor/core", () => ({
  Capacitor: { getPlatform: () => "android", isNativePlatform: () => true },
  registerPlugin: () => ({}),
}))
vi.mock("@capacitor/app", () => ({ App: {
  getInfo: bridge.info,
  addListener: vi.fn(async () => ({ remove: async () => undefined })),
} }))
vi.mock("@capacitor/browser", () => ({ Browser: { open: bridge.open } }))
vi.mock("@capacitor/app-launcher", () => ({ AppLauncher: {
  openUrl: async (options: { url: string }) => { await bridge.open(options); return { completed: true } },
} }))

const origin = "https://gateway.example.test"
type Phase = "version" | "browser"
type RawWindow = Window & { CapacitorWebFetch?: typeof fetch }
const rawWindow = window as RawWindow
const fetcher = vi.fn<typeof fetch>(async (_url, init) => {
  if (init?.signal?.aborted) throw new DOMException("Aborted", "AbortError")
  return new Response(JSON.stringify({ data: {
    device_code: "D".repeat(43), user_code: "abcd-1234-abcd-1234", interval: 5,
    verification_uri: origin + "/client/api/v1/auth/sso/nosec/start?mobile_code=abcd-1234-abcd-1234",
    expires_at: new Date(Date.now() + 600_000).toISOString(),
  } }))
})

/** The production App.getInfo/system-browser calls receive an unsettled native
 * promise. No replacement of the login function or account state is involved. */
function holdNative(phase: Phase, rejectLate = false) {
  let release!: () => void
  const pending = new Promise<void>((resolve, reject) => {
    release = rejectLate ? () => reject(new Error("native acknowledgement failed")) : resolve
  })
  bridge.info.mockImplementation(async () => {
    if (phase === "version") await pending
    return { version: "0.1.49" }
  })
  bridge.open.mockImplementation(async () => { if (phase === "browser") await pending })
  return release
}

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem("zwai.phone.gateway.origin", origin)
  vi.useFakeTimers()
  vi.clearAllMocks()
  bridge.info.mockReset()
  bridge.open.mockReset()
  rawWindow.CapacitorWebFetch = fetcher
})
afterEach(() => {
  delete rawWindow.CapacitorWebFetch
  localStorage.clear()
  vi.useRealTimers()
})

describe.each(["version", "browser"] as const)("Gateway login with an unsettled native %s promise", (phase) => {
  it("settles the login task on cancellation without waiting for a native acknowledgement", async () => {
    const release = holdNative(phase)
    const controller = new AbortController()
    let outcome = "pending"
    const login = startGatewayLogin(origin, fetcher, controller.signal).then(
      () => { outcome = "resolved" },
      (error: unknown) => { outcome = error instanceof Error || error instanceof DOMException ? error.name : "unknown" },
    )
    try {
      await vi.advanceTimersByTimeAsync(0)
      expect(phase === "version" ? bridge.info : bridge.open).toHaveBeenCalledOnce()
      controller.abort()
      await vi.advanceTimersByTimeAsync(1)
      expect(outcome).toBe("AbortError")
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      release()
      await login
    }
  })

  it("lets Sign out clear the checking UI and keeps a later login authoritative", async () => {
    const release = holdNative(phase)
    const { result, unmount } = renderHook(useGatewayAccount)
    let login!: Promise<void>
    try {
      await act(async () => { login = result.current.login(); await vi.advanceTimersByTimeAsync(0) })
      expect(result.current.status).toBe("checking")
      expect(result.current.busy).toBe(true)
      await act(async () => { await result.current.logout() })
      expect(result.current.status).toBe("signed_out")
      expect(result.current.busy).toBe(false)
      expect(loadGatewayPending()).toBeNull()
      expect(vi.getTimerCount()).toBe(1) // Only the account refresh interval remains.
      expect(loadGatewaySession()).toBeNull()
      bridge.info.mockResolvedValueOnce({ version: "0.1.49" })
      bridge.open.mockResolvedValueOnce(undefined)
      await act(async () => { await result.current.login() })
      expect(result.current.status).toBe("authorization_pending")
      const current = loadGatewayPending()
      expect(current).not.toBeNull()
      const requested = fetcher.mock.calls.length
      await act(async () => { release(); await login })
      expect(result.current.status).toBe("authorization_pending")
      expect(result.current.busy).toBe(false)
      expect(loadGatewayPending()?.candidateToken === current?.candidateToken).toBe(true)
      expect(loadGatewaySession()).toBeNull()
      expect(fetcher).toHaveBeenCalledTimes(requested)
    } finally {
      unmount()
      release()
      await login
    }
  })

  it("ends checking after fifteen seconds and consumes a late native rejection without disturbing a retry", async () => {
    const release = holdNative(phase, true)
    const { result, unmount } = renderHook(useGatewayAccount)
    let login!: Promise<void>
    try {
      await act(async () => { login = result.current.login(); await vi.advanceTimersByTimeAsync(0) })
      expect(phase === "version" ? bridge.info : bridge.open).toHaveBeenCalledOnce()
      await act(async () => { await vi.advanceTimersByTimeAsync(14_999) })
      expect(result.current.status).toBe("checking")
      await act(async () => { await vi.advanceTimersByTimeAsync(1) })
      expect(result.current.status).toBe("error")
      expect(result.current.error).toBe("gateway_timeout")
      expect(result.current.busy).toBe(false)
      expect(loadGatewayPending()).toBeNull()
      bridge.info.mockResolvedValueOnce({ version: "0.1.49" })
      bridge.open.mockResolvedValueOnce(undefined)
      await act(async () => { await result.current.login() })
      const current = loadGatewayPending()
      expect(current).not.toBeNull()
      const requested = fetcher.mock.calls.length
      await act(async () => { release(); await login; await vi.advanceTimersByTimeAsync(0) })
      expect(result.current.status).toBe("authorization_pending")
      expect(loadGatewayPending()?.candidateToken === current?.candidateToken).toBe(true)
      expect(fetcher).toHaveBeenCalledTimes(requested)
    } finally {
      unmount()
      release()
      await login
    }
  })
})

it("does not invoke a native plugin when login was already cancelled", async () => {
  const release = holdNative("version")
  const controller = new AbortController()
  controller.abort()
  let outcome = "pending"
  const login = startGatewayLogin(origin, fetcher, controller.signal).then(
    () => { outcome = "resolved" },
    (error: unknown) => { outcome = error instanceof Error || error instanceof DOMException ? error.name : "unknown" },
  )
  try {
    await vi.advanceTimersByTimeAsync(0)
    expect(bridge.info).not.toHaveBeenCalled()
    expect(bridge.open).not.toHaveBeenCalled()
    expect(fetcher).not.toHaveBeenCalled()
    expect(outcome).toBe("AbortError")
  } finally {
    release()
    await login
  }
})
