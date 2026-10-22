import { t, type Locale } from "./i18n"
import type { AgentState } from "./transcript"
import type { SwarmEvent } from "./types"
import { block, replaceComplete, replaceStreaming } from "./transcript-blocks"

export type GenerationInterruption = {
  reason: "engine_restart" | "process_exit" | "output_limit" | "model_error" | "timeout" | "cancelled" | "unknown"
  detail: string
  recovery: "pending" | "recovered" | "none"
  seq: number
  inferred?: boolean
}

export function parseGeneration(ev: SwarmEvent): GenerationInterruption {
  let body: Record<string, unknown> = {}
  try { body = JSON.parse(ev.text ?? "") ?? {} } catch { /* Older records may have no envelope. */ }
  const reason = body.reason
  return {
    reason: ["engine_restart", "process_exit", "output_limit", "model_error", "timeout", "cancelled"].includes(String(reason)) ? reason as GenerationInterruption["reason"] : "unknown",
    detail: typeof body.detail === "string" ? body.detail : "",
    recovery: ev.kind === "generation_recovered" ? "recovered" : body.recovery === "pending" ? "pending" : "none",
    seq: ev.seq,
  }
}

/** A generation receipt describes one output; it never finishes its worker. */
export function applyGenerationEvent(agent: AgentState, ev: SwarmEvent) {
  let generation = parseGeneration(ev)
  const id = `generation:${ev.message_id || `${ev.turn_id}:${ev.seq}`}`
  const index = agent.blocks.findIndex((b) => b.id === id)
  const old = agent.blocks[index]
  if (old?.generation?.inferred) generation = { ...generation, seq: Math.max(old.generation.seq, ev.seq), recovery: old.generation.recovery }
  if (old?.generation && !old.generation.inferred && (old.generation.seq > ev.seq || old.generation.recovery === "recovered" && generation.recovery !== "recovered")) return
  agent.blocks = agent.blocks.map((b) =>
    (b.kind === "reasoning" || b.kind === "answer") &&
      (ev.message_id ? b.messageId === ev.message_id : b.streaming)
      ? { ...b, streaming: false }
      : ev.message_id && b.messageId === ev.message_id && b.tool?.pending && b.tool.writing !== undefined
        ? { ...b, tool: { ...b.tool, pending: false, writing: undefined } } : b)
  const notice = { ...block(ev, "notice", ""), id, generation }
  if (old) agent.blocks[index] = { ...old, generation }
  else agent.blocks.push(notice)
  if (generation.recovery === "pending" && agent.status === "running") agent.activity = ""
}

/** Older transcripts may have output identity without lifecycle receipts.
 *  An observed recovery boundary proves the streaming tail was abandoned. */
export function recoverLegacyGeneration(agent: AgentState, ev: SwarmEvent) {
  const tail = agent.blocks.at(-1)
  if (!tail || tail.generation ||
    !tail.streaming || !(tail.kind === "reasoning" || tail.kind === "answer") || agent.status !== "running") return
  agent.blocks = agent.blocks.map((b) => b.streaming ? { ...b, streaming: false } : b)
  agent.blocks.push({ ...block({ ...ev, agent_id: agent.id, role: agent.role }, "notice", ""),
    id: tail.messageId ? `generation:${tail.messageId}` : `${tail.id}:generation`, messageId: tail.messageId, turnId: tail.turnId, generation: { reason: "unknown", detail: "", recovery: "recovered", seq: ev.seq, inferred: true } })
}

export function generationNotice(generation: GenerationInterruption, locale: Locale): string {
  return `${t(locale, "generation.interrupted", { reason: t(locale, `generation.reason.${generation.reason}`) })} ${t(locale, `generation.recovery.${generation.recovery}`)}`
}

/** Buffered output may extend its sealed text, but cannot revive its worker
 *  or close the reasoning of the replacement generation. */
export function applyInterruptedOutput(agent: AgentState, ev: SwarmEvent): boolean {
  if (!ev.message_id || !agent.blocks.some((b) => b.generation && b.messageId === ev.message_id)) return false
  if (ev.kind === "tool_call_delta") return true
  if (!["reasoning", "reasoning_delta", "delta", "agent_message"].includes(ev.kind)) return false
  const kind = ev.kind.startsWith("reasoning") ? "reasoning" : "answer"
  if (ev.kind === "reasoning_delta" || ev.kind === "delta") replaceStreaming(agent, ev, kind)
  else replaceComplete(agent, ev, kind)
  return true
}
