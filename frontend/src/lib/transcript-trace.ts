import type { Block } from "./transcript"
import { block } from "./transcript-blocks"
import type { SwarmEvent } from "./types"

/** Engine receipts kept for Trace. History rows carry raw tool arguments;
 *  mcp_catalog and account_profile carry diagnostic JSON. None of them is a chat row. */
const ENGINE_RECEIPT_KINDS = new Set([
  "history_read_started",
  "history_report_requested",
  "history_registry_inherited",
  "mcp_catalog",
  "account_profile",
])

/** Rows that live on the manager as quiet blocks so the Trace tab's Full log
 *  lists them, without a chat row or an invented worker in the roster.
 *  A title is sidebar metadata; a session briefing is read by compact and
 *  the reviewer, not by the person in the chat. */
export function traceOnlyBlock(ev: SwarmEvent): Block | undefined {
  if (ev.kind === "title" || ev.kind === "session_memory") {
    return { ...block(ev, ev.kind, ev.text || ev.err || ""), quiet: true }
  }
  if (ev.kind === "composer_prediction") {
    return { ...block(ev, "trace", ev.text || ev.err || ""), quiet: true, traceKind: "composer_prediction" }
  }
  if (!ENGINE_RECEIPT_KINDS.has(ev.kind)) return undefined
  return { ...block(ev, "trace", ev.text || ""), quiet: true, traceKind: ev.kind }
}
