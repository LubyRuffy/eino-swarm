/** Wire shapes of the graph kernel (`GET /api/threads/:id/graph`). */

export type SubjectStatus = "lead" | "confirmed" | "retracted"
export type TodoStatus = "open" | "doing" | "done" | "failed"

export interface GraphEvidence {
  thread_id?: string
  turn_id?: string
  seq?: number
  url?: string
  label?: string
  unavailable?: boolean
}

export interface GraphSubject {
  id: string
  kind: string
  name: string
  attrs: Record<string, unknown>
  status: SubjectStatus
  evidence?: GraphEvidence[]
  /** Attribute names whose values this reader never receives. */
  credentials?: string[]
  found_by_todo?: string
  version: number
  updated_at: string
}

export interface GraphLink {
  id: string
  from: string
  to: string
  rel: string
  attrs?: Record<string, unknown>
  found_by_todo?: string
}

export interface GraphTodo {
  id: string
  title: string
  about?: string
  status: TodoStatus
  result?: string
  evidence?: GraphEvidence[]
  position: number
  version: number
}

export interface GraphView {
  owner_id: string
  revision: number
  subjects: GraphSubject[]
  links: GraphLink[]
  todos: GraphTodo[]
  total: number
  more: boolean
}

export interface KindAttr {
  name: string
  type: string
  label: Record<string, string>
  secret?: boolean
}

export interface KindDef {
  id: string
  name: Record<string, string>
  description: string
  attrs: KindAttr[]
}
