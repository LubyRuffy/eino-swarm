import type { FileEntry } from "@/lib/types"

/** Nested workspace listing. The API still returns a flat walk; the Files
 *  panel is the thing that has to look like a file explorer. */
export interface FileTreeNode {
  path: string
  name: string
  dir: boolean
  size: number
  modified: string
  uploaded?: boolean
  children: FileTreeNode[]
}

export interface FileTreeRow {
  node: FileTreeNode
  depth: number
}

export type FileKind = "folder" | "code" | "doc" | "config" | "image" | "file"

export type FileTreeAction =
  | { type: "focus"; path: string }
  | { type: "toggle"; path: string }
  | { type: "open"; path: string }

const CODE_EXT = new Set([
  "c",
  "cc",
  "cpp",
  "cs",
  "go",
  "h",
  "hpp",
  "java",
  "js",
  "jsx",
  "kt",
  "php",
  "py",
  "rb",
  "rs",
  "swift",
  "ts",
  "tsx",
  "vue",
])

const DOC_EXT = new Set(["adoc", "md", "rst", "txt"])

const CONFIG_EXT = new Set([
  "env",
  "ini",
  "json",
  "lock",
  "toml",
  "yaml",
  "yml",
])

const IMAGE_EXT = new Set(["gif", "ico", "jpeg", "jpg", "png", "svg", "webp"])

const NAMED_KIND: Record<string, FileKind> = {
  dockerfile: "config",
  makefile: "config",
  "go.mod": "config",
  "go.sum": "config",
}

/** Fold the flat listing into a tree. Missing parent directories are
 *  synthesised so a file can still nest when the walk omitted the dir row. */
export function buildFileTree(entries: FileEntry[]): FileTreeNode[] {
  const map = new Map<string, FileTreeNode>()

  const ensure = (path: string, dir: boolean, entry?: FileEntry): FileTreeNode => {
    let node = map.get(path)
    if (!node) {
      const slash = path.lastIndexOf("/")
      node = {
        path,
        name: slash < 0 ? path : path.slice(slash + 1),
        dir,
        size: entry?.size ?? 0,
        modified: entry?.modified ?? "",
        uploaded: entry?.uploaded,
        children: [],
      }
      map.set(path, node)
    } else if (entry) {
      node.dir = entry.dir
      node.size = entry.size
      node.modified = entry.modified
      node.uploaded = entry.uploaded
    }
    return node
  }

  for (const entry of entries) {
    const parts = entry.path.split("/").filter(Boolean)
    let prefix = ""
    for (let i = 0; i < parts.length; i++) {
      prefix = prefix ? `${prefix}/${parts[i]}` : parts[i]
      const leaf = i === parts.length - 1
      ensure(prefix, leaf ? entry.dir : true, leaf ? entry : undefined)
    }
  }

  const roots: FileTreeNode[] = []
  for (const node of map.values()) {
    const parent = parentPath(node.path)
    if (!parent) {
      roots.push(node)
      continue
    }
    const dir = map.get(parent)
    if (dir) dir.children.push(node)
    else roots.push(node)
  }

  sortNodes(roots)
  return roots
}

function sortNodes(nodes: FileTreeNode[]): void {
  nodes.sort((a, b) => {
    if (a.dir !== b.dir) return a.dir ? -1 : 1
    return a.name.localeCompare(b.name, undefined, {
      numeric: true,
      sensitivity: "base",
    })
  })
  for (const node of nodes) sortNodes(node.children)
}

/** Directories whose name matches keep their full subtree; otherwise only
 *  matching descendants stay. Empty query returns the tree unchanged. */
export function filterFileTree(nodes: FileTreeNode[], query: string): FileTreeNode[] {
  const q = query.trim().toLowerCase()
  if (!q) return nodes
  const out: FileTreeNode[] = []
  for (const node of nodes) {
    if (nodeMatches(node, q)) {
      out.push(node)
      continue
    }
    if (!node.dir) continue
    const children = filterFileTree(node.children, query)
    if (children.length > 0) out.push({ ...node, children })
  }
  return out
}

function nodeMatches(node: FileTreeNode, q: string): boolean {
  return node.name.toLowerCase().includes(q) || node.path.toLowerCase().includes(q)
}

export function visibleRows(
  roots: FileTreeNode[],
  expanded: ReadonlySet<string>,
  query = "",
): FileTreeRow[] {
  const q = query.trim()
  const filtered = filterFileTree(roots, q)
  const forceOpen = q.length > 0
  const rows: FileTreeRow[] = []
  const walk = (nodes: FileTreeNode[], depth: number) => {
    for (const node of nodes) {
      rows.push({ node, depth })
      if (node.dir && (forceOpen || expanded.has(node.path))) {
        walk(node.children, depth + 1)
      }
    }
  }
  walk(filtered, 0)
  return rows
}

/** A workspace that is a single nested folder (the usual conversation
 *  layout) should open that folder. A repository with many top-level
 *  directories stays collapsed, or the panel is a wall again. */
export function initialExpanded(roots: FileTreeNode[]): Set<string> {
  const out = new Set<string>()
  let level = roots
  while (level.length === 1 && level[0].dir) {
    out.add(level[0].path)
    level = level[0].children
  }
  return out
}

export function allDirPaths(
  nodes: FileTreeNode[],
  out: Set<string> = new Set(),
): Set<string> {
  for (const node of nodes) {
    if (!node.dir) continue
    out.add(node.path)
    allDirPaths(node.children, out)
  }
  return out
}

export function pruneExpanded(
  expanded: ReadonlySet<string>,
  roots: FileTreeNode[],
): Set<string> {
  const known = allDirPaths(roots)
  const next = new Set<string>()
  for (const path of expanded) {
    if (known.has(path)) next.add(path)
  }
  return next
}

export function toggleExpanded(
  expanded: ReadonlySet<string>,
  path: string,
): Set<string> {
  const next = new Set(expanded)
  if (next.has(path)) next.delete(path)
  else next.add(path)
  return next
}

export function parentPath(path: string): string {
  const i = path.lastIndexOf("/")
  return i <= 0 ? "" : path.slice(0, i)
}

export function fileKind(node: { name: string; dir: boolean }): FileKind {
  if (node.dir) return "folder"
  const named = NAMED_KIND[node.name.toLowerCase()]
  if (named) return named
  const ext = extensionOf(node.name)
  if (CODE_EXT.has(ext)) return "code"
  if (DOC_EXT.has(ext)) return "doc"
  if (CONFIG_EXT.has(ext)) return "config"
  if (IMAGE_EXT.has(ext)) return "image"
  if (node.name.startsWith(".")) return "config"
  return "file"
}

function extensionOf(name: string): string {
  const i = name.lastIndexOf(".")
  if (i <= 0) return ""
  return name.slice(i + 1).toLowerCase()
}

export function moveFileFocus(
  rows: FileTreeRow[],
  focused: string | undefined,
  delta: number,
): string | undefined {
  if (rows.length === 0) return undefined
  const i = focused ? rows.findIndex((row) => row.node.path === focused) : -1
  const from = i < 0 ? (delta > 0 ? -1 : rows.length) : i
  const next = Math.max(0, Math.min(rows.length - 1, from + delta))
  return rows[next].node.path
}

/** Arrow keys match a file explorer, not a list of links. Filtering forces
 *  matches open, so Left then walks to the parent instead of collapsing. */
export function fileTreeKey(
  key: string,
  rows: FileTreeRow[],
  focused: string | undefined,
  expanded: ReadonlySet<string>,
  filtering: boolean,
): FileTreeAction | undefined {
  if (rows.length === 0) return undefined
  const row = rows.find((r) => r.node.path === focused) ?? rows[0]
  switch (key) {
    case "ArrowDown":
      return { type: "focus", path: moveFileFocus(rows, focused, 1) ?? row.node.path }
    case "ArrowUp":
      return { type: "focus", path: moveFileFocus(rows, focused, -1) ?? row.node.path }
    case "Home":
      return { type: "focus", path: rows[0].node.path }
    case "End":
      return { type: "focus", path: rows[rows.length - 1].node.path }
    case "ArrowRight": {
      if (row.node.dir && !filtering && !expanded.has(row.node.path)) {
        return { type: "toggle", path: row.node.path }
      }
      const child = row.node.children[0]
      if (row.node.dir && child) return { type: "focus", path: child.path }
      return undefined
    }
    case "ArrowLeft": {
      if (row.node.dir && !filtering && expanded.has(row.node.path)) {
        return { type: "toggle", path: row.node.path }
      }
      const parent = parentPath(row.node.path)
      if (parent) return { type: "focus", path: parent }
      return undefined
    }
    case "Enter":
    case " ":
      if (row.node.dir) return { type: "toggle", path: row.node.path }
      return { type: "open", path: row.node.path }
    default:
      return undefined
  }
}
