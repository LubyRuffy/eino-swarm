import { buildHtmlArtifact, type AssetReader } from "./html-artifact"
import { resolvePath } from "./paths"
import type { BookPreview, ChapterPreview } from "./types"
import { readZip, zipText } from "./zip"
import { elementsByLocal, parseXml } from "./xml"

function zipReader(files: Map<string, Uint8Array>): AssetReader {
  return async (path) => files.get(path) ?? null
}

/** Chapters are static HTML from the package. Scripts are removed and the
 * iframe sandbox allows none, so package script cannot run. */
export async function parseEpub(bytes: Uint8Array): Promise<BookPreview> {
  const files = await readZip(bytes)
  const container = zipText(files, "META-INF/container.xml")
  if (!container) throw new Error("epub container is missing")
  const rootfile = elementsByLocal(parseXml(container), "rootfile")[0]?.getAttribute("full-path") ?? ""
  const opfPath = resolvePath("/", rootfile)
  const opf = opfPath ? zipText(files, opfPath) : null
  if (!opfPath || !opf) throw new Error("epub package is missing")
  const pkg = parseXml(opf)
  const title = elementsByLocal(pkg, "title")[0]?.textContent?.trim() || "Book"
  const manifest = new Map<string, { href: string; type: string }>()
  for (const item of elementsByLocal(pkg, "item")) {
    const id = item.getAttribute("id") ?? ""
    const href = item.getAttribute("href") ?? ""
    if (id && href) manifest.set(id, { href, type: (item.getAttribute("media-type") ?? "").toLowerCase() })
  }
  const read = zipReader(files)
  const chapters: ChapterPreview[] = []
  for (const ref of elementsByLocal(pkg, "itemref")) {
    const item = manifest.get(ref.getAttribute("idref") ?? "")
    if (!item || !/html|xhtml/.test(item.type)) continue
    const path = resolvePath(opfPath, item.href)
    const html = path ? zipText(files, path) : null
    if (!path || !html) continue
    const srcdoc = await buildHtmlArtifact(html, path, "", read, new AbortController().signal, { scripts: false })
    const parsed = new DOMParser().parseFromString(html, "text/html")
    const chapterTitle = parsed.querySelector("title")?.textContent?.trim() || item.href.split("/").pop() || `Chapter ${chapters.length + 1}`
    chapters.push({ title: chapterTitle, srcdoc })
  }
  if (chapters.length === 0) throw new Error("epub has no chapters")
  return { title, chapters }
}
