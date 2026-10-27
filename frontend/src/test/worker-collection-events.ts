import type { SwarmEvent } from "@/lib/types"

export function collectionEvent(seq: number, kind: string, extra: Partial<SwarmEvent> = {}): SwarmEvent {
  return {
    thread_id: "thread", turn_id: "turn", agent_id: "manager", seq, kind,
    created_at: new Date(Date.UTC(2026, 0, 1, 0, 0, seq)).toISOString(), ...extra,
  }
}

export const collectionDecision = JSON.stringify({
  wait_id: "wait-1", worker_ids: ["worker-1"],
  workers: [{ agent_id: "worker-1", role: "verifier", generation: 1, running: true }],
})

// Preserve the observed Stop ordering: the durable decision precedes cleanup,
// the manager terminal, and the worker's actual cancellation finish receipt.
export function stoppedCollectionEvents(): SwarmEvent[] {
  return [
    collectionEvent(1, "user_message", { text: "Check the assigned work." }),
    collectionEvent(3, "spawned", { agent_id: "worker-1", role: "verifier", text: "Assigned verification." }),
    collectionEvent(8, "reasoning", { agent_id: "worker-1", message_id: "worker-output", text: "Partial verification." }),
    collectionEvent(9, "tool_call", { agent_id: "worker-1", tool_call_id: "call", text: "exec({})" }),
    collectionEvent(13, "max_iterations", { text: JSON.stringify({
      ...JSON.parse(collectionDecision), reason: "workers_uncollected", limit: 3, extend_by: 200, automatic_resume: true,
    }) }),
    collectionEvent(14, "workers_collection_declined", { text: collectionDecision }),
    collectionEvent(16, "cleanup", { text: "Stopped unfinished workers." }),
    collectionEvent(17, "error", { err: "stopped before collecting worker results" }),
    collectionEvent(19, "finished", { agent_id: "worker-1", role: "verifier", err: "swarm: agent worker-1 cancelled: context canceled" }),
  ]
}
