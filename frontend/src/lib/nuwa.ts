/** Shared work is a projection of durable project records, never inferred from
 * prose or worker completion. Reported facts remain reported until revised. */
export interface NuwaEvidence {
  thread_id: string
  turn_id?: string
  seq: number
  label?: string
}

export interface NuwaModelBinding {
  ref?: string
  provider_id: string
  model: string
  reasoning_effort?: string
}

export interface NuwaNode {
  id: string
  project_id?: string
  kind: "task" | "fact" | "artifact" | string
  title: string
  summary: string
  detail?: string
  status: string
  version: number
  depends_on: string[] | null
  thread_id: string
  turn_id: string
  agent_id?: string
  generation?: number
  provider_id?: string
  model?: string
  reasoning_effort?: string
  model_binding?: NuwaModelBinding
  artifact_path?: string
  evidence?: NuwaEvidence[]
  missing_dependencies?: string[]
  unavailable_evidence?: NuwaEvidence[]
  stale_dependencies?: string[]
  dependency_versions?: Record<string, number>
  updated_by?: string
  updated_thread_id?: string
  updated_turn_id?: string
  updated_generation?: number
  created_at: string
  updated_at: string
}

export interface NuwaWorker {
  agent_id: string
  role: string
  status: string
  thread_id: string
  turn_id: string
  generation?: number
  parent_id?: string
  model_binding?: NuwaModelBinding
}

export interface NuwaSnapshot {
  project_id: string
  revision: number
  nodes: NuwaNode[]
  workers: NuwaWorker[]
}

/** A thread view includes its prerequisites, even when another conversation
 * discovered them. Omitting those would invent a disconnected work path. */
export function scopeNuwaNodes(nodes: NuwaNode[], threadId?: string): NuwaNode[] {
  if (!threadId) return nodes
  const byID = new Map(nodes.map((node) => [node.id, node]))
  const included = new Set<string>()
  const visit = (id: string) => {
    if (included.has(id) || !byID.has(id)) return
    included.add(id)
    for (const parent of byID.get(id)!.depends_on ?? []) visit(parent)
  }
  for (const node of nodes)
    if (node.thread_id === threadId || node.updated_thread_id === threadId) visit(node.id)
  return nodes.filter((node) => included.has(node.id))
}

type NuwaPathNode = Pick<NuwaNode, "id" | "created_at"> & { depends_on: string[] | null }

function compareNuwaCreation(a: NuwaPathNode, b: NuwaPathNode) {
  return a.created_at.localeCompare(b.created_at) || a.id.localeCompare(b.id)
}

interface NuwaPosition<T> {
  node: T
  column: number
  row: number
}

/** Kahn layers keep real dependencies visible without trusting the stored
 * graph to be acyclic. Cyclic records are still inspectable, never dropped. */
export function layoutNuwa<T extends NuwaPathNode>(input: T[]): {
  positions: NuwaPosition<T>[]
  edges: { from: string; to: string }[]
  missing: string[]
  cyclic: boolean
} {
  const ordered = [...input].sort(compareNuwaCreation)
  const known = new Set(ordered.map((node) => node.id))
  const remaining = new Map(ordered.map((node) => [node.id, node]))
  const columns = new Map<string, number>()
  const missing = new Set<string>()
  const edges: { from: string; to: string }[] = []
  for (const node of ordered)
    for (const id of new Set(node.depends_on ?? [])) {
      if (known.has(id)) edges.push({ from: id, to: node.id })
      else missing.add(id)
    }
  while (remaining.size) {
    const ready = [...remaining.values()].filter((node) =>
      (node.depends_on ?? []).every((id) => !known.has(id) || columns.has(id))
    )
    if (!ready.length) break
    for (const node of ready) {
      const parents = (node.depends_on ?? []).filter((id) => columns.has(id))
      columns.set(
        node.id,
        parents.length ? Math.max(...parents.map((id) => columns.get(id)!)) + 1 : 0
      )
      remaining.delete(node.id)
    }
  }
  const cyclic = remaining.size > 0
  const last = Math.max(0, ...columns.values())
  for (const node of remaining.values()) columns.set(node.id, last + 1)
  const rows = new Map<number, number>()
  const positions = ordered
    .sort((a, b) => columns.get(a.id)! - columns.get(b.id)!)
    .map((node) => {
      const column = columns.get(node.id)!
      const row = rows.get(column) ?? 0
      rows.set(column, row + 1)
      return { node, column, row }
    })
  return { positions, edges, missing: [...missing].sort(), cyclic }
}

type CollaborationNode = NuwaPathNode & { depends_on: string[] } & (
  | { type: "work"; record: NuwaNode }
  | { type: "turn"; turnId: string }
  | { type: "author"; agentId: string; worker?: NuwaWorker }
)

/** Isolated records still have a durable author and turn. Keep these display
 * relationships separate from the saved dependencies between work records. */
export function layoutNuwaCollaboration(nodes: NuwaNode[], workers: NuwaWorker[]) {
  const known = new Set(nodes.map(({ id }) => id))
  const connected = new Set<string>()
  for (const node of nodes) {
    for (const dependency of node.depends_on ?? []) {
      if (!known.has(dependency)) continue
      connected.add(dependency)
      connected.add(node.id)
    }
  }

  // Stored work IDs use the nw_ prefix. Origin keys live only in this view.
  const authorKey = (thread: string, turn: string, agent: string, generation?: number) =>
    `author:${JSON.stringify([thread, turn, agent, generation])}`
  const roster = new Map(workers.map((worker) => [
    authorKey(worker.thread_id, worker.turn_id, worker.agent_id, worker.generation), worker,
  ]))
  const origins = new Map<string, CollaborationNode>()
  const entries = [...nodes].sort(compareNuwaCreation).map((record): CollaborationNode => {
    const { id, thread_id, turn_id, agent_id, generation, created_at } = record
    const entry: CollaborationNode = { type: "work", id, record, created_at, depends_on: record.depends_on ?? [] }
    if (connected.has(id) || !thread_id || !turn_id || !agent_id) return entry

    const turnKey = `turn:${JSON.stringify([thread_id, turn_id])}`
    const author = authorKey(thread_id, turn_id, agent_id, generation)
    if (!origins.has(turnKey)) {
      origins.set(turnKey, { type: "turn", id: turnKey, turnId: turn_id, created_at, depends_on: [] })
    }
    if (!origins.has(author)) {
      origins.set(author, {
        type: "author", id: author, agentId: agent_id, worker: roster.get(author),
        created_at, depends_on: [turnKey],
      })
    }
    entry.depends_on = [...entry.depends_on, author]
    return entry
  })
  return layoutNuwa([...origins.values(), ...entries])
}
