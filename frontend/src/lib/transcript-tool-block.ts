import { ASK_TOOL } from "./transcript-ask"
import type { AgentState, Block, TranscriptState } from "./transcript"
import type { SwarmEvent } from "./types"

/** A live tool snapshot for a call whose stored result already arrived, from
 *  a worker log that outran SSE. It must not reopen the call or the worker. */
export function supersededToolDelta(agent: AgentState, ev: SwarmEvent): boolean {
  if ((ev.kind !== "tool_call_delta" && ev.kind !== "tool_delta") || !ev.tool_call_id) return false
  return agent.blocks.some((b) => b.tool?.callId === ev.tool_call_id && b.tool?.pending === false)
}

/** The in-flight tool or question this call id belongs to. The returned
 *  block is a copy already written back onto the agent, so the caller can
 *  mutate it. */
export function findToolBlock(agent: AgentState, callId?: string, turnId?: string): Block | undefined {
  const blocks = agent.blocks
  for (let i = blocks.length - 1; i >= 0; i--) {
    const b = blocks[i]
    if (turnId && b.turnId !== turnId) continue
    if (b.kind === "question" && b.question) {
      if (callId && b.question.callId === callId) {
        const copy = { ...b, question: { ...b.question } }
        agent.blocks = blocks.map((x, j) => (j === i ? copy : x))
        return copy
      }
      if (!callId && b.question.pending) {
        const copy = { ...b, question: { ...b.question } }
        agent.blocks = blocks.map((x, j) => (j === i ? copy : x))
        return copy
      }
    }
    if (b.kind !== "tool" || !b.tool) continue
    if (callId && b.tool.callId === callId) {
      const copy = { ...b, tool: { ...b.tool } }
      agent.blocks = blocks.map((x, j) => (j === i ? copy : x))
      return copy
    }
    if (!callId && b.tool.pending) {
      const copy = { ...b, tool: { ...b.tool } }
      agent.blocks = blocks.map((x, j) => (j === i ? copy : x))
      return copy
    }
  }
  return undefined
}

/** The server sends `name({json})`; the UI shows the verb and hides the
 *  arguments until asked. */
export function splitToolCall(raw: string): { name: string; args: string } {
  const open = raw.indexOf("(")
  if (open === -1) return { name: raw.trim(), args: "" }
  const name = raw.slice(0, open).trim()
  let args = raw.slice(open + 1)
  if (args.endsWith(")")) args = args.slice(0, -1)
  return { name: name || "tool", args }
}

export function summarise(text: string, max = 80): string {
  const line = text.trim().replace(/\s+/g, " ")
  return line.length > max ? `${line.slice(0, max - 1)}…` : line
}

/** The tool a result belongs to, looked up by call id after the reducer has
 *  paired them. Used to refresh the Memory panel the moment a write lands,
 *  rather than waiting for the review that may never come (memory off). */
export function toolNameOf(state: TranscriptState, callId?: string): string | undefined {
  if (!callId) return undefined
  for (const id of state.agentOrder) {
    for (const b of state.agents[id]?.blocks ?? []) {
      if (b.kind === "question" && b.question?.callId === callId) return ASK_TOOL
      if (b.kind === "tool" && b.tool?.callId === callId) return b.tool.name
    }
  }
}
