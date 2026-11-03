import { bytesToDataUrl, mimeFromName } from "./paths"
import { relationPath, relationships, type ZipRelation } from "./relations"
import type { DocBlock, DocPreview, InlineRun } from "./types"
import { readZip, zipText } from "./zip"
import { attrLocal, directChildren, elementsByLocal } from "./xml"

function runsIn(paragraph: Element, rels: Map<string, ZipRelation>): InlineRun[] {
  const runs: InlineRun[] = []
  const push = (text: string, href?: string) => {
    if (!text) return
    const last = runs[runs.length - 1]
    if (last && last.href === href) last.text += text
    else runs.push(href ? { text, href } : { text })
  }
  const walk = (node: Element, href?: string) => {
    const local = node.localName
    if (local === "drawing" || local === "pict" || local === "object") return
    if (local === "hyperlink") {
      const rel = rels.get(attrLocal(node, "id"))
      const next = rel?.external && /^https?:\/\//i.test(rel.target) ? rel.target : undefined
      for (const child of node.children) walk(child, next)
      return
    }
    if (local === "t") {
      push(node.textContent ?? "", href)
      return
    }
    if (local === "tab") {
      push("\t", href)
      return
    }
    if (local === "br" || local === "cr") {
      push("\n", href)
      return
    }
    for (const child of node.children) walk(child, href)
  }
  for (const child of paragraph.children) walk(child)
  return runs
}

function headingLevel(paragraph: Element): number | null {
  const style = directChildren(paragraph, "pPr").flatMap((props) => directChildren(props, "pStyle")).map((item) => attrLocal(item, "val")).join(" ")
  const named = /heading\s*([1-6])/i.exec(style) || /^h([1-6])$/i.exec(style)
  if (named) return Number(named[1])
  if (/^title$/i.test(style)) return 1
  const outline = directChildren(paragraph, "pPr").flatMap((props) => directChildren(props, "outlineLvl")).map((item) => attrLocal(item, "val"))[0]
  if (outline !== undefined && outline !== "" && Number(outline) >= 0 && Number(outline) < 6) return Number(outline) + 1
  return null
}

function numId(paragraph: Element): string {
  const num = directChildren(paragraph, "pPr").flatMap((props) => directChildren(props, "numPr")).flatMap((props) => directChildren(props, "numId"))[0]
  return num ? attrLocal(num, "val") : ""
}

function orderedIds(xml: string | null): Set<string> {
  const ordered = new Set<string>()
  if (!xml) return ordered
  const doc = new DOMParser().parseFromString(xml, "application/xml")
  const formats = new Map<string, string>()
  for (const abstract of elementsByLocal(doc, "abstractNum")) {
    const id = attrLocal(abstract, "abstractNumId")
    const fmt = elementsByLocal(abstract, "numFmt")[0]
    if (id && fmt) formats.set(id, attrLocal(fmt, "val"))
  }
  for (const num of elementsByLocal(doc, "num")) {
    const id = attrLocal(num, "numId")
    const abstract = elementsByLocal(num, "abstractNumId")[0]
    const fmt = abstract ? formats.get(attrLocal(abstract, "val")) : ""
    if (id && fmt && /decimal|ordinal|cardinal/i.test(fmt)) ordered.add(id)
  }
  return ordered
}

function paragraphText(paragraph: Element): string {
  return runsIn(paragraph, new Map()).map((run) => run.text).join("")
}

function tableBlock(table: Element): DocBlock {
  const rows: string[][] = []
  let header = false
  for (const tr of directChildren(table, "tr")) {
    if (directChildren(tr, "trPr").some((props) => directChildren(props, "tblHeader").length > 0)) header = rows.length === 0
    const cells = directChildren(tr, "tc").map((cell) => directChildren(cell, "p").map(paragraphText).filter(Boolean).join("\n"))
    rows.push(cells)
  }
  return { kind: "table", header: header && rows.length > 0, rows }
}

async function imagesIn(scope: Element, rels: Map<string, ZipRelation>, files: Map<string, Uint8Array>, base: string): Promise<DocBlock[]> {
  const blocks: DocBlock[] = []
  for (const node of elementsByLocal(scope, "blip").concat(elementsByLocal(scope, "imagedata"))) {
    const rel = rels.get(attrLocal(node, node.localName === "blip" ? "embed" : "id"))
    const path = relationPath(base, rel)
    const bytes = path ? files.get(path) : undefined
    const mime = path ? mimeFromName(path) : ""
    if (!bytes || !mime.startsWith("image/")) continue
    const drawing = node.closest?.("drawing, pict") ?? node.parentElement
    const descr = drawing ? elementsByLocal(drawing, "docPr")[0] : undefined
    blocks.push({
      kind: "image",
      alt: descr ? (descr.getAttribute("descr") || descr.getAttribute("name") || "") : "",
      dataUrl: bytesToDataUrl(mime, bytes),
    })
  }
  return blocks
}

function appendParagraph(blocks: DocBlock[], paragraph: Element, rels: Map<string, ZipRelation>, ordered: Set<string>, list: { ordered: boolean; items: InlineRun[][] } | null): { ordered: boolean; items: InlineRun[][] } | null {
  const id = numId(paragraph)
  const runs = runsIn(paragraph, rels)
  if (id && id !== "0") {
    const isOrdered = ordered.has(id)
    const current = list && list.ordered === isOrdered ? list : { ordered: isOrdered, items: [] }
    if (list && current !== list) blocks.push({ kind: "list", ordered: list.ordered, items: list.items })
    current.items.push(runs.length ? runs : [{ text: "" }])
    return current
  }
  if (list) blocks.push({ kind: "list", ordered: list.ordered, items: list.items })
  const text = runs.map((run) => run.text).join("")
  const level = headingLevel(paragraph)
  if (level && text.trim()) blocks.push({ kind: "heading", level, text })
  else if (text.trim()) blocks.push({ kind: "paragraph", runs })
  return null
}

async function blocksFrom(container: Element, rels: Map<string, ZipRelation>, files: Map<string, Uint8Array>, base: string, ordered: Set<string>): Promise<DocBlock[]> {
  const blocks: DocBlock[] = []
  let list: { ordered: boolean; items: InlineRun[][] } | null = null
  const flush = () => {
    if (!list) return
    blocks.push({ kind: "list", ordered: list.ordered, items: list.items })
    list = null
  }
  for (const child of container.children) {
    if (child.localName === "p") {
      list = appendParagraph(blocks, child, rels, ordered, list)
      const images = await imagesIn(child, rels, files, base)
      if (images.length) {
        flush()
        blocks.push(...images)
      }
    } else if (child.localName === "tbl") {
      flush()
      blocks.push(tableBlock(child))
      blocks.push(...await imagesIn(child, rels, files, base))
    } else if (child.localName === "sdt") {
      flush()
      const content = directChildren(child, "sdtContent")[0]
      if (content) blocks.push(...await blocksFrom(content, rels, files, base, ordered))
    }
  }
  flush()
  return blocks
}

export async function parseDocx(bytes: Uint8Array): Promise<DocPreview> {
  const files = await readZip(bytes)
  const xml = zipText(files, "word/document.xml")
  if (!xml) throw new Error("document body is missing")
  const doc = new DOMParser().parseFromString(xml, "application/xml")
  const body = elementsByLocal(doc, "body")[0]
  if (!body) throw new Error("document body is missing")
  const rels = relationships(zipText(files, "word/_rels/document.xml.rels") ?? "")
  const blocks = await blocksFrom(body, rels, files, "word/document.xml", orderedIds(zipText(files, "word/numbering.xml")))
  return { blocks }
}
