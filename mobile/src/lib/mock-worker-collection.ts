import type { Step } from "./mock-turn"

export function workerCollectionHistory(state: string | null, order: string | null = null): Step[] {
  const steps: Step[] = [{ kind: "user_message", text: "Inspect the assigned inputs." }]
  if (state === "stopped" || state === "stopping") steps.push(
    { kind: "spawned", agentID: "worker-1", role: "worker" },
    { kind: "reasoning", agentID: "worker-1", text: "Inspecting the assigned inputs." },
  )
  steps.push({ kind: "max_iterations", text: JSON.stringify({ reason: "workers_uncollected", wait_id: "collection-wait", automatic_resume: state !== "paused", limit: 80, extend_by: 2000 }) })
  if (state === "done") steps.push(
    { kind: "workers_collection_resumed", text: '{"wait_id":"collection-wait","worker_ids":["worker-1"]}' },
    { kind: "agent_message", text: "The original delegated result has been collected." },
    { kind: "done" },
  )
  if (state === "stopped") steps.push(...workerCollectionStopHistory(order))
  return steps
}

/** Receipt-bearing renderer input, including a finish callback after the turn.
 * The host forwards this internal kind; its snapshots remain diagnostics. */
export function workerCollectionStopHistory(order: string | null): Step[] {
  const decline = { kind: "workers_collection_declined", text: JSON.stringify({ wait_id: "collection-wait", worker_ids: ["worker-1"],
    workers: [{ agent_id: "worker-1", role: "worker", generation: 1, running: true }] }) }
  const finished = { kind: "finished", agentID: "worker-1", err: "swarm: agent worker-1 cancelled: context canceled" }
  const cleanup = { kind: "cleanup", text: "Stopped unfinished workers." }
  const terminal = { kind: "error", err: "stopped before collecting worker results" }
  return order === "before_cleanup" ? [decline, finished, cleanup, terminal] : [decline, cleanup, terminal, finished]
}

export function workerCollectionPaged(): boolean {
  const params = new URLSearchParams(location.search)
  return params.get("history") === "worker-collection" && ["stopped", "stopping"].includes(params.get("collection") ?? "") && params.get("collection_page") === "1"
}
