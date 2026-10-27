import { afterEach, describe, expect, it, vi } from "vitest"

import { api, ApiError } from "@/lib/api"
import { MAX_FILE_PREVIEW_BYTES, readFilePreview } from "./file-preview"

afterEach(() => { vi.unstubAllGlobals() })

function serve(body: BodyInit | null, headers: HeadersInit = {}) {
  const response = new Response(body, { headers })
  const fetch = vi.fn().mockResolvedValue(response)
  vi.stubGlobal("fetch", fetch)
  return { response, fetch }
}

function read(path = "notes.md", signal = new AbortController().signal) {
  return readFilePreview("thread_1", path, signal)
}

describe("workspace file preview", () => {
  it("reads Unicode markdown through the encoded attachment URL", async () => {
    const { fetch } = serve("# 文件\n\nhello", { "content-type": "application/octet-stream" })
    const signal = new AbortController().signal
    expect(await read("目录/a #?.md", signal)).toEqual({ kind: "text", text: "# 文件\n\nhello" })
    expect(fetch).toHaveBeenCalledWith(api.downloadURL("thread_1", "目录/a #?.md"), {
      signal, credentials: "same-origin", mode: "same-origin", redirect: "error", cache: "no-store",
    })
  })

  it.each([
    ["notes", "text/plain; charset=utf-8"],
    ["data", "application/ld+json"],
    ["page.HTML", "application/octet-stream"],
    ["image.svg", "image/svg+xml"],
    ["Dockerfile", "application/octet-stream"],
    [".env.local", "application/octet-stream"],
  ])("keeps %s as text rather than executing its contents", async (path, mime) => {
    const text = '<script>alert("x")</script>'
    serve(text, { "content-type": mime })
    expect(await read(path)).toEqual({ kind: "text", text })
  })

  it("preserves Windows CRLF lines, tabs and empty lines", async () => {
    const text = "# Heading\r\n\r\n\tindented\r\n"
    serve(text, { "content-type": "text/plain" })
    expect(await read()).toEqual({ kind: "text", text })
  })

  it("preserves every original image byte and the normalized raster MIME", async () => {
    // A complete one-pixel PNG, not a magic-byte-only fixture.
    const bytes = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jWZkAAAAASUVORK5CYII="), (c) => c.charCodeAt(0))
    serve(bytes, { "content-type": "IMAGE/PNG" })
    const result = await read("pixel.png")
    expect(result.kind).toBe("image")
    if (result.kind !== "image") throw new Error("expected an image")
    expect(result.blob.type).toBe("image/png")
    const stored = await new Promise<ArrayBuffer>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(reader.result as ArrayBuffer)
      reader.onerror = () => reject(reader.error)
      reader.readAsArrayBuffer(result.blob)
    })
    expect(new Uint8Array(stored)).toEqual(bytes)
  })

  it.each([
    ["binary.md", "text/plain", new Uint8Array([65, 0, 66])],
    ["invalid.txt", "text/plain", new Uint8Array([0xc3, 0x28])],
    ["control.txt", "text/plain", new Uint8Array([65, 1, 66])],
    ["archive.zip", "application/zip", new Uint8Array([65, 66])],
    ["drawing", "image/svg+xml", new Uint8Array([65, 66])],
  ])("keeps %s downloadable when it cannot safely render", async (path, mime, bytes) => {
    serve(bytes, { "content-type": mime })
    expect(await read(path)).toEqual({ kind: "download", reason: "unsupported" })
  })

  it("refuses an oversized response before consuming its stream", async () => {
    const cancel = vi.fn()
    const body = new ReadableStream({ cancel })
    serve(body, { "content-length": String(MAX_FILE_PREVIEW_BYTES + 1) })
    expect(await read()).toEqual({ kind: "download", reason: "too-large" })
    expect(cancel).toHaveBeenCalledOnce()
  })

  it.each([undefined, "1"])("caps actual bytes even with content-length %s", async (length) => {
    const cancel = vi.fn()
    const body = new ReadableStream({
      start(controller) {
        controller.enqueue(new Uint8Array(MAX_FILE_PREVIEW_BYTES))
        controller.enqueue(new Uint8Array([65]))
      },
      cancel,
    })
    serve(body, length ? { "content-length": length } : {})
    expect(await read()).toEqual({ kind: "download", reason: "too-large" })
    expect(cancel).toHaveBeenCalledOnce()
  })

  it("accepts the exact limit and an empty text file without truncation", async () => {
    serve(new Uint8Array(MAX_FILE_PREVIEW_BYTES).fill(65))
    expect(await read()).toEqual({ kind: "text", text: "A".repeat(MAX_FILE_PREVIEW_BYTES) })
    serve(null, { "content-length": "0", "content-type": "text/plain" })
    expect(await read()).toEqual({ kind: "text", text: "" })
  })

  it("does not use an unbounded arrayBuffer fallback when no body is available", async () => {
    serve(null, { "content-type": "text/plain" })
    await expect(read()).rejects.toThrow("readable body")
  })

  it("reports an HTTP refusal and cancels its unused body", async () => {
    const cancel = vi.fn()
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(new ReadableStream({ cancel }), {
      status: 404, statusText: "Not Found",
    })))
    await expect(read()).rejects.toEqual(new ApiError("404 Not Found", 404))
    expect(cancel).toHaveBeenCalledOnce()
  })

  it("propagates transport and stream errors", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")))
    await expect(read()).rejects.toThrow("offline")
    serve(new ReadableStream({ start(controller) { controller.error(new Error("read failed")) } }))
    await expect(read()).rejects.toThrow("read failed")
  })

  it("does not start a request after cancellation", async () => {
    const { fetch } = serve("never")
    const controller = new AbortController()
    controller.abort(new Error("closed"))
    await expect(read("notes.md", controller.signal)).rejects.toThrow("closed")
    expect(fetch).not.toHaveBeenCalled()
  })

  it("cancels a pending stream read when the preview closes", async () => {
    const cancel = vi.fn()
    let markStarted = () => {}
    const started = new Promise<void>((resolve) => { markStarted = resolve })
    serve(new ReadableStream({ pull() { markStarted() }, cancel }))
    const controller = new AbortController()
    const pending = read("notes.md", controller.signal)
    await started
    controller.abort(new Error("closed"))
    await expect(pending).rejects.toThrow("closed")
    expect(cancel).toHaveBeenCalledOnce()
  })
})
