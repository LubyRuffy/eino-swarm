import { expect, it, vi } from "vitest"
import { openBody, type StreamBodyPlugin } from "./stream-body"

const input = () => ({ url: "https://model.invalid/v1/chat/completions", headers: {}, body: "{}", signal: new AbortController().signal, timeoutMs: 1000 })

it("cancels a browser response before its body iterator is first consumed", async () => {
  const cancelled = vi.fn()
  const opened = await openBody({ ...input(), fetchImpl: vi.fn(async () => new Response(new ReadableStream({ cancel: cancelled }), { status: 401 })) })
  expect(opened.status).toBe(401)
  await opened.cancel()
  expect(cancelled).toHaveBeenCalledTimes(1)
})

it("cancels and removes native listeners before its body iterator is first consumed", async () => {
  const listeners: Partial<Record<"status" | "chunk", (event: { id?: string; status?: number; text?: string }) => void>> = {}
  const removed = vi.fn()
  let complete!: (result: { status: number; aborted: boolean }) => void
  const native: StreamBodyPlugin = {
    addListener: vi.fn<StreamBodyPlugin["addListener"]>(async (event, callback) => { listeners[event] = callback; return { remove: async () => { removed(event) } } }),
    open: vi.fn<StreamBodyPlugin["open"]>((options) => new Promise((resolve) => { complete = resolve; listeners.status?.({ id: options.id, status: 401 }) })),
    cancel: vi.fn(async () => { complete({ status: 401, aborted: true }) }),
  }
  const opened = await openBody({ ...input(), nativeStream: native })
  expect(opened.status).toBe(401)
  const first = opened.cancel()
  const concurrent = opened.cancel()
  expect(concurrent).toBe(first)
  await Promise.all([first, concurrent])
  expect(native.cancel).toHaveBeenCalledTimes(1)
  expect(removed.mock.calls.map(([event]) => event).sort()).toEqual(["chunk", "status"])
})

it.each(["status", "chunk"] as const)("an abort during native %s listener registration does not start a transport", async (stage) => {
  const controller = new AbortController()
  const removed = vi.fn()
  const native: StreamBodyPlugin = {
    addListener: vi.fn<StreamBodyPlugin["addListener"]>(async (event) => {
      if (event === stage) controller.abort("stop")
      return { remove: async () => { removed(event) } }
    }),
    open: vi.fn(async () => ({ status: 200 })),
    cancel: vi.fn(async () => undefined),
  }
  await expect(openBody({ ...input(), signal: controller.signal, nativeStream: native })).rejects.toMatchObject({ kind: "abort" })
  expect(native.open).not.toHaveBeenCalled()
  expect(native.cancel).not.toHaveBeenCalled()
  expect(removed).toHaveBeenCalledTimes(vi.mocked(native.addListener).mock.calls.length)
})

it("a native plugin that synchronously aborts from open is cancelled and cleaned exactly once", async () => {
  const controller = new AbortController()
  const removed = vi.fn()
  const native: StreamBodyPlugin = {
    addListener: vi.fn<StreamBodyPlugin["addListener"]>(async (event) => ({ remove: async () => { removed(event) } })),
    open: vi.fn(async () => { controller.abort("stop"); return { status: 200, aborted: true } }),
    cancel: vi.fn(async () => { controller.abort("stop") }),
  }
  await expect(openBody({ ...input(), signal: controller.signal, nativeStream: native })).rejects.toMatchObject({ kind: "abort" })
  expect(native.open).toHaveBeenCalledTimes(1)
  expect(native.cancel).toHaveBeenCalledTimes(1)
  expect(removed.mock.calls.map(([event]) => event).sort()).toEqual(["chunk", "status"])
})

it("an already cancelled native call registers no listeners and never starts a transport", async () => {
  const controller = new AbortController(); controller.abort("stop")
  const native: StreamBodyPlugin = {
    addListener: vi.fn<StreamBodyPlugin["addListener"]>(async () => ({ remove: async () => undefined })),
    open: vi.fn(async () => ({ status: 200, aborted: true })),
    cancel: vi.fn(async () => undefined),
  }
  await expect(openBody({ ...input(), signal: controller.signal, nativeStream: native })).rejects.toMatchObject({ kind: "abort" })
  expect(native.open).not.toHaveBeenCalled()
  expect(native.addListener).not.toHaveBeenCalled()
})
