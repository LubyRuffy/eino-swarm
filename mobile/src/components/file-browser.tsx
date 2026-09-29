import { useEffect, useMemo, useState } from "react"
import { ChevronDown, ChevronLeft, ChevronRight, FileText, Folder, Loader2, RefreshCw } from "lucide-react"

import { Button } from "@/components/ui/button"
import { installAndroidBack } from "@/lib/android-back"
import { t } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import { listWorkspaceFiles, maxPhoneFileBytes, previewKind, readWorkspaceFile, RemoteFileError } from "@/lib/remote-files"
import type { WorkspaceFile } from "@/lib/rpc"

type LoadedFile = { url: string; kind: ReturnType<typeof previewKind>; text: string }

function fileSize(size: number): string {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / (1024 * 1024)).toFixed(1)} MB`
}

function fileFailure(cause: unknown): string {
  if (cause instanceof RemoteFileError && cause.code === "unknown_op") return t("files.updatePC")
  return cause instanceof Error ? cause.message : String(cause)
}

// Files arrive over the sealed PC link. Agent-authored HTML/SVG stays text;
// executable content must never be rendered in the phone's app origin.
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
  const [fileLoading, setFileLoading] = useState(false)
  const [fileError, setFileError] = useState("")

  useEffect(() => {
    let active = true
    setLoading(true)
    setError("")
    void listWorkspaceFiles(link, threadId).then((rows) => {
      if (active) setFiles(rows.sort((a, b) => a.path.localeCompare(b.path)))
    }).catch((cause: unknown) => {
      if (active) setError(fileFailure(cause))
    }).finally(() => {
      if (active) setLoading(false)
    })
    return () => { active = false }
  }, [link, threadId, revision])

  useEffect(() => {
    if (!selected) return
    let active = true
    let url = ""
    setFile(null)
    setFileLoading(true)
    setFileError("")
    if (selected.size > maxPhoneFileBytes) {
      setFileLoading(false)
      setFileError(t("files.tooLarge"))
      return
    }
    void readWorkspaceFile(link, threadId, selected.path).then(({ bytes, mime }) => {
      if (!active) return
      const kind = previewKind(selected.name, mime, bytes.length)
      const safeMime = kind === "image" || kind === "pdf" ? mime : "application/octet-stream"
      url = URL.createObjectURL(new Blob([new Uint8Array(bytes)], { type: safeMime }))
      const text = kind === "text" ? new TextDecoder().decode(bytes) : ""
      setFile({ url, kind, text })
    }).catch((cause: unknown) => {
      if (active) setFileError(fileFailure(cause))
    }).finally(() => {
      if (active) setFileLoading(false)
    })
    return () => {
      active = false
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
        <section data-testid="phone-file-preview" className="flex min-h-0 flex-1 flex-col overflow-auto p-4">
          <p className="mb-3 break-all text-xs text-muted-foreground">{selected.path}</p>
          {fileLoading ? <p className="flex items-center gap-2 text-sm"><Loader2 className="size-4 animate-spin" />{t("files.loading")}</p> : null}
          {fileError ? <p role="alert" className="text-sm text-destructive">{fileError}</p> : null}
          {file?.kind === "text" ? <pre className="min-w-0 flex-1 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-3 text-sm">{file.text}</pre> : null}
          {file?.kind === "image" ? <img src={file.url} alt={selected.name} className="max-h-full max-w-full self-center object-contain" /> : null}
          {file?.kind === "pdf" ? <iframe src={file.url} title={selected.name} sandbox="allow-scripts" className="min-h-0 w-full flex-1 border-0" /> : null}
          {file?.kind === "download" ? <p className="text-sm text-muted-foreground">{t("files.unavailable")}</p> : null}
          {file ? <a href={file.url} download={selected.name} className="mt-4 self-start text-sm text-primary underline">{t("files.download")}</a> : null}
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
