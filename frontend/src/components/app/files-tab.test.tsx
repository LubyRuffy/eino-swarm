import { fireEvent, render, screen, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { FilesTab } from "./files-tab"
import type { FileEntry } from "@/lib/types"
import { readFilePreview } from "@/lib/file-preview"

vi.mock("@/lib/file-preview", () => ({
  MAX_RENDERED_PREVIEW_CHARS: 256 * 1024,
  readFilePreview: vi.fn(),
}))

beforeEach(() => {
  vi.mocked(readFilePreview).mockReset().mockResolvedValue({ kind: "text", text: "# Local file" })
})

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

  it("previews a file on Enter and keeps native reveal as an explicit action", async () => {
    const { onReveal } = renderTab()
    const tree = screen.getByTestId("file-tree")
    tree.focus()
    fireEvent.keyDown(tree, { key: "End" })
    fireEvent.keyDown(tree, { key: "Enter" })
    expect(await screen.findByRole("heading", { name: "Local file" })).toBeInTheDocument()
    expect(readFilePreview).toHaveBeenCalledWith("th_1", "readme.md", expect.any(AbortSignal))
    expect(onReveal).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Show file in file manager" }))
    expect(onReveal).toHaveBeenCalledWith("readme.md")
  })

  it("opens files with a single click in the browser shell too", async () => {
    const { onReveal } = renderTab(listing, { canReveal: false })
    fireEvent.click(screen.getByRole("treeitem", { name: "readme.md" }))
    expect(await screen.findByRole("heading", { name: "Local file" })).toBeInTheDocument()
    expect(onReveal).not.toHaveBeenCalled()
    expect(screen.queryByRole("button", { name: "Show file in file manager" })).not.toBeInTheDocument()
  })

  it("aborts and closes the old preview when changing conversations", async () => {
    const { rerender } = render(
      <FilesTab files={listing} workspace="/tmp/ws" threadId="first" canReveal={false}
        onUpload={vi.fn()} onDelete={vi.fn()} onRefresh={vi.fn()} onReveal={vi.fn()} />,
    )
    fireEvent.click(screen.getByRole("treeitem", { name: "readme.md" }))
    await screen.findByRole("heading", { name: "Local file" })
    const signal = vi.mocked(readFilePreview).mock.calls[0][2]
    rerender(<FilesTab files={listing} workspace="/tmp/ws" threadId="second" canReveal={false}
      onUpload={vi.fn()} onDelete={vi.fn()} onRefresh={vi.fn()} onReveal={vi.fn()} />)
    expect(screen.queryByTestId("file-preview")).not.toBeInTheDocument()
    expect(signal.aborted).toBe(true)
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
