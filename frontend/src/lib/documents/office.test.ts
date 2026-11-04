import { parseDocx } from "./docx"
import { parseEpub } from "./epub"
import { PACKAGE_DOCUMENT_SANDBOX } from "./html-artifact"
import { parsePptx } from "./pptx"
import { parseXlsx } from "./xlsx"
import { zipStore } from "./zip-store"

const png = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jWZkAAAAASUVORK5CYII="), (char) => char.charCodeAt(0))

describe("office and book preview", () => {
  it("reads a docx body, list, table, link and embedded image", async () => {
    const doc = await parseDocx(zipStore([
      { name: "word/document.xml", data: `<?xml version="1.0" encoding="UTF-8"?>
        <w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
          <w:body>
            <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Title</w:t></w:r></w:p>
            <w:p><w:r><w:t>Hello </w:t></w:r><w:hyperlink r:id="rId2"><w:r><w:t>link</w:t></w:r></w:hyperlink></w:p>
            <w:p><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>One</w:t></w:r></w:p>
            <w:p><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Two</w:t></w:r></w:p>
            <w:tbl>
              <w:tr><w:trPr><w:tblHeader/></w:trPr><w:tc><w:p><w:r><w:t>H</w:t></w:r></w:p></w:tc></w:tr>
              <w:tr><w:tc><w:p><w:r><w:t>C</w:t></w:r></w:p></w:tc></w:tr>
            </w:tbl>
            <w:p><w:r><w:drawing><w:docPr descr="pixel"/><a:blip r:embed="rId1"/></w:drawing></w:r></w:p>
          </w:body>
        </w:document>` },
      { name: "word/_rels/document.xml.rels", data: `<?xml version="1.0" encoding="UTF-8"?>
        <Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
          <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/pixel.png"/>
          <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/doc" TargetMode="External"/>
          <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="https://example.invalid/pixel.png" TargetMode="External"/>
        </Relationships>` },
      { name: "word/numbering.xml", data: `<?xml version="1.0" encoding="UTF-8"?>
        <w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
          <w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/></w:lvl></w:abstractNum>
          <w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
        </w:numbering>` },
      { name: "word/media/pixel.png", data: png },
    ]))
    expect(doc.blocks).toEqual([
      { kind: "heading", level: 1, text: "Title" },
      { kind: "paragraph", runs: [{ text: "Hello " }, { text: "link", href: "https://example.com/doc" }] },
      { kind: "list", ordered: true, items: [[{ text: "One" }], [{ text: "Two" }]] },
      { kind: "table", header: true, rows: [["H"], ["C"]] },
      { kind: "image", alt: "pixel", dataUrl: expect.stringMatching(/^data:image\/png;base64,/) },
    ])
    expect(JSON.stringify(doc)).not.toContain("example.invalid")
  })

  it("reads cached sheet values and skips an external sheet", async () => {
    const book = await parseXlsx(zipStore([
      { name: "xl/workbook.xml", data: `<?xml version="1.0" encoding="UTF-8"?>
        <workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
          <sheets>
            <sheet name="Data" sheetId="1" r:id="rId1"/>
            <sheet name="Remote" sheetId="2" r:id="rId2"/>
          </sheets>
        </workbook>` },
      { name: "xl/_rels/workbook.xml.rels", data: `<?xml version="1.0" encoding="UTF-8"?>
        <Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
          <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
          <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/externalLinkPath" Target="https://example.invalid/book.xlsx" TargetMode="External"/>
        </Relationships>` },
      { name: "xl/sharedStrings.xml", data: `<?xml version="1.0" encoding="UTF-8"?>
        <sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>Hello</t></si></sst>` },
      { name: "xl/worksheets/sheet1.xml", data: `<?xml version="1.0" encoding="UTF-8"?>
        <worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
          <sheetData>
            <row r="1">
              <c r="A1" t="s"><v>0</v></c>
              <c r="B1"><v>2</v></c>
              <c r="C1"><f>A1+1</f><v>3</v></c>
              <c r="D1"><f>SUM(1,1)</f></c>
              <c r="E1" t="inlineStr"><is><t>Now</t></is></c>
            </row>
          </sheetData>
        </worksheet>` },
    ]))
    expect(book.sheets.map((sheet) => sheet.name)).toEqual(["Data"])
    expect(book.sheets[0].rows).toEqual([["Hello", "2", "3", "=SUM(1,1)", "Now"]])
    expect(book.truncated).toBe(false)
  })

  it("reads slide text and an embedded picture in order", async () => {
    const deck = await parsePptx(zipStore([
      { name: "ppt/presentation.xml", data: `<?xml version="1.0" encoding="UTF-8"?>
        <p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
          <p:sldIdLst>
            <p:sldId id="256" r:id="rId2"/>
            <p:sldId id="257" r:id="rId3"/>
          </p:sldIdLst>
        </p:presentation>` },
      { name: "ppt/_rels/presentation.xml.rels", data: `<?xml version="1.0" encoding="UTF-8"?>
        <Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
          <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>
          <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/>
        </Relationships>` },
      { name: "ppt/slides/slide1.xml", data: `<?xml version="1.0" encoding="UTF-8"?>
        <p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
          <p:cSld><p:spTree>
            <p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Cover</a:t></a:r></a:p><a:p><a:r><a:t>Subtitle</a:t></a:r></a:p></p:txBody></p:sp>
            <p:sp><p:txBody><a:p><a:r><a:t>Body</a:t></a:r></a:p></p:txBody></p:sp>
          </p:spTree></p:cSld>
        </p:sld>` },
      { name: "ppt/slides/_rels/slide1.xml.rels", data: `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>` },
      { name: "ppt/slides/slide2.xml", data: `<?xml version="1.0" encoding="UTF-8"?>
        <p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
          <p:cSld><p:spTree>
            <p:pic><p:nvPicPr><p:cNvPr id="2" descr="pixel"/></p:nvPicPr><p:blipFill><a:blip r:embed="rId1"/></p:blipFill></p:pic>
          </p:spTree></p:cSld>
        </p:sld>` },
      { name: "ppt/slides/_rels/slide2.xml.rels", data: `<?xml version="1.0"?>
        <Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
          <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/pixel.png"/>
        </Relationships>` },
      { name: "ppt/media/pixel.png", data: png },
    ]))
    expect(deck.slides).toEqual([
      { title: "Cover", blocks: [{ kind: "paragraph", runs: [{ text: "Subtitle" }] }, { kind: "paragraph", runs: [{ text: "Body" }] }] },
      { title: "Slide 2", blocks: [{ kind: "image", alt: "pixel", dataUrl: expect.stringMatching(/^data:image\/png;base64,/) }] },
    ])
  })

  it("renders an epub chapter from package files without scripts", async () => {
    const book = await parseEpub(zipStore([
      { name: "META-INF/container.xml", data: `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>` },
      { name: "OEBPS/content.opf", data: `<?xml version="1.0"?>
        <package xmlns="http://www.idpf.org/2007/opf" xmlns:dc="http://purl.org/dc/elements/1.1/">
          <metadata><dc:title>Sample Book</dc:title></metadata>
          <manifest>
            <item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/>
            <item id="css" href="style.css" media-type="text/css"/>
            <item id="img" href="cover.png" media-type="image/png"/>
          </manifest>
          <spine><itemref idref="c1"/></spine>
        </package>` },
      { name: "OEBPS/style.css", data: "p { color: rgb(4, 5, 6); }" },
      { name: "OEBPS/cover.png", data: png },
      { name: "OEBPS/c1.xhtml", data: `<html><head><title>Chapter One</title><link rel="stylesheet" href="style.css"><link rel="preconnect" href="https://example.org/preconnect"></head><body><p>Readable</p><img src="cover.png"><script>bad()</script><a href="#note">Note</a><a href="https://example.org/outside">Outside</a><iframe src="https://example.org/nested"></iframe></body></html>` },
    ]))
    expect(book.title).toBe("Sample Book")
    expect(book.chapters).toHaveLength(1)
    expect(book.chapters[0].title).toBe("Chapter One")
    expect(book.chapters[0].srcdoc).toContain("Readable")
    expect(book.chapters[0].srcdoc).toContain("rgb(4, 5, 6)")
    expect(book.chapters[0].srcdoc).toContain("data:image/png;base64,")
    expect(book.chapters[0].srcdoc).not.toContain("bad()")
    expect(book.chapters[0].srcdoc).toContain('href="#note"')
    expect(book.chapters[0].srcdoc).not.toContain("https://example.org/")
    expect(PACKAGE_DOCUMENT_SANDBOX).toBe("")
  })
})
