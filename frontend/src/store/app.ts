import { create } from "zustand"

import { ApiError, api } from "@/lib/api"
import { subscribeEvents } from "@/lib/stream"
import {
  emptyTranscript,
  reduceEvent,
  type TranscriptState,
} from "@/lib/transcript"
import type {
  FileEntry,
  Meta,
  ModelInfo,
  SwarmEvent,
  Thread,
  ThreadStatus,
  Turn,
} from "@/lib/types"

export type Theme = "light" | "dark" | "system"

interface AppState {
  meta?: Meta
  models: ModelInfo[]
  threads: Thread[]
  activeId?: string
  transcript: TranscriptState
  status: ThreadStatus
  turns: Turn[]
  files: FileEntry[]
  workspace: string
  /** Undefined until the first stream has replayed, so the transcript can
   *  show a skeleton instead of an empty conversation. */
  loaded: boolean
  connected: boolean
  error?: string
  theme: Theme
  /** Which sub-agent the right-hand panel is showing, if any. */
  selectedAgent?: string

  boot: () => Promise<void>
  refreshThreads: () => Promise<void>
  openThread: (id: string) => Promise<void>
  newThread: () => Promise<string | undefined>
  renameThread: (id: string, title: string) => Promise<void>
  deleteThread: (id: string) => Promise<void>
  send: (text: string) => Promise<void>
  interrupt: () => Promise<void>
  upload: (files: File[]) => Promise<void>
  refreshFiles: () => Promise<void>
  removeFile: (path: string) => Promise<void>
  setTheme: (t: Theme) => void
  selectAgent: (id?: string) => void
  setError: (message?: string) => void
}

let unsubscribe: (() => void) | undefined

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
  files: [],
  workspace: "",
  loaded: false,
  connected: false,
  theme: readTheme(),

  boot: async () => {
    applyTheme(get().theme)
    try {
      const [meta, models, threads] = await Promise.all([
        api.meta(),
        api.models(),
        api.threads(),
      ])
      set({ meta, models: models.models, threads })
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

  openThread: async (id) => {
    if (get().activeId === id) return
    unsubscribe?.()
    unsubscribe = undefined
    set({
      activeId: id,
      transcript: emptyTranscript(),
      turns: [],
      files: [],
      loaded: false,
      selectedAgent: undefined,
      status: { running: false },
    })

    unsubscribe = subscribeEvents(id, {
      onEvent: (ev) => ingest(set, get, id, ev),
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
      const [{ status }, turns] = await Promise.all([api.thread(id), api.turns(id)])
      set({ status, turns })
    } catch (e) {
      set({ error: message(e) })
    }
  },

  newThread: async () => {
    creating = (async () => {
      try {
        const thread = await api.createThread()
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
        set({ activeId: undefined, transcript: emptyTranscript(), loaded: false })
        if (remaining.length > 0) await get().openThread(remaining[0].id)
      }
    } catch (e) {
      set({ error: message(e) })
    }
  },

  send: async (text) => {
    let id = await currentThread(get)
    if (!id) {
      id = await get().newThread()
      if (!id) return
    }
    try {
      // One call for both cases: the server steers a running turn and starts
      // a new one otherwise, so the composer never has to race the status it
      // last saw.
      await api.steer(id, text)
      set({ status: { ...get().status, running: true } })
      void get().refreshThreads()
    } catch (e) {
      set({ error: message(e) })
    }
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

  upload: async (files) => {
    let id = await currentThread(get)
    if (!id) {
      id = await get().newThread()
      if (!id) return
    }
    try {
      await api.upload(id, files)
      await get().refreshFiles()
    } catch (e) {
      set({ error: message(e) })
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
 *  the user has already navigated away from. */
function ingest(
  set: (partial: Partial<AppState>) => void,
  get: () => AppState,
  threadId: string,
  ev: SwarmEvent,
) {
  const state = get()
  if (state.activeId !== threadId) return

  const transcript = reduceEvent(state.transcript, ev)
  const patch: Partial<AppState> = { transcript }

  if (ev.kind === "user_message") {
    // The event's own timestamp is the turn's start, so the header's clock is
    // right without waiting for a status fetch to come back.
    patch.status = {
      ...state.status,
      running: true,
      turn_id: ev.turn_id,
      started_at: ev.created_at,
    }
  }
  if (ev.kind === "done" || ev.kind === "error") {
    patch.status = { running: false }
    // The turn produced files and a title; both are worth refreshing once.
    void get().refreshFiles()
    void get().refreshThreads()
    void api
      .turns(threadId)
      .then((turns) => set({ turns }))
      .catch(() => undefined)
  }
  set(patch)
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
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
