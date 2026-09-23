import {
  ASK_TOOL,
  parseAskToolArgs,
  splitToolCall,
  type AskQuestion,
} from "./ask"
import type { RemoteEvent } from "./rpc"
import { t } from "./i18n"
import { flatten, jsonPreview, looksPacked } from "./tool-preview"
import { scheduleToolNotice } from "./inbox-preview"

export type BlockKind =
  | "user"
  | "steer"
  | "answer"
  | "reasoning"
  | "tool"
  | "question"
  | "notice"
  | "spawn"
  | "error"

export type CompactBlock = {
  id: string
  kind: BlockKind
  text: string
  streaming?: boolean
  pending?: boolean
  failed?: boolean
  toolName?: string
  callId?: string
  args?: string
  hasImages?: boolean
  questions?: AskQuestion[]
}

export function applyEvent(blocks: CompactBlock[], ev: RemoteEvent): CompactBlock[] {
  const next = blocks.slice()
  switch (ev.kind) {
    case "user_message":
      if (ev.text.startsWith("This turn is a scheduled check.")) return next
      next.push({
        id: blockId(ev),
        kind: "user",
        text: ev.text,
        hasImages: ev.has_images,
      })
      return next
    case "steer":
      next.push({ id: blockId(ev), kind: "steer", text: ev.text })
      return next
    case "reasoning_delta": {
      const i = lastIndex(next, (b) => b.kind === "reasoning" && Boolean(b.streaming))
      if (i >= 0) {
        next[i] = { ...next[i], text: ev.text, streaming: true }
        return next
      }
      next.push({
        id: blockId(ev),
        kind: "reasoning",
        text: ev.text,
        streaming: true,
      })
      return next
    }
    case "reasoning": {
      const i = lastIndex(next, (b) => b.kind === "reasoning" && Boolean(b.streaming))
      if (i >= 0) {
        next[i] = {
          ...next[i],
          text: ev.text || next[i].text,
          streaming: false,
        }
        return next
      }
      if (ev.text) {
        next.push({ id: blockId(ev), kind: "reasoning", text: ev.text })
      }
      return next
    }
    case "delta": {
      settleReasoning(next)
      const i = lastIndex(next, (b) => b.kind === "answer" && Boolean(b.streaming))
      if (i >= 0) {
        next[i] = { ...next[i], text: ev.text }
        return next
      }
      // A late snapshot arrives after the answer was closed. It carries the
      // full text so far; pushing it stacks the same sentence again.
      const tail = tailAnswer(next)
      if (tail >= 0 && answerContinues(next[tail].text, ev.text)) {
        next[tail] = { ...next[tail], text: ev.text, streaming: true }
        return next
      }
      next.push({
        id: blockId(ev),
        kind: "answer",
        text: ev.text,
        streaming: true,
      })
      return next
    }
    case "agent_message": {
      settleReasoning(next)
      const i = lastIndex(next, (b) => b.kind === "answer" && Boolean(b.streaming))
      if (i >= 0) {
        next[i] = {
          ...next[i],
          text: ev.text || next[i].text,
          streaming: false,
        }
        return next
      }
      const tail = tailAnswer(next)
      if (tail >= 0 && ev.text && next[tail].text === ev.text) return next
      if (ev.text) {
        next.push({ id: blockId(ev), kind: "answer", text: ev.text })
      }
      return next
    }
    case "tool_call": {
      settleReasoning(next)
      const parsed = splitToolCall(ev.text)
      if (parsed.name === ASK_TOOL) {
        next.push({
          id: blockId(ev),
          kind: "question",
          text: "",
          pending: true,
          callId: ev.tool_call_id,
          questions: parseAskToolArgs(parsed.args) ?? [],
        })
        return next
      }
      const notice = scheduleToolNotice(parsed.name, parsed.args)
      if (notice !== undefined) {
        if (notice) {
          next.push({ id: blockId(ev), kind: "notice", text: notice })
        }
        return next
      }
      next.push({
        id: blockId(ev),
        kind: "tool",
        text: parsed.name || ev.text,
        toolName: parsed.name,
        args: parsed.args,
        callId: ev.tool_call_id,
        pending: true,
      })
      return next
    }
    case "tool_result": {
      const i = lastIndex(
        next,
        (b) => Boolean(b.callId) && b.callId === ev.tool_call_id,
      )
      if (i >= 0) {
        next[i] = {
          ...next[i],
          pending: false,
          failed: Boolean(ev.err),
          text:
            next[i].kind === "question"
              ? next[i].text
              : ev.text || next[i].text,
        }
      }
      return next
    }
    case "tool_delta": {
      const i = lastIndex(
        next,
        (b) => b.kind === "tool" && (!ev.tool_call_id || b.callId === ev.tool_call_id),
      )
      if (i >= 0 && ev.text) {
        next[i] = { ...next[i], text: ev.text }
      }
      return next
    }
    case "spawned":
      next.push({
        id: blockId(ev),
        kind: "spawn",
        text: ev.role || ev.agent_id || "",
      })
      return next
    case "error":
      next.push({ id: blockId(ev), kind: "error", text: ev.err || ev.text })
      return next
    case "done":
      return next.map((b) => (b.streaming ? { ...b, streaming: false } : b))
    case "goal":
    case "goal_complete":
    case "goal_capped":
    case "goal_idle":
    case "goal_blocked":
    case "goal_edited":
    case "goal_resumed":
    case "goal_continued":
    case "plan":
    case "plan_updated":
    case "plan_implemented":
    case "plan_cancelled":
    case "max_iterations":
    case "max_iterations_continued":
    case "model_retry":
    case "compacted":
      // Packed JSON here is a pulse/cap/retry envelope. Desktop never
      // paints it into the chat; a notice would be the elapsed_ms wall.
      if (ev.text && !looksPacked(ev.text)) {
        next.push({ id: blockId(ev), kind: "notice", text: ev.text })
      }
      return next
    case "progress":
      // Live chrome on desktop, not a transcript fact. Pulses are not
      // stored; stacking each one on the phone was a new JSON dump every
      // progress_interval_seconds.
      return next
    case "schedule":
      next.push({ id: blockId(ev), kind: "notice", text: "A wait is armed." })
      return next
    case "schedule_fired": {
      const text = ev.text.trim()
      next.push({
        id: blockId(ev),
        kind: "notice",
        text: !text || looksPacked(text) ? "Scheduled check." : text,
      })
      return next
    }
    case "schedule_skipped":
    case "schedule_report":
      return next
    case "schedule_cancelled":
      next.push({ id: blockId(ev), kind: "notice", text: "A wait was cancelled." })
      return next
    default:
      return next
  }
}

export function pendingAsk(blocks: CompactBlock[]): CompactBlock | undefined {
  return [...blocks].reverse().find((b) => b.kind === "question" && b.pending)
}

function blockId(ev: RemoteEvent): string {
  return `${ev.seq}:${ev.kind}:${ev.tool_call_id ?? ""}`
}

function lastIndex(
  blocks: CompactBlock[],
  pred: (b: CompactBlock) => boolean,
): number {
  for (let i = blocks.length - 1; i >= 0; i--) {
    if (pred(blocks[i])) return i
  }
  return -1
}

function settleReasoning(blocks: CompactBlock[]) {
  const i = lastIndex(blocks, (b) => b.kind === "reasoning" && Boolean(b.streaming))
  if (i >= 0) blocks[i] = { ...blocks[i], streaming: false }
}

const ANSWER_BOUNDARY = new Set<BlockKind>([
  "tool",
  "question",
  "user",
  "steer",
  "spawn",
  "error",
])

/** The answer still on the tail, until a later turn of speech starts. */
function tailAnswer(blocks: CompactBlock[]): number {
  for (let i = blocks.length - 1; i >= 0; i--) {
    const kind = blocks[i].kind
    if (ANSWER_BOUNDARY.has(kind)) return -1
    if (kind === "answer") return i
  }
  return -1
}

/** A delta is the text so far. Equal or longer-with-the-same-prefix continues
 *  the bubble. A shorter different start is a new sentence. */
function answerContinues(prev: string, next: string): boolean {
  if (!prev || !next) return false
  return next === prev || next.startsWith(prev)
}

const OMITTED_TOOLS = new Set(["spawn_agent", "close_agent"])

export type PhoneItem =
  | { type: "block"; block: CompactBlock }
  | { type: "work"; blocks: CompactBlock[] }

export type PhoneTickerKind =
  | "thinking"
  | "planning"
  | "editing"
  | "reading"
  | "exec"
  | "using"

export type PhoneTicker = {
  kind: PhoneTickerKind
  detail: string
}

/** Bookkeeping the phone never paints. Skipping it keeps a thought and the
 *  next real tool in one fold. */
export function isPhoneOmitted(block: CompactBlock): boolean {
  if (block.kind === "spawn") return true
  if (block.kind !== "tool") return false
  const name = block.toolName ?? ""
  if (OMITTED_TOOLS.has(name)) return true
  return scheduleToolNotice(name, block.args || "") === ""
}

function isAlwaysVisible(block: CompactBlock): boolean {
  return (
    block.kind === "user" ||
    block.kind === "steer" ||
    block.kind === "answer" ||
    block.kind === "question" ||
    block.kind === "error" ||
    block.kind === "notice"
  )
}

/** Compact mode (the phone default) folds consecutive thinking and tools.
 *  An answer stays on screen and splits the group. */
export function foldPhoneItems(blocks: CompactBlock[]): PhoneItem[] {
  const items: PhoneItem[] = []
  let group: CompactBlock[] = []
  const flush = () => {
    if (group.length === 0) return
    items.push({ type: "work", blocks: group })
    group = []
  }
  for (const block of blocks) {
    if (isPhoneOmitted(block)) continue
    if (isAlwaysVisible(block)) {
      flush()
      items.push({ type: "block", block })
      continue
    }
    if (block.kind === "reasoning" || block.kind === "tool") {
      group.push(block)
      continue
    }
    flush()
    items.push({ type: "block", block })
  }
  flush()
  return items
}

const PHONE_TAIL_BLOCKER = new Set(["question", "error"])

/** Quiet gap after a closed answer. The ticker belongs under that text.
 *  A streaming answer is the activity; a question or an error is the tail
 *  instead. A pending tool is not this gap. */
export function phonePlanningTail(items: PhoneItem[], running: boolean): boolean {
  if (!running) return false
  let last = -1
  let blocks: CompactBlock[] | undefined
  for (let i = 0; i < items.length; i++) {
    const item = items[i]
    if (item.type !== "work") continue
    last = i
    blocks = item.blocks
  }
  if (!blocks) return false
  let sawAnswer = false
  for (let i = last + 1; i < items.length; i++) {
    const item = items[i]
    if (item.type !== "block") continue
    const kind = item.block.kind
    if (PHONE_TAIL_BLOCKER.has(kind)) return false
    if (kind !== "answer") continue
    if (item.block.streaming) return false
    sawAnswer = true
  }
  if (!sawAnswer) return false
  return phoneWorkTicker(blocks, true)?.kind === "planning"
}

/** One current activity on the live tail. Idle folds keep the count. */
export function phoneWorkTicker(
  blocks: CompactBlock[],
  running: boolean,
): PhoneTicker | null {
  if (!running) return null
  let pending: CompactBlock | undefined
  let thought: CompactBlock | undefined
  for (let i = blocks.length - 1; i >= 0; i--) {
    const b = blocks[i]
    if (!pending && b.kind === "tool" && b.pending) pending = b
    if (!thought && b.kind === "reasoning" && b.streaming) thought = b
  }
  if (pending) return toolTicker(pending)
  if (thought) return { kind: "thinking", detail: lastLine(thought.text) }
  return { kind: "planning", detail: "" }
}

export function formatPhoneTicker(frame: PhoneTicker): string {
  switch (frame.kind) {
    case "planning":
      return t("thread.planningMoves")
    case "thinking":
      return frame.detail.trim() || t("thread.thinking")
    case "editing":
      return t("thread.workEditing", { name: frame.detail })
    case "reading":
      return t("thread.workReading", { name: frame.detail })
    case "exec":
      return t("thread.workExec", { name: frame.detail })
    default:
      return frame.detail
  }
}

function toolTicker(block: CompactBlock): PhoneTicker {
  const name = block.toolName || ""
  const preview = jsonPreview(block.args) || jsonPreview(block.text)
  if (name === "read") {
    return { kind: "reading", detail: fileLeaf(preview) || name }
  }
  if (name === "edit" || name === "write") {
    return { kind: "editing", detail: fileLeaf(preview) || name }
  }
  if (name === "exec" || name === "python_runner") {
    return { kind: "exec", detail: flatten(preview) || name }
  }
  const detail = preview && preview !== name ? `${name}  ${preview}` : name
  return { kind: "using", detail }
}

function fileLeaf(path: string): string {
  const s = path.trim().replace(/\\/g, "/")
  if (!s) return ""
  const i = s.lastIndexOf("/")
  return i >= 0 ? s.slice(i + 1) : s
}

function lastLine(text: string): string {
  const lines = text.split("\n").map((row) => row.trim()).filter(Boolean)
  return lines[lines.length - 1] ?? ""
}
