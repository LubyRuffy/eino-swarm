import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { zipStore } from "../../../frontend/src/lib/documents/zip-store"
import type { RemoteLink } from "@/lib/link"
import { PhoneRichPreview } from "./document-preview"

const link = { path: "relay", alive: () => true, close: () => undefined, announceDevice: () => undefined, rpc: () => Promise.resolve({}) } as unknown as RemoteLink

function preview(path: string, text: string, bytes: Uint8Array = new Uint8Array()) {
  return render(<PhoneRichPreview link={link} threadId="t1" path={path} text={text} bytes={bytes} />)
}

describe("phone rich file preview", () => {
  it("renders a CSV table", () => {
    preview("data.csv", "Name,Qty\nAda,2\n")
    expect(screen.getByRole("columnheader", { name: "Name" })).toBeInTheDocument()
    expect(screen.getByRole("cell", { name: "Ada" })).toBeInTheDocument()
  })

  it("renders a TSV table", () => {
    preview("data.tsv", "Name\tQty\nAda\t2\n")
    expect(screen.getByRole("columnheader", { name: "Name" })).toBeInTheDocument()
    expect(screen.getByRole("cell", { name: "Ada" })).toBeInTheDocument()
  })

  it("renders a DOCX heading", async () => {
    const bytes = zipStore([{
      name: "word/document.xml",
      data: '<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Preview title</w:t></w:r></w:p></w:body></w:document>',
    }])
    preview("note.docx", "", bytes)
    expect(await screen.findByRole("heading", { name: "Preview title" })).toBeInTheDocument()
  })

  it("renders saved spreadsheet values", async () => {
    const bytes = zipStore([
      { name: "xl/workbook.xml", data: '<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Data" sheetId="1" r:id="rId1"/></sheets></workbook>' },
      { name: "xl/_rels/workbook.xml.rels", data: '<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>' },
      { name: "xl/worksheets/sheet1.xml", data: '<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>Name</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>Ada</t></is></c></row></sheetData></worksheet>' },
    ])
    preview("book.xlsx", "", bytes)
    expect(await screen.findByRole("columnheader", { name: "Name" })).toBeInTheDocument()
    expect(screen.getByRole("cell", { name: "Ada" })).toBeInTheDocument()
  })

  it("renders slide text in deck order", async () => {
    const bytes = zipStore([
      { name: "ppt/presentation.xml", data: '<?xml version="1.0"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst><p:sldId id="256" r:id="rId2"/></p:sldIdLst></p:presentation>' },
      { name: "ppt/_rels/presentation.xml.rels", data: '<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/></Relationships>' },
      { name: "ppt/slides/slide1.xml", data: '<?xml version="1.0"?><p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Cover</a:t></a:r></a:p></p:txBody></p:sp><p:sp><p:txBody><a:p><a:r><a:t>Body</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>' },
    ])
    preview("deck.pptx", "", bytes)
    expect(await screen.findByRole("heading", { name: "Cover" })).toBeInTheDocument()
    expect(screen.getByText("Body")).toBeInTheDocument()
  })

  it("renders an EPUB chapter without its script", async () => {
    const bytes = zipStore([
      { name: "META-INF/container.xml", data: '<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>' },
      { name: "OEBPS/content.opf", data: '<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf"><manifest><item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c1"/></spine></package>' },
      { name: "OEBPS/c1.xhtml", data: "<html><head><title>Chapter One</title></head><body><p>Readable</p><script>bad()</script></body></html>" },
    ])
    preview("story.epub", "", bytes)
    const frame = await screen.findByTitle("Chapter One")
    expect(frame.getAttribute("sandbox")).toBe("")
    expect(frame.getAttribute("srcdoc")).toContain("Readable")
    expect(frame.getAttribute("srcdoc")).not.toContain("bad()")
  })
})