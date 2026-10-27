import type { Step } from "./mock-turn"

export function workerCollectionHistory(state: string | null): Step[] {
  const steps: Step[] = [
    { kind: "user_message", text: "Inspect the assigned inputs." },
    { kind: "max_iterations", text: JSON.stringify({ reason: "workers_uncollected", wait_id: "collection-wait", automatic_resume: state !== "paused", limit: 80, extend_by: 2000 }) },
  ]
  if (state === "done") steps.push(
    { kind: "workers_collection_resumed", text: '{"wait_id":"collection-wait","worker_ids":["worker-1"]}' },
    { kind: "agent_message", text: "The original delegated result has been collected." },
    { kind: "done" },
  )
  if (state === "stopped") steps.push({ kind: "error", err: "stopped before collecting worker results" })
  return steps
}
