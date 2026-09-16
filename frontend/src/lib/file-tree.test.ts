import { describe, expect, it } from "vitest"

import type { FileEntry } from "@/lib/types"
import {
  allDirPaths,
  buildFileTree,
  fileKind,
  fileTreeKey,
  filterFileTree,
  initialExpanded,
  moveFileFocus,
  parentPath,
  pruneExpanded,
  toggleExpanded,
  visibleRows,
} from "./file-tree"

function entry(path: string, extra: Partial<FileEntry> = {}): FileEntry {
  const slash = path.lastIndexOf("/")
  return {
    path,
    name: slash < 0 ? path : path.slice(slash + 1),
    size: extra.dir ? 0 : 12,
    dir: false,
    modified: "2026-01-01T00:00:00Z",
    ...extra,
  }
}

const nested: FileEntry[] = [
  entry("pkg", { dir: true }),
  entry("pkg/a.go"),
  entry("pkg/b.go"),
  entry("readme.md"),
  entry("uploads", { dir: true }),
  entry("uploads/brief.txt", { uploaded: true, size: 40 }),
]

describe("buildFileTree", () => {
  it("nests files under their directories, directories first", () => {
    const tree = buildFileTree(nested)
    expect(tree.map((n) => n.name)).toEqual(["pkg", "uploads", "readme.md"])
    expect(tree[0].children.map((n) => n.name)).toEqual(["a.go", "b.go"])
    expect(tree[1].children[0].uploaded).toBe(true)
  })

  it("synthesises a missing parent so a leaf still nests", () => {
    const tree = buildFileTree([entry("src/lib/util.go")])
    expect(tree).toHaveLength(1)
    expect(tree[0].name).toBe("src")
    expect(tree[0].dir).toBe(true)
    expect(tree[0].children[0].name).toBe("lib")
    expect(tree[0].children[0].children[0].name).toBe("util.go")
  })

  it("returns an empty tree for an empty listing", () => {
    expect(buildFileTree([])).toEqual([])
  })
})

describe("filterFileTree", () => {
  const tree = buildFileTree(nested)

  it("keeps ancestors of a matching file", () => {
    const filtered = filterFileTree(tree, "brief")
    expect(filtered.map((n) => n.name)).toEqual(["uploads"])
    expect(filtered[0].children.map((n) => n.name)).toEqual(["brief.txt"])
  })

  it("keeps a matching directory's full subtree", () => {
    const filtered = filterFileTree(tree, "pkg")
    expect(filtered).toHaveLength(1)
    expect(filtered[0].children).toHaveLength(2)
  })

  it("is case-insensitive and leaves the tree alone when empty", () => {
    expect(filterFileTree(tree, "  ")).toBe(tree)
    expect(filterFileTree(tree, "README")).toEqual([tree[2]])
  })
})

describe("visibleRows", () => {
  const tree = buildFileTree(nested)

  it("hides children of a collapsed directory", () => {
    const rows = visibleRows(tree, new Set())
    expect(rows.map((r) => r.node.name)).toEqual(["pkg", "uploads", "readme.md"])
  })

  it("shows children of an expanded directory", () => {
    const rows = visibleRows(tree, new Set(["pkg"]))
    expect(rows.map((r) => r.node.name)).toEqual([
      "pkg",
      "a.go",
      "b.go",
      "uploads",
      "readme.md",
    ])
    expect(rows[1].depth).toBe(1)
  })

  it("opens matching branches while filtering, ignoring collapse", () => {
    const rows = visibleRows(tree, new Set(), "a.go")
    expect(rows.map((r) => r.node.name)).toEqual(["pkg", "a.go"])
  })
})

describe("initialExpanded", () => {
  it("opens a unique directory chain so a small workspace is readable", () => {
    const tree = buildFileTree([
      entry("out", { dir: true }),
      entry("out/alpha.md"),
      entry("out/beta.md"),
    ])
    expect([...initialExpanded(tree)]).toEqual(["out"])
  })

  it("leaves a repository-shaped listing collapsed", () => {
    const tree = buildFileTree(nested)
    expect(initialExpanded(tree).size).toBe(0)
  })
})

describe("expand helpers", () => {
  const tree = buildFileTree(nested)

  it("toggles a path in and out of the expanded set", () => {
    const open = toggleExpanded(new Set(), "pkg")
    expect(open.has("pkg")).toBe(true)
    expect(toggleExpanded(open, "pkg").has("pkg")).toBe(false)
  })

  it("drops expanded paths that disappeared from the listing", () => {
    const pruned = pruneExpanded(new Set(["pkg", "gone"]), tree)
    expect([...pruned]).toEqual(["pkg"])
  })

  it("lists every directory path", () => {
    expect([...allDirPaths(tree)].sort()).toEqual(["pkg", "uploads"])
  })
})

describe("fileKind", () => {
  it("classifies by extension and well-known names, not by path words", () => {
    expect(fileKind({ name: "pkg", dir: true })).toBe("folder")
    expect(fileKind({ name: "main.go", dir: false })).toBe("code")
    expect(fileKind({ name: "readme.md", dir: false })).toBe("doc")
    expect(fileKind({ name: "config.yaml", dir: false })).toBe("config")
    expect(fileKind({ name: "shot.png", dir: false })).toBe("image")
    expect(fileKind({ name: "Dockerfile", dir: false })).toBe("config")
    expect(fileKind({ name: ".gitignore", dir: false })).toBe("config")
    expect(fileKind({ name: "blob.bin", dir: false })).toBe("file")
  })
})

describe("keyboard", () => {
  const tree = buildFileTree(nested)
  const rows = visibleRows(tree, new Set(["pkg"]))

  it("moves focus without wrapping off the list", () => {
    expect(moveFileFocus(rows, undefined, 1)).toBe("pkg")
    expect(moveFileFocus(rows, "pkg", 1)).toBe("pkg/a.go")
    expect(moveFileFocus(rows, "readme.md", 1)).toBe("readme.md")
    expect(moveFileFocus(rows, "pkg", -1)).toBe("pkg")
  })

  it("expands a collapsed folder with ArrowRight and opens a file with Enter", () => {
    const collapsed = visibleRows(tree, new Set())
    expect(fileTreeKey("ArrowRight", collapsed, "pkg", new Set(), false)).toEqual({
      type: "toggle",
      path: "pkg",
    })
    expect(fileTreeKey("Enter", collapsed, "readme.md", new Set(), false)).toEqual({
      type: "open",
      path: "readme.md",
    })
  })

  it("walks to the parent with ArrowLeft when filtering has forced folders open", () => {
    const filtered = visibleRows(tree, new Set(), "a.go")
    expect(fileTreeKey("ArrowLeft", filtered, "pkg/a.go", new Set(), true)).toEqual({
      type: "focus",
      path: "pkg",
    })
  })

  it("jumps to the ends with Home and End", () => {
    expect(fileTreeKey("Home", rows, "pkg/b.go", new Set(["pkg"]), false)).toEqual({
      type: "focus",
      path: "pkg",
    })
    expect(fileTreeKey("End", rows, "pkg", new Set(["pkg"]), false)).toEqual({
      type: "focus",
      path: "readme.md",
    })
  })
})

describe("parentPath", () => {
  it("returns empty at the workspace root", () => {
    expect(parentPath("pkg")).toBe("")
    expect(parentPath("pkg/a.go")).toBe("pkg")
  })
})
