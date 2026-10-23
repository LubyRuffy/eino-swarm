import { afterEach, describe, expect, it, vi } from "vitest"
import { api, ApiError } from "./api"
import { composerDraftSession, type ComposerDraftRecord } from "./composer-draft"

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}
const settled = () => new Promise((done) => setTimeout(done, 0))

function storage() {
  const records = new Map<string, ComposerDraftRecord>()
  const files = new Map<string, File>()
  const load = vi.spyOn(api, "composerDraft").mockImplementation(async (key) => structuredClone(records.get(key) ?? {
    key, revision: 0, text: "", quotes: [], files: [], state: "editing",
  }))
  const file = vi.spyOn(api, "composerDraftFile").mockImplementation(async (key, id) => files.get(key + id)!)
  const save = vi.spyOn(api, "saveComposerDraft").mockImplementation(async (draft, incoming) => {
    if (draft.revision !== (records.get(draft.key)?.revision ?? 0)) throw new ApiError("conflict", 409, "draft_conflict")
    incoming.forEach((bytes, id) => files.set(draft.key + id, bytes))
    const saved = { ...structuredClone(draft), revision: draft.revision + 1 }
    records.set(draft.key, saved)
    return structuredClone(saved)
  })
  const clear = vi.spyOn(api, "deleteComposerDraft").mockImplementation(async (key, revision) => {
    if (records.get(key)?.revision !== revision) throw new ApiError("conflict", 409, "draft_conflict")
    const draft: ComposerDraftRecord = { key, revision: revision + 1, text: "", quotes: [], files: [], state: "editing" }
    records.set(key, draft)
    return structuredClone(draft)
  })
  let count = 0
  vi.stubGlobal("URL", Object.assign(class extends URL {}, {
    createObjectURL: vi.fn(() => `blob:preview-${++count}`), revokeObjectURL: vi.fn(),
  }))
  return { load, file, save, clear, records }
}

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe("Composer attachment cache ownership", () => {
  it("releases saved inactive scopes and restores their complete snapshot from durable storage", async () => {
    const f = storage()
    for (let index = 0; index < 12; index++) {
      const key = `thread:cache-saved-${index}`
      const session = composerDraftSession(key)
      const unsubscribe = session.subscribe(vi.fn())
      await session.load()
      session.update({ text: `draft ${index}`, quotes: [{ id: "q", text: "context" }] })
      session.addSelection([new File([new Uint8Array([0, 255, index])], "photo.png", { type: "image/png", lastModified: 123 })])
      await session.flush()
      const before = session.getSnapshot().draft
      expect(composerDraftSession(key)).toBe(session)
      unsubscribe()
      await settled()
      expect(URL.revokeObjectURL).toHaveBeenCalledWith(before.files[0].previewUrl)
      const restored = composerDraftSession(key)
      expect(restored).not.toBe(session)
      const release = restored.subscribe(vi.fn())
      await restored.load()
      expect(restored.getSnapshot().draft).toMatchObject({ text: `draft ${index}`, quotes: before.quotes, revision: before.revision })
      expect(restored.getSnapshot().draft.files[0]).toMatchObject({ id: before.files[0].id, size: 3, mime: "image/png", last_modified: 123 })
      expect(restored.getSnapshot().draft.files[0].previewUrl).not.toBe(before.files[0].previewUrl)
      release()
      await settled()
    }
    expect(f.file).toHaveBeenCalledTimes(12)
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(24)
  })

  it("does not evict an active subscriber or revoke a preview during a subscription handoff", async () => {
    storage()
    const key = "thread:cache-subscriptions"
    const session = composerDraftSession(key)
    const first = session.subscribe(vi.fn())
    const second = session.subscribe(vi.fn())
    await session.load()
    session.addSelection([new File(["image"], "photo.png", { type: "image/png" })])
    await session.flush()
    first()
    await settled()
    expect(composerDraftSession(key)).toBe(session)
    second()
    const resubscribed = session.subscribe(vi.fn())
    await settled()
    expect(composerDraftSession(key)).toBe(session)
    expect(URL.revokeObjectURL).not.toHaveBeenCalled()
    resubscribed()
    await settled()
    expect(composerDraftSession(key)).not.toBe(session)
  })

  it("keeps dirty and failed/conflicted saves reachable until their version is resolved and acknowledged", async () => {
    const f = storage()
    const key = "thread:cache-save-failure"
    const session = composerDraftSession(key)
    const unsubscribe = session.subscribe(vi.fn())
    await session.load()
    session.update({ text: "only local copy" })
    session.addSelection([new File(["bytes"], "photo.png", { type: "image/png" })])
    unsubscribe()
    await settled()
    expect(composerDraftSession(key)).toBe(session)
    f.save.mockRejectedValueOnce(new Error("disk full"))
    await expect(session.flush()).rejects.toThrow("disk full")
    await settled()
    expect(composerDraftSession(key)).toBe(session)
    f.save.mockRejectedValueOnce(new ApiError("conflict", 409, "draft_conflict"))
    await expect(session.flush()).rejects.toThrow("conflict")
    await settled()
    expect(composerDraftSession(key)).toBe(session)
    expect(URL.revokeObjectURL).not.toHaveBeenCalled()
    await session.saveCurrentDraft()
    await settled()
    expect(composerDraftSession(key)).not.toBe(session)
    expect(f.records.get(key)?.text).toBe("only local copy")
  })

  it("keeps an in-flight save until its actual acknowledgement then releases the inactive scope", async () => {
    const f = storage()
    const key = "thread:cache-save-pending"
    const session = composerDraftSession(key)
    const unsubscribe = session.subscribe(vi.fn())
    await session.load()
    session.update({ text: "await acknowledgement" })
    const blocked = deferred<void>()
    const save = f.save.getMockImplementation()!
    f.save.mockImplementationOnce(async (...args) => { await blocked.promise; return save(...args) })
    const writing = session.flush()
    unsubscribe()
    await settled()
    expect(composerDraftSession(key)).toBe(session)
    blocked.resolve()
    await writing
    await settled()
    expect(composerDraftSession(key)).not.toBe(session)
    expect(f.records.get(key)?.text).toBe("await acknowledgement")
  })

  it("preserves an unsubscribed sending scope and applies its late acknowledgement to that revision only", async () => {
    const f = storage()
    const key = "thread:cache-sending"
    const session = composerDraftSession(key)
    const unsubscribe = session.subscribe(vi.fn())
    await session.load()
    session.update({ text: "submitted" })
    const receipt = await session.beginSend()
    unsubscribe()
    await settled()
    expect(composerDraftSession(key)).toBe(session)
    await session.completeSend(receipt)
    await settled()
    expect(composerDraftSession(key)).not.toBe(session)
    expect(f.records.get(key)).toMatchObject({ text: "", state: "editing", revision: receipt.revision + 1 })
  })

  it("retains both migration scopes across navigation and releases each only after the submitted send settles", async () => {
    const f = storage()
    const source = composerDraftSession("new:cache-migration")
    const target = composerDraftSession("thread:cache-migration")
    const sourceSubscription = source.subscribe(vi.fn())
    const targetSubscription = target.subscribe(vi.fn())
    const releaseSend = source.retain()
    await source.load()
    source.update({ text: "first request" })
    source.addSelection([new File(["unchanged image bytes"], "photo.png", { type: "image/png" })])
    const sourcePreview = source.getSnapshot().draft.files[0].previewUrl
    sourceSubscription()
    await source.transferTo(target)
    await settled()
    expect(composerDraftSession(source.key)).toBe(source)
    expect(URL.revokeObjectURL).not.toHaveBeenCalledWith(sourcePreview)
    const sentTarget = await target.beginSend()
    const sentSource = await source.beginSend()
    targetSubscription()
    releaseSend()
    releaseSend() // Ownership cleanup is idempotent, including error/finally paths.
    await settled()
    expect(composerDraftSession(source.key)).toBe(source)
    expect(composerDraftSession(target.key)).toBe(target)
    await target.completeSend(sentTarget)
    await source.completeSend(sentSource)
    await settled()
    expect(composerDraftSession(source.key)).not.toBe(source)
    expect(composerDraftSession(target.key)).not.toBe(target)
    expect(f.records.get(source.key)?.text).toBe("")
    expect(f.records.get(target.key)?.text).toBe("")
    expect(URL.revokeObjectURL).toHaveBeenCalledWith(sourcePreview)
  })

  it("keeps pending restoration and deletion owned until their acknowledgements arrive", async () => {
    const f = storage()
    const key = "thread:cache-load-clear"
    const session = composerDraftSession(key)
    const blockedLoad = deferred<void>()
    const load = f.load.getMockImplementation()!
    f.load.mockImplementationOnce(async (...args) => { await blockedLoad.promise; return load(...args) })
    const restoring = session.load()
    await settled()
    expect(composerDraftSession(key)).toBe(session)
    const subscribed = session.subscribe(vi.fn())
    blockedLoad.resolve()
    await restoring
    session.update({ text: "draft to explicitly remove" })
    await session.flush()
    const blockedClear = deferred<void>()
    const clear = f.clear.getMockImplementation()!
    f.clear.mockImplementationOnce(async (...args) => { await blockedClear.promise; return clear(...args) })
    const deleting = session.discard()
    subscribed()
    await settled()
    expect(composerDraftSession(key)).toBe(session)
    blockedClear.resolve()
    await deleting
    await settled()
    expect(composerDraftSession(key)).not.toBe(session)
    expect(f.records.get(key)?.text).toBe("")
  })

  it("releases an acknowledged uncertain draft and restores its refusal to resend automatically", async () => {
    const f = storage()
    const key = "thread:cache-uncertain"
    const session = composerDraftSession(key)
    const unsubscribe = session.subscribe(vi.fn())
    await session.load()
    session.update({ text: "acknowledgement lost" })
    session.addSelection([new File(["bytes"], "file.bin")])
    await session.beginSend()
    unsubscribe()
    await session.markUncertain()
    await settled()
    const restored = composerDraftSession(key)
    expect(restored).not.toBe(session)
    const release = restored.subscribe(vi.fn())
    await restored.load()
    await expect(restored.beginSend()).rejects.toThrow("Check the conversation")
    expect(restored.getSnapshot().draft).toMatchObject({ text: "acknowledgement lost", state: "uncertain", files: [{ size: 5 }] })
    expect(f.clear).not.toHaveBeenCalled()
    release()
    await settled()
  })

  it("owns an unsubscribed migration destination through blocked load and save before a new page reads it", async () => {
    const f = storage()
    const source = composerDraftSession("new:cache-unsubscribed-migration")
    const destinationKey = "thread:cache-unsubscribed-migration"
    const target = composerDraftSession(destinationKey)
    const release = source.retain()
    await source.load()
    source.update({ text: "first send before target mounts" })
    source.addSelection([new File(["original bytes"], "photo.png", { type: "image/png" })])
    const blockedLoad = deferred<void>()
    const blockedSave = deferred<void>()
    const startedSave = deferred<void>()
    const load = f.load.getMockImplementation()!
    f.load.mockImplementation(async (key) => {
      if (key === destinationKey) await blockedLoad.promise
      return load(key)
    })
    const save = f.save.getMockImplementation()!
    f.save.mockImplementation(async (...args) => {
      if (args[0].key === destinationKey) { startedSave.resolve(); await blockedSave.promise }
      return save(...args)
    })
    const migrating = source.transferTo(target)
    await settled()
    expect(composerDraftSession(destinationKey)).toBe(target)
    blockedLoad.resolve()
    await startedSave.promise
    await settled()
    expect(composerDraftSession(destinationKey)).toBe(target)
    const preview = target.getSnapshot().draft.files[0].previewUrl
    expect(URL.revokeObjectURL).not.toHaveBeenCalledWith(preview)
    blockedSave.resolve()
    await migrating
    await settled()
    const mounted = composerDraftSession(destinationKey)
    const unmount = mounted.subscribe(vi.fn())
    await mounted.load()
    expect(mounted.getSnapshot().draft).toMatchObject({ text: "first send before target mounts", files: [{ size: 14 }] })
    expect(f.file).toHaveBeenCalledWith(destinationKey, target.getSnapshot().draft.files[0].id)
    expect(composerDraftSession(source.key)).toBe(source)
    release()
    unmount()
    await settled()
  })
})
