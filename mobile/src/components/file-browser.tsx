import { useEffect, useMemo, useState } from "react"
import { TextFilePreview } from "@/components/text-file-preview"
import { ImageFilePreview } from "@/components/image-file-preview"
import { PhoneRichPreview } from "@/components/document-preview"
import { PhoneEpubPreview } from "@/components/epub-preview"
import { FileDownload } from "@/components/file-download"
import { ChevronDown, ChevronLeft, ChevronRight, FileText, Folder, Loader2, RefreshCw } from "lucide-react"

import { Button } from "@/components/ui/button"
import { installAndroidBack } from "@/lib/android-back"
import { t } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import { listWorkspaceFiles, previewKind, readWorkspaceFile, RemoteFileError } from "@/lib/remote-files"
import { documentFormat, isHtmlPreviewPath, tableDelimiter } from "../../../frontend/src/lib/documents/kind"
import type { WorkspaceFile } from "@/lib/rpc"

type LoadedFile = { url: string; kind: ReturnType<typeof previewKind>; text: string; bytes: Uint8Array; mime: string }
type FileProgress = { loaded: number; total?: number; waiting?: boolean }

function fileSize(size: number): string {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / (1024 * 1024)).toFixed(1)} MB`
}

function fileFailure(cause: unknown): string {
  if (cause instanceof RemoteFileError && cause.code === "unknown_op") return t("files.updatePC")
  if (cause instanceof RemoteFileError && cause.code === "file_too_large") return t("files.tooLarge")
  return cause instanceof Error ? cause.message : String(cause)
}

// Files arrive over the sealed PC link. HTML is a sandboxed artifact.
// EPUB stays script-free. Office packages are parsed, never executed.
export function FileBrowser({ link, threadId, onClose }: {
  link: RemoteLink
  threadId: string
  onClose: () => void
}) {
  const [files, setFiles] = useState<WorkspaceFile[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [revision, setRevision] = useState(0)
  const [query, setQuery] = useState("")
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const [selected, setSelected] = useState<WorkspaceFile | null>(null)
  const [file, setFile] = useState<LoadedFile | null>(null)
  const [readRevision, setReadRevision] = useState(0)
  const [progress, setProgress] = useState<FileProgress | null>(null)
  const [fileError, setFileError] = useState("")

  useEffect(() => {
    let active = true
    setLoading(true)
    setError("")
    void listWorkspaceFiles(link, threadId).then((rows) => {
      if (active) {
        setFiles(rows.sort((a, b) => a.path.localeCompare(b.path)))
        setCollapsed(new Set(rows.filter((entry) => entry.dir).map((entry) => entry.path)))
      }
    }).catch((cause: unknown) => {
      if (active) setError(fileFailure(cause))
    }).finally(() => {
      if (active) setLoading(false)
    })
    return () => { active = false }
  }, [link, threadId, revision])

  useEffect(() => {
    if (!selected) return
    const controller = new AbortController()
    const { signal } = controller
    let url = ""
    // Pending download intent belongs to this read, including across link replacement.
    setReadRevision((current) => current + 1)
    setFile(null)
    setProgress({ loaded: 0 })
    setFileError("")
    void readWorkspaceFile(link, threadId, selected.path, {
      signal,
      onProgress: (loaded, total) => setProgress({ loaded, total }),
      onSlow: () => setProgress((current) => current && { ...current, waiting: true }),
    }).then(({ bytes, mime }) => {
      if (signal.aborted) return
      const kind = previewKind(selected.name, mime, bytes.length)
      const safeMime = kind === "image" || kind === "pdf" ? mime : "application/octet-stream"
      url = URL.createObjectURL(new Blob([bytes], { type: safeMime }))
      const text = kind === "text" ? new TextDecoder().decode(bytes) : ""
      setFile({ url, kind, text, bytes, mime })
    }).catch((cause: unknown) => {
      if (!signal.aborted) setFileError(fileFailure(cause))
    }).finally(() => {
      if (!signal.aborted) setProgress(null)
    })
    return () => {
      controller.abort()
      if (url) URL.revokeObjectURL(url)
    }
  }, [link, threadId, selected])

  useEffect(() => installAndroidBack(() => {
    if (selected) setSelected(null)
    else onClose()
    return true
  }), [selected, onClose])

  const visible = useMemo(() => files.filter((entry) => {
    if (query && !entry.path.toLowerCase().includes(query.toLowerCase())) return false
    if (query) return true
    const parts = entry.path.split("/")
    for (let n = 1; n < parts.length; n++) {
      if (collapsed.has(parts.slice(0, n).join("/"))) return false
    }
    return true
  }), [files, query, collapsed])

  const toggle = (path: string) => setCollapsed((current) => {
    const next = new Set(current)
    if (next.has(path)) next.delete(path)
    else next.add(path)
    return next
  })

  const progressText = progress?.total ? t("files.progress", {
    percent: Math.floor(100 * progress.loaded / progress.total),
    loaded: fileSize(progress.loaded),
    total: fileSize(progress.total),
  }) : undefined
  const epub = selected && documentFormat(selected.name) === "epub"
  const previewStatus = <>
    {progress ? (
      <div className="space-y-2">
        <p role="status" className="flex items-center gap-2 text-sm">
          <Loader2 aria-hidden="true" className="size-4 animate-spin" />
          {t(progress.waiting ? "files.waiting" : "files.loading")}
        </p>
        <progress aria-label={t("files.progressLabel")} aria-valuetext={progressText}
          max={progress.total || undefined} value={progress.total ? progress.loaded : undefined}
          className="block h-2 w-full accent-primary" />
        {progressText ? <p className="text-xs tabular-nums text-muted-foreground">{progressText}</p> : null}
      </div>
    ) : null}
    {fileError ? <p role="alert" className="text-sm text-destructive">{fileError}</p> : null}
    {file?.kind === "download" ? <p className="text-sm text-muted-foreground">{t("files.unavailable")}</p> : null}
  </>
  const download = selected && !fileError ? <FileDownload key={readRevision} name={selected.name} file={file} /> : null

  return (
    <main data-testid="phone-file-browser" className="mx-auto flex h-full min-w-0 w-full max-w-lg flex-col overflow-hidden bg-background">
      <header className="flex h-12 shrink-0 items-center gap-1 border-b border-border px-1">
        <Button variant="ghost" className="size-10 shrink-0 px-0" onClick={() => selected ? setSelected(null) : onClose()}
          aria-label={selected ? t("files.backList") : t("thread.back")}>
          <ChevronLeft className="size-5" />
        </Button>
        <h1 className="min-w-0 flex-1 truncate text-sm font-medium">{selected?.name ?? t("thread.files")}</h1>
        {selected ? null : (
          <Button variant="ghost" className="size-10 shrink-0 px-0" onClick={() => setRevision((n) => n + 1)}
            aria-label={t("files.refresh")}><RefreshCw className="size-4" /></Button>
        )}
      </header>
      {selected ? (
        <section data-testid="phone-file-preview" className={`flex min-h-0 flex-1 flex-col overflow-hidden ${epub ? "" : "p-4"}`}>
          {epub ? (
            <PhoneEpubPreview key={selected.path} bytes={file?.kind === "document" ? file.bytes : undefined} status={previewStatus} download={download} />
          ) : <>
            <p className="mb-3 max-h-16 shrink-0 overflow-auto break-all text-xs text-muted-foreground">{selected.path}</p>
            {previewStatus}
            {file?.kind === "text" && (isHtmlPreviewPath(selected.path) || tableDelimiter(selected.path)) ? <PhoneRichPreview key={file.url} link={link} threadId={threadId} path={selected.path} text={file.text} bytes={file.bytes} /> : null}
            {file?.kind === "text" && !isHtmlPreviewPath(selected.path) && !tableDelimiter(selected.path) ? <TextFilePreview key={file.url} path={selected.path} text={file.text} /> : null}
            {file?.kind === "document" ? <PhoneRichPreview key={file.url} link={link} threadId={threadId} path={selected.path} text="" bytes={file.bytes} /> : null}
            {file?.kind === "image" ? <ImageFilePreview key={file.url} src={file.url} name={selected.name} /> : null}
            {file?.kind === "pdf" ? <iframe src={file.url} title={selected.name} sandbox="allow-scripts" className="min-h-0 w-full flex-1 border-0" /> : null}
            {download ? <div className="shrink-0">{download}</div> : null}
          </>}
        </section>
      ) : (
        <section className="flex min-h-0 flex-1 flex-col">
          <div className="border-b border-border p-3">
            <input value={query} onChange={(event) => setQuery(event.target.value)} aria-label={t("files.search")}
              placeholder={t("files.search")} className="w-full rounded-md border border-input bg-background px-3 py-2 text-base" />
          </div>
          {loading ? <p className="p-4 text-sm text-muted-foreground">{t("files.loading")}</p> : null}
          {error ? <div className="p-4 text-sm text-destructive" role="alert">{error}
            <Button variant="ghost" onClick={() => setRevision((n) => n + 1)}>{t("files.retry")}</Button>
          </div> : null}
          {!loading && !error && visible.length === 0 ? <p className="p-4 text-sm text-muted-foreground">{t("files.empty")}</p> : null}
          <div className="min-h-0 flex-1 overflow-auto">
            {visible.map((entry) => (
              <button key={entry.path} type="button" onClick={() => entry.dir ? toggle(entry.path) : setSelected(entry)}
                className="flex min-h-11 w-full items-center gap-2 border-b border-border px-3 text-left text-sm hover:bg-muted"
                style={{ paddingLeft: `${12 + Math.min(entry.path.split("/").length - 1, 8) * 16}px` }}>
                {entry.dir ? (collapsed.has(entry.path) ? <ChevronRight className="size-4 shrink-0" /> : <ChevronDown className="size-4 shrink-0" />) : null}
                {entry.dir ? <Folder className="size-4 shrink-0" /> : <FileText className="size-4 shrink-0" />}
                <span className="min-w-0 flex-1 truncate">{entry.name}</span>
                {entry.uploaded ? <span className="text-xs text-muted-foreground">{t("files.uploaded")}</span> : null}
                {!entry.dir ? <span aria-hidden className="shrink-0 text-xs text-muted-foreground">{fileSize(entry.size)}</span> : null}
              </button>
            ))}
          </div>
        </section>
      )}
    </main>
  )
}
