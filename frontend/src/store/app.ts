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
import { closeClient } from "@/lib/client-open"
import { listDestForThread, setDeskDest } from "@/lib/desk-nav"
import { desktopShell, startPresence } from "@/lib/shell"
import {
  applyLocale,
  normalizeLocalePref,
  readLocalePref,
  writeLocalePref,
  type LocalePref,
} from "@/lib/i18n"
import type { SendImage } from "@/lib/paste-image"
import {
  setThreadRunning,
} from "@/lib/thread-title"
import {
  emptyTranscript,
  MANAGER_ID,
  placePendingEdit,
  rewindTranscript,
  type TranscriptState,
} from "@/lib/transcript"
import { mergeModelContext } from "@/lib/usage"
import { persistPayload } from "@/lib/settings-persist"
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
import { followupActions } from "./app-followup"
import { scheduleActions, type ScheduleSlice } from "./app-schedule"
import { steerInjectActions } from "./app-steer"
import { dropQueued, withRunningClock } from "./app-stream"
import { loadOpenedThread } from "./app-open"
import { applyThreadListing } from "./app-thread-list"
import {
  bumpFollowups,
  dropMatchingFollowups,
  liveTurnUserText,
} from "./followup-sync"
import { explicitComposerRecovery } from "@/lib/composer-recovery"
import { composerDraftSession } from "@/lib/composer-draft"
import {
  loadAgentHistory,
  loadOlderHistory,
  loadUntilTurnHistory,
  rememberRewind,
  resetThreadHistory,
} from "./thread-history"

export type Theme = "light" | "dark" | "system"

export interface AppState extends ScheduleSlice {
  completionVisit?: number
  meta?: Meta
  models: ModelInfo[]
  threads: Thread[]
  activeId?: string
  creatingThread: boolean
  transcript: TranscriptState
  status: ThreadStatus
  turns: Turn[]
  followups: Followup[]
  files: FileEntry[]
  workspace: string
  usage?: UsageSnapshot
  /** The first tail page is ready, or fallback SSE replay has acknowledged.
   *  Context usage and other metadata can still be loading. */
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
  openThread: (id: string, opts?: { keepList?: boolean }) => Promise<void>
  /** Fetch an older page of the event log. Sized from the scroller height. */
  loadOlder: (clientHeight?: number) => Promise<void>
  /** Keep paging until this turn's user row is in the transcript. */
  loadUntilTurn: (turnId: string, clientHeight?: number) => Promise<boolean>
  /** Lands in projectId. Omit it and the conversation sits in Recents. */
  newThread: (projectId?: string, draftKey?: string) => Promise<string | undefined>
  /** Highlights a project and loads its memory. Does not filter the list. */
  selectProject: (projectId?: string) => Promise<void>
  pinThread: (id: string, pinned: boolean) => Promise<void>
  reviewNow: () => Promise<void>
  renameThread: (id: string, title: string) => Promise<void>
  deleteThread: (id: string) => Promise<void>
  reorderThreads: (ids: string[]) => Promise<void>
  send: (text: string, images?: SendImage[], opts?: { steer?: boolean; files?: string[]; fromEventSeq?: number; threadId?: string }) => Promise<boolean>
  setGoal: (text: string, threadId?: string) => Promise<boolean>
  setPlan: (text: string, threadId?: string) => Promise<boolean>
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
  retractSteer: (seq: number) => Promise<boolean>
  reviseSteer: (seq: number, text: string) => Promise<void>
  steerFollowup: (id: string) => Promise<void>
  deleteFollowup: (id: string) => Promise<boolean>
  requeueFollowup: (id: string, text: string) => Promise<void>
  clearFollowups: () => Promise<void>
  refreshFollowups: () => Promise<void>
  extendTurn: (proceed: boolean) => Promise<void>
  upload: (files: File[], threadId?: string) => Promise<Attachment[]>
  refreshFiles: () => Promise<void>
  removeFile: (path: string) => Promise<void>
  setTheme: (t: Theme) => void
  setLocale: (pref: LocalePref, opts?: { persist?: boolean }) => void
  setAppearance: (patch: Partial<Appearance>, opts?: { persist?: boolean }) => void
  selectAgent: (id?: string) => void
  setError: (message?: string) => void
}

let unsubscribe: (() => void) | undefined
let openGeneration = 0

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

const THEME_KEY = "zwai.theme"

export const useApp = create<AppState>((set, get) => ({
  models: [],
  threads: [],
  creatingThread: false,
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
      // Home page on launch. Nothing is selected: sidebar order is not
      // a remembered conversation.
      const recovery = explicitComposerRecovery(window.location.search)
      if (recovery.threadId) await get().openThread(recovery.threadId)
      else if (recovery.projectId && useProjects.getState().projects.some((project) => project.id === recovery.projectId)) {
        await get().selectProject(recovery.projectId)
      }
    } catch (e) {
      set({ error: message(e) })
    }
  },

  refreshThreads: async () => {
    const baseline = get().threads
    try {
      const incoming = await api.threads()
      set((s) => ({
        threads: applyThreadListing(s.threads, incoming, s.activeId, s.status, baseline),
      }))
    } catch (e) {
      set({ error: message(e) })
    }
    await get().refreshSchedules()
  },

  /** Same listing as refreshThreads, but a dropped packet must not toast:
   *  this is the background pass that keeps folder progress honest. */
  syncThreads: async () => {
    const baseline = get().threads
    try {
      const [incoming, meta] = await Promise.all([
        api.threads(),
        api.meta().catch(() => undefined),
      ])
      set((s) => ({
        threads: applyThreadListing(s.threads, incoming, s.activeId, s.status, baseline),
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
      await api.saveSettings(persistPayload({
        ...settings, models: { default: settings.models.default, providers },
      }, undefined, settings))
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
    useProjects.getState().beginReview(id, useProjects.getState().memoryProjectId, get().transcript.lastSeq)
    try {
      const { turn } = await api.reviewThread(id)
      if (turn?.id) useProjects.getState().awaitReview(turn.id, id)
    } catch (e) {
      // Idle is an answer: no finished turn, or memory is off. The banner
      // would make it look like the app broke; the panel is where they clicked.
      if (e instanceof ApiError && e.code === "idle") {
        useProjects.getState().endReview(
          "Nothing to review. Finish a turn in this project first.", id,
        )
        return
      }
      useProjects.getState().endReview(undefined, id)
      set({ error: message(e) })
    }
  },

  openThread: async (id, opts) => {
    set((s) => ({ completionVisit: (s.completionVisit ?? 0) + 1 }))
    // Leave Scheduled even when this conversation is already open; Open
    // findings would otherwise be a no-op and leave the page up. A project
    // topic stays on the project list; a loose one opens Conversations.
    // keepList leaves the rail where the reader already put it.
    const known = get().threads.find((th) => th.id === id)
    const guess = listDestForThread(known?.project_id)
    if (!opts?.keepList) setDeskDest(guess)
    closeClient()
    if (get().scheduleInboxOpen) {
      set({ scheduleInboxOpen: false, error: undefined })
    }
    if (get().activeId === id) return
    const generation = ++openGeneration
    dropQueued()
    unsubscribe?.()
    unsubscribe = undefined
    resetThreadHistory()
    set((s) => ({
      activeId: id,
      error: undefined,
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

    await loadOpenedThread(
      id, get, set,
      () => get().activeId === id && generation === openGeneration,
      (stop) => { unsubscribe = stop }, message, guess, opts?.keepList,
    )
  },

  loadOlder: async (clientHeight) => {
    await loadOlderHistory(get, set, message, clientHeight)
  },

  loadUntilTurn: async (turnId, clientHeight) =>
    loadUntilTurnHistory(get, turnId, clientHeight),

  newThread: async (projectId, draftKey) => {
    set({ creatingThread: true })
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
        // The recovery attribution must not advertise a new empty scope
        // while the only durable copy still belongs to the home composer.
        if (draftKey) await composerDraftSession(draftKey).transferTo(composerDraftSession(`thread:${thread.id}`))
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
      set({ creatingThread: false })
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
    if (get().meta?.startup === "restoring") return false
    if (opts?.fromEventSeq) {
      const id = get().activeId
      if (!id) return false
      const from = opts.fromEventSeq
      const cut = get().transcript.agents[MANAGER_ID]?.blocks.find(
        (b) => b.kind === "user" && b.seq === from,
      )
      // Before any await: this bubble stays with the new text, everything
      // below it is gone. Waiting on currentThread first would paint the
      // unedited bubble for a frame, which is the opposite of Codex.
      rememberRewind(id, from, get().transcript.lastSeq)
      dropQueued()
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
        return true
      } catch (e) {
        set({ error: message(e) })
        set({ activeId: undefined })
        await get().openThread(id)
        return false
      }
    }
    const target = opts?.threadId ?? (creating ? undefined : get().activeId)
    const key = inflightKey(target, text)
    if (inflightDrafts.has(key)) return false
    inflightDrafts.add(key)
    try {
      let id = target ?? await currentThread(get)
      if (!id) {
        id = await get().newThread()
        if (!id) return false
      }
      const status = id === get().activeId ? get().status : (await api.thread(id)).status
      // A live question is not a follow-up: Enter is Other for every
      // unanswered prompt. Queueing here would wait for a turn that cannot
      // finish until this answer lands.
      if (status.awaiting_answer) {
        if (images?.length || opts?.files?.length) {
          set({ error: "Attachments cannot be sent as an answer to a pending question. Answer the question with text first; the draft was kept." })
          return false
        }
        await api.answerTurn(id, { text })
        set({ error: undefined })
        return true
      }
      // Pasted images have nowhere to wait: the follow-up row is text. They
      // inject now rather than being dropped on the floor.
      const queue =
        !opts?.steer &&
        status.running &&
        !(images && images.length > 0) &&
        !(opts?.files && opts.files.length > 0)
      if (queue) {
        if (id === get().activeId && liveTurnUserText(status, get().transcript) === text.trim()) {
          return false
        }
        try {
          const item = await api.enqueueFollowup(id, text)
          bumpFollowups()
          set((s) => s.activeId === id ? { followups: [...s.followups, item] } : {})
          return true
        } catch (e) {
          if (!(e instanceof ApiError && e.code === "idle")) throw e
        }
      }
      if (opts?.steer && id === get().activeId) {
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
      const sentSeq = get().transcript.lastSeq
      const accepted = await api.steer(id, text, images, opts?.files)
      set((s) => {
        // The acknowledgement may arrive after this turn's terminal event.
        if (s.activeId !== id || (!s.status.running &&
          ((accepted.turn?.id && (s.status.turn_id === accepted.turn.id ||
            s.transcript.turns.some((t) => t.id === accepted.turn?.id && t.status !== "running"))) ||
            (accepted.steered && s.transcript.lastSeq > sentSeq)))) return {}
        return {
          status: withRunningClock(s.status),
          threads: setThreadRunning(s.threads, id, true),
        }
      })
      void get().refreshFollowups()
      void get().refreshThreads()
      return true
    } catch (e) {
      set({ error: message(e) })
      return false
    } finally {
      inflightDrafts.delete(key)
    }
  },

  setGoal: async (text, threadId) => {
    let id = threadId ?? await currentThread(get)
    if (!id) {
      id = await get().newThread()
      if (!id) return false
    }
    try {
      const running = id === get().activeId ? get().status.running : (await api.thread(id)).status.running
      const updated = await api.patchThread(id, { goal: text })
      set((s) => ({
        threads: s.threads.map((t) => (t.id === id ? { ...t, ...updated } : t)),
        error: undefined,
      }))
      // Codex / Cursor: `/goal <objective>` is the first unit of work, not a
      // pin that waits for another Enter. A live turn is steered by the
      // engine; starting a second one here would 409 or queue the objective.
      const objective = updated.goal?.trim() || text.trim()
      if (objective && !running) return await get().send(objective, undefined, { threadId: id })
      return true
    } catch (e) {
      set({ error: message(e) })
      return false
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

  ...followupActions(set, get, message),

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

  upload: async (files, threadId) => {
    const target = threadId ?? (creating ? undefined : get().activeId)
    let id = target ?? await currentThread(get)
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
