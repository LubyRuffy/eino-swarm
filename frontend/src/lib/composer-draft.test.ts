import { describe, expect, it, vi } from "vitest"
import { ComposerDraftSession, type ComposerDraftRecord, type ComposerDraftTransport } from "./composer-draft"
import { ApiError } from "./api"

function fixture(key = "thread:a") {
  let stored: ComposerDraftRecord = { key, revision: 0, text: "", quotes: [], files: [], state: "editing" }
  const blobs = new Map<string, File>()
  const transport: ComposerDraftTransport = {
    load: vi.fn(async () => structuredClone(stored)),
    file: vi.fn(async (_key, id) => blobs.get(id)!),
    save: vi.fn(async (next, files) => {
      if (next.revision !== stored.revision) throw new Error("draft conflict")
      files.forEach((file: File, id: string) => blobs.set(id, file))
      stored = { ...structuredClone(next), revision: stored.revision + 1 }
      return structuredClone(stored)
    }),
    clear: vi.fn(async (_key, revision) => {
      if (revision !== stored.revision) throw new Error("draft conflict")
      stored = { ...stored, revision: stored.revision + 1, text: "", quotes: [], files: [], state: "editing" }
      return structuredClone(stored)
    }),
  }
  return { transport, blobs, stored: () => stored }
}

describe("Durable composer snapshots", () => {
  it("saves text, quotes and unchanged file/image bytes together and restores a new session", async () => {
    const f = fixture()
    const session = new ComposerDraftSession("thread:a", f.transport)
    await session.load()
    session.update({ text: "mixed 中文", quotes: [{ id: "q1", text: "context" }] })
    const file = new File([new Uint8Array([0, 255, 12])], "bytes.bin", { type: "application/octet-stream", lastModified: 123 })
    session.addFiles([file], "file")
    const pixels = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="), (c) => c.charCodeAt(0))
    session.addFiles([new File([pixels], "photo.png", { type: "image/png" })], "image")
    await session.flush()
    expect(f.stored()).toMatchObject({ text: "mixed 中文", quotes: [{ id: "q1", text: "context" }] })
    expect(f.stored().files.map((file) => file.kind)).toEqual(["file", "image"])
    expect(f.blobs.size).toBe(2)
    const recovered = new ComposerDraftSession("thread:a", f.transport)
    await recovered.load()
    expect(recovered.getSnapshot().draft.text).toBe("mixed 中文")
    expect(recovered.getSnapshot().draft.files[0].file.size).toBe(3)
    expect(recovered.getSnapshot().draft.files[0].file.lastModified).toBe(123)
    expect(recovered.getSnapshot().draft.files[1].file.type).toBe("image/png")
    expect(recovered.getSnapshot().draft.files[1].file.size).toBe(pixels.length)
    expect(await new Promise((resolve) => {
      const reader = new FileReader()
      reader.onload = () => resolve([...new Uint8Array(reader.result as ArrayBuffer)])
      reader.readAsArrayBuffer(recovered.getSnapshot().draft.files[0].file)
    })).toEqual([0, 255, 12])
    session.update({ text: "later edit" })
    await session.flush()
    expect(vi.mocked(f.transport.save).mock.calls.at(-1)?.[1].size).toBe(0)
  })

  it("keeps a failed save and requires an acknowledged transaction before send", async () => {
    const f = fixture()
    const session = new ComposerDraftSession("thread:a", f.transport)
    await session.load()
    session.update({ text: "not yet saved" })
    vi.mocked(f.transport.save).mockRejectedValueOnce(new Error("quota exceeded"))
    await expect(session.beginSend()).rejects.toThrow("quota exceeded")
    expect(session.getSnapshot()).toMatchObject({ draft: { text: "not yet saved" }, saved: false, error: "quota exceeded" })
    await session.flush()
    const sent = await session.beginSend()
    expect(f.stored().state).toBe("sending")
    await session.completeSend(sent)
    expect(session.getSnapshot().draft.text).toBe("")
    expect(f.stored().revision).toBeGreaterThan(sent.revision)
  })

  it("restores an interrupted send as uncertain and never sends it automatically", async () => {
    const f = fixture()
    const session = new ComposerDraftSession("thread:a", f.transport)
    await session.load()
    session.update({ text: "server may have accepted" })
    await session.beginSend()
    const recovered = new ComposerDraftSession("thread:a", f.transport)
    await recovered.load()
    expect(recovered.getSnapshot().draft.state).toBe("uncertain")
    await expect(recovered.beginSend()).rejects.toThrow()
    expect(recovered.getSnapshot().draft.text).toBe("server may have accepted")
    await recovered.resumeEditing()
    expect(recovered.getSnapshot().draft.state).toBe("editing")
  })

  it("keeps the original snapshot when deleting it fails", async () => {
    const f = fixture()
    const session = new ComposerDraftSession("thread:a", f.transport)
    await session.load()
    session.update({ text: "preserve on delete failure" })
    await session.flush()
    vi.mocked(f.transport.clear).mockRejectedValueOnce(new Error("disk unavailable"))
    await expect(session.discard()).rejects.toThrow("disk unavailable")
    expect(session.getSnapshot()).toMatchObject({ draft: { text: "preserve on delete failure" }, error: "disk unavailable" })
  })

  it("keeps the original new-conversation draft through migration and an interrupted send", async () => {
    const source = fixture("new:home")
    const destination = fixture("thread:new")
    const home = new ComposerDraftSession("new:home", source.transport)
    const thread = new ComposerDraftSession("thread:new", destination.transport)
    await home.load()
    home.update({ text: "first message", quotes: [{ id: "q", text: "context" }] })
    home.addFiles([new File(["original bytes"], "file.txt")], "file")
    await home.transferTo(thread)
    expect(source.stored().text).toBe("first message")
    expect(destination.stored().text).toBe("first message")
    expect(destination.blobs.size).toBe(1)
    const targetReceipt = await thread.beginSend()
    const sourceReceipt = await home.beginSend()
    const staleNativeProbe = new ComposerDraftSession("new:home", source.transport)
    await staleNativeProbe.load()
    expect(staleNativeProbe.getSnapshot().draft).toMatchObject({ state: "uncertain", text: "first message" })
    await thread.completeSend(targetReceipt)
    await home.completeSend(sourceReceipt)
    expect(source.stored().text).toBe("")
    expect(destination.stored().text).toBe("")
  })

  it("does not remove the source when destination storage fails during migration", async () => {
    const source = fixture("new:home")
    const destination = fixture("thread:new")
    const home = new ComposerDraftSession("new:home", source.transport)
    await home.load()
    home.update({ text: "not moved yet" })
    vi.mocked(destination.transport.save).mockRejectedValueOnce(new Error("disk full"))
    await expect(home.transferTo(new ComposerDraftSession("thread:new", destination.transport))).rejects.toThrow("disk full")
    expect(source.stored().text).toBe("not moved yet")
    expect(source.transport.clear).not.toHaveBeenCalled()
  })

  it("does not clear a newer draft when an earlier send acknowledgement arrives", async () => {
    const f = fixture()
    const session = new ComposerDraftSession("thread:a", f.transport)
    await session.load()
    session.update({ text: "submitted" })
    const sent = await session.beginSend()
    session.update({ text: "newer input", state: "editing" })
    await session.completeSend(sent)
    await session.flush()
    expect(f.stored().text).toBe("newer input")
    expect(f.transport.clear).not.toHaveBeenCalled()
  })

  it("preserves the prior draft and exposes rejected attachment limits", async () => {
    const f = fixture()
    const session = new ComposerDraftSession("thread:a", f.transport)
    await session.load()
    session.update({ text: "keep existing" })
    await session.flush()
    session.addFiles(Array.from({ length: 33 }, (_, i) => new File(["x"], `file${i}.txt`)), "file")
    expect(session.getSnapshot()).toMatchObject({ draft: { text: "keep existing", files: [] }, error: expect.stringContaining("32 files") })
    expect(f.stored().files).toEqual([])
  })

  it("does not call an incomplete attachment restore saved or allow sending it", async () => {
    const f = fixture()
    const session = new ComposerDraftSession("thread:a", f.transport)
    await session.load()
    session.addFiles([new File(["long bytes"], "file.txt")], "file")
    await session.flush()
    vi.mocked(f.transport.file).mockResolvedValueOnce(new Blob(["short"]))
    const recovery = new ComposerDraftSession("thread:a", f.transport)
    await expect(recovery.load()).rejects.toThrow("completely")
    expect(recovery.getSnapshot()).toMatchObject({ loaded: false, saved: false })
  })

  it("requires an explicit version choice after a revision conflict", async () => {
    const f = fixture()
    const session = new ComposerDraftSession("thread:a", f.transport)
    await session.load()
    session.update({ text: "local version" })
    vi.mocked(f.transport.save).mockRejectedValueOnce(new ApiError("another window changed", 409, "draft_conflict"))
    await expect(session.flush()).rejects.toThrow()
    expect(session.getSnapshot()).toMatchObject({ conflict: true, draft: { text: "local version" } })
    await session.saveCurrentDraft()
    expect(f.stored().text).toBe("local version")
    session.update({ text: "discard only by explicit action" })
    await session.useStoredDraft()
    expect(session.getSnapshot().draft.text).toBe("local version")
  })

  it("does not erase input entered while an explicit delete is awaiting its acknowledgement", async () => {
    const f = fixture()
    const session = new ComposerDraftSession("thread:a", f.transport)
    await session.load()
    session.update({ text: "old draft" })
    await session.flush()
    let release!: () => void
    const blocked = new Promise<void>((done) => { release = done })
    const clear = f.transport.clear
    f.transport.clear = async (key, revision) => { await blocked; return clear(key, revision) }
    const deleting = session.discard()
    await new Promise((resolve) => setTimeout(resolve, 0))
    session.update({ text: "new input during delete" })
    release()
    await deleting
    await session.flush()
    expect(session.getSnapshot().draft.text).toBe("new input during delete")
    expect(f.stored().text).toBe("new input during delete")
  })

  it("does not delete a queued source until its draft copy is durably saved", async () => {
    const f = fixture()
    const session = new ComposerDraftSession("thread:a", f.transport)
    const remove = vi.fn(async () => true)
    vi.mocked(f.transport.save).mockRejectedValueOnce(new Error("disk full"))
    await expect(session.importQueuedDraft("queued input", remove)).rejects.toThrow("disk full")
    expect(remove).not.toHaveBeenCalled()
    await session.importQueuedDraft("queued input", async () => {
      expect(f.stored().text).toBe("queued input")
      expect(session.getSnapshot().draft.text).toBe("")
      return true
    })
    expect(session.getSnapshot().draft.text).toBe("queued input")
  })

  it("keeps both the saved copy and original queue when removal fails, without retrying", async () => {
    const f = fixture()
    const session = new ComposerDraftSession("thread:a", f.transport)
    const remove = vi.fn(async () => false)
    await expect(session.importQueuedDraft("queued input", remove)).rejects.toThrow("could not be removed")
    expect(remove).toHaveBeenCalledOnce()
    expect(f.stored()).toMatchObject({ text: "queued input", state: "uncertain" })
    expect(session.getSnapshot()).toMatchObject({ draft: { text: "queued input", state: "uncertain" }, error: expect.stringContaining("queue") })
  })
})
