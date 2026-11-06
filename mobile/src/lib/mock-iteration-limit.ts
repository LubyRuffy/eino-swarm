import type { RemoteEvent, RemoteRequest, RemoteResponse } from "./rpc"
import type { Step } from "./mock-turn"

const turnID = "iteration-turn"
const refused = new WeakSet<object>()

type PausedThread = { running: boolean; events: RemoteEvent[] }

export function iterationLimitHistory(): Step[] {
  return [
    { kind: "user_message", turnID, text: "Inspect the current workspace." },
    { kind: "max_iterations", turnID, text: JSON.stringify({ limit: 6, extend_by: 24 }) },
  ]
}

export function mockAwaitingContinue(thread: PausedThread): boolean {
  if (!thread.running) return false
  const last = [...thread.events].reverse().find((event) =>
    (!event.agent_id || event.agent_id === "manager") &&
    ["max_iterations", "max_iterations_continued", "done", "error"].includes(event.kind))
  if (last?.kind !== "max_iterations") return false
  try {
    const pause = JSON.parse(last.text)
    return pause.reason !== "workers_uncollected" || pause.automatic_resume !== true
  } catch { return false }
}

export function iterationSnapshotOnly(thread: { id: string }): boolean {
  const params = new URLSearchParams(location.search)
  return thread.id === "t-live" && params.get("history") === "iteration-limit" && params.get("cap_snapshot") === "1"
}

export function mockContinueIteration<T extends PausedThread>(
  host: { push(thread: T, step: Step): void; settle(thread: T): void },
  thread: T | undefined,
  request: Pick<RemoteRequest, "continue">,
  id: string,
): RemoteResponse {
  const response: RemoteResponse = { v: 1, id, ok: false }
  if (!thread) return { ...response, code: "not_found", error: "no such thread" }
  if (typeof request.continue !== "boolean") return { ...response, code: "bad_request", error: "continue is required" }
  if (!mockAwaitingContinue(thread)) return { ...response, code: "idle", error: "continuation no longer awaited" }
  if (new URLSearchParams(location.search).get("continuefail") === "1" && !refused.has(host)) {
    refused.add(host)
    return { ...response, code: "bad_request", error: "PC rejected continuation" }
  }
  if (request.continue) {
    host.push(thread, { kind: "max_iterations_continued", turnID, text: "continuing for another 24 tool rounds" })
    host.push(thread, { kind: "agent_message", turnID, text: "The paused turn resumed." })
  }
  host.settle(thread)
  host.push(thread, request.continue ? { kind: "done", turnID } : { kind: "error", turnID, err: "interrupted" })
  return { ...response, ok: true }
}
