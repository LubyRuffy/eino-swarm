// state.ts — swarm state + notification reducer (mirror of the Go TUI logic).
export interface Block {
  kind: "thinking" | "answer" | "tool"
  text: string
  name?: string
  args?: string
  res?: string
  open: boolean
  live?: boolean
  startedAt?: number
  elapsedMs?: number
}

export interface AgentState {
  id: string
  role: string
  blocks: Block[]
  curThink: Block | null
  curAnswer: Block | null
  curTool: Block | null
  finished: boolean
  finErr: Error | null
  activityTail: string
}

export interface Notification {
  kind: string
  agent_id: string
  role?: string
  text: string
  err?: string
  time?: string
}

export interface SwarmState {
  manager: AgentState
  agents: AgentState[]
}

export function newSwarmState(): SwarmState {
  return { manager: newAgent("manager", "manager"), agents: [] }
}

function newAgent(id: string, role: string): AgentState {
  return { id, role, blocks: [], curThink: null, curAnswer: null, curTool: null, finished: false, finErr: null, activityTail: "" }
}

export function toolNameOf(s: string): string {
  const i = s.indexOf("(")
  return i > 0 ? s.slice(0, i) : s
}
export function argsOf(s: string): string {
  const i = s.indexOf("(")
  return i >= 0 ? s.slice(i + 1).replace(/\)$/, "") : ""
}
export function lastLine(s: string): string {
  const i = s.lastIndexOf("\n")
  return i >= 0 ? s.slice(i + 1) : s
}
export function firstWords(s: string, n: number): string {
  const f = s.split(/\s+/)
  return f.length <= n ? s : f.slice(0, n).join(" ") + "…"
}
export function firstLine(s: string): string {
  const i = s.indexOf("\n")
  return i >= 0 ? s.slice(0, i) : s
}
export function truncate(s: string, n: number): string {
  return s.length <= n ? s : s.slice(0, n) + "…"
}

function pushThinking(a: AgentState): Block {
  const blk: Block = { kind: "thinking", text: "", open: true, live: true, startedAt: Date.now() }
  a.blocks.push(blk)
  a.curThink = blk
  return blk
}
function pushAnswer(a: AgentState): Block {
  const blk: Block = { kind: "answer", text: "", open: true }
  a.blocks.push(blk)
  a.curAnswer = blk
  return blk
}
function closeThinking(a: AgentState) {
  if (a.curThink) {
    a.curThink.open = false
    a.curThink.live = false
    a.curThink.elapsedMs = a.curThink.startedAt ? Date.now() - a.curThink.startedAt : 0
    a.curThink = null
  }
}

// apply mutates the swarm state from one notification.
export function apply(s: SwarmState, n: Notification) {
  if (n.kind === "spawned") {
    s.agents.push(newAgent(n.agent_id, n.role || n.agent_id))
    return
  }
  const a = n.agent_id === "manager" ? s.manager : s.agents.find(x => x.id === n.agent_id)
  if (!a) return
  switch (n.kind) {
    case "reasoning_delta": {
      const blk = a.curThink ?? pushThinking(a)
      blk.text = n.text
      a.activityTail = truncate(lastLine(n.text), 90)
      break
    }
    case "delta": {
      closeThinking(a)
      const blk = a.curAnswer ?? pushAnswer(a)
      blk.text = n.text
      a.activityTail = truncate(lastLine(n.text), 90)
      break
    }
    case "agent_message": {
      closeThinking(a)
      const blk = a.curAnswer ?? pushAnswer(a)
      blk.text = n.text
      blk.open = false
      a.curAnswer = null
      break
    }
    case "tool_call": {
      closeThinking(a)
      // seal the current answer block: the next streamed/complete message
      // must open a NEW block BELOW the tool call (chronological order).
      a.curAnswer = null
      const blk: Block = { kind: "tool", text: "", name: toolNameOf(n.text), args: argsOf(n.text), res: "", open: true }
      a.blocks.push(blk)
      a.curTool = blk
      a.activityTail = truncate(n.text, 90)
      break
    }
    case "tool_result":
      if (a.curTool) {
        a.curTool.res = n.text
        a.curTool.open = false
        a.curTool = null
        a.activityTail = truncate("← " + n.text, 90)
      }
      break
    case "finished":
      closeThinking(a)
      a.finished = true
      a.finErr = n.err ? new Error(n.err) : null
      for (const b of a.blocks) if (b.kind === "tool") b.open = false
      a.curTool = null
      break
    case "error":
      closeThinking(a)
      if (a.curTool) { a.curTool.res = "error: " + (n.err || n.text); a.curTool.open = false; a.curTool = null }
      a.finished = true
      a.finErr = n.err ? new Error(n.err) : null
      break
  }
}
