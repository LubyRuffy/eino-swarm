import type { Block } from "./transcript"
import { block } from "./transcript-blocks"
import type { SwarmEvent } from "./types"

/** Engine admission receipts for original-history reads and report mode.
 *  Their text is raw tool arguments: evidence for Trace, never a chat row. */
const ENGINE_RECEIPT_KINDS = new Set([
  "history_read_started",
  "history_report_requested",
  "history_registry_inherited",
])

/** Rows that live on the manager as quiet blocks so the Trace tab's Full log
 *  lists them, without a chat row or an invented worker in the roster.
 *  A title is sidebar metadata; a session briefing is read by compact and
 *  the reviewer, not by the person in the chat. */
export function traceOnlyBlock(ev: SwarmEvent): Block | undefined {
  if (ev.kind === "title" || ev.kind === "session_memory") {
    return { ...block(ev, ev.kind, ev.text || ev.err || ""), quiet: true }
  }
  if (!ENGINE_RECEIPT_KINDS.has(ev.kind)) return undefined
  return { ...block(ev, "trace", ev.text || ""), quiet: true, traceKind: ev.kind }
}
