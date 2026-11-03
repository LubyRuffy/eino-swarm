import { bytesToDataUrl, mimeFromName } from "./paths"
import { relationPath, relationshipId, relationships } from "./relations"
import type { DeckPreview, DocBlock, SlidePreview } from "./types"
import { readZip, zipText } from "./zip"
import { attrLocal, elementsByLocal, parseXml } from "./xml"

function shapeText(shape: Element): string[] {
  return elementsByLocal(shape, "p").map((paragraph) => elementsByLocal(paragraph, "t").map((text) => text.textContent ?? "").join("")).filter((text) => text.trim() !== "")
}

function isTitle(shape: Element): boolean {
  const kind = elementsByLocal(shape, "ph").map((item) => item.getAttribute("type") ?? "").join(" ")
  return /\b(?:title|ctrTitle)\b/.test(kind)
}

async function slideImages(slide: ParentNode, rels: ReturnType<typeof relationships>, files: Map<string, Uint8Array>, base: string): Promise<DocBlock[]> {
  const blocks: DocBlock[] = []
  for (const blip of elementsByLocal(slide, "blip")) {
    const path = relationPath(base, rels.get(attrLocal(blip, "embed")))
    const bytes = path ? files.get(path) : undefined
    const mime = path ? mimeFromName(path) : ""
    if (!bytes || !mime.startsWith("image/")) continue
    const descr = elementsByLocal(slide, "cNvPr").find((item) => item.getAttribute("descr"))
    blocks.push({ kind: "image", alt: descr?.getAttribute("descr") ?? "", dataUrl: bytesToDataUrl(mime, bytes) })
  }
  return blocks
}

async function parseSlide(files: Map<string, Uint8Array>, path: string, index: number): Promise<SlidePreview> {
  const xml = zipText(files, path)
  if (!xml) throw new Error("slide is missing")
  const slide = parseXml(xml)
  const rels = relationships(zipText(files, path.replace(/([^/]+)$/, "_rels/$1.rels")) ?? "")
  const blocks: DocBlock[] = []
  let title = ""
  for (const shape of elementsByLocal(slide, "sp")) {
    const lines = shapeText(shape)
    if (lines.length === 0) continue
    if (!title && isTitle(shape)) {
      title = lines[0]
      for (const line of lines.slice(1)) blocks.push({ kind: "paragraph", runs: [{ text: line }] })
      continue
    }
    for (const line of lines) blocks.push({ kind: "paragraph", runs: [{ text: line }] })
  }
  blocks.push(...await slideImages(slide, rels, files, path))
  return { title: title || `Slide ${index + 1}`, blocks }
}

/** Slides keep document order of text and embedded pictures. Drawing
 * coordinates are not reconstructed. */
export async function parsePptx(bytes: Uint8Array): Promise<DeckPreview> {
  const files = await readZip(bytes)
  const presentation = zipText(files, "ppt/presentation.xml")
  if (!presentation) throw new Error("presentation is missing")
  const rels = relationships(zipText(files, "ppt/_rels/presentation.xml.rels") ?? "")
  const slides: SlidePreview[] = []
  for (const id of elementsByLocal(parseXml(presentation), "sldId")) {
    const path = relationPath("ppt/presentation.xml", rels.get(relationshipId(id)))
    if (!path) continue
    slides.push(await parseSlide(files, path, slides.length))
  }
  if (slides.length === 0) throw new Error("presentation has no slides")
  return { slides }
}
