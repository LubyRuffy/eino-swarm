import { describe, expect, it, vi } from "vitest"

import type { RemoteLink } from "./link"
import { listWorkspaceFiles, previewKind, readWorkspaceFile } from "./remote-files"
import type { RemoteResponse } from "./rpc"

function link(responses: Partial<RemoteResponse>[]) {
  const rpc = vi.fn(async () => ({ v: 1, id: "", ok: true, ...responses.shift() } as RemoteResponse))
  return { path: "relay", alive: () => true, close: vi.fn(), announceDevice: vi.fn(), rpc } as RemoteLink & { rpc: typeof rpc }
}

describe("phone workspace files", () => {
  it("collects every page while preserving the PC workspace paths", async () => {
    const remote = link([
      { files: [{ path: "outputs", name: "outputs", size: 0, dir: true, modified: "", uploaded: false }], more: true, next: "1" },
      { files: [{ path: "outputs/report.md", name: "report.md", size: 4, dir: false, modified: "", uploaded: false }] },
    ])
    expect((await listWorkspaceFiles(remote, "t1")).map((f) => f.path)).toEqual(["outputs", "outputs/report.md"])
    expect(remote.rpc).toHaveBeenNthCalledWith(2, { op: "files", thread_id: "t1", cursor: "1" })
  })

  it("reassembles bounded chunks and rejects a stalled or oversized response", async () => {
    const remote = link([
      { file_chunk: { path: "a.txt", name: "a.txt", size: 5, mime: "text/plain", offset: 0,
        next_offset: 3, data: btoa("abc"), more: true } },
      { file_chunk: { path: "a.txt", name: "a.txt", size: 5, mime: "text/plain", offset: 3,
        next_offset: 5, data: btoa("de"), more: false } },
    ])
    const got = await readWorkspaceFile(remote, "t1", "a.txt")
    expect(new TextDecoder().decode(got.bytes)).toBe("abcde")
    expect(remote.rpc).toHaveBeenNthCalledWith(2, {
      op: "file_chunk", thread_id: "t1", file_path: "a.txt", before: 3,
    })
    const stalled = link([{ file_chunk: { path: "a.txt", name: "a.txt", size: 2, mime: "text/plain",
      offset: 0, next_offset: 0, data: "", more: true } }])
    await expect(readWorkspaceFile(stalled, "t1", "a.txt")).rejects.toThrow(/no progress/)
    const huge = link([{ file_chunk: { path: "a.txt", name: "a.txt", size: 33 * 1024 * 1024,
      mime: "text/plain", offset: 0, next_offset: 0, data: "", more: true } }])
    await expect(readWorkspaceFile(huge, "t1", "a.txt")).rejects.toThrow(/changed/)
  })

  it("never renders agent HTML or SVG as same-origin markup", () => {
    expect(previewKind("index.html", "text/html", 10)).toBe("text")
    expect(previewKind("image.svg", "image/svg+xml", 10)).toBe("text")
    expect(previewKind("photo.png", "image/png", 10)).toBe("image")
    expect(previewKind("report.pdf", "application/pdf", 10)).toBe("pdf")
    expect(previewKind("archive.zip", "application/zip", 10)).toBe("download")
  })
})
