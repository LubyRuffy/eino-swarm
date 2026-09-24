import { create } from "zustand"

import { applyPinnedOrder } from "@/lib/reorder"
import { api, ApiError, type ProjectPatch } from "@/lib/api"
import { reviewPanelHint } from "@/lib/transcript"
import type { MemoryEntries, Project, ProjectMemory, ReviewOutcome, SkillTidyReport, TidyLive } from "@/lib/types"
import { normalizeTidyReport } from "@/lib/skill-tidy"

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
  /** True between a "Review now" click and the matching memory_review event. */
  reviewing: boolean
  /** What the last click decided, once it has an answer. */
  reviewHint?: string
  /** The turn a click is waiting on. Absent means the next review event. */
  pendingReviewTurnId?: string
  error?: string

  refresh: () => Promise<void>
  select: (id?: string) => void
  beginReview: () => void
  awaitReview: (turnId: string) => void
  endReview: (hint?: string) => void
  finishReview: (turnId: string, outcome?: ReviewOutcome) => void
  /** create and update throw on refusal: the dialog shows the server's
   *  message against the field it names, which an error swallowed into the
   *  store could not do. */
  create: (patch: ProjectPatch) => Promise<Project>
  update: (id: string, patch: ProjectPatch) => Promise<Project>
  remove: (id: string) => Promise<void>
  reorder: (ids: string[]) => Promise<void>
  loadMemory: (projectId?: string) => Promise<void>
  saveMemory: (text: string, rev?: string) => Promise<void>
  removeSkill: (name: string) => Promise<void>
  tidySkills: () => Promise<void>
  clearTidy: () => void
  /** True between a tidy click and the response, for the project on screen. */
  tidying: boolean
  /** What the last tidy decided for the project on screen. */
  tidyReport?: SkillTidyReport
  tidyError?: string
  /** Model prose and skill writes while this project's tidy is still open. */
  tidyLive?: TidyLive
  /** One slot per project. The fields above are only the open project's slot,
   *  so leaving and coming back does not throw the click away. */
  tidySlots: Record<string, TidySlot>
  noteMemoryWrite: () => void
  seeMemory: () => void
  setError: (message?: string) => void
}

export const useProjects = create<ProjectsState>((set, get) => ({
  projects: [],
  memoryLoading: false,
  memoryUnread: false,
  reviewing: false,
  tidying: false,
  tidySlots: {},

  beginReview: () =>
    set({ reviewing: true, reviewHint: undefined, pendingReviewTurnId: undefined }),

  awaitReview: (turnId) => {
    if (!get().reviewing) return
    set({ pendingReviewTurnId: turnId })
  },

  endReview: (reviewHint) =>
    set({ reviewing: false, reviewHint, pendingReviewTurnId: undefined }),

  finishReview: (turnId, outcome) => {
    if (!get().reviewing) return
    const pending = get().pendingReviewTurnId
    // A click that has not yet heard which turn was accepted still owns the
    // next review event: the job can finish before the 202 body is parsed.
    if (pending && pending !== turnId) return
    set({
      reviewing: false,
      reviewHint: reviewPanelHint(outcome),
      pendingReviewTurnId: undefined,
    })
  },

  refresh: async () => {
    try {
      const projects = await api.projects()
      set((s) => ({
        projects,
        // A project deleted in another window must not stay selected.
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
      set((s) => {
        const tidySlots = dropSlot(s.tidySlots, id)
        const leaving = s.memoryProjectId === id
        return {
          projects: s.projects.filter((p) => p.id !== id),
          selectedId: s.selectedId === id ? undefined : s.selectedId,
          memory: leaving ? undefined : s.memory,
          memoryProjectId: leaving ? undefined : s.memoryProjectId,
          tidySlots,
          ...(leaving ? viewOf(slotOf(tidySlots)) : {}),
        }
      })
    } catch (e) {
      set({ error: message(e) })
    }
  },

  reorder: async (ids) => {
    set({ projects: applyPinnedOrder(get().projects, ids) })
    try {
      set({ projects: await api.reorderProjects(ids) })
    } catch (e) {
      set({ error: message(e) })
      await get().refresh()
    }
  },

  loadMemory: async (projectId) => {
    const id = projectId ?? get().memoryProjectId
    if (!id) {
      set({
        memory: undefined,
        memoryProjectId: undefined,
        ...viewOf(slotOf(get().tidySlots)),
      })
      return
    }
    // A catalog tidy records memory_review on the same turn. Re-reading the
    // files must not wipe the card the click just painted. Opening a different
    // project shows that project's slot instead of deleting the one we left:
    // the click belongs to the project, and coming back has to find it.
    const switched = get().memoryProjectId !== id
    const inflight = slotOf(get().tidySlots, id).tidying
    set({
      memoryLoading: true,
      memoryProjectId: id,
      ...(switched ? viewOf(slotOf(get().tidySlots, id)) : {}),
    })
    try {
      const memory = await api.memory(id)
      // The panel may have moved on while this was in flight; showing one
      // project's notes under another's name is worse than showing none.
      if (get().memoryProjectId !== id) return
      const slot = slotOf(get().tidySlots, id)
      // This read started while a tidy was still running and the tidy has
      // since written the catalog. Applying the older snapshot would put
      // the pre-tidy list back under the card that says the fold happened.
      if (inflight && !slot.tidying && slot.report) {
        set({ memoryLoading: false })
        return
      }
      set((s) => ({
        memory,
        memoryLoading: false,
        // Skills stay behind the Memory tab. A review that just wrote one
        // would otherwise leave the panel looking empty until reload.
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

  tidySkills: async () => {
    const id = get().memoryProjectId
    if (!id) return
    set((s) =>
      applySlot(s, id, { tidying: true, report: undefined, error: undefined, live: { text: "", changes: [] } }),
    )
    try {
      const result = await api.tidySkills(id, (ev) => {
        set((s) => {
          const prev = slotOf(s.tidySlots, id).live ?? { text: "", changes: [] }
          if (ev.phase === "text" && typeof ev.text === "string") {
            return applySlot(s, id, { live: { ...prev, text: ev.text } })
          }
          if (ev.phase === "change" && ev.action) {
            return applySlot(s, id, {
              live: {
                ...prev,
                changes: [...prev.changes, { action: ev.action, name: ev.name ?? "" }],
              },
            })
          }
          return s
        })
      })
      const report = normalizeTidyReport(result.report, result.memory.skills.length)
      set((s) => ({
        ...applySlot(s, id, { tidying: false, report, error: undefined }),
        // The sidebar index follows the catalog even when another project is
        // on screen. The notes on screen stay the open project's.
        projects: s.projects.map((p) =>
          p.id === id ? { ...p, skills: result.memory.skills } : p,
        ),
        ...(s.memoryProjectId === id ? { memory: result.memory } : {}),
      }))
    } catch (e) {
      const error = message(e)
      set((s) => applySlot(s, id, { tidying: false, error }))
    }
  },

  clearTidy: () => {
    const id = get().memoryProjectId
    if (!id) {
      set({ tidyReport: undefined, tidyError: undefined })
      return
    }
    set((s) => applySlot(s, id, { report: undefined, error: undefined }))
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

/** A tidy click's card, kept after the panel moves on. */
interface TidySlot {
  tidying: boolean
  report?: SkillTidyReport
  error?: string
  live?: TidyLive
}

function slotOf(slots: Record<string, TidySlot>, id?: string): TidySlot {
  if (!id) return { tidying: false }
  return slots[id] ?? { tidying: false }
}

function paintSlot(
  slots: Record<string, TidySlot>,
  id: string,
  patch: Partial<TidySlot>,
): Record<string, TidySlot> {
  const prev = slots[id] ?? { tidying: false }
  return { ...slots, [id]: { ...prev, ...patch } }
}

function dropSlot(slots: Record<string, TidySlot>, id: string): Record<string, TidySlot> {
  if (!(id in slots)) return slots
  const next = { ...slots }
  delete next[id]
  return next
}

function viewOf(slot: TidySlot) {
  return {
    tidying: slot.tidying,
    tidyReport: slot.report,
    tidyError: slot.error,
    tidyLive: slot.live,
  }
}

/** Write one project's card and, when that project is the one on screen,
 *  the fields the panel actually reads. */
function applySlot(
  s: { tidySlots: Record<string, TidySlot>; memoryProjectId?: string },
  id: string,
  patch: Partial<TidySlot>,
) {
  const tidySlots = paintSlot(s.tidySlots, id, patch)
  return {
    tidySlots,
    ...(s.memoryProjectId === id ? viewOf(tidySlots[id]) : {}),
  }
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
