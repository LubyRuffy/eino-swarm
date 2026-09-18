import { create } from "zustand"

import {
  canOpenTerminal,
  MAX_TERMINALS,
  type TerminalTarget,
} from "@/lib/terminal"

export type TerminalSession = TerminalTarget & {
  id: string
  cwd?: string
}

let nextId = 1

function newId() {
  const id = `term_${nextId}`
  nextId += 1
  return id
}

interface TerminalState {
  open: boolean
  height: number
  sessions: TerminalSession[]
  activeId?: string
  /** Every call opens a new PTY in the current target's directory. */
  spawn: (target: TerminalTarget) => string | undefined
  toggle: (target?: TerminalTarget) => void
  closePanel: () => void
  closeSession: (id: string) => void
  select: (id: string) => void
  setCwd: (id: string, cwd: string) => void
  setHeight: (height: number) => void
}

const defaultHeight = 240

export const useTerminal = create<TerminalState>((set, get) => ({
  open: false,
  height: defaultHeight,
  sessions: [],
  activeId: undefined,

  spawn: (target) => {
    if (!canOpenTerminal(target)) return undefined
    if (get().sessions.length >= MAX_TERMINALS) {
      set({ open: true })
      return get().activeId
    }
    const id = newId()
    const session: TerminalSession = {
      id,
      threadId: target.threadId,
      projectId: target.projectId,
    }
    set((s) => ({
      open: true,
      sessions: [...s.sessions, session],
      activeId: id,
    }))
    return id
  },

  toggle: (target) => {
    if (get().open) {
      set({ open: false })
      return
    }
    if (get().sessions.length === 0) {
      if (target) get().spawn(target)
      return
    }
    set({ open: true })
  },

  closePanel: () => set({ open: false }),

  closeSession: (id) => {
    const sessions = get().sessions.filter((s) => s.id !== id)
    const activeId =
      get().activeId === id ? sessions[sessions.length - 1]?.id : get().activeId
    set({
      sessions,
      activeId,
      open: sessions.length > 0 ? get().open : false,
    })
  },

  select: (id) => set({ activeId: id, open: true }),

  setCwd: (id, cwd) =>
    set((s) => ({
      sessions: s.sessions.map((session) =>
        session.id === id ? { ...session, cwd } : session,
      ),
    })),

  setHeight: (height) =>
    set({ height: Math.min(640, Math.max(120, Math.round(height))) }),
}))

export function resetTerminalStore() {
  nextId = 1
  useTerminal.setState({
    open: false,
    height: defaultHeight,
    sessions: [],
    activeId: undefined,
  })
}
