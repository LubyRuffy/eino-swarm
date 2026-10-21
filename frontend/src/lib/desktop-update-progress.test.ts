import { afterEach, describe, expect, it, vi } from "vitest"

import { DesktopUpdateController } from "./desktop-update"
import type { DesktopUpdateProgress } from "./api"

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

const downloading: DesktopUpdateProgress = {
  stage: "downloading", version: "1.2.4", downloaded_bytes: 512, total_bytes: 1024,
}

function setup() {
  const pending = deferred<{ status: string }>()
  const check = vi.fn().mockResolvedValue({ status: "available", offer: { version: "1.2.4" } })
  const install = vi.fn().mockReturnValue(pending.promise)
  const progress = vi.fn().mockResolvedValue(downloading)
  const onChange = vi.fn()
  const controller = new DesktopUpdateController({
    check, install, progress, onChange, dismissedVersion: () => "", dismissVersion: vi.fn(),
  })
  return { controller, check, install, progress, pending, onChange }
}

const settle = async () => { await Promise.resolve(); await Promise.resolve() }
afterEach(() => vi.useRealTimers())

describe("desktop installation progress", () => {
  it("shows actual bytes and stages, tolerates a failed poll and stops polling after install", async () => {
    vi.useFakeTimers()
    const { controller, progress, pending } = setup()
    await controller.checkManually()
    controller.install()
    await settle()
    expect(controller.phase).toEqual({ kind: "working", progress: downloading })
    progress.mockRejectedValueOnce(new Error("temporary polling failure"))
    await vi.advanceTimersByTimeAsync(500)
    expect(controller.phase.kind).toBe("working")
    progress.mockResolvedValue({ ...downloading, stage: "installing", downloaded_bytes: 1024 })
    await vi.advanceTimersByTimeAsync(500)
    expect(controller.phase).toMatchObject({ kind: "working", progress: { stage: "installing" } })
    pending.resolve({ status: "restarting" })
    await settle()
    expect(controller.phase.kind).toBe("restarting")
    const calls = progress.mock.calls.length
    await vi.advanceTimersByTimeAsync(2000)
    expect(progress).toHaveBeenCalledTimes(calls)
    controller.dispose()
  })

  it("retries the already confirmed version directly and preserves its failure on focus", async () => {
    const { controller, check, install, pending } = setup()
    await controller.checkManually()
    controller.install()
    pending.reject(new Error("download timed out"))
    await settle()
    expect(controller.phase).toMatchObject({ kind: "error", version: "1.2.4" })
    await controller.autoCheck()
    expect(controller.phase.kind).toBe("error")
    const next = deferred<{ status: string }>()
    install.mockReturnValue(next.promise)
    controller.retry()
    controller.retry()
    expect(install.mock.calls).toEqual([["1.2.4"], ["1.2.4"]])
    expect(check).toHaveBeenCalledTimes(1)
    controller.dispose()
    next.resolve({ status: "restarting" })
  })

  it("ignores mismatched versions and late progress from a failed attempt", async () => {
    const { controller, progress, install, pending } = setup()
    const stale = deferred<DesktopUpdateProgress>()
    progress.mockReturnValueOnce(stale.promise)
    await controller.checkManually()
    controller.install()
    const signal = progress.mock.calls[0][0] as AbortSignal
    pending.reject(new Error("failed"))
    await settle()
    expect(signal.aborted).toBe(true)
    const next = deferred<{ status: string }>()
    install.mockReturnValue(next.promise)
    progress.mockResolvedValue({ ...downloading, version: "another-version" })
    controller.retry()
    await settle()
    stale.resolve({ ...downloading, stage: "installing" })
    await settle()
    expect(controller.phase).toEqual({ kind: "working" })
    controller.dispose()
    next.resolve({ status: "restarting" })
  })

  it("aborts an outstanding poll and ignores completion after unmount", async () => {
    const { controller, progress, pending, onChange } = setup()
    const polling = deferred<DesktopUpdateProgress>()
    progress.mockReturnValue(polling.promise)
    await controller.checkManually()
    controller.install()
    const signal = progress.mock.calls[0][0] as AbortSignal
    controller.dispose()
    expect(signal.aborted).toBe(true)
    const calls = onChange.mock.calls.length
    polling.resolve(downloading)
    pending.resolve({ status: "restarting" })
    await settle()
    expect(onChange).toHaveBeenCalledTimes(calls)
  })

  it("cancels a stalled observation and continues polling without failing the install", async () => {
    vi.useFakeTimers()
    const { controller, progress, pending } = setup()
    progress.mockImplementationOnce((signal: AbortSignal) => new Promise((_, reject) => {
      signal.addEventListener("abort", () => reject(new Error("poll timed out")), { once: true })
    }))
    await controller.checkManually()
    controller.install()
    await vi.advanceTimersByTimeAsync(5500)
    expect(progress).toHaveBeenCalledTimes(2)
    expect(controller.phase).toEqual({ kind: "working", progress: downloading })
    controller.dispose()
    pending.resolve({ status: "restarting" })
  })
})
