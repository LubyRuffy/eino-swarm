import { lastLine } from "./carriage"
import type { MessageKey, Vars } from "./i18n"
import { execCommand, summariseToolCall, toolRowSummary, viewTool } from "./tool-view"
import type { Block } from "./transcript"
import type { TranscriptModePref } from "./appearance"

const CHROME_TOOLS = new Set(["spawn_agent", "close_agent"])

export type TurnItem =
  | { type: "block"; block: Block }
  | { type: "work"; blocks: Block[] }

export type WorkTickerKind =
  | "thinking"
  | "planning"
  | "editing"
  | "reading"
  | "exec"
  | "using"

export type WorkTickerFrame = {
  id: string
  kind: WorkTickerKind
  detail: string
  pending: boolean
  failed: boolean
}

export type WorkFoldStats = {
  thoughts: number
  tools: number
  failed: boolean
  live: boolean
}

/** Bookkeeping rows that BlockView already drops. Folding them would open
 *  an empty group for a spawn_agent the chat never showed. */
export function isOmittedBlock(block: Block): boolean {
  if (block.kind === "title" || block.kind === "session_memory") return true
  return block.kind === "tool" && CHROME_TOOLS.has(block.tool?.name ?? "")
}

export function isFoldableBlock(block: Block): boolean {
  if (isOmittedBlock(block)) return false
  return block.kind === "reasoning" || block.kind === "tool" || block.kind === "spawn"
}

function isAlwaysVisible(block: Block): boolean {
  return (
    block.kind === "user" ||
    block.kind === "steer" ||
    block.kind === "question" ||
    block.kind === "confirm" ||
    block.kind === "error" ||
    block.kind === "notice" ||
    block.kind === "answer"
  )
}

/** User mode folds consecutive thinking, tools, and spawns. Answers stay
 *  visible — they split the group so 正文 is never behind the ticker. */
export function foldTurnItems(
  blocks: Block[],
  mode: TranscriptModePref,
): TurnItem[] {
  const visible = blocks.filter((b) => !isOmittedBlock(b))
  if (mode !== "user") {
    return visible.map((block) => ({ type: "block", block }))
  }

  const items: TurnItem[] = []
  let group: Block[] = []
  const flush = () => {
    if (group.length === 0) return
    items.push({ type: "work", blocks: group })
    group = []
  }

  visible.forEach((block) => {
    if (isAlwaysVisible(block)) {
      flush()
      items.push({ type: "block", block })
      return
    }
    if (isFoldableBlock(block)) {
      group.push(block)
      return
    }
    flush()
    items.push({ type: "block", block })
  })
  flush()
  return items
}

export function workFoldStats(blocks: Block[]): WorkFoldStats {
  let thoughts = 0
  let tools = 0
  let failed = false
  let live = false
  for (const b of blocks) {
    if (b.kind === "reasoning") {
      thoughts++
      if (b.streaming) live = true
    }
    if (b.kind === "tool") {
      tools++
      if (b.tool?.pending) live = true
      if (b.tool?.failed) failed = true
    }
    if (b.kind === "spawn" && b.streaming) live = true
  }
  return { thoughts, tools, failed, live }
}

/** Cursor paints `appearance.ts`, not a workspace path. */
export function fileLeaf(path: string): string {
  const s = path.trim().replace(/\\/g, "/")
  if (!s) return ""
  const i = s.lastIndexOf("/")
  return i >= 0 ? s.slice(i + 1) : s
}

function flattenLine(text: string): string {
  return text.replace(/\s+/g, " ").trim()
}

function thinkingFrame(block: Block): WorkTickerFrame {
  return {
    id: `think:${block.id}`,
    kind: "thinking",
    detail: lastLine(block.text),
    pending: Boolean(block.streaming),
    failed: false,
  }
}

function planningFrame(anchor: string): WorkTickerFrame {
  return {
    id: `planning:${anchor}`,
    kind: "planning",
    detail: "",
    pending: true,
    failed: false,
  }
}

function toolFrame(block: Block): WorkTickerFrame | undefined {
  const tool = block.tool
  if (!tool) return undefined
  const pending = Boolean(tool.pending)
  const failed = Boolean(tool.failed)
  const id = `tool:${tool.callId || block.id}`
  if (tool.name === "read") {
    return {
      id,
      kind: "reading",
      detail: fileLeaf(summariseToolCall(tool.name, tool.args)) || tool.name,
      pending,
      failed,
    }
  }
  if (tool.name === "edit" || tool.name === "write") {
    return {
      id,
      kind: "editing",
      detail: fileLeaf(summariseToolCall(tool.name, tool.args)) || tool.name,
      pending,
      failed,
    }
  }
  if (tool.name === "exec" || tool.name === "python_runner") {
    const cmd =
      tool.name === "exec"
        ? execCommand(tool.args)
        : summariseToolCall(tool.name, tool.args)
    return {
      id,
      kind: "exec",
      detail: flattenLine(cmd) || tool.name,
      pending,
      failed,
    }
  }
  const view = viewTool(tool.name, tool.args, tool.result, tool.failed)
  const summary = toolRowSummary(view)
  return {
    id,
    kind: "using",
    detail: summary ? `${tool.name}  ${summary}` : tool.name,
    pending,
    failed,
  }
}

/** One current activity. Execution beats thinking; a quiet gap between
 *  model turns is Planning next moves, not a frozen empty row. Idle turns
 *  keep the finished fold label even if a leftover tool is still pending. */
export function workTickerFrames(
  blocks: Block[],
  running = false,
): WorkTickerFrame[] {
  if (!running) return []
  let pendingTool: Block | undefined
  let streamingThought: Block | undefined
  for (let i = blocks.length - 1; i >= 0; i--) {
    const b = blocks[i]!
    if (!pendingTool && b.kind === "tool" && b.tool?.pending) pendingTool = b
    if (!streamingThought && b.kind === "reasoning" && b.streaming) {
      streamingThought = b
    }
  }
  if (pendingTool) {
    const frame = toolFrame(pendingTool)
    return frame ? [frame] : []
  }
  if (streamingThought) return [thinkingFrame(streamingThought)]
  return [planningFrame(blocks.at(-1)?.id ?? "work")]
}

export function formatWorkTicker(
  frame: WorkTickerFrame,
  t: (key: MessageKey, vars?: Vars) => string,
): string {
  switch (frame.kind) {
    case "planning":
      return t("transcript.planningMoves")
    case "thinking":
      return frame.detail.trim() || t("transcript.thinking")
    case "editing":
      return t("transcript.workEditing", { name: frame.detail })
    case "reading":
      return t("transcript.workReading", { name: frame.detail })
    case "exec":
      return t("transcript.workExec", { name: frame.detail })
    default:
      return frame.detail
  }
}
