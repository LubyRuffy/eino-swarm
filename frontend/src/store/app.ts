import { create } from "zustand"

import { applyPinnedOrder } from "@/lib/reorder"
import {
  applyAppearance,
  appearanceToUI,
  normalizeAppearance,
  readAppearance,
  writeAppearance,
  type Appearance,
} from "@/lib/appearance"
import { ApiError, api } from "@/lib/api"
import { desktopShell, startPresence } from "@/lib/shell"
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
  mergeThreadList,
  setThreadRunning,
  threadListOverlay,
  upsertThread,
} from "@/lib/thread-title"
import { logPageSize, type ThreadLog } from "@/lib/thread-log"
import {
  emptyTranscript,
  MANAGER_ID,
  placePendingEdit,
  rewindTranscript,
  type TranscriptState,
} from "@/lib/transcript"
import { mergeModelContext } from "@/lib/usage"
import type {
  Attachment,
  FileEntry,
  Followup,
  Meta,
  ModelInfo,
  Thread,
  ThreadStatus,
  Turn,
  UsageSnapshot,
} from "@/lib/types"
import { useProjects } from "./projects"
import { planAskActions } from "./app-plan"
import { scheduleActions, type ScheduleSlice } from "./app-schedule"
import { steerInjectActions } from "./app-steer"
import { dropQueued, queueEvent, withRunningClock } from "./app-stream"
import {
  bumpFollowups,
  dropMatchingFollowups,
  isFollowupGeneration,
  liveTurnUserText,
} from "./followup-sync"
import { managerHasVisibleBlocks } from "@/lib/welcome"
import {
  applyTail,
  historyNewestSeq,
  loadAgentHistory,
  loadOlderHistory,
  loadUntilTurnHistory,
  loadUntilVisibleHistory,
  rememberRewind,
  resetThreadHistory,
} from "./thread-history"

export type Theme = "light" | "dark" | "system"

interface AppState extends ScheduleSlice {
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
  /** Older event pages exist above the loaded tail. */
  historyHasMore: boolean
  historyLoading: boolean
  connected: boolean
  error?: string
  theme: Theme
  locale: LocalePref
  font: Appearance["font"]
  uiFontSize: Appearance["uiFontSize"]
  contentFont: Appearance["contentFont"]
  fontSize: Appearance["fontSize"]
  codeFont: Appearance["codeFont"]
  codeFontSize: Appearance["codeFontSize"]
  contentWidth: Appearance["contentWidth"]
  transcriptMode: Appearance["transcriptMode"]
  palette: Appearance["palette"]
  /** Which sub-agent the right-hand panel is showing, if any. */
  selectedAgent?: string
  /** Worker id whose log is being fetched after a click. */
  agentLogLoading?: string

  boot: () => Promise<void>
  refreshThreads: () => Promise<void>
  /** Re-read running flags without toasting a dropped packet. */
  syncThreads: () => Promise<void>
  /** Re-lists every endpoint and writes the catalogs. Does not reboot the
   *  conversation — boot() would yank the open thread. */
  refreshCatalogs: () => Promise<void>
  openThread: (id: string) => Promise<void>
  /** Fetch an older page of the event log. Sized from the scroller height. */
  loadOlder: (clientHeight?: number) => Promise<void>
  /** Keep paging until this turn's user row is in the transcript. */
  loadUntilTurn: (turnId: string, clientHeight?: number) => Promise<boolean>
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
  setPlan: (text: string) => Promise<void>
  savePlan: (text: string) => Promise<void>
  leavePlan: () => Promise<void>
  implementPlan: () => Promise<void>
  answerAsk: (
    callId: string,
    answers: Record<string, { answers: string[] }>,
  ) => Promise<void>
  editGoal: (text: string) => Promise<void>
  resumeGoal: () => Promise<void>
  compactThread: () => Promise<void>
  interrupt: () => Promise<void>
  preempt: () => Promise<void>
  retractSteer: (seq: number) => Promise<void>
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

/** Set while a conversation is being created. Someone who clicks "New
 *  conversation" starts typing immediately, and a send that read activeId
 *  before the new id arrived would run the turn in the conversation they just
 *  left — visibly nowhere. */
let creating: Promise<string | undefined> | undefined

/** Same draft already in flight. A leftover Enter / IME echo must not start
 *  the turn and also enqueue it. */
const inflightDrafts = new Set<string>()

function inflightKey(id: string | undefined, text: string) {
  return `${id ?? ""}:${text.trim()}`
}

function applyThreadListing(
  threads: Thread[],
  incoming: Thread[],
  activeId: string | undefined,
  status: ThreadStatus,
): Thread[] {
  return mergeThreadList(threads, incoming, threadListOverlay(activeId, status))
}

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
  historyHasMore: false,
  historyLoading: false,
  connected: false,
  theme: readTheme(),
  locale: readLocalePref(),
  ...readAppearance(),

  boot: async () => {
    applyTheme(get().theme)
    applyLocale(get().locale)
    applyAppearance(normalizeAppearance({
      font: get().font,
      ui_font_size: get().uiFontSize,
      content_font: get().contentFont,
      font_size: get().fontSize,
      code_font: get().codeFont,
      code_font_size: get().codeFontSize,
      content_width: get().contentWidth,
      transcript_mode: get().transcriptMode,
      palette: get().palette,
    }))
    startPresence(api, desktopShell() ? "desktop" : "web")
    try {
      const [meta, models, threads] = await Promise.all([
        api.meta(),
        api.models(),
        api.threads(),
        useProjects.getState().refresh(),
      ])
      set({ meta, models: models.models, threads })
      await get().refreshSchedules()
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
      const incoming = await api.threads()
      set((s) => ({
        threads: applyThreadListing(s.threads, incoming, s.activeId, s.status),
      }))
    } catch (e) {
      set({ error: message(e) })
    }
    await get().refreshSchedules()
  },

  /** Same listing as refreshThreads, but a dropped packet must not toast:
   *  this is the background pass that keeps folder progress honest. */
  syncThreads: async () => {
    try {
      const [incoming, meta] = await Promise.all([
        api.threads(),
        api.meta().catch(() => undefined),
      ])
      set((s) => ({
        threads: applyThreadListing(s.threads, incoming, s.activeId, s.status),
        ...(meta ? { meta } : {}),
      }))
    } catch {
      // A background tick is not a user action.
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
    // Leave Scheduled even when this conversation is already open; Open
    // findings would otherwise be a no-op and leave the page up.
    if (get().scheduleInboxOpen) {
      set({ scheduleInboxOpen: false, error: undefined })
    }
    if (get().activeId === id) return
    dropQueued()
    unsubscribe?.()
    unsubscribe = undefined
    resetThreadHistory()
    set((s) => ({
      activeId: id,
      transcript: emptyTranscript(),
      turns: [],
      followups: [],
      files: [],
      loaded: false,
      historyHasMore: false,
      historyLoading: false,
      selectedAgent: undefined,
      agentLogLoading: undefined,
      status: { running: false },
      usage: undefined,
      // The header clock dies with `status`. Keep the row's progress mark
      // so switching away from a live turn does not make the folder look idle.
      threads:
        s.activeId && s.activeId !== id && s.status.running
          ? setThreadRunning(
              s.threads,
              s.activeId,
              true,
              Boolean(s.status.awaiting_answer),
            )
          : s.threads,
    }))

    try {
      const [{ thread, status, usage }, turns, followups] = await Promise.all([
        api.thread(id),
        api.turns(id),
        api.followups(id),
      ])
      if (get().activeId !== id) return
      let since = 0
      let haveTail = false
      let log: ThreadLog = { events: [], has_more: false }
      try {
        log = await api.threadLog(id, { limit: logPageSize(0) })
        haveTail = true
      } catch {
        // SSE still replays from the start so a missing log endpoint
        // does not leave the skeleton up forever.
      }
      if (get().activeId !== id) return
      if (haveTail) {
        const transcript = applyTail(id, log.events ?? [], log.roster ?? [])
        if (status.running) transcript.running = true
        since = historyNewestSeq()
        set((s) => ({
          connected: true,
          transcript,
          historyHasMore: Boolean(log.has_more),
          status,
          turns,
          followups,
          usage,
          threads: setThreadRunning(
            upsertThread(s.threads, thread),
            id,
            status.running,
            Boolean(status.awaiting_answer),
          ),
        }))
        if (Boolean(log.has_more) && !managerHasVisibleBlocks(transcript)) {
          await loadUntilVisibleHistory(get)
          if (get().activeId !== id) return
        }
        set((s) => ({
          loaded: true,
          transcript: s.status.running ? { ...s.transcript, running: true } : s.transcript,
        }))
      } else {
        set((s) => ({
          status,
          turns,
          followups,
          usage,
          threads: setThreadRunning(
            upsertThread(s.threads, thread),
            id,
            status.running,
            Boolean(status.awaiting_answer),
          ),
        }))
      }
      // The Memory tab belongs to the project, not the conversation. Opening
      // a Recents chat must drop the previous project's highlight or the
      // folder would still look selected.
      useProjects.getState().select(thread.project_id || undefined)
      if (thread.project_id) {
        void useProjects.getState().loadMemory(thread.project_id)
      }
      void get().refreshSchedules()
      unsubscribe = subscribeEvents(
        id,
        {
          onEvent: (ev) => queueEvent(set, get, id, ev),
          onReady: ({ status: live }) => {
            set((s) => ({
              loaded: true,
              connected: true,
              status: live
                ? { ...live, waiting: live.waiting ?? s.status.waiting }
                : s.status,
              historyHasMore: since > 0 ? s.historyHasMore : false,
              threads:
                live && s.activeId
                  ? setThreadRunning(
                      s.threads,
                      s.activeId,
                      Boolean(live.running),
                      Boolean(live.awaiting_answer),
                    )
                  : s.threads,
            }))
            void get().refreshFiles()
          },
          onClose: (reason) => set({ connected: reason !== "error" }),
        },
        since,
      )
    } catch (e) {
      if (get().activeId !== id) return
      set({ error: message(e) })
    }
  },

  loadOlder: async (clientHeight) => {
    await loadOlderHistory(get, set, message, clientHeight)
  },

  loadUntilTurn: async (turnId, clientHeight) =>
    loadUntilTurnHistory(get, turnId, clientHeight),

  newThread: async (projectId) => {
    creating = (async () => {
      try {
        // Stamp before the create round-trip: a listing refresh that lands
        // while we wait must not idle the folder we are about to leave.
        const keepId = get().activeId
        const keepBusy = Boolean(keepId && get().status.running)
        const keepAsking = Boolean(get().status.awaiting_answer)
        if (keepBusy && keepId) {
          set((s) => ({
            threads: setThreadRunning(s.threads, keepId, true, keepAsking),
          }))
        }
        const thread = await api.createThread(undefined, undefined, projectId)
        set((s) => {
          const rest = s.threads.filter((t) => t.id !== thread.id)
          return {
            threads: [
              thread,
              ...(keepBusy && keepId
                ? setThreadRunning(rest, keepId, true, keepAsking)
                : rest),
            ],
          }
        })
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
      rememberRewind(id, from)
      bumpFollowups()
      set((s) => ({
        transcript: placePendingEdit(
          rewindTranscript(s.transcript, from),
          text,
          cut?.images,
        ),
        followups: [],
        status: withRunningClock(s.status),
        threads: s.activeId ? setThreadRunning(s.threads, s.activeId, true) : s.threads,
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
    const key = inflightKey(get().activeId, text)
    if (inflightDrafts.has(key)) return
    inflightDrafts.add(key)
    try {
      let id = await currentThread(get)
      if (!id) {
        id = await get().newThread()
        if (!id) return
      }
      // A live question is not a follow-up: Enter is Other for every
      // unanswered prompt. Queueing here would wait for a turn that cannot
      // finish until this answer lands.
      if (get().status.awaiting_answer) {
        await api.answerTurn(id, { text })
        set({ error: undefined })
        return
      }
      // Pasted images have nowhere to wait: the follow-up row is text. They
      // inject now rather than being dropped on the floor.
      const queue =
        !opts?.steer &&
        get().status.running &&
        !(images && images.length > 0) &&
        !(opts?.files && opts.files.length > 0)
      if (queue) {
        if (liveTurnUserText(get().status, get().transcript) === text.trim()) {
          return
        }
        try {
          const item = await api.enqueueFollowup(id, text)
          bumpFollowups()
          set((s) => ({ followups: [...s.followups, item] }))
          return
        } catch (e) {
          if (!(e instanceof ApiError && e.code === "idle")) throw e
        }
      }
      if (opts?.steer) {
        const kept = dropMatchingFollowups(get().followups, text)
        if (kept.length !== get().followups.length) {
          const dropped = get().followups.filter((f) => !kept.includes(f))
          bumpFollowups()
          set({ followups: kept })
          await Promise.all(
            dropped.map((f) => api.deleteFollowup(id, f.id).catch(() => undefined)),
          )
        }
      }
      await api.steer(id, text, images, opts?.files)
      set((s) => ({
        status: withRunningClock(s.status),
        threads: setThreadRunning(s.threads, id, true),
      }))
      void get().refreshFollowups()
      void get().refreshThreads()
    } catch (e) {
      set({ error: message(e) })
    } finally {
      inflightDrafts.delete(key)
    }
  },

  setGoal: async (text) => {
    let id = await currentThread(get)
    if (!id) {
      id = await get().newThread()
      if (!id) return
    }
    const running = get().status.running
    try {
      const updated = await api.patchThread(id, { goal: text })
      set((s) => ({
        threads: s.threads.map((t) => (t.id === id ? { ...t, ...updated } : t)),
        error: undefined,
      }))
      // Codex / Cursor: `/goal <objective>` is the first unit of work, not a
      // pin that waits for another Enter. A live turn is steered by the
      // engine; starting a second one here would 409 or queue the objective.
      const objective = updated.goal?.trim() || text.trim()
      if (objective && !running) await get().send(objective)
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
          threads: setThreadRunning(
            s.threads.map((t) => (t.id === id ? { ...t, ...updated } : t)),
            id,
            Boolean(updated.running),
          ),
          status: updated.running ? withRunningClock(s.status) : s.status,
          error: undefined,
        }))
    } catch (e) {
      set({ error: message(e) })
    }
  },

  ...planAskActions(set, get, {
    currentThread: () => currentThread(get),
    fail: message,
    withRunningClock,
  }),

  ...scheduleActions(set, get, { fail: message }),

  ...steerInjectActions(set, get, message),

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
    const token = bumpFollowups()
    try {
      const list = await api.followups(id)
      if (!isFollowupGeneration(token) || get().activeId !== id) return
      set({ followups: list })
    } catch (e) {
      if (!isFollowupGeneration(token)) return
      set({ error: message(e) })
    }
  },

  steerFollowup: async (fid) => {
    const id = get().activeId
    if (!id) return
    try {
      await api.steerFollowup(id, fid)
      bumpFollowups()
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
      bumpFollowups()
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
      bumpFollowups()
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
    const cur = get()
    const next = normalizeAppearance({
      font: patch.font ?? cur.font,
      ui_font_size: patch.uiFontSize ?? cur.uiFontSize,
      content_font: patch.contentFont ?? cur.contentFont,
      font_size: patch.fontSize ?? cur.fontSize,
      code_font: patch.codeFont ?? cur.codeFont,
      code_font_size: patch.codeFontSize ?? cur.codeFontSize,
      content_width: patch.contentWidth ?? cur.contentWidth,
      transcript_mode: patch.transcriptMode ?? cur.transcriptMode,
      palette: patch.palette ?? cur.palette,
    })
    writeAppearance(next)
    applyAppearance(next)
    set(next)
    if (opts?.persist === false) return
    persistChrome(get())
  },

  selectAgent: (selectedAgent) => {
    set({ selectedAgent, agentLogLoading: undefined })
    if (selectedAgent) void loadAgentHistory(get, set, message, selectedAgent)
  },
  setError: (error) => set({ error }),
}))

/** The conversation an action should apply to, once any conversation being
 *  created has arrived. */
async function currentThread(get: () => AppState): Promise<string | undefined> {
  if (creating) await creating
  return get().activeId
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

function persistChrome(state: {
  locale: LocalePref
} & Appearance) {
  void api
    .saveSettings({
      ui: appearanceToUI(state, state.locale),
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
