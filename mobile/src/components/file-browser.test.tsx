import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { FileBrowser } from "./file-browser"
import type { RemoteLink } from "@/lib/link"
import type { WorkspaceFile } from "@/lib/rpc"

const entry = { path: "outputs/report.html", name: "report.html", size: 17, dir: false, modified: "", uploaded: false }
const content = "<script>bad()</script>"

function link(unknownOp = false, files: WorkspaceFile[] = [
  { path: "outputs", name: "outputs", size: 0, dir: true, modified: "", uploaded: false }, entry,
]) {
  const rpc = vi.fn(async (request: { op: string }) => {
    if (unknownOp) return { v: 1, id: "", ok: false, code: "unknown_op", error: "unknown op" }
    if (request.op === "files") return { v: 1, id: "", ok: true, files }
    return { v: 1, id: "", ok: true, file_chunk: {
      path: entry.path, name: entry.name, size: content.length, mime: "text/html",
      offset: 0, next_offset: content.length, data: btoa(content), more: false,
    } }
  })
  return { path: "relay", alive: () => true, close: vi.fn(), announceDevice: vi.fn(), rpc } as unknown as RemoteLink
}

describe("phone file browser", () => {
  beforeEach(() => {
    Object.defineProperty(URL, "createObjectURL", { configurable: true, value: vi.fn(() => "blob:test") })
    Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: vi.fn() })
  })
  afterEach(() => vi.restoreAllMocks())

  it("shows only the first workspace level until each folder is opened", async () => {
    const row = (path: string, dir = false): WorkspaceFile => ({
      path, name: path.split("/").at(-1)!, dir, size: 0, modified: "", uploaded: false,
    })
    render(<FileBrowser link={link(false, [
      row("README.md"), row("outputs", true), row("outputs/report.html"),
      row("outputs/daily", true), row("outputs/daily/report.md"), row("notes", true),
    ])} threadId="t1" onClose={vi.fn()} />)
    expect(await screen.findByRole("button", { name: "outputs" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "README.md" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "notes" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "report.html" })).not.toBeInTheDocument()
    fireEvent.change(screen.getByRole("textbox", { name: "Search files" }), { target: { value: "report.md" } })
    expect(screen.getByRole("button", { name: "report.md" })).toBeInTheDocument()
    fireEvent.change(screen.getByRole("textbox", { name: "Search files" }), { target: { value: "" } })
    expect(screen.queryByRole("button", { name: "report.md" })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "outputs" }))
    expect(screen.getByRole("button", { name: "report.html" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "daily" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "report.md" })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "daily" }))
    expect(screen.getByRole("button", { name: "report.md" })).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Refresh files" }))
    await waitFor(() => expect(screen.queryByRole("button", { name: "report.html" })).not.toBeInTheDocument())
  })

  it("keeps PC-authored HTML inert and returns through file and conversation layers", async () => {
    const close = vi.fn()
    render(<FileBrowser link={link()} threadId="t1" onClose={close} />)
    fireEvent.click(await screen.findByRole("button", { name: "outputs" }))
    fireEvent.click(await screen.findByRole("button", { name: "report.html" }))
    expect(await screen.findByText(content)).toBeInTheDocument()
    expect(document.querySelector("script")).toBeNull()
    expect(URL.createObjectURL).toHaveBeenCalledWith(expect.objectContaining({ type: "application/octet-stream" }))
    act(() => { expect(window.__zwaiAndroidBack?.()).toBe(true) })
    await waitFor(() => expect(screen.getByRole("button", { name: "report.html" })).toBeInTheDocument())
    expect(close).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "outputs" }))
    expect(screen.queryByRole("button", { name: "report.html" })).not.toBeInTheDocument()
    fireEvent.change(screen.getByRole("textbox", { name: "Search files" }), { target: { value: "report" } })
    expect(screen.getByRole("button", { name: "report.html" })).toBeInTheDocument()
    act(() => { expect(window.__zwaiAndroidBack?.()).toBe(true) })
    expect(close).toHaveBeenCalledOnce()
  })

  it("tells the user when a paired PC is too old for file browsing", async () => {
    render(<FileBrowser link={link(true)} threadId="t1" onClose={vi.fn()} />)
    expect(await screen.findByRole("alert")).toHaveTextContent("Update the PC first")
  })
})
