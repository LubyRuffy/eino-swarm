import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { FilePreviewDialog } from "./file-preview"
import { readFilePreview, type FilePreview } from "@/lib/file-preview"
import { copyText } from "@/lib/copy-text"

vi.mock("@/lib/file-preview", () => ({
  MAX_RENDERED_PREVIEW_CHARS: 256 * 1024,
  readFilePreview: vi.fn(),
}))
vi.mock("@/lib/copy-text", () => ({ copyText: vi.fn().mockResolvedValue(true) }))

const base = {
  threadId: "th_preview",
  path: "notes/设计 draft.md",
  canReveal: false,
  onReveal: vi.fn(),
  onClose: vi.fn(),
  onReturnFocus: vi.fn(),
}

function preview(path = base.path) {
  return render(<FilePreviewDialog {...base} path={path} />)
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(readFilePreview).mockReset()
  vi.stubGlobal("URL", class extends URL {
    static createObjectURL = vi.fn().mockReturnValue("blob:private-image")
    static revokeObjectURL = vi.fn()
  })
})
afterEach(() => vi.unstubAllGlobals())

describe("Workspace file preview", () => {
  it("renders Markdown, shows its exact source, and copies the original text", async () => {
    const text = "# Report\n\n**Ready**\n\n| Item | Count |\n| --- | --- |\n| A | 2 |\n"
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "text", text })
    preview()
    expect(await screen.findByRole("heading", { name: "Report" })).toBeInTheDocument()
    expect(screen.getByRole("table")).toHaveTextContent("Count")
    expect(screen.getByRole("link", { name: "Download" })).toHaveAttribute("href", "/api/threads/th_preview/download/notes/%E8%AE%BE%E8%AE%A1%20draft.md")
    await userEvent.click(screen.getByRole("button", { name: "Source" }))
    expect(screen.getByTestId("file-source").textContent).toBe(text)
    expect(screen.queryByRole("heading", { name: "Report" })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Copy file content" }))
    expect(copyText).toHaveBeenCalledWith(text)
    await userEvent.click(screen.getByRole("button", { name: "Preview" }))
    expect(screen.getByRole("heading", { name: "Report" })).toBeInTheDocument()
  })

  it("keeps source text literal and highlights supported code", async () => {
    const text = 'const value = "<script>bad()</script>"\n'
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "text", text })
    preview("sample.ts")
    const source = await screen.findByTestId("file-source")
    expect(source.textContent).toBe(text)
    expect(source.querySelector(".text-syntax-keyword")).toHaveTextContent("const")
    expect(source.querySelector("script")).toBeNull()
    expect(screen.queryByRole("button", { name: "Source" })).not.toBeInTheDocument()
  })

  it("never evaluates raw HTML or MDX components in Markdown", async () => {
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "text", text: '# Safe\n\n<script>bad()</script>\n\n<Component run={bad()} />' })
    preview("content.mdx")
    await screen.findByRole("heading", { name: "Safe" })
    const dialog = screen.getByTestId("file-preview")
    expect(dialog.querySelector("script, component")).toBeNull()
  })

  it("shows large readable Markdown as complete plain text", async () => {
    const text = "# Large\n" + "x".repeat(256 * 1024)
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "text", text })
    preview()
    expect((await screen.findByTestId("file-source")).textContent).toBe(text)
    expect(screen.queryByRole("heading", { name: "Large" })).not.toBeInTheDocument()
  })

  it("explains empty, unsupported and oversized files while retaining download", async () => {
    vi.mocked(readFilePreview).mockResolvedValueOnce({ kind: "text", text: "" })
      .mockResolvedValueOnce({ kind: "download", reason: "unsupported" })
      .mockResolvedValueOnce({ kind: "download", reason: "too-large" })
    preview()
    expect(await screen.findByText("This file is empty.")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
    expect(await screen.findByText(/Preview is unavailable/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
    expect(await screen.findByText(/8 MiB preview limit/)).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Download" })).toHaveAttribute("download")
  })

  it("shows a read failure and allows an explicit refresh", async () => {
    vi.mocked(readFilePreview).mockRejectedValueOnce(new Error("404 Not Found"))
      .mockResolvedValueOnce({ kind: "text", text: "# Recovered" })
    preview()
    expect(await screen.findByRole("alert")).toHaveTextContent("404 Not Found")
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
    expect(await screen.findByRole("heading", { name: "Recovered" })).toBeInTheDocument()
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
  })

  it("cancels loading on close and ignores a late file result", async () => {
    let finish!: (file: FilePreview) => void
    vi.mocked(readFilePreview).mockReturnValue(new Promise((resolve) => { finish = resolve }))
    const view = preview()
    expect(screen.getByRole("status")).toHaveTextContent("Loading file")
    const signal = vi.mocked(readFilePreview).mock.calls[0][2]
    view.unmount()
    expect(signal.aborted).toBe(true)
    await act(async () => finish({ kind: "image", blob: new Blob(["private"]) }))
    expect(URL.createObjectURL).not.toHaveBeenCalled()
  })

  it("discards an old selection even if its response finishes last", async () => {
    let finish!: (file: FilePreview) => void
    vi.mocked(readFilePreview).mockReturnValueOnce(new Promise((resolve) => { finish = resolve }))
      .mockResolvedValueOnce({ kind: "text", text: "new content" })
    const view = preview("old.txt")
    view.rerender(<FilePreviewDialog {...base} path="new.txt" />)
    expect(await screen.findByTestId("file-source")).toHaveTextContent("new content")
    await act(async () => finish({ kind: "text", text: "old secret content" }))
    expect(screen.getByTestId("file-source")).toHaveTextContent("new content")
    expect(screen.queryByText("old secret content")).not.toBeInTheDocument()
  })

  it("revokes image URLs on refresh and close and reports failed decoding", async () => {
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "image", blob: new Blob(["image"], { type: "image/png" }) })
    const view = preview("plot.png")
    const image = await screen.findByRole("img", { name: "plot.png" })
    fireEvent.error(image)
    expect(screen.getByRole("alert")).toHaveTextContent("This image could not be displayed")
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
    await waitFor(() => expect(URL.createObjectURL).toHaveBeenCalledTimes(2))
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:private-image")
    view.unmount()
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(2)
  })

  it("lets Escape close the dialog", async () => {
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "text", text: "ready" })
    preview("file.txt")
    await screen.findByTestId("file-source")
    await userEvent.keyboard("{Escape}")
    expect(base.onClose).toHaveBeenCalledOnce()
  })
})
