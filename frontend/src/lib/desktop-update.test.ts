import { afterEach, describe, expect, it, vi } from "vitest"

import {
  DesktopUpdateController,
  DESKTOP_UPDATE_INTERVAL_MS,
  watchDesktopUpdates,
} from "./desktop-update"
import type { DesktopUpdate } from "./api"

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

const available: DesktopUpdate = { status: "available", offer: { version: "1.2.4" } }
const settle = async () => { await Promise.resolve(); await Promise.resolve() }

function setup() {
  let now = 0
  const check = vi.fn<(fresh: boolean) => Promise<DesktopUpdate>>().mockResolvedValue({ status: "current" })
  const install = vi.fn<(version: string) => Promise<{ status: string }>>().mockResolvedValue({ status: "restarting" })
  const dismissVersion = vi.fn()
  const onChange = vi.fn()
  const controller = new DesktopUpdateController({
    check, install, progress: vi.fn().mockResolvedValue({ stage: "idle" }), dismissVersion, dismissedVersion: () => "", onChange, now: () => now,
  })
  return { controller, check, install, progress: vi.fn().mockResolvedValue({ stage: "idle" }), dismissVersion, onChange, advance: (ms: number) => { now += ms } }
}

afterEach(() => vi.useRealTimers())

describe("desktop update checks", () => {
  it("checks at startup and again after six hours without bypassing the backend cache", async () => {
    const { controller, check, advance } = setup()
    controller.autoCheck()
    await settle()
    advance(DESKTOP_UPDATE_INTERVAL_MS - 1)
    controller.autoCheck()
    expect(check).toHaveBeenCalledTimes(1)
    advance(1)
    check.mockResolvedValue(available)
    controller.autoCheck()
    await settle()
    expect(check.mock.calls).toEqual([[false], [false]])
    expect(controller.phase).toEqual({ kind: "available", version: "1.2.4" })
  })

  it("keeps a failed background check quiet and allows the next foreground or online event to retry", async () => {
    const { controller, check } = setup()
    check.mockRejectedValueOnce(new Error("offline"))
    controller.autoCheck()
    await settle()
    expect(controller.phase).toEqual({ kind: "hidden" })
    check.mockResolvedValue(available)
    controller.autoCheck()
    await settle()
    expect(check).toHaveBeenCalledTimes(2)
    expect(controller.phase.kind).toBe("available")
  })

  it("preserves an existing offer when a later background request fails", async () => {
    const { controller, check, advance } = setup()
    check.mockResolvedValueOnce(available)
    controller.autoCheck()
    await settle()
    advance(DESKTOP_UPDATE_INTERVAL_MS)
    check.mockResolvedValueOnce({ status: "error", message: "rate limited" })
    controller.autoCheck()
    await settle()
    expect(controller.phase).toEqual({ kind: "available", version: "1.2.4" })
    check.mockRejectedValueOnce(new Error("offline"))
    controller.autoCheck()
    await settle()
    expect(controller.phase.kind).toBe("available")
    check.mockResolvedValueOnce({ status: "unsupported", message: "development build" })
    controller.autoCheck()
    await settle()
    expect(controller.phase).toEqual({ kind: "available", version: "1.2.4" })
    controller.autoCheck()
    expect(check).toHaveBeenCalledTimes(4)
  })

  it("clears a stale manual error after an automatic check reports the build cannot update", async () => {
    const { controller, check } = setup()
    check.mockRejectedValueOnce(new Error("offline"))
    controller.checkManually()
    await settle()
    check.mockResolvedValueOnce({ status: "unsupported", message: "development build" })
    controller.autoCheck()
    await settle()
    expect(controller.phase.kind).toBe("hidden")
    controller.autoCheck()
    expect(check).toHaveBeenCalledTimes(2)
  })

  it("does not overlap automatic requests and does not repeat a pending manual check", async () => {
    const { controller, check } = setup()
    const pending = deferred<DesktopUpdate>()
    check.mockReturnValue(pending.promise)
    controller.autoCheck()
    controller.autoCheck()
    expect(check).toHaveBeenCalledTimes(1)
    controller.checkManually()
    controller.checkManually()
    controller.autoCheck()
    expect(check.mock.calls).toEqual([[false], [true]])
    expect(controller.phase).toEqual({ kind: "checking" })
    pending.resolve({ status: "current" })
    await settle()
    expect(controller.phase).toEqual({ kind: "current" })
  })

  it("ignores an old startup result after the manual check has completed", async () => {
    const { controller, check } = setup()
    const startup = deferred<DesktopUpdate>()
    check.mockReturnValueOnce(startup.promise).mockResolvedValueOnce(available)
    controller.autoCheck()
    controller.checkManually()
    await settle()
    startup.resolve({ status: "current" })
    await settle()
    expect(controller.phase).toEqual({ kind: "available", version: "1.2.4" })
  })

  it("ignores an old rejected request after a manual check", async () => {
    const { controller, check } = setup()
    const startup = deferred<DesktopUpdate>()
    check.mockReturnValueOnce(startup.promise)
    controller.autoCheck()
    controller.checkManually()
    await settle()
    startup.reject(new Error("old network failure"))
    await settle()
    expect(controller.phase).toEqual({ kind: "current" })
  })

  it("shows manual unsupported and server errors, and can close them", async () => {
    const { controller, check } = setup()
    check.mockResolvedValueOnce({ status: "unsupported", message: "Use the packaged Mac app" })
    controller.checkManually()
    await settle()
    expect(controller.phase).toEqual({ kind: "unsupported", message: "Use the packaged Mac app" })
    controller.close()
    expect(controller.phase.kind).toBe("hidden")
    check.mockResolvedValueOnce({ status: "error", message: "GitHub request timed out" })
    controller.checkManually()
    await settle()
    expect(controller.phase).toEqual({ kind: "error", message: "GitHub request timed out" })
    controller.close()
    expect(controller.phase.kind).toBe("hidden")
  })

  it("shows a rejected manual request and retries only when requested", async () => {
    const { controller, check } = setup()
    check.mockRejectedValueOnce("offline")
    controller.checkManually()
    await settle()
    expect(controller.phase).toEqual({ kind: "error", message: "offline" })
    controller.checkManually()
    await settle()
    expect(controller.phase.kind).toBe("current")
    expect(check.mock.calls).toEqual([[true], [true]])
  })

  it("retries after a failed manual check even when an earlier success was recent", async () => {
    const { controller, check, advance } = setup()
    controller.autoCheck()
    await settle()
    advance(60 * 60 * 1000)
    check.mockRejectedValueOnce(new Error("offline"))
    controller.checkManually()
    await settle()
    check.mockResolvedValueOnce(available)
    controller.autoCheck()
    await settle()
    expect(check.mock.calls).toEqual([[false], [true], [false]])
    expect(controller.phase).toEqual({ kind: "available", version: "1.2.4" })
  })

  it("uses safe fallback messages for incomplete API responses", async () => {
    const { controller, check } = setup()
    check.mockResolvedValueOnce({ status: "available" })
    controller.checkManually()
    await settle()
    expect(controller.phase).toEqual({ kind: "error", message: "" })
    check.mockResolvedValueOnce({ status: "unsupported" })
    controller.checkManually()
    await settle()
    expect(controller.phase).toEqual({ kind: "unsupported", message: "" })
  })

  it("hides definitive unsupported automatic checks and throttles repeated focus events", async () => {
    const { controller, check } = setup()
    check.mockResolvedValue({ status: "unsupported", message: "development build" })
    controller.autoCheck()
    await settle()
    controller.autoCheck()
    expect(controller.phase.kind).toBe("hidden")
    expect(check).toHaveBeenCalledTimes(1)
  })

  it("remembers Not now without dismissing a later manual offer", async () => {
    const { controller, check, dismissVersion } = setup()
    check.mockResolvedValue(available)
    controller.autoCheck()
    await settle()
    controller.dismiss()
    expect(dismissVersion).toHaveBeenCalledWith("1.2.4")
    expect(controller.phase.kind).toBe("hidden")
    controller.checkManually()
    await settle()
    expect(controller.phase.kind).toBe("available")
  })

  it("ignores a dismissed version during automatic checks", async () => {
    const check = vi.fn().mockResolvedValue(available)
    const controller = new DesktopUpdateController({
      check, install: vi.fn(), progress: vi.fn().mockResolvedValue({ stage: "idle" }), dismissedVersion: () => "1.2.4", dismissVersion: vi.fn(), onChange: vi.fn(),
    })
    controller.autoCheck()
    await settle()
    expect(controller.phase.kind).toBe("hidden")
    controller.checkManually()
    await settle()
    expect(controller.phase.kind).toBe("available")
  })
})

describe("desktop update installation", () => {
  it("installs once after confirmation and ignores checks and stale results while installing", async () => {
    const { controller, check, install, advance } = setup()
    const download = deferred<{ status: string }>()
    const stale = deferred<DesktopUpdate>()
    check.mockResolvedValueOnce(available).mockReturnValueOnce(stale.promise)
    install.mockReturnValueOnce(download.promise)
    controller.autoCheck()
    await settle()
    expect(install).not.toHaveBeenCalled()
    advance(DESKTOP_UPDATE_INTERVAL_MS)
    controller.autoCheck()
    controller.install()
    controller.install()
    controller.autoCheck()
    controller.checkManually()
    controller.close()
    controller.dismiss()
    stale.resolve({ status: "current" })
    await settle()
    expect(controller.phase).toEqual({ kind: "working" })
    expect(install.mock.calls).toEqual([["1.2.4"]])
    expect(check).toHaveBeenCalledTimes(2)
    download.resolve({ status: "restarting" })
    await settle()
    controller.autoCheck()
    controller.checkManually()
    expect(controller.phase).toEqual({ kind: "restarting" })
    expect(check).toHaveBeenCalledTimes(2)
  })

  it("allows a failed install to be checked and explicitly confirmed again", async () => {
    const { controller, check, install } = setup()
    check.mockResolvedValue(available)
    install.mockRejectedValueOnce(new Error("download failed"))
    controller.install()
    controller.dismiss()
    expect(install).not.toHaveBeenCalled()
    controller.autoCheck()
    await settle()
    controller.install()
    await settle()
    expect(controller.phase).toEqual({ kind: "error", message: "download failed", version: "1.2.4" })
    controller.checkManually()
    await settle()
    expect(install).toHaveBeenCalledTimes(1)
    controller.install()
    await settle()
    expect(controller.phase.kind).toBe("restarting")
  })

  it("ignores request completion and actions after disposal", async () => {
    const { controller, check, install, onChange } = setup()
    const pending = deferred<DesktopUpdate>()
    check.mockReturnValue(pending.promise)
    controller.autoCheck()
    controller.dispose()
    pending.resolve(available)
    await settle()
    controller.checkManually()
    controller.install()
    controller.autoCheck()
    expect(onChange).not.toHaveBeenCalled()
    expect(install).not.toHaveBeenCalled()
    expect(check).toHaveBeenCalledTimes(1)
  })

  it("does not paint a completed install after the window has unmounted", async () => {
    const { controller, check, install, onChange } = setup()
    const pending = deferred<{ status: string }>()
    check.mockResolvedValue(available)
    install.mockReturnValue(pending.promise)
    controller.autoCheck()
    await settle()
    controller.install()
    controller.dispose()
    pending.reject(new Error("window closed"))
    await settle()
    expect(onChange).toHaveBeenLastCalledWith({ kind: "working" })
  })
})

describe("desktop update window lifecycle", () => {
  it("schedules six hours from a successful response even when the startup request takes time", async () => {
    vi.useFakeTimers()
    vi.setSystemTime(0)
    const startup = deferred<DesktopUpdate>()
    const check = vi.fn().mockReturnValueOnce(startup.promise).mockResolvedValue({ status: "current" })
    const controller = new DesktopUpdateController({
      check, install: vi.fn(), progress: vi.fn().mockResolvedValue({ stage: "idle" }), dismissedVersion: () => "", dismissVersion: vi.fn(), onChange: vi.fn(),
    })
    const stop = watchDesktopUpdates(controller)
    await vi.advanceTimersByTimeAsync(2500)
    startup.resolve({ status: "current" })
    await settle()
    await vi.advanceTimersByTimeAsync(DESKTOP_UPDATE_INTERVAL_MS - 1)
    expect(check).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(check).toHaveBeenCalledTimes(2)
    stop()
  })

  it("checks startup, foreground, network recovery and six-hour ticks, and removes all listeners on stop", async () => {
    vi.useFakeTimers()
    const { controller, check, advance } = setup()
    check.mockRejectedValueOnce(new Error("offline"))
    const stop = watchDesktopUpdates(controller)
    await settle()
    window.dispatchEvent(new Event("online"))
    await settle()
    expect(check).toHaveBeenCalledTimes(2)
    for (let i = 0; i < 5; i++) {
      window.dispatchEvent(new Event("focus"))
      document.dispatchEvent(new Event("visibilitychange"))
    }
    expect(check).toHaveBeenCalledTimes(2)
    advance(DESKTOP_UPDATE_INTERVAL_MS)
    await vi.advanceTimersByTimeAsync(DESKTOP_UPDATE_INTERVAL_MS)
    expect(check).toHaveBeenCalledTimes(3)
    advance(DESKTOP_UPDATE_INTERVAL_MS)
    vi.spyOn(document, "visibilityState", "get").mockReturnValueOnce("hidden")
    document.dispatchEvent(new Event("visibilitychange"))
    expect(check).toHaveBeenCalledTimes(3)
    document.dispatchEvent(new Event("visibilitychange"))
    await settle()
    expect(check).toHaveBeenCalledTimes(4)
    stop()
    advance(DESKTOP_UPDATE_INTERVAL_MS)
    window.dispatchEvent(new Event("focus"))
    window.dispatchEvent(new Event("online"))
    document.dispatchEvent(new Event("visibilitychange"))
    await vi.advanceTimersByTimeAsync(DESKTOP_UPDATE_INTERVAL_MS)
    expect(check).toHaveBeenCalledTimes(4)
  })
})
