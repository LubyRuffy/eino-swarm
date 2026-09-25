import type { AgentState, Block, BlockKind } from "./transcript"
import type { SwarmEvent } from "./types"

export const MANAGER_ID = "manager"

export function block(ev: SwarmEvent, kind: BlockKind, text: string): Block {
  return {
    id: `${ev.turn_id}:${ev.agent_id || MANAGER_ID}:${kind}:${ev.seq || `d${ev.created_at}`}:${text.length}`,
    kind,
    agentId: ev.agent_id || MANAGER_ID,
    role: ev.role,
    text,
    turnId: ev.turn_id,
    seq: ev.seq,
    at: ev.created_at,
    images: ev.images,
  }
}

export function append(agent: AgentState, b: Block) {
  agent.blocks = [...agent.blocks, b]
}

/** Replace the open streaming block of this kind, or open one. */
export function replaceStreaming(
  agent: AgentState,
  ev: SwarmEvent,
  kind: "reasoning" | "answer",
) {
  const blocks = agent.blocks.slice()
  const i = lastOpenIndex(blocks, kind)
  if (i === -1) {
    blocks.push({ ...block(ev, kind, ev.text ?? ""), streaming: true, id: openId(ev, kind) })
  } else {
    blocks[i] = { ...blocks[i], text: ev.text ?? "", streaming: true }
  }
  agent.blocks = blocks
}

/** Replace the open streaming block with the server's complete text, or add
 *  it if nothing was streamed (a non-streaming model, or a replay). */
export function replaceComplete(
  agent: AgentState,
  ev: SwarmEvent,
  kind: "reasoning" | "answer",
) {
  const blocks = agent.blocks.slice()
  const i = lastOpenIndex(blocks, kind)
  if (i === -1) {
    blocks.push({ ...block(ev, kind, ev.text ?? "") })
  } else {
    blocks[i] = { ...blocks[i], text: ev.text ?? "", streaming: false, seq: ev.seq }
  }
  agent.blocks = blocks
}

export function closeStreaming(agent: AgentState, kind: "reasoning" | "answer") {
  const i = lastOpenIndex(agent.blocks, kind)
  if (i === -1) return
  const blocks = agent.blocks.slice()
  blocks[i] = { ...blocks[i], streaming: false }
  agent.blocks = blocks
}

function lastOpenIndex(blocks: Block[], kind: BlockKind): number {
  for (let i = blocks.length - 1; i >= 0; i--) {
    if (blocks[i].kind === kind && blocks[i].streaming) return i
    // Argument previews sit in front of the still-open commentary. They are
    // not a finished call, so they must not hide that answer from the flush.
    if (
      kind === "answer" &&
      blocks[i].kind === "tool" &&
      blocks[i].tool?.pending &&
      blocks[i].tool?.writing !== undefined &&
      blocks[i].tool?.result === undefined
    ) {
      continue
    }
    // A block of another kind closes the run: thinking then a tool call then
    // more thinking is two separate thinking blocks, not one.
    if (blocks[i].kind === "tool" || blocks[i].kind === "spawn" || blocks[i].kind === "question") return -1
  }
  return -1
}

function openId(ev: SwarmEvent, kind: string): string {
  return `${ev.turn_id}:${ev.agent_id || MANAGER_ID}:${kind}:open:${ev.created_at}`
}
