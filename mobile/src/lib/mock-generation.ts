type Step = { kind: string; text?: string; agentID?: string; role?: string; messageID?: string }

/** Retained history describes an old interrupted output and a distinct live
 *  generation owned by the same worker. No finished receipt is fabricated. */
export function generationHistory(): Step[] {
  const first = "generation-before-restart"
  const cause = { reason: "engine_restart", detail: "", recovery: "pending" }
  return [
    { kind: "user_message", text: "Continue the assigned work." },
    { kind: "spawned", agentID: "worker-1", role: "worker" },
    { kind: "reasoning", agentID: "worker-1", messageID: first, text: "Interrupted thought before restart." },
    { kind: "generation_interrupted", agentID: "worker-1", messageID: first, text: JSON.stringify(cause) },
    { kind: "generation_recovered", agentID: "worker-1", messageID: first, text: JSON.stringify({ ...cause, recovery: "recovered" }) },
    { kind: "reasoning", agentID: "worker-1", messageID: "generation-after-restart", text: "New thought after recovery." },
  ]
}
