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

export type CollaborationNode = NuwaPathNode & { depends_on: string[] } & (
  | { type: "work"; record: NuwaNode }
  | { type: "loose"; kind: string; records: NuwaNode[] }
  | { type: "goal" }
)

/** Stored work IDs use the nw_ prefix, so these view-only ids never collide. */
export const NUWA_GOAL_ID = "__nuwa_goal__"

const looseKindOrder = ["task", "fact", "artifact"]

/** One row per kind for records that neither depend on nor support another record. */
export function nuwaLooseGroupID(kind: string) {
  return `__nuwa_loose_${kind}__`
}

/** Status counts for a collapsed row, most common first. */
export function nuwaStatusTally(records: Pick<NuwaNode, "status">[]) {
  const counts = new Map<string, number>()
  for (const record of records) counts.set(record.status, (counts.get(record.status) ?? 0) + 1)
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([status, count]) => ({ status, count }))
}

/** Newest recorded work first, so a long result list opens on the latest entry. */
export function compareNuwaRecent(a: Pick<NuwaNode, "id" | "title" | "updated_at">, b: Pick<NuwaNode, "id" | "title" | "updated_at">) {
  return b.updated_at.localeCompare(a.updated_at) || a.title.localeCompare(b.title) || a.id.localeCompare(b.id)
}

function looseKindRank(kind: string) {
  const index = looseKindOrder.indexOf(kind)
  return index === -1 ? `1-${kind}` : `0-${String(index).padStart(2, "0")}`
}

/** Records that share a saved dependency stay on the path. Everything else is
 * grouped by kind so an unconnected result does not become its own card. */
export function partitionNuwaCollaboration(nodes: NuwaNode[]) {
  const known = new Set(nodes.map((node) => node.id))
  const linked = new Set<string>()
  const missing = new Set<string>()
  for (const node of nodes) {
    for (const id of new Set(node.depends_on ?? [])) {
      if (known.has(id)) {
        linked.add(node.id)
        linked.add(id)
      } else {
        missing.add(id)
      }
    }
  }
  const loose = new Map<string, NuwaNode[]>()
  const path = nodes.filter((node) => linked.has(node.id))
  for (const node of nodes) {
    if (linked.has(node.id)) continue
    loose.set(node.kind, [...(loose.get(node.kind) ?? []), node])
  }
  const groups: { kind: string; id: string; records: NuwaNode[] }[] = []
  for (const kind of [...loose.keys()].sort((a, b) => looseKindRank(a).localeCompare(looseKindRank(b)) || a.localeCompare(b))) {
    const records = loose.get(kind)!.sort(compareNuwaCreation)
    // One unconnected record is still the thing to read. A crowd of the same
    // kind collapses so the dependency path stays visible.
    if (records.length === 1) path.push(records[0])
    else groups.push({ kind, id: nuwaLooseGroupID(kind), records })
  }
  return { path, groups, missing: [...missing].sort() }
}

/** The upstream and downstream records of a selection. Null means nothing is
 * selected, so the whole path stays visible. An unconnected record focuses its
 * kind group instead of a dependency chain. */
export function nuwaPathFocus(nodes: NuwaNode[], selected?: string) {
  if (!selected) return null
  const { path, groups } = partitionNuwaCollaboration(nodes)
  const group = groups.find((entry) => entry.id === selected || entry.records.some((node) => node.id === selected))
  const byID = new Map(path.map((node) => [node.id, node]))
  if (!byID.has(selected)) return group ? new Set([group.id, selected]) : null
  const upstream = new Set<string>()
  const visitUpstream = (id: string) => {
    if (upstream.has(id) || !byID.has(id)) return
    upstream.add(id)
    for (const parent of byID.get(id)!.depends_on ?? []) visitUpstream(parent)
  }
  visitUpstream(selected)
  const children = new Map<string, string[]>()
  for (const node of path) {
    for (const parent of node.depends_on ?? []) {
      if (!byID.has(parent)) continue
      children.set(parent, [...(children.get(parent) ?? []), node.id])
    }
  }
  const downstream = new Set<string>()
  const visitDownstream = (id: string) => {
    if (downstream.has(id)) return
    downstream.add(id)
    for (const child of children.get(id) ?? []) visitDownstream(child)
  }
  visitDownstream(selected)
  return new Set([...upstream, ...downstream])
}

/** Work records are the backbone, connected only by their saved dependencies.
 * Records with no dependency collapse into one row per kind. Those rows, and
 * the real chain terminals, share the goal so nothing is left floating. Who
 * recorded a result stays on the record, not a structural graph node. */
export function layoutNuwaCollaboration(nodes: NuwaNode[]) {
  const partitioned = partitionNuwaCollaboration(nodes)
  const known = new Set(partitioned.path.map(({ id }) => id))
  const dependedUpon = new Set<string>()
  for (const node of partitioned.path)
    for (const dependency of node.depends_on ?? [])
      if (known.has(dependency)) dependedUpon.add(dependency)

  const entries: CollaborationNode[] = partitioned.path.map((record) => ({
    type: "work", id: record.id, record, created_at: record.created_at,
    depends_on: record.depends_on ?? [],
  }))
  partitioned.groups.forEach((group, index) => {
    entries.push({
      type: "loose", id: group.id, kind: group.kind, records: group.records,
      // Rank sorts the rows without pretending the group was recorded then.
      created_at: `0001-01-${String(index + 1).padStart(2, "0")}T00:00:00Z`,
      depends_on: [],
    })
  })
  const graph: CollaborationNode[] = [...entries]
  if (entries.length) {
    const terminals = partitioned.path.filter(({ id }) => !dependedUpon.has(id)).map(({ id }) => id)
    const created = entries.reduce((latest, node) =>
      node.created_at > latest ? node.created_at : latest, entries[0].created_at)
    graph.push({
      type: "goal", id: NUWA_GOAL_ID, created_at: created,
      depends_on: [...terminals, ...partitioned.groups.map(({ id }) => id)],
    })
  }
  const laid = layoutNuwa(graph)
  return { ...laid, missing: [...new Set([...laid.missing, ...partitioned.missing])].sort() }
}
