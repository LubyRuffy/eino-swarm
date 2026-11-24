export interface KnowledgeStatus {
  enabled: boolean
  schema?: {
    types?: { id: string; label?: string }[]
    views?: { id: string; label?: string }[]
  }
  page_count: number
  pending_count: number
}

export interface KnowledgeNode {
  id: string
  label: string
  kind: "type" | "relation" | "page"
  type?: string
  aliases?: string[]
  cycle?: boolean
  children?: KnowledgeNode[]
}

export interface KnowledgeIssue {
  kind: "broken_link" | "stale" | "missing_relation" | "invalid_page"
  page_id?: string
  target?: string
  relation?: string
  path?: string
  detail?: string
}

export interface KnowledgeTree {
  view: string
  nodes?: KnowledgeNode[]
  issues?: KnowledgeIssue[]
  truncated?: boolean
}

export interface KnowledgePageRef {
  id: string
  title: string
  type: string
}

export interface KnowledgeGraphNode extends KnowledgePageRef {
  aliases?: string[]
}

export interface KnowledgeGraphEdge {
  source: string
  target: string
  relations?: string[]
  wikilink?: boolean
}

export interface KnowledgeGraph {
  nodes: KnowledgeGraphNode[]
  edges: KnowledgeGraphEdge[]
}

export function matchKnowledgeGraphNodes(nodes: readonly KnowledgeGraphNode[], query: string) {
  const search = query.trim().toLowerCase()
  return nodes.filter((node) => [node.title, node.id, ...(node.aliases ?? [])]
    .some((name) => name.toLowerCase().includes(search)))
}

export interface KnowledgeConnection {
  relation: string
  label: string
  page?: KnowledgePageRef
  target: string
  resolved: boolean
}

export interface KnowledgeSource {
  uri?: string
  kind?: "message" | "event"
  thread_id?: string
  turn_id?: string
  seq?: number
  timestamp?: string
  classification?: "primary" | "analysis" | "external"
  available?: boolean
}

export interface KnowledgePage extends KnowledgePageRef {
  body: string
  wiki?: { kind: "entity" | "event"; category: string; document_pending: boolean }
  fields?: Record<string, unknown>
  as_of?: string
  updated_at?: string
  updated_turn?: string
  updated_thread?: string
  outgoing?: KnowledgeConnection[]
  incoming?: KnowledgeConnection[]
  mentions?: KnowledgePageRef[]
  unresolved?: string[]
  sources?: KnowledgeSource[]
  wiki_observations?: { source: KnowledgeSource; quote: string }[]
}


export const KNOWLEDGE_TYPE_VIEW = "type"
export const KNOWLEDGE_PENDING_VIEW = "pending"

export function knowledgePath(project: string): string {
  return `/api/projects/${encodeURIComponent(project)}/knowledge`
}

export function pushKnowledgePage(stack: readonly string[], id: string): readonly string[] {
  return stack.at(-1) === id ? stack : [...stack, id]
}

export function backKnowledgePage(stack: readonly string[]): readonly string[] {
  return stack.length ? stack.slice(0, -1) : stack
}

/** Keep matching ancestors and every copy of a page with multiple parents. */
export function filterKnowledgeTree(nodes: KnowledgeNode[], query: string): KnowledgeNode[] {
  const search = query.trim().toLowerCase()
  if (!search) return nodes
  const filter = (entries: KnowledgeNode[]): KnowledgeNode[] => entries.flatMap((node) => {
    const matches = [node.label, node.id, node.type, ...(node.aliases ?? [])]
      .some((text) => text?.toLowerCase().includes(search))
    if (matches) return [node]
    const children = filter(node.children ?? [])
    return children.length ? [{ ...node, children }] : []
  })
  return filter(nodes)
}

export function filterKnowledgeIssues(issues: KnowledgeIssue[], query: string): KnowledgeIssue[] {
  const search = query.trim().toLowerCase()
  if (!search) return issues
  return issues.filter(({ page_id, target, relation, path, detail }) =>
    [page_id, target, relation, path, detail].some((text) => text?.toLowerCase().includes(search)),
  )
}

export function groupKnowledgeConnections(connections: readonly KnowledgeConnection[]) {
  const groups = new Map<string, { label: string; links: KnowledgeConnection[] }>()
  for (const link of connections) {
    let group = groups.get(link.relation)
    if (!group) {
      group = { label: link.label || link.relation, links: [] }
      groups.set(link.relation, group)
    }
    group.links.push(link)
  }
  return groups
}

export function knowledgeSourceURL(uri: string | undefined): string | undefined {
  if (!uri) return
  try {
    const url = new URL(uri)
    if (url.protocol === "http:" || url.protocol === "https:") return url.href
  } catch {
    return
  }
}
