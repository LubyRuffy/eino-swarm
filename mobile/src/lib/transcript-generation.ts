import type { CompactBlock } from "./transcript"
import type { RemoteEvent } from "./rpc"

export type GenerationInterruption = {
  reason: "engine_restart" | "process_exit" | "output_limit" | "model_error" | "timeout" | "cancelled" | "unknown"
  detail: string
  recovery: "pending" | "recovered" | "none"
  seq: number
  inferred?: boolean
}

export function generationEvent(blocks: CompactBlock[], ev: RemoteEvent): CompactBlock[] {
  let body: Record<string, unknown> = {}
  try { body = JSON.parse(ev.text) ?? {} } catch { /* Old records have no cause. */ }
  let generation: GenerationInterruption = {
    reason: ["engine_restart", "process_exit", "output_limit", "model_error", "timeout", "cancelled"].includes(String(body.reason)) ? body.reason as GenerationInterruption["reason"] : "unknown",
    detail: typeof body.detail === "string" ? body.detail : "",
    recovery: ev.kind === "generation_recovered" ? "recovered" : body.recovery === "pending" ? "pending" : "none",
    seq: ev.seq,
  }
  const old = blocks.find((b) => b.generation && (ev.message_id ? b.messageId === ev.message_id : !b.messageId && b.generation.seq === ev.seq))
  if (old?.generation?.inferred) generation = { ...generation, seq: Math.max(old.generation.seq, ev.seq), recovery: old.generation.recovery }
  if (old?.generation && !old.generation.inferred && (old.generation.seq > ev.seq || old.generation.recovery === "recovered" && generation.recovery !== "recovered")) return blocks
  const next = blocks.map((b) => {
    if (b === old) return { ...b, generation }
    if ((b.kind === "reasoning" || b.kind === "answer") && (ev.message_id ? b.messageId === ev.message_id : b.streaming)) return { ...b, streaming: false }
    if (ev.message_id && b.messageId === ev.message_id && b.pending && b.writing) return { ...b, pending: false, writing: false }
    return b
  })
  if (!old) next.push({ id: `generation:${ev.message_id || ev.seq}`, kind: "notice", messageId: ev.message_id, text: "", generation })
  return next
}

export function recoverLegacyGeneration(blocks: CompactBlock[], ev: RemoteEvent): CompactBlock[] {
  const tail = blocks.at(-1)
  if (!tail || tail.generation || !tail.streaming || !(tail.kind === "reasoning" || tail.kind === "answer")) return blocks
  return blocks.map((b) => b.streaming ? { ...b, streaming: false } : b).concat({
    id: tail.messageId ? `generation:${tail.messageId}` : `${tail.id}:generation`, kind: "notice", text: "", agentId: tail.agentId, messageId: tail.messageId,
    generation: { reason: "unknown", detail: "", recovery: "recovered", seq: ev.seq, inferred: true },
  })
}

/** Cumulative snapshots share an output id, including across manager checks.
 *  An interruption seals that id even when an already queued delta arrives. */
export function identifiedOutput(blocks: CompactBlock[], ev: RemoteEvent): CompactBlock[] | undefined {
  if (ev.kind === "tool_call_delta" && ev.message_id && blocks.some((b) => b.generation && b.messageId === ev.message_id)) return blocks
  if (!ev.message_id || !["reasoning", "reasoning_delta", "delta", "agent_message"].includes(ev.kind)) return
  const kind = ev.kind.startsWith("reasoning") ? "reasoning" : "answer"
  const interrupted = blocks.findIndex((b) => b.generation && b.messageId === ev.message_id)
  const index = blocks.findIndex((b) => b.kind === kind && b.messageId === ev.message_id)
  const streaming = interrupted === -1 && (ev.kind === "reasoning_delta" || ev.kind === "delta")
  const next = blocks.slice()
  if (index !== -1) {
    const old = next[index]
    if (ev.seq > 0 && (old.sourceSeq ?? 0) > ev.seq || ev.text.length < old.text.length) return next
    if (streaming && next.slice(index + 1).some((b) => b.kind === kind && b.messageId !== ev.message_id && b.messageId)) return next
    next[index] = { ...old, text: ev.text, streaming: streaming && (old.streaming || ev.text !== old.text), sourceSeq: ev.seq || old.sourceSeq }
  } else {
    for (let i = 0; i < next.length; i++) if (interrupted === -1 && (next[i].kind === kind && next[i].streaming || kind === "answer" && next[i].kind === "reasoning" && next[i].streaming)) next[i] = { ...next[i], streaming: false }
    const row: CompactBlock = { id: `${kind}:${ev.message_id}`, kind, text: ev.text, messageId: ev.message_id, sourceSeq: ev.seq, streaming }
    if (interrupted === -1) next.push(row)
    else next.splice(interrupted, 0, row)
  }
  return next
}
