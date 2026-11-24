import type { MemoryChange } from "./types"

export type DistillScope = "latest_turn" | "thread" | "project"
export type DistillState = "idle" | "no_work" | "running" | "pausing" | "paused" | "completed" | "partial" | "failed" | "interrupted"
export type DistillErrorCode = "evidence" | "first_byte_timeout" | "idle_timeout" | "agent_timeout" |
  "context_overflow" | "iteration_limit" | "rate_limited" | "temporary_model" | "model_settings"

export type DistillRequest = {
  scope: DistillScope
  thread_id?: string
  enable_knowledge?: boolean
  full_text?: boolean
} | { pause_review_id: string } | { resume_review_id: string }

export interface DistillResult {
  state: DistillState
  review_id?: string
  scope?: DistillScope
  full_text?: boolean
  wiki_graph?: boolean
  wiki_phase?: "scan" | "graph" | "documents"
  wiki_scan_processed?: number
  wiki_scan_skipped?: number
  wiki_scan_deferred?: number
  wiki_scan_complete?: boolean
  wiki_documents?: number
  wiki_documented?: number
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
  knowledge_enabled?: boolean
  changes?: MemoryChange[]
  knowledge?: { action: "write_page" | "append_schema"; id?: string; title?: string; revision: string }[]
  error?: string
  error_code?: DistillErrorCode
  error_provider_id?: string
  uncertain?: boolean
}

export function distillationPath(projectId: string, wikiGraph = false): string {
  return `/api/projects/${encodeURIComponent(projectId)}/distill${wikiGraph ? "?wiki_graph=true" : ""}`
}

export function hasPendingDistillation(result?: DistillResult): boolean {
  return (result?.progress?.remaining ?? 0) > 0 ||
    Boolean(result?.wiki_graph && result.progress && result.state !== "completed")
}

export function isActiveDistillation(state?: DistillState): boolean {
  return state === "running" || state === "pausing"
}
