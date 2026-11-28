import type { GraphSubject, GraphTodo } from "../../../frontend/src/lib/graph/types"
import type { EngineStartup, RemoteResponse } from "./rpc"

/** `?graph=on` serves a small graph, `large` an oversized refusal, anything
 *  else behaves like a PC with work records switched off. */
function mode() {
  return new URLSearchParams(location.search).get("graph")
}

export function mockGraphMeta(): EngineStartup {
  const value = mode()
  return value === "on" || value === "large" ? { graph_available: true, data_generation: 1 } : {}
}

export function mockGraph(thread: { id: string } | undefined, since: number, owner = ""): Partial<RemoteResponse> {
  const value = mode()
  if (value !== "on" && value !== "large") return { ok: false, code: "unknown_op" }
  if (!thread) return { ok: false, code: "not_found" }
  if (value === "large") return { ok: false, code: "graph_too_large" }
  const revision = 3
  if (since === revision && owner === thread.id) return { data_generation: 1, graph: { owner: thread.id, revision, unchanged: true } }
  const updated_at = "2026-11-24T00:00:00Z"
  const subject = (id: string, kind: string, name: string, status: GraphSubject["status"] = "confirmed"): GraphSubject =>
    ({ id, kind, name, attrs: {}, status, version: 1, updated_at })
  const todo = (id: string, title: string, status: GraphTodo["status"], position: number, about?: string): GraphTodo =>
    ({ id, title, status, position, about, version: 1 })
  return {
    data_generation: 1,
    graph: {
      owner: thread.id, revision, total: 3,
      subjects: [subject("s1", "org", "Example Org"), subject("s2", "domain", "example.test"), subject("s3", "domain", "old.example.test", "retracted")],
      links: [{ id: "l1", from: "s1", to: "s2", rel: "owns" }, { id: "l2", from: "s1", to: "s3", rel: "was" }],
      todos: [todo("t1", "Identify the organisation", "done", 1, "s1"), todo("t2", "List its domains", "open", 2, "s1")],
    },
  }
}
