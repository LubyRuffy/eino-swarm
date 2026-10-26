import type { Step } from "./mock-turn"

/** Different roles retain their Trace receipts but share one chat cooldown. */
export function backoffHistory(): Step[] {
  const origin = (agentID: string, cooldown_id: string, wait_ms: number): Step => ({
    kind: "model_backoff", agentID, text: JSON.stringify({ scope: "origin", cooldown_id, wait_ms }),
  })
  const candidate = (code: string, wait_ms: number): Step => ({
    kind: "model_backoff", agentID: "schedule-answer-policy",
    text: JSON.stringify({ scope: "schedule_answer", candidate_id: "private-answer", code, wait_ms }),
  })
  return [
    { kind: "user_message", text: "Continue the assigned work." },
    origin("manager", "service:1", 5000),
    origin("worker-1", "service:1", 4000),
    origin("worker-2", "service:1", 0),
    origin("manager", "service:2", 10000),
    candidate("classification_unavailable", 5000),
    candidate("classification_deferred", 0),
  ]
}
