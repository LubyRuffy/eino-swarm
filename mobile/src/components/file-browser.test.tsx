import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { FileBrowser } from "./file-browser"
import type { RemoteLink } from "@/lib/link"
import type { RemoteResponse, WorkspaceFile } from "@/lib/rpc"
import { t } from "@/lib/i18n"

const nativeSave = vi.hoisted(() => ({ enabled: false, saveFile: vi.fn() }))
vi.mock("@capacitor/core", async (original) => {
  const actual = await original<typeof import("@capacitor/core")>()
  return {
    ...actual,
    Capacitor: { ...actual.Capacitor, isNativePlatform: () => nativeSave.enabled },
    registerPlugin: (name: string, options?: Parameters<typeof actual.registerPlugin>[1]) =>
      name === "FileSave" ? { saveFile: nativeSave.saveFile } : actual.registerPlugin(name, options),
  }
})

const entry = { path: "outputs/report.html", name: "report.html", size: 17, dir: false, modified: "", uploaded: false }
const content = "<script>bad()</script>"

function link(unknownOp = false, files: WorkspaceFile[] = [
  { path: "outputs", name: "outputs", size: 0, dir: true, modified: "", uploaded: false }, entry,
], text: string | Uint8Array = content, mime = "text/html") {
  const bytes = typeof text === "string" ? new TextEncoder().encode(text) : text
  const rpc = vi.fn(async (request: { op: string }) => {
    if (unknownOp) return { v: 1, id: "", ok: false, code: "unknown_op", error: "unknown op" }
    if (request.op === "files") return { v: 1, id: "", ok: true, files }
    return { v: 1, id: "", ok: true, file_chunk: {
      path: files.at(-1)!.path, name: files.at(-1)!.name, size: bytes.length, mime,
      offset: 0, next_offset: bytes.length, data: btoa(String.fromCharCode(...bytes)), more: false,
    } }
  })
  return { path: "relay", alive: () => true, close: vi.fn(), announceDevice: vi.fn(), rpc } as unknown as RemoteLink
}

describe("phone file browser", () => {
  beforeEach(() => {
    nativeSave.enabled = false
    nativeSave.saveFile.mockReset().mockResolvedValue({ saved: true })
    Object.defineProperty(URL, "createObjectURL", { configurable: true, value: vi.fn(() => "blob:test") })
    Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: vi.fn() })
  })
  afterEach(() => vi.restoreAllMocks())

  it("saves a phone preview through the native picker with its original UTF-8 bytes", async () => {
    nativeSave.enabled = true
    const text = "# Summary\r\n\r\n数据与条件\r\n"
    const bytes = new TextEncoder().encode(text)
    const file = { ...entry, path: "outputs/summary.md", name: "summary.md", size: bytes.length }
    render(<FileBrowser link={link(false, [file], text, "text/markdown")} threadId="t1" onClose={vi.fn()} />)
    fireEvent.click(await screen.findByRole("button", { name: file.name }))
    fireEvent.click(await screen.findByRole("button", { name: t("files.textLarger") }))
    expect(screen.getByTestId("phone-text-content")).toHaveStyle({ fontSize: "16px" })
    fireEvent.click(await screen.findByText("Download"))
    await waitFor(() => expect(nativeSave.saveFile).toHaveBeenCalledWith({
      name: file.name, mime: "text/markdown", size: bytes.length,
      data: btoa(String.fromCharCode(...bytes)),
    }))
    expect(await screen.findByText(t("files.saved"))).toHaveAttribute("role", "status")
  })

  it("queues an early native download and exports the same read exactly once", async () => {
    nativeSave.enabled = true
    const file = { ...entry, path: "data.bin", name: "data.bin" }
    const remote = link(false, [file])
    const list = remote.rpc
    let finish!: (result: RemoteResponse) => void
    const read = vi.fn(() => new Promise<RemoteResponse>((resolve) => { finish = resolve }))
    remote.rpc = (req) => req.op === "files" ? list(req) : read()
    render(<FileBrowser link={remote} threadId="t1" onClose={vi.fn()} />)
    fireEvent.click(await screen.findByRole("button", { name: file.name }))
    const download = screen.getByRole("button", { name: t("files.download") })
    fireEvent.click(download)
    fireEvent.click(download)
    expect(download).toBeDisabled()
    expect(nativeSave.saveFile).not.toHaveBeenCalled()
    expect(read).toHaveBeenCalledOnce()

    const data = btoa(String.fromCharCode(0, 255, 128))
    await act(async () => finish({ v: 1, id: "", ok: true, file_chunk: {
      path: file.path, name: file.name, mime: "application/octet-stream", size: 3,
      offset: 0, next_offset: 3, data, more: false,
    } }))
    expect(nativeSave.saveFile).toHaveBeenCalledOnce()
    expect(nativeSave.saveFile).toHaveBeenCalledWith({
      name: file.name, mime: "application/octet-stream", size: 3, data,
    })
    expect(read).toHaveBeenCalledOnce()
    expect(await screen.findByText(t("files.saved"))).toBeInTheDocument()
  })

  it("makes a native save refusal visible and keeps the preview available for retry", async () => {
    nativeSave.enabled = true
    nativeSave.saveFile.mockRejectedValueOnce(new Error("provider_refused"))
    const file = { ...entry, path: "report.md", name: "report.md" }
    render(<FileBrowser link={link(false, [file], "# Report", "text/markdown")} threadId="t1" onClose={vi.fn()} />)
    fireEvent.click(await screen.findByRole("button", { name: file.name }))
    fireEvent.click(await screen.findByText("Download"))
    expect(await screen.findByRole("alert")).toHaveTextContent(t("files.saveFailed"))
    expect(screen.getByRole("heading", { name: "Report" })).toBeInTheDocument()
    fireEvent.click(screen.getByText("Download"))
    expect(await screen.findByText(t("files.saved"))).toHaveAttribute("role", "status")
  })

  it.each([new Uint8Array(), new Uint8Array([0, 255, 128, 13, 10])])(
    "exports empty or non-text bytes without passing through the text preview", async (bytes) => {
      nativeSave.enabled = true
      const file = { ...entry, path: "result.bin", name: "result.bin", size: bytes.length }
      render(<FileBrowser link={link(false, [file], bytes, "application/octet-stream")} threadId="t1" onClose={vi.fn()} />)
      fireEvent.click(await screen.findByRole("button", { name: file.name }))
      fireEvent.click(await screen.findByText("Download"))
      await waitFor(() => expect(nativeSave.saveFile).toHaveBeenCalledWith({
        name: file.name, mime: "application/octet-stream", size: bytes.length,
        data: btoa(String.fromCharCode(...bytes)),
      }))
    },
  )

  it("waits for one native export and does not call a cancelled save successful", async () => {
    nativeSave.enabled = true
    let settle!: (result: { saved: boolean }) => void
    nativeSave.saveFile.mockReturnValueOnce(new Promise((resolve) => { settle = resolve }))
    const file = { ...entry, path: "report.md", name: "report.md" }
    render(<FileBrowser link={link(false, [file], "# Report", "text/markdown")} threadId="t1" onClose={vi.fn()} />)
    fireEvent.click(await screen.findByRole("button", { name: file.name }))
    await screen.findByRole("heading", { name: "Report" })
    const download = screen.getByRole("button", { name: "Download" })
    fireEvent.click(download)
    fireEvent.click(download)
    expect(nativeSave.saveFile).toHaveBeenCalledOnce()
    expect(screen.getByRole("button", { name: t("files.saving") })).toBeDisabled()
    await act(async () => { settle({ saved: false }) })
    expect(screen.queryByText(t("files.saved"))).not.toBeInTheDocument()
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Download" }))
    expect(await screen.findByText(t("files.saved"))).toHaveAttribute("role", "status")
    expect(nativeSave.saveFile).toHaveBeenCalledTimes(2)
  })

  it("renders a Markdown output as a document selected by suffix, not MIME", async () => {
    const text = '# Summary\n\n> A finding\n\n- First\n\n<script>bad()</script>\n\n[unsafe](javascript:bad())'
    const file = { ...entry, path: "summary.MD", name: "summary.MD", size: text.length }
    render(<FileBrowser link={link(false, [file], text, "text/plain")} threadId="t1" onClose={vi.fn()} />)
    fireEvent.click(await screen.findByRole("button", { name: file.name }))
    expect(await screen.findByRole("heading", { name: "Summary", level: 1 })).toBeInTheDocument()
    expect(screen.getByText("A finding").closest("blockquote")).not.toBeNull()
    expect(screen.getByText("First").closest("li")).not.toBeNull()
    expect(document.querySelector("script")).toBeNull()
    expect(document.querySelector('a[href^="javascript:"]')).toBeNull()
    expect(screen.getByRole("link", { name: "Download" })).toHaveAttribute("download", file.name)
  })

  it("resets text size when returning to a file instead of carrying over another reading session", async () => {
    const file = { ...entry, path: "notes.md", name: "notes.md" }
    render(<FileBrowser link={link(false, [file], "# Notes", "text/markdown")} threadId="t1" onClose={vi.fn()} />)
    fireEvent.click(await screen.findByRole("button", { name: file.name }))
    fireEvent.click(await screen.findByRole("button", { name: t("files.textLarger") }))
    expect(screen.getByTestId("phone-text-content")).toHaveStyle({ fontSize: "16px" })
    fireEvent.click(screen.getByRole("button", { name: t("files.backList") }))
    fireEvent.click(await screen.findByRole("button", { name: file.name }))
    await waitFor(() => expect(screen.getByTestId("phone-text-content")).toHaveStyle({ fontSize: "14px" }))
  })

  it.each([
    ["sample.go", 'package main\nconst value = "hello"\n', true],
    ["sample.json", '{"value": true}\n', true],
    ["sample.unknown", '# Plain text\n<script>bad()</script>\n', false],
  ])("keeps %s intact and highlights only a known language", async (name, text, highlighted) => {
    const file = { ...entry, path: name, name, size: text.length }
    render(<FileBrowser link={link(false, [file], text, "text/plain")} threadId="t1" onClose={vi.fn()} />)
    fireEvent.click(await screen.findByRole("button", { name }))
    const preview = await screen.findByTestId("phone-file-preview")
    await waitFor(() => expect(preview.querySelector("pre")?.textContent).toBe(text))
    expect(Boolean(preview.querySelector(".text-syntax-keyword"))).toBe(highlighted)
    expect(document.querySelector("script")).toBeNull()
    expect(URL.createObjectURL).toHaveBeenCalledWith(expect.objectContaining({ type: "application/octet-stream" }))
  })

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
    const frame = await screen.findByTestId("html-artifact")
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts allow-forms allow-modals")
    expect(frame.getAttribute("sandbox")).not.toContain("allow-same-origin")
    expect(frame.getAttribute("srcdoc")).toContain(content)
    expect(document.querySelector("script")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: t("files.source") }))
    await waitFor(() => expect(screen.getByTestId("phone-file-preview").querySelector("pre")?.textContent).toBe(content))
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

  it("keeps confirmed progress while waiting, resumes on new data, and surfaces a definite host error", async () => {
    // The listing is a snapshot. Use the first chunk's size for the live total.
    const photo = { ...entry, path: "photo.png", name: "photo.png", size: 40 * 1024 * 1024 }
    const reads: {
      finish: (result: Partial<RemoteResponse>) => void
      slow: () => void
      signal: AbortSignal
    }[] = []
    const remote = link(false, [photo])
    remote.rpc = (request, options) => {
      if (request.op === "files") return Promise.resolve({ v: 1, id: "", ok: true, files: [photo] })
      return new Promise((resolve) => {
        reads.push({
          finish: (result) => resolve({ v: 1, id: "", ok: true, ...result }),
          slow: options!.onSlow!,
          signal: options!.signal!,
        })
      })
    }
    const send = (offset: number) => reads[offset].finish({
      file_chunk: { path: photo.path, name: photo.name, size: 3, mime: "image/png",
        offset, next_offset: offset + 1, data: btoa("a"), more: true },
    })

    render(<FileBrowser link={remote} threadId="t1" onClose={vi.fn()} />)
    fireEvent.click(await screen.findByRole("button", { name: photo.name }))
    const progress = screen.getByRole("progressbar", { name: t("files.progressLabel") })
    expect(progress).not.toHaveAttribute("value")
    await act(async () => send(0))
    expect(progress).toHaveAttribute("max", "3")
    expect(progress).toHaveAttribute("value", "1")
    act(() => reads[1].slow())
    expect(screen.getByRole("status")).toHaveTextContent(t("files.waiting"))
    expect(progress).toHaveAttribute("value", "1")
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()

    await act(async () => send(1))
    expect(screen.getByRole("status")).toHaveTextContent(t("files.loading"))
    expect(progress).toHaveAttribute("value", "2")
    await act(async () => reads[2].finish({ ok: false, code: "not_found", error: "file removed on PC" }))
    expect(screen.getByRole("alert")).toHaveTextContent("file removed on PC")
    expect(progress).not.toBeInTheDocument()
  })

  it("cancels a queued download and read when leaving the preview", async () => {
    nativeSave.enabled = true
    const remote = link(false, [entry])
    const list = remote.rpc
    let readSignal: AbortSignal | undefined
    let finish!: (result: RemoteResponse) => void
    const read = vi.fn(() => new Promise<RemoteResponse>((resolve) => { finish = resolve }))
    remote.rpc = (req, options) => {
      if (req.op === "files") return list(req)
      readSignal = options?.signal
      return read()
    }
    render(<FileBrowser link={remote} threadId="t1" onClose={vi.fn()} />)
    fireEvent.click(await screen.findByRole("button", { name: entry.name }))
    fireEvent.click(screen.getByRole("button", { name: t("files.download") }))
    fireEvent.click(screen.getByRole("button", { name: t("files.backList") }))
    expect(readSignal?.aborted).toBe(true)
    await act(async () => finish({ v: 1, id: "", ok: true, file_chunk: {
      path: entry.path, name: entry.name, mime: "text/html", size: 3,
      offset: 0, next_offset: 1, data: btoa("a"), more: true,
    } }))
    expect(read).toHaveBeenCalledOnce()
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
    expect(URL.createObjectURL).not.toHaveBeenCalled()
    expect(nativeSave.saveFile).not.toHaveBeenCalled()
  })

  it("tells the user when a paired PC is too old for file browsing", async () => {
    render(<FileBrowser link={link(true)} threadId="t1" onClose={vi.fn()} />)
    expect(await screen.findByRole("alert")).toHaveTextContent("Update the PC first")
  })
})
