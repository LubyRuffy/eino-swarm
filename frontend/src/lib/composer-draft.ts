import { api, ApiError } from "./api"
import { isPasteImage, visionMIME, type PasteImage } from "./paste-image"
import { composerDraftFromStored, type Quote } from "./quote"

export type ComposerDraftState = "editing" | "sending" | "uncertain"
export interface ComposerDraftFile {
  id: string
  name: string
  mime: string
  kind: "file" | "image"
  size: number
  last_modified: number
}
export interface ComposerDraftRecord {
  key: string
  revision: number
  text: string
  quotes: Quote[]
  files: ComposerDraftFile[]
  state: ComposerDraftState
  updated_at?: string
}
export interface LocalDraftFile extends ComposerDraftFile { file: File; previewUrl?: string }
export interface LocalComposerDraft extends Omit<ComposerDraftRecord, "files"> { files: LocalDraftFile[] }
export interface ComposerDraftSnapshot {
  draft: LocalComposerDraft
  loaded: boolean
  loading: boolean
  saving: boolean
  saved: boolean
  error?: string
  conflict?: boolean
  busy?: boolean
}
export interface ComposerDraftTransport {
  load: (key: string) => Promise<ComposerDraftRecord>
  file: (key: string, id: string) => Promise<Blob>
  save: (record: ComposerDraftRecord, files: Map<string, File>) => Promise<ComposerDraftRecord>
  clear: (key: string, revision: number) => Promise<ComposerDraftRecord>
}
export interface DraftSend { revision: number; generation: number }

const transport: ComposerDraftTransport = {
  load: (key) => api.composerDraft(key),
  file: (key, id) => api.composerDraftFile(key, id),
  save: (draft, files) => api.saveComposerDraft(draft, files),
  clear: (key, revision) => api.deleteComposerDraft(key, revision),
}

function emptyDraft(key: string): LocalComposerDraft {
  return { key, revision: 0, text: "", quotes: [], files: [], state: "editing" }
}
function errorMessage(error: unknown): string { return error instanceof Error ? error.message : String(error) }
function revoke(file: LocalDraftFile) { if (file.previewUrl) URL.revokeObjectURL?.(file.previewUrl) }
function recordOf(draft: LocalComposerDraft): ComposerDraftRecord {
  return { ...draft, files: draft.files.map(({ file: _file, previewUrl: _url, ...meta }) => meta) }
}

/** A scope owns its pending writes even after the visible conversation changes.
 *  Only acknowledged revisions are called saved; bytes are uploaded once. */
export class ComposerDraftSession {
  private snapshot: ComposerDraftSnapshot
  private listeners = new Set<() => void>()
  private generation = 0
  private savedGeneration = 0
  private persisted = new Set<string>()
  private loading?: Promise<void>
  private writing?: Promise<void>
  private timer?: ReturnType<typeof setTimeout>
  private users = 0
  private collectionQueued = false

  constructor(readonly key: string, private client: ComposerDraftTransport = transport, private onUnused?: () => void) {
    this.snapshot = { draft: emptyDraft(key), loaded: false, loading: false, saving: false, saved: false }
  }
  getSnapshot = () => this.snapshot
  subscribe = (listener: () => void) => {
    this.listeners.add(listener)
    return () => { this.listeners.delete(listener); this.collectWhenUnused() }
  }
  /** A send owns its source across navigation, not just while it has a subscriber. */
  retain() {
    this.users++
    let released = false
    return () => {
      if (released) return
      released = true
      this.users--
      this.collectWhenUnused()
    }
  }
  private async using<T>(operation: () => Promise<T>): Promise<T> {
    const release = this.retain()
    try { return await operation() } finally { release() }
  }
  private collectWhenUnused() {
    if (!this.onUnused || this.collectionQueued) return
    this.collectionQueued = true
    // React can unsubscribe/resubscribe during the same commit. Neither that
    // handoff nor an operation awaiting a storage ACK can invalidate its files.
    queueMicrotask(() => {
      this.collectionQueued = false
      const state = this.snapshot
      if (this.listeners.size || this.users || this.loading || this.writing || this.timer
          || !state.loaded || !state.saved || state.busy || state.error || state.conflict
          || state.draft.state === "sending" || this.savedGeneration !== this.generation) return
      state.draft.files.forEach(revoke)
      this.onUnused?.()
      this.onUnused = undefined
    })
  }
  private publish(patch: Partial<ComposerDraftSnapshot>) {
    this.snapshot = { ...this.snapshot, ...patch }
    this.listeners.forEach((listener) => listener())
  }

  load(): Promise<void> {
    return this.using(async () => {
      if (this.snapshot.loaded) return Promise.resolve()
      if (this.loading) return this.loading
      this.publish({ loading: true, error: undefined })
      this.loading = this.readStored().then((draft) => {
        this.persisted = new Set(draft.files.map((file) => file.id))
        this.publish({ draft, loaded: true, saved: true })
      }).catch((error: unknown) => {
        this.publish({ error: errorMessage(error) })
        throw error
      }).finally(() => { this.loading = undefined; this.publish({ loading: false }) })
      return this.loading
    })
  }

  private async readStored(): Promise<LocalComposerDraft> {
    const stored = await this.client.load(this.key)
    const files: LocalDraftFile[] = []
    try {
      for (const meta of stored.files) {
        const bytes = await this.client.file(this.key, meta.id)
        if (bytes.size !== meta.size) throw new Error("The saved attachment could not be restored completely.")
        const file = new File([bytes], meta.name, { type: meta.mime, lastModified: meta.last_modified })
        files.push({ ...meta, file, previewUrl: meta.kind === "image" ? URL.createObjectURL?.(file) : undefined })
      }
      return { ...stored, files, state: stored.state === "sending" ? "uncertain" : stored.state }
    } catch (error) { files.forEach(revoke); throw error }
  }

  update(patch: Partial<Pick<LocalComposerDraft, "text" | "quotes" | "files" | "state">>) {
    if (!this.snapshot.loaded) return
    const draft = { ...this.snapshot.draft, ...patch }
    const reason = invalidFiles(draft.files)
    if (reason) {
      const oldIds = new Set(this.snapshot.draft.files.map((file) => file.id))
      draft.files.filter((file) => !oldIds.has(file.id)).forEach(revoke)
      this.publish({ error: reason, saved: false }); return
    }
    const kept = new Set(draft.files.map((file) => file.id))
    this.snapshot.draft.files.filter((file) => !kept.has(file.id)).forEach(revoke)
    this.generation++
    this.publish({ draft, saved: false, error: this.snapshot.conflict ? this.snapshot.error : undefined })
    if (!this.snapshot.conflict) this.schedule()
  }

  replaceFiles(files: File[], kind: ComposerDraftFile["kind"], images?: PasteImage[]) {
    const current = this.snapshot.draft.files
    const next = files.map((file, i) => {
      const old = current.find((row) => row.file === file && row.kind === kind)
      if (old) return old
      return {
        id: crypto.randomUUID(), name: file.name, mime: kind === "image" ? visionMIME(file) : file.type,
        kind, size: file.size, last_modified: file.lastModified, file,
        previewUrl: kind === "image" ? (images?.[i]?.previewUrl || URL.createObjectURL?.(file)) : undefined,
      }
    })
    this.update({ files: [...current.filter((row) => row.kind !== kind), ...next] })
  }
  addFiles(files: File[], kind: ComposerDraftFile["kind"]) {
    this.replaceFiles([...this.snapshot.draft.files.filter((file) => file.kind === kind).map((file) => file.file), ...files], kind)
  }
  addSelection(files: File[]) {
    const incoming: LocalDraftFile[] = files.map((file) => {
      const kind = isPasteImage(file) ? "image" : "file"
      return { id: crypto.randomUUID(), name: file.name, mime: kind === "image" ? visionMIME(file) : file.type,
        kind, size: file.size, last_modified: file.lastModified, file,
        previewUrl: kind === "image" ? URL.createObjectURL?.(file) : undefined }
    })
    this.update({ files: [...this.snapshot.draft.files, ...incoming] })
  }

  private schedule() {
    if (this.timer) clearTimeout(this.timer)
    this.timer = setTimeout(() => { this.timer = undefined; void this.flush().catch(() => undefined) }, 150)
  }

  async flush(): Promise<void> {
    return this.using(async () => {
      if (this.timer) { clearTimeout(this.timer); this.timer = undefined }
      await this.load()
      if (this.writing) { await this.writing; return this.flush() }
      if (this.generation === this.savedGeneration) return
      this.publish({ saving: true, error: undefined })
      this.writing = (async () => {
        while (this.savedGeneration !== this.generation) {
          const generation = this.generation
          const draft = this.snapshot.draft
          const files = new Map(draft.files.filter((file) => !this.persisted.has(file.id)).map((file) => [file.id, file.file]))
          const saved = await this.client.save(recordOf(draft), files)
          this.savedGeneration = generation
          this.persisted = new Set(draft.files.map((file) => file.id))
          this.publish({ draft: { ...this.snapshot.draft, revision: saved.revision }, saved: this.savedGeneration === this.generation })
        }
      })().catch((error: unknown) => {
        this.publish({ error: errorMessage(error), saved: false, conflict: error instanceof ApiError && error.code === "draft_conflict" })
        throw error
      }).finally(() => { this.writing = undefined; this.publish({ saving: false }) })
      return this.writing
    })
  }

  async beginSend(): Promise<DraftSend> {
    return this.using(async () => {
      await this.flush()
      if (this.snapshot.draft.state !== "editing") throw new Error("Check the conversation before retrying this draft.")
      this.update({ state: "sending" })
      await this.flush()
      return { revision: this.snapshot.draft.revision, generation: this.generation }
    })
  }
  async transferTo(target: ComposerDraftSession) {
    return this.using(async () => {
      const releaseTarget = target.retain()
      try {
        await this.flush()
        await target.load()
        const existing = target.getSnapshot().draft
        if (existing.text || existing.quotes.length || existing.files.length) throw new Error("The destination already has a draft. The original draft was kept.")
        const source = this.snapshot.draft
        target.update({ text: source.text, quotes: source.quotes, files: source.files.map((file) => ({
          ...file, previewUrl: file.kind === "image" ? URL.createObjectURL?.(file.file) : undefined,
        })), state: source.state })
        await target.flush()
        // A native probe may still identify this source. Keep it until the
        // actual send is acknowledged, including its immutable attachment bytes.
      } finally { releaseTarget() }
    })
  }
  async markUncertain() { return this.using(async () => { this.update({ state: "uncertain" }); await this.flush() }) }
  async resumeEditing() { return this.using(async () => { this.update({ state: "editing" }); await this.flush() }) }
  async importQueuedDraft(raw: string, remove: () => Promise<boolean>) {
    return this.using(async () => {
      await this.load()
      await this.flush()
      const current = this.snapshot.draft
      if (current.text || current.quotes.length || current.files.length) throw new Error("Send or delete the current draft before editing a queued message. The queue was kept.")
      this.publish({ busy: true, saved: false, error: undefined })
      try {
        const parsed = composerDraftFromStored(raw)
        const saved = await this.client.save({ ...recordOf(current), text: parsed.body, quotes: parsed.quotes, files: [], state: "editing" }, new Map())
        let removed = false
        let removalError: unknown
        try { removed = await remove() } catch (error) { removalError = error }
        this.generation++
        this.savedGeneration = this.generation
        this.persisted.clear()
        this.publish({ draft: { ...saved, files: [] }, saved: true })
        if (!removed) {
          await this.markUncertain()
          throw removalError ?? new Error("The draft was saved, but the queued message could not be removed. Check the queue before sending this copy.")
        }
      } catch (error) {
        this.publish({ error: errorMessage(error), conflict: error instanceof ApiError && error.code === "draft_conflict" })
        throw error
      } finally { this.publish({ busy: false }) }
    })
  }
  async useStoredDraft() {
    return this.using(async () => {
      try {
        const stored = await this.readStored()
        this.snapshot.draft.files.forEach(revoke)
        this.generation++
        this.savedGeneration = this.generation
        this.persisted = new Set(stored.files.map((file) => file.id))
        this.publish({ draft: stored, saved: true, error: undefined, conflict: false })
      } catch (error) { this.publish({ error: errorMessage(error) }); throw error }
    })
  }
  async saveCurrentDraft() {
    return this.using(async () => {
      try {
        const stored = await this.client.load(this.key)
        this.persisted = new Set(stored.files.map((file) => file.id))
        this.publish({ draft: { ...this.snapshot.draft, revision: stored.revision }, conflict: false })
        this.generation++
        await this.flush()
      } catch (error) { this.publish({ error: errorMessage(error) }); throw error }
    })
  }
  async completeSend(sent: DraftSend) {
    return this.using(async () => {
      if (sent.generation !== this.generation) { this.update({ state: "editing" }); await this.flush(); return }
      await this.clear(sent.revision)
    })
  }
  async discard() { return this.using(async () => { await this.flush(); await this.clear(this.snapshot.draft.revision) }) }
  private async clear(revision: number) {
    const generation = this.generation
    try {
      const cleared = await this.client.clear(this.key, revision)
      if (generation !== this.generation) {
        this.persisted.clear()
        this.publish({ draft: { ...this.snapshot.draft, revision: cleared.revision }, saved: false })
        this.schedule()
        return
      }
      this.snapshot.draft.files.forEach(revoke)
      this.persisted.clear()
      this.generation++
      this.savedGeneration = this.generation
      this.publish({ draft: { ...cleared, files: [] }, saved: true, error: undefined })
    } catch (error) { this.publish({ error: errorMessage(error) }); throw error }
  }
}

function invalidFiles(files: LocalDraftFile[]): string | undefined {
  if (files.length > 32 || files.reduce((n, file) => n + file.size, 0) > 256 * 1024 * 1024) {
    return "Draft attachments exceed 32 files or 256 MiB. The previous draft was kept."
  }
  const images = files.filter((file) => file.kind === "image")
  if (images.length > 8 || images.some((file) => file.size > 8 * 1024 * 1024)) {
    return "A draft supports 8 images, up to 8 MiB each. The previous draft was kept."
  }
}

const sessions = new Map<string, ComposerDraftSession>()
export function composerDraftSession(key: string) {
  let session = sessions.get(key)
  if (!session) {
    const created = new ComposerDraftSession(key, transport, () => { if (sessions.get(key) === created) sessions.delete(key) })
    sessions.set(key, created)
    session = created
  }
  return session
}
export function composerDraftKey(threadId?: string, projectId?: string) { return threadId ? `thread:${threadId}` : `new:${projectId || "home"}` }
