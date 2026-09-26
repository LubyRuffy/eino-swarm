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
    // The coalesce timer can lose the race and deliver this snapshot again
    // after the tool row. A delta is the text so far, not a second sentence,
    // so an exact copy of the bubble already on screen is dropped.
    if (echoesSealed(blocks, ev, kind)) {
      agent.blocks = blocks
      return
    }
    const draft = unfinishedDraft(blocks, ev, kind)
    if (draft !== -1) {
      blocks[draft] = { ...blocks[draft], text: ev.text ?? "", streaming: true }
      agent.blocks = blocks
      return
    }
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
    // tool_call seals the open bubble before this flush arrives. The draft
    // still has no stored seq; the flush belongs in it. A later message that
    // happens to repeat the same words is a new sentence and stays.
    const draft = unfinishedDraft(blocks, ev, kind)
    if (draft === -1) {
      blocks.push({ ...block(ev, kind, ev.text ?? "") })
    } else {
      blocks[draft] = {
        ...blocks[draft],
        text: ev.text ?? "",
        streaming: false,
        seq: ev.seq,
      }
    }
  } else {
    blocks[i] = { ...blocks[i], text: ev.text ?? "", streaming: false, seq: ev.seq }
  }
  agent.blocks = blocks
}

/** Exact copy of a sealed paragraph, with only tool rows after it. */
function echoesSealed(
  blocks: Block[],
  ev: SwarmEvent,
  kind: "reasoning" | "answer",
): boolean {
  const hit = paragraphBeforeTools(blocks, ev, kind)
  return hit !== -1 && blocks[hit].text === (ev.text ?? "")
}

/** A streamed paragraph that a tool call closed before the flush arrived.
 *  It has no stored seq yet, and the incoming text is that paragraph finished. */
function unfinishedDraft(
  blocks: Block[],
  ev: SwarmEvent,
  kind: "reasoning" | "answer",
): number {
  const text = ev.text ?? ""
  const hit = paragraphBeforeTools(blocks, ev, kind)
  if (hit === -1) return -1
  const prev = blocks[hit].text
  if (blocks[hit].seq !== 0 || !prev || !text.startsWith(prev)) return -1
  return hit
}

function paragraphBeforeTools(
  blocks: Block[],
  ev: SwarmEvent,
  kind: "reasoning" | "answer",
): number {
  for (let i = blocks.length - 1; i >= 0; i--) {
    const b = blocks[i]
    if (b.kind === "tool" || b.kind === "spawn" || b.kind === "question") continue
    if (b.kind === kind && !b.streaming && b.turnId === ev.turn_id) return i
    return -1
  }
  return -1
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
