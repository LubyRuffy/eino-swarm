import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { FilePreviewDialog } from "./file-preview"
import { readFilePreview, type FilePreview } from "@/lib/file-preview"
import { copyText } from "@/lib/copy-text"
import { zipStore } from "@/lib/documents/zip-store"

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
afterEach(() => { cleanup(); vi.unstubAllGlobals() })

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

  it("lets readers zoom a loaded workspace image and return to fit", async () => {
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "image", blob: new Blob(["image"], { type: "image/png" }) })
    preview("plot.png")
    const image = await screen.findByRole("img", { name: "plot.png" })
    Object.defineProperties(image, { naturalWidth: { value: 1200 }, naturalHeight: { value: 800 } })
    fireEvent.load(image)
    await userEvent.click(screen.getByRole("button", { name: "Actual size" }))
    expect(image).toHaveStyle({ width: "1200px", height: "800px" })
    await userEvent.click(screen.getByRole("button", { name: "Zoom in" }))
    expect(parseFloat(image.style.width)).toBeGreaterThan(1200)
    await userEvent.click(screen.getByRole("button", { name: "Zoom out" }))
    expect(image).toHaveStyle({ width: "1200px", height: "800px" })
    await userEvent.click(screen.getByRole("button", { name: "Fit to window" }))
    expect(screen.getByRole("button", { name: "Fit to window" })).toHaveAttribute("aria-pressed", "true")
  })

  it("resets image zoom when refreshing or selecting another file", async () => {
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "image", blob: new Blob(["image"], { type: "image/png" }) })
    vi.mocked(URL.createObjectURL).mockReturnValueOnce("blob:first").mockReturnValueOnce("blob:refreshed").mockReturnValueOnce("blob:other")
    const view = preview("plot.png")
    async function load(name: string) {
      const image = await screen.findByRole("img", { name })
      Object.defineProperties(image, { naturalWidth: { value: 1200 }, naturalHeight: { value: 800 } })
      fireEvent.load(image)
    }
    await load("plot.png")
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }))
    expect(screen.getByRole("button", { name: "Fit to window" })).toHaveAttribute("aria-pressed", "false")
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
    await load("plot.png")
    expect(screen.getByRole("button", { name: "Fit to window" })).toHaveAttribute("aria-pressed", "true")
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }))
    view.rerender(<FilePreviewDialog {...base} path="other.png" />)
    await load("other.png")
    expect(screen.getByRole("button", { name: "Fit to window" })).toHaveAttribute("aria-pressed", "true")
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:first")
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:refreshed")
  })

  it("lets Escape close the dialog", async () => {
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "text", text: "ready" })
    preview("file.txt")
    await screen.findByTestId("file-source")
    await userEvent.keyboard("{Escape}")
    expect(base.onClose).toHaveBeenCalledOnce()
  })

  it("renders HTML in a sandboxed artifact and can show the original source", async () => {
    const text = '<p id="preview-script-target">Hello</p><script>parent.__ran = true</script>'
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "text", text })
    preview("page.html")
    const frame = await screen.findByTestId("html-artifact")
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts allow-forms allow-modals")
    expect(frame.getAttribute("sandbox")).not.toContain("allow-same-origin")
    expect(frame.getAttribute("srcdoc")).toContain("preview-script-target")
    expect(frame.getAttribute("srcdoc")).toContain("connect-src 'none'")
    expect(screen.getByTestId("file-preview").querySelector("script, #preview-script-target")).toBeNull()
    await userEvent.click(screen.getByRole("button", { name: "Source" }))
    expect(screen.getByTestId("file-source").textContent).toBe(text)
  })

  it("renders CSV as a table and can show the original text", async () => {
    const text = "Name,Qty\nA,2\n"
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "text", text })
    preview("data.csv")
    expect(await screen.findByRole("columnheader", { name: "Name" })).toBeInTheDocument()
    expect(screen.getByRole("cell", { name: "2" })).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Source" }))
    expect(screen.getByTestId("file-source").textContent).toBe(text)
  })

  it("renders a DOCX heading from package bytes", async () => {
    const bytes = zipStore([{
      name: "word/document.xml",
      data: '<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Preview title</w:t></w:r></w:p></w:body></w:document>',
    }])
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "document", format: "docx", bytes })
    preview("note.docx")
    expect(await screen.findByRole("heading", { name: "Preview title" })).toBeInTheDocument()
  })

  it("renders a TSV file as a table", async () => {
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "text", text: "Name\tQty\nAda\t2\n" })
    preview("data.tsv")
    expect(await screen.findByRole("columnheader", { name: "Name" })).toBeInTheDocument()
    expect(screen.getByRole("cell", { name: "Ada" })).toBeInTheDocument()
  })

  it("renders saved spreadsheet values", async () => {
    const bytes = zipStore([
      { name: "xl/workbook.xml", data: '<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Data" sheetId="1" r:id="rId1"/></sheets></workbook>' },
      { name: "xl/_rels/workbook.xml.rels", data: '<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>' },
      { name: "xl/worksheets/sheet1.xml", data: '<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>Name</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>Ada</t></is></c></row></sheetData></worksheet>' },
    ])
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "document", format: "xlsx", bytes })
    preview("book.xlsx")
    expect(await screen.findByRole("columnheader", { name: "Name" })).toBeInTheDocument()
    expect(screen.getByRole("cell", { name: "Ada" })).toBeInTheDocument()
  })

  it("renders slide text in deck order", async () => {
    const bytes = zipStore([
      { name: "ppt/presentation.xml", data: '<?xml version="1.0"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst><p:sldId id="256" r:id="rId2"/></p:sldIdLst></p:presentation>' },
      { name: "ppt/_rels/presentation.xml.rels", data: '<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/></Relationships>' },
      { name: "ppt/slides/slide1.xml", data: '<?xml version="1.0"?><p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Cover</a:t></a:r></a:p></p:txBody></p:sp><p:sp><p:txBody><a:p><a:r><a:t>Body</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>' },
    ])
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "document", format: "pptx", bytes })
    preview("deck.pptx")
    expect(await screen.findByRole("heading", { name: "Cover" })).toBeInTheDocument()
    expect(screen.getByText("Body")).toBeInTheDocument()
  })

  it("renders an EPUB chapter without its script", async () => {
    const bytes = zipStore([
      { name: "META-INF/container.xml", data: '<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>' },
      { name: "OEBPS/content.opf", data: '<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf"><manifest><item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c1"/></spine></package>' },
      { name: "OEBPS/c1.xhtml", data: "<html><head><title>Chapter One</title></head><body><p>Readable</p><script>bad()</script></body></html>" },
    ])
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "document", format: "epub", bytes })
    preview("story.epub")
    const frame = await screen.findByTitle("Chapter One")
    expect(frame.getAttribute("sandbox")).toBe("")
    expect(frame.getAttribute("srcdoc")).toContain("Readable")
    expect(frame.getAttribute("srcdoc")).not.toContain("bad()")
  })

  it("keeps download available when a document cannot be parsed", async () => {
    vi.mocked(readFilePreview).mockResolvedValue({ kind: "document", format: "docx", bytes: new Uint8Array([1, 2, 3]) })
    preview("broken.docx")
    expect(await screen.findByText("Preview is unavailable for this file type or encoding. Download it to view the content.")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Download" })).toBeInTheDocument()
  })
})
