import {
  ASK_TOOL,
  parseAskToolArgs,
  splitToolCall,
  type AskQuestion,
} from "./ask"
import type { RemoteEvent } from "./rpc"
import { looksPacked } from "./tool-preview"

export type BlockKind =
  | "user"
  | "steer"
  | "answer"
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
    case "delta": {
      const i = lastIndex(next, (b) => b.kind === "answer" && Boolean(b.streaming))
      if (i >= 0) {
        next[i] = { ...next[i], text: ev.text }
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
      const i = lastIndex(next, (b) => b.kind === "answer" && Boolean(b.streaming))
      if (i >= 0) {
        next[i] = {
          ...next[i],
          text: ev.text || next[i].text,
          streaming: false,
        }
        return next
      }
      if (ev.text) {
        next.push({ id: blockId(ev), kind: "answer", text: ev.text })
      }
      return next
    }
    case "tool_call": {
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
    case "progress":
    case "max_iterations":
    case "max_iterations_continued":
    case "model_retry":
    case "compacted":
      if (ev.text) {
        next.push({ id: blockId(ev), kind: "notice", text: ev.text })
      }
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
