import { parseSlashSubmit } from "../../../frontend/src/lib/slash-core"
import type { MockThread } from "./mock-threads"
import type { Step } from "./mock-turn"
import { PROTOCOL_V, type RemoteRequest, type RemoteResponse } from "./rpc"

export function mockEditInput(
  thread: MockThread | undefined,
  req: Pick<RemoteRequest, "from_event_seq" | "replacement">,
  id: string,
  host: { push: (thread: MockThread, step: Step) => void; run: (thread: MockThread, text: string, imageIDs?: string[]) => void },
): RemoteResponse {
  const base = { v: PROTOCOL_V, id }
  if (new URLSearchParams(location.search).get("edit-input") === "legacy") {
    return { ...base, ok: false, code: "unknown_op" }
  }
  const event = thread?.events.find((row) => row.seq === req.from_event_seq && row.kind === "user_message")
  if (!thread || !event) return { ...base, ok: false, code: "not_found" }
  if (req.replacement === undefined) return { ...base, ok: true, event }
  if (parseSlashSubmit(req.replacement)) return { ...base, ok: false, code: "edit_input_command" }
  thread.events = thread.events.filter((row) => row.seq < event.seq)
  thread.followups = []
  thread.askUser = false
  host.push(thread, { kind: "rewound", text: String(event.seq) })
  host.run(thread, req.replacement, event.image_ids)
  return { ...base, ok: true }
}
