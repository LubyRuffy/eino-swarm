import { Loader2 } from "lucide-react"
import { useEffect, useState } from "react"

import { Button } from "@/components/ui/button"
import { limitCells, parseDelimited } from "@/lib/documents/delimited"
import { buildHtmlArtifact, HTML_ARTIFACT_SANDBOX, PACKAGE_DOCUMENT_SANDBOX } from "@/lib/documents/html-artifact"
import type { DocumentFormat } from "@/lib/documents/kind"
import { parseDocx } from "@/lib/documents/docx"
import { parseEpub } from "@/lib/documents/epub"
import { parsePptx } from "@/lib/documents/pptx"
import { parseXlsx } from "@/lib/documents/xlsx"
import type { BookPreview, DeckPreview, DocBlock, DocPreview, InlineRun, WorkbookPreview } from "@/lib/documents/types"
import { readWorkspaceBytes } from "@/lib/documents/workspace-bytes"
import { useT } from "@/lib/use-t"

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
        <tbody>
          {body.map((row, rowIndex) => (
            <tr key={rowIndex}>{Array.from({ length: width }, (_, index) => <td key={index}>{row[index] ?? ""}</td>)}</tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function Blocks({ blocks }: { blocks: DocBlock[] }) {
  return (
    <article className="md md-body">
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

function Chooser({ label, names, index, onChange }: { label: string; names: string[]; index: number; onChange: (index: number) => void }) {
  const t = useT()
  if (names.length <= 1) return null
  return (
    <div className="mb-3 flex flex-wrap items-center gap-2">
      <Button size="sm" variant="ghost" disabled={index === 0} onClick={() => onChange(index - 1)}>{t("files.previous")}</Button>
      <label className="text-xs text-muted-foreground">
        <span className="sr-only">{label}</span>
        <select aria-label={label} className="rounded-md border border-input bg-background px-2 py-1 text-sm text-foreground" value={index} onChange={(event) => onChange(Number(event.target.value))}>
          {names.map((name, item) => <option key={`${item}-${name}`} value={item}>{name}</option>)}
        </select>
      </label>
      <Button size="sm" variant="ghost" disabled={index === names.length - 1} onClick={() => onChange(index + 1)}>{t("files.next")}</Button>
    </div>
  )
}

export function TableFilePreview({ text, delimiter }: { text: string; delimiter: "," | "\t" }) {
  const t = useT()
  const grid = limitCells(parseDelimited(text, delimiter))
  if (grid.rows.length === 0) return <p className="text-sm text-muted-foreground">{t("files.emptyFile")}</p>
  return (
    <div data-testid="table-preview">
      {grid.truncated ? <p className="mb-3 text-sm text-muted-foreground">{t("files.previewLimited", { shown: grid.shown, total: grid.total })}</p> : null}
      <article className="md md-body"><DataTable rows={grid.rows} header /></article>
    </div>
  )
}

export function HtmlFilePreview({ threadId, path, html }: { threadId: string; path: string; html: string }) {
  const t = useT()
  const [srcDoc, setSrcDoc] = useState("")
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setSrcDoc("")
    setFailed(false)
    void buildHtmlArtifact(html, path, window.location.origin, (asset, signal) => readWorkspaceBytes(threadId, asset, signal), controller.signal, { scripts: true })
      .then((doc) => { if (active) setSrcDoc(doc) })
      .catch(() => { if (active && !controller.signal.aborted) setFailed(true) })
    return () => { active = false; controller.abort() }
  }, [threadId, path, html])
  if (failed) return <p className="text-sm text-muted-foreground">{t("files.unsupported")}</p>
  if (!srcDoc) return <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />{t("files.loading")}</p>
  const name = path.slice(path.lastIndexOf("/") + 1)
  return <iframe data-testid="html-artifact" title={name} sandbox={HTML_ARTIFACT_SANDBOX} referrerPolicy="no-referrer" srcDoc={srcDoc} className="min-h-0 w-full flex-1 border-0 bg-white" />
}

type Loaded = { format: "docx"; doc: DocPreview } | { format: "xlsx"; doc: WorkbookPreview } | { format: "pptx"; doc: DeckPreview } | { format: "epub"; doc: BookPreview }

async function loadDocument(format: DocumentFormat, bytes: Uint8Array): Promise<Loaded> {
  if (format === "docx") return { format, doc: await parseDocx(bytes) }
  if (format === "xlsx") return { format, doc: await parseXlsx(bytes) }
  if (format === "pptx") return { format, doc: await parsePptx(bytes) }
  return { format, doc: await parseEpub(bytes) }
}

export function PackageFilePreview({ format, bytes }: { format: DocumentFormat; bytes: Uint8Array }) {
  const t = useT()
  const [loaded, setLoaded] = useState<Loaded | null>(null)
  const [failed, setFailed] = useState(false)
  const [index, setIndex] = useState(0)
  useEffect(() => {
    let active = true
    setLoaded(null)
    setFailed(false)
    setIndex(0)
    void loadDocument(format, bytes).then((value) => { if (active) setLoaded(value) }).catch(() => { if (active) setFailed(true) })
    return () => { active = false }
  }, [format, bytes])
  if (failed) return <p className="text-sm text-muted-foreground">{t("files.unsupported")}</p>
  if (!loaded) return <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />{t("files.loading")}</p>
  if (loaded.format === "docx") {
    if (loaded.doc.blocks.length === 0) return <p className="text-sm text-muted-foreground">{t("files.emptyFile")}</p>
    return <div data-testid="document-preview"><Blocks blocks={loaded.doc.blocks} /></div>
  }
  if (loaded.format === "xlsx") {
    const sheet = loaded.doc.sheets[index] ?? loaded.doc.sheets[0]
    return (
      <div data-testid="document-preview">
        <Chooser label={t("files.sheets")} names={loaded.doc.sheets.map((item) => item.name)} index={index} onChange={setIndex} />
        {sheet ? <article className="md md-body"><DataTable rows={sheet.rows} header /></article> : null}
        {loaded.doc.truncated ? <p className="mt-3 text-sm text-muted-foreground">{t("files.previewCapped", { shown: loaded.doc.shown })}</p> : null}
      </div>
    )
  }
  if (loaded.format === "pptx") {
    const slide = loaded.doc.slides[index] ?? loaded.doc.slides[0]
    return (
      <div data-testid="document-preview">
        <Chooser label={t("files.slides")} names={loaded.doc.slides.map((item) => item.title)} index={index} onChange={setIndex} />
        {slide ? (
          <section className="rounded-md border bg-card p-6">
            <h2 className="mb-3 text-lg font-semibold">{slide.title}</h2>
            <Blocks blocks={slide.blocks} />
          </section>
        ) : null}
      </div>
    )
  }
  const chapter = loaded.doc.chapters[index] ?? loaded.doc.chapters[0]
  return (
    <div data-testid="document-preview" className="flex min-h-0 flex-1 flex-col">
      <Chooser label={t("files.chapters")} names={loaded.doc.chapters.map((item) => item.title)} index={index} onChange={setIndex} />
      {chapter ? <iframe title={chapter.title} sandbox={PACKAGE_DOCUMENT_SANDBOX} referrerPolicy="no-referrer" srcDoc={chapter.srcdoc} className="min-h-0 w-full flex-1 border-0 bg-white" /> : null}
    </div>
  )
}
