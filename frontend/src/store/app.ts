import { create } from "zustand"

import { applyPinnedOrder } from "@/lib/reorder"
import {
  applyAppearance,
  normalizeAppearance,
  normalizeUISettings,
  readAppearance,
  writeAppearance,
  type Appearance,
  type ContentWidthPref,
  type FontPref,
  type FontSizePref,
} from "@/lib/appearance"
import { ApiError, api } from "@/lib/api"
import {
  applyLocale,
  normalizeLocalePref,
  readLocalePref,
  writeLocalePref,
  type LocalePref,
} from "@/lib/i18n"
import type { SendImage } from "@/lib/paste-image"
import { subscribeEvents } from "@/lib/stream"
import {
  emptyTranscript,
  reduceEvent,
  collapseLiveEvents,
  parseReview,
  MANAGER_ID,
  placePendingEdit,
  rewindTranscript,
  toolNameOf,
  type TranscriptState,
} from "@/lib/transcript"
import { MEMORY_WRITE_TOOLS, memoryWriteLanded } from "@/lib/tool-view"
import { mergeModelContext, parseUsage } from "@/lib/usage"
import type {
  Attachment,
  FileEntry,
  Followup,
  Meta,
  ModelInfo,
  SwarmEvent,
  Thread,
  ThreadStatus,
  Turn,
  UsageSnapshot,
} from "@/lib/types"
import { useProjects } from "./projects"

export type Theme = "light" | "dark" | "system"

interface AppState {
  meta?: Meta
  models: ModelInfo[]
  threads: Thread[]
  activeId?: string
  transcript: TranscriptState
  status: ThreadStatus
  turns: Turn[]
  followups: Followup[]
  files: FileEntry[]
  workspace: string
  usage?: UsageSnapshot
  /** Undefined until the first stream has replayed, so the transcript can
   *  show a skeleton instead of an empty conversation. */
  loaded: boolean
  connected: boolean
  error?: string
  theme: Theme
  locale: LocalePref
  font: FontPref
  fontSize: FontSizePref
  contentWidth: ContentWidthPref
  /** Which sub-agent the right-hand panel is showing, if any. */
  selectedAgent?: string

  boot: () => Promise<void>
  refreshThreads: () => Promise<void>
  /** Re-lists every endpoint and writes the catalogs. Does not reboot the
   *  conversation — boot() would yank the open thread. */
  refreshCatalogs: () => Promise<void>
  openThread: (id: string) => Promise<void>
  /** Lands in projectId. Omit it and the conversation sits in Recents. */
  newThread: (projectId?: string) => Promise<string | undefined>
  /** Highlights a project and loads its memory. Does not filter the list. */
  selectProject: (projectId?: string) => Promise<void>
  pinThread: (id: string, pinned: boolean) => Promise<void>
  reviewNow: () => Promise<void>
  renameThread: (id: string, title: string) => Promise<void>
  deleteThread: (id: string) => Promise<void>
  reorderThreads: (ids: string[]) => Promise<void>
  send: (text: string, images?: SendImage[], opts?: { steer?: boolean; files?: string[]; fromEventSeq?: number }) => Promise<void>
  setGoal: (text: string) => Promise<void>
  editGoal: (text: string) => Promise<void>
  resumeGoal: () => Promise<void>
  compactThread: () => Promise<void>
  interrupt: () => Promise<void>
  steerFollowup: (id: string) => Promise<void>
  deleteFollowup: (id: string) => Promise<void>
  requeueFollowup: (id: string, text: string) => Promise<void>
  clearFollowups: () => Promise<void>
  refreshFollowups: () => Promise<void>
  extendTurn: (proceed: boolean) => Promise<void>
  upload: (files: File[]) => Promise<Attachment[]>
  refreshFiles: () => Promise<void>
  removeFile: (path: string) => Promise<void>
  setTheme: (t: Theme) => void
  setLocale: (pref: LocalePref, opts?: { persist?: boolean }) => void
  setAppearance: (patch: Partial<Appearance>, opts?: { persist?: boolean }) => void
  selectAgent: (id?: string) => void
  setError: (message?: string) => void
}

let unsubscribe: (() => void) | undefined

/** Streamed events that have arrived since the last animation frame. One
 *  rAF applies them together, so a burst of tokens is one React render. */
let queued: SwarmEvent[] = []
let queuedThread = ""
let raf = 0

/** Set while a conversation is being created. Someone who clicks "New
 *  conversation" starts typing immediately, and a send that read activeId
 *  before the new id arrived would run the turn in the conversation they just
 *  left — visibly nowhere. */
let creating: Promise<string | undefined> | undefined

const THEME_KEY = "zwai.theme"

export const useApp = create<AppState>((set, get) => ({
  models: [],
  threads: [],
  transcript: emptyTranscript(),
  status: { running: false },
  turns: [],
  followups: [],
  files: [],
  workspace: "",
  loaded: false,
  connected: false,
  theme: readTheme(),
  locale: readLocalePref(),
  ...readAppearance(),

  boot: async () => {
    applyTheme(get().theme)
    applyLocale(get().locale)
    applyAppearance({
      font: get().font,
      fontSize: get().fontSize,
      contentWidth: get().contentWidth,
    })
    try {
      const [meta, models, threads] = await Promise.all([
        api.meta(),
        api.models(),
        api.threads(),
        useProjects.getState().refresh(),
      ])
      set({ meta, models: models.models, threads })
      if (meta.ui) {
        get().setAppearance(normalizeAppearance(meta.ui), { persist: false })
      }
      const locale = meta.ui?.locale || meta.locale
      if (locale) {
        get().setLocale(normalizeLocalePref(locale), { persist: false })
      }
      if (threads.length > 0) await get().openThread(threads[0].id)
    } catch (e) {
      set({ error: message(e) })
    }
  },

  refreshThreads: async () => {
    try {
      set({ threads: await api.threads() })
    } catch (e) {
      set({ error: message(e) })
    }
  },

  refreshCatalogs: async () => {
    try {
      const settings = await api.settings()
      const providers = []
      for (const p of settings.models.providers) {
        // GET never returns the key; do not PUT an empty one or we wipe it.
        const rest = { ...p }
        delete rest.api_key
        if (!p.base_url.trim()) {
          providers.push(rest)
          continue
        }
        try {
          const listed = await api.discoverModels({
            provider_id: p.id,
            base_url: p.base_url,
          })
          providers.push({
            ...rest,
            catalog: listed.models,
            model: p.model || listed.models[0] || "",
            model_context: mergeModelContext(
              p.model_context,
              listed.context_windows,
            ),
          })
        } catch (e) {
          providers.push(rest)
          set({ error: message(e) })
        }
      }
      await api.saveSettings({
        models: { default: settings.models.default, providers },
      })
      const listed = await api.models()
      set({ models: listed.models })
    } catch (e) {
      set({ error: message(e) })
    }
  },

  selectProject: async (projectId) => {
    useProjects.getState().select(projectId)
    await useProjects.getState().loadMemory(projectId)
  },

  reviewNow: async () => {
    const id = get().activeId
    if (!id) {
      useProjects.getState().endReview("Open a conversation first.")
      return
    }
    useProjects.getState().beginReview()
    try {
      const { turn } = await api.reviewThread(id)
      if (turn?.id) useProjects.getState().awaitReview(turn.id)
    } catch (e) {
      // Idle is an answer: no finished turn, or memory is off. The banner
      // would make it look like the app broke; the panel is where they clicked.
      if (e instanceof ApiError && e.code === "idle") {
        useProjects.getState().endReview(
          "Nothing to review. Finish a turn in this project first.",
        )
        return
      }
      useProjects.getState().endReview()
      set({ error: message(e) })
    }
  },

  openThread: async (id) => {
    if (get().activeId === id) return
    dropQueued()
    unsubscribe?.()
    unsubscribe = undefined
    set({
      activeId: id,
      transcript: emptyTranscript(),
      turns: [],
      followups: [],
      files: [],
      loaded: false,
      selectedAgent: undefined,
      status: { running: false },
      usage: undefined,
    })

    unsubscribe = subscribeEvents(id, {
      onEvent: (ev) => queueEvent(set, get, id, ev),
      onReady: ({ status }) => {
        set((s) => ({
          loaded: true,
          connected: true,
          status: status ?? s.status,
        }))
        void get().refreshFiles()
      },
      onClose: (reason) => set({ connected: reason !== "error" }),
    })

    try {
      const [{ thread, status, usage }, turns, followups] = await Promise.all([
        api.thread(id),
        api.turns(id),
        api.followups(id),
      ])
      set((s) => ({
        status,
        turns,
        followups,
        usage,
        threads: s.threads.map((t) => (t.id === thread.id ? { ...t, ...thread } : t)),
      }))
      // The Memory tab belongs to the project, not the conversation. Opening
      // a Recents chat must drop the previous project's highlight or the
      // folder would still look selected.
      useProjects.getState().select(thread.project_id || undefined)
      if (thread.project_id) {
        void useProjects.getState().loadMemory(thread.project_id)
      }
    } catch (e) {
      set({ error: message(e) })
    }
  },

  newThread: async (projectId) => {
    creating = (async () => {
      try {
        const thread = await api.createThread(undefined, undefined, projectId)
        set((s) => ({ threads: [thread, ...s.threads] }))
        await get().openThread(thread.id)
        return thread.id
      } catch (e) {
        set({ error: message(e) })
        return undefined
      }
    })()
    try {
      return await creating
    } finally {
      creating = undefined
    }
  },

  pinThread: async (id, pinned) => {
    try {
      const updated = await api.patchThread(id, { pinned })
      set((s) => ({
        threads: s.threads.map((t) => (t.id === id ? { ...t, ...updated } : t)),
      }))
    } catch (e) {
      set({ error: message(e) })
    }
  },

  renameThread: async (id, title) => {
    try {
      const updated = await api.patchThread(id, { title })
      set((s) => ({
        threads: s.threads.map((t) => (t.id === id ? updated : t)),
      }))
    } catch (e) {
      set({ error: message(e) })
    }
  },

  deleteThread: async (id) => {
    try {
      await api.deleteThread(id)
      const remaining = get().threads.filter((t) => t.id !== id)
      set({ threads: remaining })
      if (get().activeId === id) {
        unsubscribe?.()
        unsubscribe = undefined
        set({ activeId: undefined, transcript: emptyTranscript(), loaded: false, followups: [] })
        if (remaining.length > 0) await get().openThread(remaining[0].id)
      }
    } catch (e) {
      set({ error: message(e) })
    }
  },

  reorderThreads: async (ids) => {
    set({ threads: applyPinnedOrder(get().threads, ids) })
    try {
      const listed = await api.reorderThreads(ids)
      set({ threads: listed })
    } catch (e) {
      set({ error: message(e) })
      await get().refreshThreads()
    }
  },

  send: async (text, images, opts) => {
    if (opts?.fromEventSeq) {
      const id = get().activeId
      if (!id) return
      const from = opts.fromEventSeq
      const cut = get().transcript.agents[MANAGER_ID]?.blocks.find(
        (b) => b.kind === "user" && b.seq === from,
      )
      // Before any await: this bubble stays with the new text, everything
      // below it is gone. Waiting on currentThread first would paint the
      // unedited bubble for a frame, which is the opposite of Codex.
      set((s) => ({
        transcript: placePendingEdit(
          rewindTranscript(s.transcript, from),
          text,
          cut?.images,
        ),
        followups: [],
        status: { ...s.status, running: true },
        error: undefined,
      }))
      try {
        await api.startTurn(id, text, images, opts.files, from)
        void get().refreshFollowups()
        void get().refreshThreads()
      } catch (e) {
        set({ error: message(e) })
        set({ activeId: undefined })
        await get().openThread(id)
      }
      return
    }
    let id = await currentThread(get)
    if (!id) {
      id = await get().newThread()
      if (!id) return
    }
    try {
      // Pasted images have nowhere to wait: the follow-up row is text. They
      // inject now rather than being dropped on the floor.
      const queue =
        !opts?.steer &&
        get().status.running &&
        !(images && images.length > 0) &&
        !(opts?.files && opts.files.length > 0)
      if (queue) {
        try {
          const item = await api.enqueueFollowup(id, text)
          set((s) => ({ followups: [...s.followups, item] }))
          return
        } catch (e) {
          if (!(e instanceof ApiError && e.code === "idle")) throw e
        }
      }
      await api.steer(id, text, images, opts?.files)
      set({ status: { ...get().status, running: true } })
      void get().refreshFollowups()
      void get().refreshThreads()
    } catch (e) {
      set({ error: message(e) })
    }
  },

  setGoal: async (text) => {
    let id = await currentThread(get)
    if (!id) {
      id = await get().newThread()
      if (!id) return
    }
    try {
      const updated = await api.patchThread(id, { goal: text })
      set((s) => ({
        threads: s.threads.map((t) => (t.id === id ? { ...t, ...updated } : t)),
        error: undefined,
      }))
    } catch (e) {
      set({ error: message(e) })
    }
  },

  editGoal: async (text) => {
    const id = get().activeId
    if (!id) return
    try {
      const updated = await api.patchThread(id, { goal: text, goal_edit: true })
      set((s) => ({
        threads: s.threads.map((t) => (t.id === id ? { ...t, ...updated } : t)),
        error: undefined,
      }))
    } catch (e) {
      set({ error: message(e) })
    }
  },

  resumeGoal: async () => {
    const id = get().activeId
    if (!id) return
    try {
      const updated = await api.patchThread(id, { goal_resume: true })
      set((s) => ({
        threads: s.threads.map((t) => (t.id === id ? { ...t, ...updated } : t)),
        status: updated.running ? { ...s.status, running: true } : s.status,
        error: undefined,
      }))
    } catch (e) {
      set({ error: message(e) })
    }
  },

  compactThread: async () => {
    const id = get().activeId
    if (!id) return
    try {
      const got = await api.compactThread(id)
      set((s) => ({
        threads: s.threads.map((t) => (t.id === id ? { ...t, ...got.thread } : t)),
        usage: got.usage ?? s.usage,
        error: undefined,
      }))
    } catch (e) {
      set({ error: message(e) })
    }
  },

  refreshFollowups: async () => {
    const id = get().activeId
    if (!id) {
      set({ followups: [] })
      return
    }
    try {
      set({ followups: await api.followups(id) })
    } catch (e) {
      set({ error: message(e) })
    }
  },

  steerFollowup: async (fid) => {
    const id = get().activeId
    if (!id) return
    try {
      await api.steerFollowup(id, fid)
      set((s) => ({ followups: s.followups.filter((f) => f.id !== fid) }))
    } catch (e) {
      void get().refreshFollowups()
      if (!(e instanceof ApiError && e.code === "idle")) {
        set({ error: message(e) })
      }
    }
  },

  deleteFollowup: async (fid) => {
    const id = get().activeId
    if (!id) return
    try {
      await api.deleteFollowup(id, fid)
      set((s) => ({ followups: s.followups.filter((f) => f.id !== fid) }))
    } catch (e) {
      void get().refreshFollowups()
      if (!(e instanceof ApiError && e.status === 404)) {
        set({ error: message(e) })
      }
    }
  },

  requeueFollowup: async (fid, text) => {
    const id = get().activeId
    if (!id) return
    try {
      const item = await api.requeueFollowup(id, fid, text)
      set((s) => ({
        followups: [...s.followups.filter((f) => f.id !== fid), item],
      }))
    } catch (e) {
      void get().refreshFollowups()
      set({ error: message(e) })
    }
  },

  clearFollowups: async () => {
    const items = get().followups
    await Promise.all(items.map((f) => get().deleteFollowup(f.id)))
  },

  interrupt: async () => {
    const id = get().activeId
    if (!id) return
    try {
      await api.interrupt(id)
    } catch (e) {
      // Interrupting something that already finished is not worth an alert.
      if (!(e instanceof ApiError && e.code === "idle")) {
        set({ error: message(e) })
      }
    }
  },

  extendTurn: async (proceed) => {
    const id = get().activeId
    if (!id) return
    try {
      await api.continueTurn(id, proceed)
    } catch (e) {
      if (!(e instanceof ApiError && e.code === "idle")) {
        set({ error: message(e) })
      }
    }
  },

  upload: async (files) => {
    let id = await currentThread(get)
    if (!id) {
      id = await get().newThread()
      if (!id) return []
    }
    try {
      const saved = await api.upload(id, files)
      await get().refreshFiles()
      return saved
    } catch (e) {
      set({ error: message(e) })
      return []
    }
  },

  refreshFiles: async () => {
    const id = get().activeId
    if (!id) return
    try {
      const { files, workspace } = await api.files(id)
      set({ files, workspace })
    } catch {
      // The Files panel is secondary; a failure here must not break the chat.
    }
  },

  removeFile: async (path) => {
    const id = get().activeId
    if (!id) return
    try {
      await api.deleteFile(id, path)
      await get().refreshFiles()
    } catch (e) {
      set({ error: message(e) })
    }
  },

  setTheme: (theme) => {
    try {
      localStorage.setItem(THEME_KEY, theme)
    } catch {
      // Not remembering the choice is better than refusing to apply it.
    }
    applyTheme(theme)
    set({ theme })
  },

  setLocale: (pref, opts) => {
    const locale = normalizeLocalePref(pref)
    writeLocalePref(locale)
    applyLocale(locale)
    set({ locale })
    if (opts?.persist === false) return
    persistChrome(get())
  },

  setAppearance: (patch, opts) => {
    const next = normalizeAppearance({
      font: patch.font ?? get().font,
      font_size: patch.fontSize ?? get().fontSize,
      content_width: patch.contentWidth ?? get().contentWidth,
    })
    writeAppearance(next)
    applyAppearance(next)
    set(next)
    if (opts?.persist === false) return
    persistChrome(get())
  },

  selectAgent: (selectedAgent) => set({ selectedAgent }),
  setError: (error) => set({ error }),
}))

/** The conversation an action should apply to, once any conversation being
 *  created has arrived. */
async function currentThread(get: () => AppState): Promise<string | undefined> {
  if (creating) await creating
  return get().activeId
}

/** Fold one event into the transcript, ignoring anything for a conversation
 *  the user has already navigated away from. Streamed tokens are queued and
 *  applied on the next animation frame so a burst of deltas is one render,
 *  not one render per token. Terminal events flush immediately: a `done` that
 *  sat behind a rAF would leave the composer looking busy after the turn ended. */
function queueEvent(
  set: (partial: Partial<AppState> | ((s: AppState) => Partial<AppState>)) => void,
  get: () => AppState,
  threadId: string,
  ev: SwarmEvent,
) {
  if (get().activeId !== threadId) return
  if (queuedThread !== threadId) {
    flushQueued(set, get)
    queuedThread = threadId
  }
  queued.push(ev)
  if (ev.kind === "done" || ev.kind === "error" || ev.kind === "user_message" ||
      ev.kind === "max_iterations" || ev.kind === "max_iterations_continued" ||
      ev.kind === "resumed" || ev.kind === "goal" || ev.kind === "goal_complete" ||
      ev.kind === "goal_continued" || ev.kind === "goal_capped" || ev.kind === "goal_blocked" ||
      ev.kind === "goal_edited" || ev.kind === "goal_resumed" || ev.kind === "compacted" ||
      ev.kind === "rewound") {
    flushQueued(set, get)
    return
  }
  if (!raf) {
    raf = requestAnimationFrame(() => {
      raf = 0
      flushQueued(set, get)
    })
  }
}

function dropQueued() {
  queued = []
  queuedThread = ""
  if (raf) {
    cancelAnimationFrame(raf)
    raf = 0
  }
}

function flushQueued(
  set: (partial: Partial<AppState> | ((s: AppState) => Partial<AppState>)) => void,
  get: () => AppState,
) {
  if (raf) {
    cancelAnimationFrame(raf)
    raf = 0
  }
  if (queued.length === 0) return
  const threadId = queuedThread
  const events = collapseLiveEvents(queued)
  queued = []
  const state = get()
  if (state.activeId !== threadId) return

  let transcript = state.transcript
  let status = state.status
  let threads = state.threads
  let usage = state.usage
  let closed = false
  for (const ev of events) {
    transcript = reduceEvent(transcript, ev)
    if (ev.kind === "user_message" || ev.kind === "resumed") {
      status = {
        ...status,
        running: true,
        turn_id: ev.turn_id,
        started_at: ev.created_at,
        awaiting_continue: ev.kind === "resumed" ? false : status.awaiting_continue,
      }
    }
    if (ev.kind === "max_iterations") {
      status = { ...status, running: true, awaiting_continue: true, turn_id: ev.turn_id }
    }
    if (ev.kind === "max_iterations_continued") {
      status = { ...status, running: true, awaiting_continue: false }
    }
    if (ev.kind === "done" || ev.kind === "error") {
      status = { running: false }
      closed = true
    }
    if (ev.kind === "title" && ev.text && !ev.err) {
      const title = ev.text
      threads = threads.map((t) => (t.id === threadId ? { ...t, title } : t))
    }
    if (ev.kind === "goal") {
      threads = threads.map((t) =>
        t.id === threadId
          ? {
              ...t,
              goal: ev.text ?? "",
              goal_complete: false,
              goal_capped: false,
              goal_blocked: false,
              goal_block_reason: "",
            }
          : t,
      )
    }
    if (ev.kind === "goal_edited") {
      threads = threads.map((t) =>
        t.id === threadId ? { ...t, goal: ev.text ?? "" } : t,
      )
    }
    if (ev.kind === "goal_complete") {
      threads = threads.map((t) =>
        t.id === threadId
          ? { ...t, goal_complete: true, goal_capped: false, goal_blocked: false, goal_block_reason: "" }
          : t,
      )
    }
    if (ev.kind === "goal_capped") {
      threads = threads.map((t) =>
        t.id === threadId ? { ...t, goal_capped: true } : t,
      )
    }
    if (ev.kind === "goal_blocked") {
      threads = threads.map((t) =>
        t.id === threadId
          ? {
              ...t,
              goal_blocked: true,
              goal_capped: false,
              goal_block_reason: parseGoalReason(ev.text),
            }
          : t,
      )
    }
    if (ev.kind === "goal_resumed") {
      threads = threads.map((t) =>
        t.id === threadId
          ? { ...t, goal_blocked: false, goal_capped: false, goal_block_reason: "" }
          : t,
      )
      status = { ...status, running: true, turn_id: ev.turn_id }
    }
    if (ev.kind === "goal_continued") {
      status = { ...status, running: true, turn_id: ev.turn_id }
    }
    if (ev.kind === "compacted" && !ev.err) {
      threads = threads.map((t) =>
        t.id === threadId ? { ...t, compacted: true } : t,
      )
    }
    if (ev.kind === "usage") {
      const next = parseUsage(ev.text)
      if (next) usage = next
    }
    if (ev.kind === "memory_review") {
      // The review wrote the files directly, so the panel has to re-read them
      // rather than derive the new state from the event.
      const outcome = parseReview(ev)
      void useProjects.getState().loadMemory()
      if (outcome?.changed) useProjects.getState().noteMemoryWrite()
      useProjects.getState().finishReview(ev.turn_id, outcome)
    }
    if (ev.kind === "tool_result") {
      const name = toolNameOf(transcript, ev.tool_call_id)
      if (name && MEMORY_WRITE_TOOLS.has(name) && memoryWriteLanded(ev.text)) {
        void useProjects.getState().loadMemory()
        useProjects.getState().noteMemoryWrite()
      }
    }
  }
  set({ transcript, status, threads, usage })
  if (closed) {
    void get().refreshFiles()
    void get().refreshThreads()
    void get().refreshFollowups()
    void api
      .turns(threadId)
      .then((turns) => set({ turns }))
      .catch(() => undefined)
  }
}

function parseGoalReason(text?: string): string {
  const raw = text?.trim() ?? ""
  if (!raw) return ""
  try {
    const v = JSON.parse(raw) as { reason?: unknown }
    return typeof v.reason === "string" ? v.reason : ""
  } catch {
    return raw
  }
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

function persistChrome(state: {
  locale: LocalePref
  font: FontPref
  fontSize: FontSizePref
  contentWidth: ContentWidthPref
}) {
  void api
    .saveSettings({
      ui: normalizeUISettings({
        locale: state.locale,
        font: state.font,
        font_size: state.fontSize,
        content_width: state.contentWidth,
      }),
    })
    .catch(() => undefined)
}

function readTheme(): Theme {
  // Storage can be missing or throw outright (private browsing, an embedded
  // webview). The theme is a preference, not something worth failing to start
  // the app over.
  try {
    const stored = localStorage.getItem(THEME_KEY)
    return stored === "light" || stored === "dark" ? stored : "system"
  } catch {
    return "system"
  }
}

export function applyTheme(theme: Theme) {
  if (typeof document === "undefined") return
  const dark =
    theme === "dark" ||
    (theme === "system" &&
      window.matchMedia?.("(prefers-color-scheme: dark)").matches)
  document.documentElement.classList.toggle("dark", Boolean(dark))
}
