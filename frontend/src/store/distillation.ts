import { create } from "zustand"

import { api } from "@/lib/api"
import { isActiveDistillation, type DistillRequest, type DistillResult } from "@/lib/distillation"
import { useKnowledge } from "./knowledge"
import { useProjects } from "./projects"
import { errorMessage } from "./toasts"

interface DistillationSlot {
  generation: number
  starting?: boolean
  controlling?: boolean
  result?: DistillResult
  error?: string
}

interface DistillationState {
  projects: Record<string, DistillationSlot>
  refresh(projectId: string, signal: AbortSignal): Promise<void>
  start(projectId: string, request: DistillRequest): Promise<void>
}

export const useDistillation = createDistillationStore(false)
export const useWikiBuild = createDistillationStore(true)

function createDistillationStore(wikiGraph: boolean) {
  return create<DistillationState>((set, get) => {
    let nextRefresh = 0
    const appliedRefresh = new Map<string, number>()
    const read = (projectId: string): DistillationSlot => get().projects[projectId] ?? { generation: 0 }
    const save = (projectId: string, slot: DistillationSlot) => set((s) => ({
      projects: { ...s.projects, [projectId]: slot },
    }))
    const report = (projectId: string, slot: DistillationSlot, result: DistillResult) => {
      const changed = slot.result?.review_id !== result.review_id || slot.result?.state !== result.state
      const knowledgeChanged = slot.result?.knowledge?.length !== result.knowledge?.length ||
        (wikiGraph && (slot.result?.progress?.batches !== result.progress?.batches ||
          slot.result?.wiki_scan_complete !== result.wiki_scan_complete ||
          slot.result?.wiki_documented !== result.wiki_documented))
      const memoryChanged = slot.result?.changes?.length !== result.changes?.length
      save(projectId, { generation: slot.generation, result })
      if (!result.review_id) return
      if (result.knowledge_enabled && (changed || knowledgeChanged)) useKnowledge.getState().invalidate(projectId)
      if (memoryChanged || (changed && (result.changes?.length || result.uncertain))) {
        const projects = useProjects.getState()
        if (projects.memoryProjectId === projectId) void projects.loadMemory(projectId)
      }
    }

    return {
      projects: {},
      refresh: async (projectId, signal) => {
        const slot = read(projectId)
        if (slot.starting || slot.controlling) return
        const request = ++nextRefresh
        try {
          const result = await api.distillStatus(projectId, signal, wikiGraph)
          const current = read(projectId)
          if (signal.aborted || current.generation !== slot.generation || request < (appliedRefresh.get(projectId) ?? 0)) return
          appliedRefresh.set(projectId, request)
          // no_work is an immediate response, not a replacement for the last
          // durable receipt. Keep it visible when switching between the panels.
          if (current.result?.state === "no_work" && sourceIsPrevious(current.result, result)) return
          report(projectId, current, result)
        } catch (error) {
          if (!signal.aborted && read(projectId).generation === slot.generation && request >= (appliedRefresh.get(projectId) ?? 0)) {
            save(projectId, { ...read(projectId), error: errorMessage(error) })
          }
        }
      },
      start: async (projectId, request) => {
        const slot = read(projectId)
        const pause = "pause_review_id" in request
        const resume = "resume_review_id" in request
        if (slot.starting || slot.controlling) return
        if (slot.result?.state === "pausing" || (!pause && isActiveDistillation(slot.result?.state))) return
        const current = { ...slot, generation: slot.generation + 1 }
        save(projectId, { ...current, starting: !pause && !resume, controlling: pause || resume, error: undefined })
        try {
          const result = await api.startDistillation(projectId, request, wikiGraph)
          report(projectId, current, result.state === "no_work" ? { ...result, review_id: slot.result?.review_id } : result)
        } catch (error) {
          save(projectId, { ...current, error: errorMessage(error) })
        }
      },
    }
  })
}

function sourceIsPrevious(current: DistillResult, fetched: DistillResult): boolean {
  return fetched.state === "idle" || (current.review_id === fetched.review_id && !isActiveDistillation(fetched.state))
}
