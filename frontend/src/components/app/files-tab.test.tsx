import { fireEvent, render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { FilesTab } from "./files-tab"
import type { FileEntry } from "@/lib/types"

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

const listing: FileEntry[] = [
  entry("pkg", { dir: true }),
  entry("pkg/a.go"),
  entry("pkg/b.go"),
  entry("readme.md"),
  entry("uploads", { dir: true }),
  entry("uploads/brief.txt", { uploaded: true, size: 40 }),
]

function renderTab(files: FileEntry[] = listing, extra: Partial<Parameters<typeof FilesTab>[0]> = {}) {
  const handlers = {
    onUpload: vi.fn().mockResolvedValue(undefined),
    onDelete: vi.fn(),
    onRefresh: vi.fn(),
    onReveal: vi.fn(),
  }
  render(
    <FilesTab
      files={files}
      workspace="/tmp/ws"
      threadId="th_1"
      canReveal
      {...handlers}
      {...extra}
    />,
  )
  return handlers
}

describe("Files tab", () => {
  it("names the filter so a screen reader and a test can find it", () => {
    renderTab()
    expect(screen.getByLabelText("Filter files")).toBeInTheDocument()
    expect(screen.getByRole("tree", { name: "Workspace files" })).toBeInTheDocument()
  })

  it("asks for an upload when the workspace is empty", () => {
    renderTab([])
    expect(screen.getByText(/Nothing here yet/)).toBeInTheDocument()
    expect(screen.queryByLabelText("Filter files")).not.toBeInTheDocument()
  })

  it("starts a repository-shaped listing collapsed", () => {
    renderTab()
    const tree = screen.getByTestId("file-tree")
    expect(within(tree).getByRole("treeitem", { name: "pkg" })).toHaveAttribute(
      "aria-expanded",
      "false",
    )
    expect(within(tree).queryByRole("treeitem", { name: "a.go" })).not.toBeInTheDocument()
    expect(within(tree).getByRole("treeitem", { name: "readme.md" })).toBeInTheDocument()
  })

  it("opens a unique nested folder so a small workspace is readable", () => {
    renderTab([
      entry("out", { dir: true }),
      entry("out/alpha.md"),
      entry("out/beta.md"),
    ])
    const tree = screen.getByTestId("file-tree")
    expect(within(tree).getByRole("treeitem", { name: "out" })).toHaveAttribute(
      "aria-expanded",
      "true",
    )
    expect(within(tree).getByRole("treeitem", { name: "alpha.md" })).toBeInTheDocument()
  })

  it("expands and collapses a directory from the row", () => {
    renderTab()
    const tree = screen.getByTestId("file-tree")
    fireEvent.click(within(tree).getByRole("treeitem", { name: "pkg" }))
    expect(within(tree).getByRole("treeitem", { name: "a.go" })).toBeInTheDocument()
    fireEvent.click(within(tree).getByRole("treeitem", { name: "pkg" }))
    expect(within(tree).queryByRole("treeitem", { name: "a.go" })).not.toBeInTheDocument()
  })

  it("filters without needing the parent expanded first", () => {
    renderTab()
    fireEvent.change(screen.getByLabelText("Filter files"), {
      target: { value: "brief" },
    })
    const tree = screen.getByTestId("file-tree")
    expect(within(tree).getByRole("treeitem", { name: "brief.txt" })).toBeInTheDocument()
    expect(within(tree).getByText("yours")).toBeInTheDocument()
    expect(within(tree).queryByRole("treeitem", { name: "readme.md" })).not.toBeInTheDocument()
  })

  it("says when the filter matches nothing", () => {
    renderTab()
    fireEvent.change(screen.getByLabelText("Filter files"), {
      target: { value: "zzz" },
    })
    expect(screen.getByText("No matching files.")).toBeInTheDocument()
    expect(screen.queryByTestId("file-tree")).not.toBeInTheDocument()
  })

  it("keeps download and delete off the directory row", () => {
    renderTab()
    const pkg = screen.getByRole("treeitem", { name: "pkg" })
    expect(within(pkg).queryByTitle("Download")).not.toBeInTheDocument()
    expect(within(pkg).queryByTitle("Delete")).not.toBeInTheDocument()
  })

  it("deletes a file from the row without toggling a parent", () => {
    const { onDelete } = renderTab([
      entry("out", { dir: true }),
      entry("out/alpha.md"),
    ])
    fireEvent.click(screen.getByTitle("Delete"))
    expect(onDelete).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Delete file" }))
    expect(onDelete).toHaveBeenCalledWith("out/alpha.md")
    expect(screen.getByRole("treeitem", { name: "alpha.md" })).toBeInTheDocument()
  })

  it("reveals a file on Enter when the shell can", () => {
    const { onReveal } = renderTab()
    const tree = screen.getByTestId("file-tree")
    tree.focus()
    fireEvent.keyDown(tree, { key: "End" })
    fireEvent.keyDown(tree, { key: "Enter" })
    expect(onReveal).toHaveBeenCalledWith("readme.md")
  })

  it("expands from the keyboard", () => {
    renderTab()
    const tree = screen.getByTestId("file-tree")
    tree.focus()
    fireEvent.keyDown(tree, { key: "Home" })
    fireEvent.keyDown(tree, { key: "ArrowRight" })
    expect(within(tree).getByRole("treeitem", { name: "a.go" })).toBeInTheDocument()
  })
})
