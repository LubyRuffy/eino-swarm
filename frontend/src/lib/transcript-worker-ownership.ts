import type { AgentState, TranscriptState } from "./transcript"
import type { SwarmEvent } from "./types"
import { closeStreaming, MANAGER_ID } from "./transcript-blocks"

export interface WorkerRetention {
  seq: number
  workerIds: string[]
}

/** A terminal error alone cannot prove that a worker survived. Only the
 *  engine's durable, turn-attributed ownership receipt grants that exception. */
export function parseWorkerRetention(ev: SwarmEvent): WorkerRetention | undefined {
  if (!ev.turn_id || !(ev.seq > 0) || (ev.agent_id && ev.agent_id !== MANAGER_ID)) return
  try {
    const body = JSON.parse(ev.text ?? "")
    if (!body || body.reason !== "scheduled_check_failed" ||
      !["goal", "thread_wake"].includes(body.owner_kind) ||
      typeof body.owner_id !== "string" || !body.owner_id.trim() ||
      !Array.isArray(body.worker_ids) ||
      !body.worker_ids.every((id: unknown) => typeof id === "string" && id.trim() === id && id !== "" && id !== MANAGER_ID)) return
    return { seq: ev.seq, workerIds: [...new Set<string>(body.worker_ids)] }
  } catch {
    return undefined
  }
}

export function settleFailedWorkers(state: TranscriptState, ev: SwarmEvent) {
  const cancelled = ev.err === "interrupted"
  const retained = cancelled ? undefined : state.turns.find((turn) => turn.id === ev.turn_id)?.workerRetention
  for (const id of state.agentOrder) {
    if (id === MANAGER_ID) continue
    const previous = state.agents[id]
    if (!previous || (previous.status === "running" && retained && retained.seq < ev.seq && retained.workerIds.includes(id))) continue
    const child = { ...previous, blocks: previous.blocks.slice() }
    state.agents[id] = child
    closeStreaming(child, "reasoning")
    closeStreaming(child, "answer")
    closePendingTools(child, ev.err)
    if (child.status !== "running") continue
    child.status = cancelled ? "cancelled" : "failed"
    child.activity = "stopped"
    child.endedAt = ev.created_at
    if (!cancelled && ev.err) {
      child.error = ev.err
      child.errorSource = "turn"
    }
  }
}

/** Unfinished calls close only when their owning turn or worker stopped;
 *  retained workers keep both their live output and pending tools open. */
export function closePendingTools(agent: AgentState, reason?: string, keepQuestions?: boolean) {
  let changed = false
  const blocks = agent.blocks.map((b) => {
    if (b.kind === "question" && b.question?.pending) {
      if (keepQuestions) return b
      changed = true
      return { ...b, question: { ...b.question, pending: false } }
    }
    if (b.kind !== "tool" || !b.tool?.pending) return b
    changed = true
    return {
      ...b,
      tool: { ...b.tool, pending: false, failed: true, result: b.tool.result ?? reason ?? "" },
    }
  })
  if (changed) agent.blocks = blocks
}
