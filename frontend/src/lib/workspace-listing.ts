import type { FileEntry } from "@/lib/types"

export type FileListings = Record<string, FileEntry[]>

export function parentDir(path: string): string {
  const slash = path.lastIndexOf("/")
  return slash < 0 ? "" : path.slice(0, slash)
}

/** Drop listings whose directory is no longer a child of a loaded parent. */
export function pruneFileListings(listings: FileListings): FileListings {
  const next: FileListings = { ...listings }
  let changed = true
  while (changed) {
    changed = false
    for (const dir of Object.keys(next)) {
      if (dir === "") continue
      const parent = next[parentDir(dir)]
      if (!parent?.some((entry) => entry.dir && entry.path === dir)) {
        delete next[dir]
        changed = true
      }
    }
  }
  return next
}

export function withFileDir(listings: FileListings, dir: string, entries: FileEntry[]): FileListings {
  return pruneFileListings({ ...listings, [dir]: entries })
}

export function flattenFileListings(listings: FileListings): FileEntry[] {
  const seen = new Set<string>()
  const out: FileEntry[] = []
  for (const entries of Object.values(listings)) {
    for (const entry of entries) {
      if (seen.has(entry.path)) continue
      seen.add(entry.path)
      out.push(entry)
    }
  }
  return out
}
