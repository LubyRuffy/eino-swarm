import { block } from "./transcript-blocks"
import type { Block } from "./transcript"
import type { SwarmEvent } from "./types"

import { modelBackoffNotice } from "./transcript-backoff"

export interface BackoffContext {
  scope: "origin" | "schedule_answer" | "unknown"
  cooldownId?: string
  attempt?: number
  code?: "classification_unavailable" | "classification_invalid" | "classification_deferred"
  callers: { agentId: string; role?: string; seq: number }[]
}

/** Only durable attribution and allowlisted diagnostics enter the explanation. */
export function modelBackoffContext(text: string | undefined, agentId: string, role?: string, seq = 0): BackoffContext {
  const context: BackoffContext = { scope: "unknown", callers: [{ agentId, role, seq }] }
  try {
    const body = JSON.parse(text ?? "")
    if (!body || typeof body !== "object") return context
    if (body.scope === "origin") context.scope = "origin"
    if (body.scope === "schedule_answer" || typeof body.candidate_id === "string") context.scope = "schedule_answer"
    if (context.scope === "origin" && typeof body.cooldown_id === "string" && body.cooldown_id) context.cooldownId = body.cooldown_id
    if (Number.isSafeInteger(body.attempt) && body.attempt > 0) context.attempt = body.attempt
    if (["classification_unavailable", "classification_invalid", "classification_deferred"].includes(body.code)) context.code = body.code
  } catch { /* Older receipts may have no structured context. */ }
  return context
}

export function backoffBlocks(blocks: Block[], ev: SwarmEvent, role?: string): Block[] {
  const context = modelBackoffContext(ev.text, ev.agent_id || "manager", ev.role || role, ev.seq)
  const existing = context.cooldownId ? blocks.find((old) => old.cooldownId === context.cooldownId) : undefined
  if (existing?.backoff) {
    const previous = existing.backoff
    const caller = context.callers[0]
    if (previous.callers.some((old) => old.agentId === caller.agentId)) return blocks
    return blocks.map((old) => old.id === existing.id
      ? { ...old, backoff: { ...previous, callers: [...previous.callers, caller] } } : old)
  }
  const notice = modelBackoffNotice(ev.text)
  if (!notice) return blocks
  const row = { ...block(ev, "notice", notice.text), cooldownId: notice.cooldownId, backoff: context }
  return blocks.some((old) => old.id === row.id) ? blocks : [...blocks, row]
}

export function rewindBackoff(block: Block, fromSeq: number): Block {
  if (!block.backoff) return block
  return { ...block, backoff: { ...block.backoff, callers: block.backoff.callers.filter((caller) => caller.seq < fromSeq) } }
}
