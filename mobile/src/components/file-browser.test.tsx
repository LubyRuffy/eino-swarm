import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { FileBrowser } from "./file-browser"
import type { RemoteLink } from "@/lib/link"

const entry = { path: "outputs/report.html", name: "report.html", size: 17, dir: false, modified: "", uploaded: false }
const content = "<script>bad()</script>"

function link(unknownOp = false) {
  const rpc = vi.fn(async (request: { op: string }) => {
    if (unknownOp) return { v: 1, id: "", ok: false, code: "unknown_op", error: "unknown op" }
    if (request.op === "files") return { v: 1, id: "", ok: true, files: [
      { path: "outputs", name: "outputs", size: 0, dir: true, modified: "", uploaded: false }, entry,
    ] }
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

  it("keeps PC-authored HTML inert and returns through file and conversation layers", async () => {
    const close = vi.fn()
    render(<FileBrowser link={link()} threadId="t1" onClose={close} />)
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
