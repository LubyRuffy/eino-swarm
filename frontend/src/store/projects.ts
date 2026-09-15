import { create } from "zustand"

import { api, ApiError, type ProjectPatch } from "@/lib/api"
import type { MemoryEntries, Project, ProjectMemory } from "@/lib/types"

/** Projects and the memory panel live in their own store.
 *
 *  They are read by the sidebar, the header and one panel tab, and none of
 *  that has to re-render when a token arrives — which everything in the
 *  conversation store does, many times a second. */
interface ProjectsState {
  projects: Project[]
  /** The sidebar's filter, and the project a new conversation lands in. */
  selectedId?: string
  /** Whose memory `memory` belongs to, so a stale panel is never shown next
   *  to another project's name. */
  memoryProjectId?: string
  memory?: ProjectMemory
  memoryLoading: boolean
  /** A write landed while the Memory tab was not the one on screen. */
  memoryUnread: boolean
  error?: string

  refresh: () => Promise<void>
  select: (id?: string) => void
  /** create and update throw on refusal: the dialog shows the server's
   *  message against the field it names, which an error swallowed into the
   *  store could not do. */
  create: (patch: ProjectPatch) => Promise<Project>
  update: (id: string, patch: ProjectPatch) => Promise<Project>
  remove: (id: string) => Promise<void>
  loadMemory: (projectId?: string) => Promise<void>
  saveMemory: (text: string, rev?: string) => Promise<void>
  removeSkill: (name: string) => Promise<void>
  noteMemoryWrite: () => void
  seeMemory: () => void
  setError: (message?: string) => void
}

export const useProjects = create<ProjectsState>((set, get) => ({
  projects: [],
  memoryLoading: false,
  memoryUnread: false,

  refresh: async () => {
    try {
      const projects = await api.projects()
      set((s) => ({
        projects,
        // A project deleted in another window must not stay selected, or the
        // sidebar filters by an id the server has never heard of.
        selectedId: projects.some((p) => p.id === s.selectedId)
          ? s.selectedId
          : undefined,
      }))
    } catch (e) {
      set({ error: message(e) })
    }
  },

  select: (selectedId) => set({ selectedId }),

  create: async (patch) => {
    const project = await api.createProject(patch)
    set((s) => ({ projects: [project, ...s.projects] }))
    return project
  },

  update: async (id, patch) => {
    const project = await api.patchProject(id, patch)
    set((s) => ({
      projects: s.projects.map((p) => (p.id === id ? project : p)),
      memory: s.memoryProjectId === id ? undefined : s.memory,
      memoryProjectId: s.memoryProjectId === id ? undefined : s.memoryProjectId,
    }))
    await get().loadMemory(id)
    return project
  },

  remove: async (id) => {
    try {
      await api.deleteProject(id)
      set((s) => ({
        projects: s.projects.filter((p) => p.id !== id),
        selectedId: s.selectedId === id ? undefined : s.selectedId,
        memory: s.memoryProjectId === id ? undefined : s.memory,
        memoryProjectId: s.memoryProjectId === id ? undefined : s.memoryProjectId,
      }))
    } catch (e) {
      set({ error: message(e) })
    }
  },

  loadMemory: async (projectId) => {
    const id = projectId ?? get().memoryProjectId
    if (!id) {
      set({ memory: undefined, memoryProjectId: undefined })
      return
    }
    set({ memoryLoading: true, memoryProjectId: id })
    try {
      const memory = await api.memory(id)
      // The panel may have moved on while this was in flight; showing one
      // project's notes under another's name is worse than showing none.
      if (get().memoryProjectId !== id) return
      set((s) => ({
        memory,
        memoryLoading: false,
        // The sidebar lists skills under the project. A review that just
        // wrote one would otherwise leave the row looking empty until reload.
        projects: s.projects.map((p) =>
          p.id === id ? { ...p, skills: memory.skills } : p,
        ),
      }))
    } catch (e) {
      if (get().memoryProjectId !== id) return
      set({ memoryLoading: false, error: message(e) })
    }
  },

  saveMemory: async (text, rev) => {
    const id = get().memoryProjectId
    if (!id) return
    const known = rev ?? get().memory?.memory.rev
    try {
      const memory = await api.saveMemory(id, text, known)
      set((s) =>
        s.memory && s.memoryProjectId === id
          ? { memory: { ...s.memory, memory }, memoryUnread: false }
          : {},
      )
    } catch (e) {
      if (e instanceof ApiError && e.code === "conflict") {
        const current = snapshotOf(e.details?.memory)
        if (current && get().memoryProjectId === id) {
          set((s) => ({
            memory: s.memory ? { ...s.memory, memory: current } : s.memory,
          }))
        }
      }
      throw e
    }
  },

  removeSkill: async (name) => {
    const id = get().memoryProjectId
    if (!id) return
    try {
      await api.deleteSkill(id, name)
      await get().loadMemory(id)
    } catch (e) {
      set({ error: message(e) })
    }
  },

  noteMemoryWrite: () => set({ memoryUnread: true }),
  seeMemory: () => set({ memoryUnread: false }),

  setError: (error) => set({ error }),
}))

/** The project a conversation belongs to, for the header chip. */
export function projectOf(
  projects: Project[],
  projectId?: string,
): Project | undefined {
  if (!projectId) return undefined
  return projects.find((p) => p.id === projectId)
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

function snapshotOf(raw: unknown): MemoryEntries | undefined {
  if (!raw || typeof raw !== "object") return undefined
  const body = raw as Partial<MemoryEntries>
  if (typeof body.text !== "string") return undefined
  return {
    text: body.text,
    entries: Array.isArray(body.entries) ? body.entries.map(String) : [],
    chars: typeof body.chars === "number" ? body.chars : body.text.length,
    limit: typeof body.limit === "number" ? body.limit : 0,
    rev: typeof body.rev === "string" ? body.rev : "",
  }
}
