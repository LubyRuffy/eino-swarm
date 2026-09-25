import type { AgentState, Block } from "./transcript"

/** The in-flight tool or question this call id belongs to. The returned
 *  block is a copy already written back onto the agent, so the caller can
 *  mutate it. */
export function findToolBlock(agent: AgentState, callId?: string): Block | undefined {
  const blocks = agent.blocks
  for (let i = blocks.length - 1; i >= 0; i--) {
    const b = blocks[i]
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
