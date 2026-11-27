import type { MemoryChange } from "./types"

export type DistillScope = "latest_turn" | "thread" | "project"
export type DistillState = "idle" | "no_work" | "running" | "pausing" | "paused" | "completed" | "partial" | "failed" | "interrupted"
export type DistillErrorCode = "evidence" | "first_byte_timeout" | "idle_timeout" | "agent_timeout" |
  "context_overflow" | "iteration_limit" | "rate_limited" | "temporary_model" | "model_settings"

export type DistillRequest = {
  scope: DistillScope
  thread_id?: string
  full_text?: boolean
} | { pause_review_id: string } | { resume_review_id: string }

export interface DistillResult {
  state: DistillState
  review_id?: string
  scope?: DistillScope
  full_text?: boolean
  thread_id?: string
  turn_id?: string
  progress?: {
    total: number; processed: number; remaining: number; skipped: number; batches: number
    coverage_known?: boolean
    excerpted?: number
    oversized_skipped?: number
    segment_bytes?: number
    segment_total_bytes?: number
  }
  history_omitted?: boolean
  memory_enabled?: boolean
  changes?: MemoryChange[]
  error?: string
  error_code?: DistillErrorCode
  error_provider_id?: string
  uncertain?: boolean
}

export function distillationPath(projectId: string): string {
  return `/api/projects/${encodeURIComponent(projectId)}/distill`
}

export function hasPendingDistillation(result?: DistillResult): boolean {
  return (result?.progress?.remaining ?? 0) > 0
}

export function isActiveDistillation(state?: DistillState): boolean {
  return state === "running" || state === "pausing"
}
