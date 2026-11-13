import { describe, expect, it } from "vitest"

import { flattenFileListings, pruneFileListings, withFileDir } from "./workspace-listing"
import type { FileEntry } from "@/lib/types"

function entry(path: string, dir = false): FileEntry {
  const slash = path.lastIndexOf("/")
  return {
    path,
    name: slash < 0 ? path : path.slice(slash + 1),
    size: dir ? 0 : 4,
    dir,
    modified: "2026-01-01T00:00:00Z",
  }
}

describe("workspace directory listings", () => {
  it("keeps a loaded directory only while its parent still lists it", () => {
    const root = [entry("notes", true), entry("readme.md")]
    const notes = [entry("notes/a.md"), entry("notes/b.md")]
    const kept = withFileDir(withFileDir({}, "", root), "notes", notes)
    expect(flattenFileListings(kept).map((row) => row.path)).toEqual([
      "notes",
      "readme.md",
      "notes/a.md",
      "notes/b.md",
    ])

    const removed = pruneFileListings(withFileDir(kept, "", [entry("readme.md")]))
    expect(removed.notes).toBeUndefined()
    expect(flattenFileListings(removed).map((row) => row.path)).toEqual(["readme.md"])
  })
})
