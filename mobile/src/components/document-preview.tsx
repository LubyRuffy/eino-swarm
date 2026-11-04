import { useEffect, useState } from "react"
import { Loader2 } from "lucide-react"

import { TextFilePreview } from "@/components/text-file-preview"
import { Button } from "@/components/ui/button"
import { limitCells, parseDelimited } from "../../../frontend/src/lib/documents/delimited"
import { buildHtmlArtifact, HTML_ARTIFACT_SANDBOX, type AssetReader } from "../../../frontend/src/lib/documents/html-artifact"
import { documentFormat, isHtmlPreviewPath, tableDelimiter } from "../../../frontend/src/lib/documents/kind"
import { parseDocx } from "../../../frontend/src/lib/documents/docx"
import { parsePptx } from "../../../frontend/src/lib/documents/pptx"
import { parseXlsx } from "../../../frontend/src/lib/documents/xlsx"
import type { DocBlock, InlineRun } from "../../../frontend/src/lib/documents/types"
import { t } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import { readWorkspaceFile } from "@/lib/remote-files"

function Runs({ runs }: { runs: InlineRun[] }) {
  return runs.map((run, index) => run.href
    ? <a key={index} href={run.href} target="_blank" rel="noreferrer">{run.text}</a>
    : <span key={index}>{run.text}</span>)
}

function DataTable({ rows, header }: { rows: string[][]; header: boolean }) {
  if (rows.length === 0) return null
  const width = rows.reduce((max, row) => Math.max(max, row.length), 0)
  const head = header ? rows[0] : undefined
  const body = header ? rows.slice(1) : rows
  return (
    <div className="md-table">
      <table>
        {head ? <thead><tr>{Array.from({ length: width }, (_, index) => <th key={index}>{head[index] ?? ""}</th>)}</tr></thead> : null}
        <tbody>{body.map((row, rowIndex) => <tr key={rowIndex}>{Array.from({ length: width }, (_, index) => <td key={index}>{row[index] ?? ""}</td>)}</tr>)}</tbody>
      </table>
    </div>
  )
}

function Blocks({ blocks }: { blocks: DocBlock[] }) {
  return (
    <article className="md-body">
      {blocks.map((block, index) => {
        if (block.kind === "heading") {
          const Tag = `h${Math.min(6, Math.max(1, block.level))}` as "h1"
          return <Tag key={index}>{block.text}</Tag>
        }
        if (block.kind === "paragraph") return <p key={index}><Runs runs={block.runs} /></p>
        if (block.kind === "list") {
          const Tag = block.ordered ? "ol" : "ul"
          return <Tag key={index}>{block.items.map((item, itemIndex) => <li key={itemIndex}><Runs runs={item} /></li>)}</Tag>
        }
        if (block.kind === "table") return <DataTable key={index} rows={block.rows} header={block.header} />
        return <img key={index} src={block.dataUrl} alt={block.alt} className="max-w-full" />
      })}
    </article>
  )
}

function Chooser({ label, names, index, onChange }: { label: string; names: string[]; index: number; onChange: (next: number) => void }) {
  if (names.length <= 1) return null
  return (
    <div className="mb-3 flex flex-wrap items-center gap-2">
      <Button variant="ghost" disabled={index === 0} onClick={() => onChange(index - 1)}>{t("files.previous")}</Button>
      <select aria-label={label} className="rounded-md border border-input bg-background px-2 py-1 text-sm" value={index} onChange={(event) => onChange(Number(event.target.value))}>
        {names.map((name, item) => <option key={`${item}-${name}`} value={item}>{name}</option>)}
      </select>
      <Button variant="ghost" disabled={index === names.length - 1} onClick={() => onChange(index + 1)}>{t("files.next")}</Button>
    </div>
  )
}

function readerFor(link: RemoteLink, threadId: string): AssetReader {
  return async (path, signal) => {
    if (signal.aborted) return null
    try {
      return (await readWorkspaceFile(link, threadId, path)).bytes
    } catch {
      return null
    }
  }
}

export function PhoneRichPreview({ link, threadId, path, text, bytes }: {
  link: RemoteLink
  threadId: string
  path: string
  text: string
  bytes: Uint8Array
}) {
  const format = documentFormat(path)
  const delimiter = tableDelimiter(path)
  const html = isHtmlPreviewPath(path)
  const [mode, setMode] = useState<"preview" | "source">("preview")
  const switchable = !format && (html || delimiter)
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      {switchable ? (
        <div role="group" aria-label={t("files.preview")} className="flex gap-1">
          <Button variant={mode === "preview" ? "outline" : "ghost"} aria-pressed={mode === "preview"} onClick={() => setMode("preview")}>{t("files.preview")}</Button>
          <Button variant={mode === "source" ? "outline" : "ghost"} aria-pressed={mode === "source"} onClick={() => setMode("source")}>{t("files.source")}</Button>
        </div>
      ) : null}
      {switchable && mode === "source" ? <TextFilePreview path={path} text={text} /> : format && format !== "epub" ? (
        <PackageView format={format} bytes={bytes} />
      ) : html ? (
        <HtmlView link={link} threadId={threadId} path={path} html={text} />
      ) : delimiter ? (
        <TableView text={text} delimiter={delimiter} />
      ) : null}
    </div>
  )
}

function TableView({ text, delimiter }: { text: string; delimiter: "," | "\t" }) {
  const grid = limitCells(parseDelimited(text, delimiter))
  if (grid.rows.length === 0) return <p className="text-sm text-muted-foreground">{t("files.emptyFile")}</p>
  return (
    <div data-testid="table-preview" className="min-h-0 flex-1 overflow-auto">
      {grid.truncated ? <p className="mb-3 text-sm text-muted-foreground">{t("files.previewLimited", { shown: grid.shown, total: grid.total })}</p> : null}
      <DataTable rows={grid.rows} header />
    </div>
  )
}

function HtmlView({ link, threadId, path, html }: { link: RemoteLink; threadId: string; path: string; html: string }) {
  const [srcDoc, setSrcDoc] = useState("")
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setSrcDoc("")
    setFailed(false)
    void buildHtmlArtifact(html, path, window.location.origin, readerFor(link, threadId), controller.signal, { scripts: true })
      .then((doc) => { if (active) setSrcDoc(doc) })
      .catch(() => { if (active && !controller.signal.aborted) setFailed(true) })
    return () => { active = false; controller.abort() }
  }, [link, threadId, path, html])
  if (failed) return <p className="text-sm text-muted-foreground">{t("files.unavailable")}</p>
  if (!srcDoc) return <p role="status" className="flex items-center gap-2 text-sm"><Loader2 className="size-4 animate-spin" />{t("files.loading")}</p>
  const name = path.slice(path.lastIndexOf("/") + 1)
  return <iframe data-testid="html-artifact" title={name} sandbox={HTML_ARTIFACT_SANDBOX} referrerPolicy="no-referrer" srcDoc={srcDoc} className="min-h-0 w-full flex-1 border-0 bg-white" />
}

function PackageView({ format, bytes }: { format: "docx" | "xlsx" | "pptx"; bytes: Uint8Array }) {
  const [index, setIndex] = useState(0)
  const [failed, setFailed] = useState(false)
  const [view, setView] = useState<
    | { kind: "docx"; blocks: DocBlock[] }
    | { kind: "xlsx"; names: string[]; rows: string[][][]; truncated: boolean; shown: number }
    | { kind: "pptx"; titles: string[]; slides: { title: string; blocks: DocBlock[] }[] }
    | null
  >(null)
  useEffect(() => {
    let active = true
    setFailed(false)
    setView(null)
    setIndex(0)
    void (async () => {
      if (format === "docx") return { kind: "docx" as const, blocks: (await parseDocx(bytes)).blocks }
      if (format === "xlsx") {
        const book = await parseXlsx(bytes)
        return { kind: "xlsx" as const, names: book.sheets.map((sheet) => sheet.name), rows: book.sheets.map((sheet) => sheet.rows), truncated: book.truncated, shown: book.shown }
      }
      const deck = await parsePptx(bytes)
      return { kind: "pptx" as const, titles: deck.slides.map((slide) => slide.title), slides: deck.slides }
    })().then((next) => { if (active) setView(next) }).catch(() => { if (active) setFailed(true) })
    return () => { active = false }
  }, [format, bytes])
  if (failed) return <p className="text-sm text-muted-foreground">{t("files.unavailable")}</p>
  if (!view) return <p role="status" className="flex items-center gap-2 text-sm"><Loader2 className="size-4 animate-spin" />{t("files.loading")}</p>
  if (view.kind === "docx") {
    if (view.blocks.length === 0) return <p className="text-sm text-muted-foreground">{t("files.emptyFile")}</p>
    return <div data-testid="document-preview" className="min-h-0 flex-1 overflow-auto"><Blocks blocks={view.blocks} /></div>
  }
  if (view.kind === "xlsx") {
    return (
      <div data-testid="document-preview" className="min-h-0 flex-1 overflow-auto">
        <Chooser label={t("files.sheets")} names={view.names} index={index} onChange={setIndex} />
        <DataTable rows={view.rows[index] ?? []} header />
        {view.truncated ? <p className="mt-3 text-sm text-muted-foreground">{t("files.previewCapped", { shown: view.shown })}</p> : null}
      </div>
    )
  }
  const slide = view.slides[index]
  return (
    <div data-testid="document-preview" className="min-h-0 flex-1 overflow-auto">
      <Chooser label={t("files.slides")} names={view.titles} index={index} onChange={setIndex} />
      {slide ? <section className="rounded-md border bg-card p-4"><h2 className="mb-3 text-lg font-semibold">{slide.title}</h2><Blocks blocks={slide.blocks} /></section> : null}
    </div>
  )
}
