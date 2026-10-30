import { Download, FileText, FolderOpen, Loader2, RefreshCw } from "lucide-react"
import { useMemo, useState } from "react"

import { CopyButton } from "@/components/app/copy-button"
import { MemoMarkdown } from "@/components/app/markdown"
import { SOURCE_KIND_CLASS } from "@/components/app/source-code"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { api } from "@/lib/api"
import { MAX_RENDERED_PREVIEW_CHARS } from "@/lib/file-preview"
import { isMarkdownPath } from "@/lib/read-result"
import { languageFromPath, tokenizeSource } from "@/lib/source-highlight"
import { useFilePreview } from "@/lib/use-file-preview"
import { FilePreviewImage } from "@/components/app/file-preview-image"
import { useT } from "@/lib/use-t"

export function FilePreviewDialog({ threadId, path, canReveal, onReveal, onClose, onReturnFocus }: {
  threadId: string
  path: string
  canReveal: boolean
  onReveal: (path: string) => void
  onClose: () => void
  onReturnFocus: () => void
}) {
  const t = useT()
  const { file, url, error, loading, refresh } = useFilePreview(threadId, path)
  const [mode, setMode] = useState("preview")
  const markdown = file?.kind === "text" && isMarkdownPath(path) && file.text.length <= MAX_RENDERED_PREVIEW_CHARS
  const name = path.slice(path.lastIndexOf("/") + 1)

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent
        data-testid="file-preview"
        className="flex h-[85dvh] w-[calc(100%_-_2rem)] max-w-5xl flex-col gap-0 overflow-hidden p-0 data-[state=open]:slide-in-from-left-1/2 data-[state=open]:slide-in-from-top-1/2 data-[state=closed]:slide-out-to-left-1/2 data-[state=closed]:slide-out-to-top-1/2"
        onCloseAutoFocus={(event) => { event.preventDefault(); onReturnFocus() }}
      >
        <header className="flex shrink-0 items-start gap-2 border-b px-5 py-4 pr-12">
          <FileText className="mt-0.5 size-4 shrink-0 text-file-doc" aria-hidden />
          <div className="min-w-0 flex-1">
            <DialogTitle className="truncate leading-5">{name}</DialogTitle>
            <DialogDescription className="mt-1 break-all text-xs">{path}</DialogDescription>
          </div>
        </header>
        <div className="flex shrink-0 flex-wrap items-center gap-1 border-b px-3 py-2">
          {markdown ? (
            <div role="group" aria-label={t("files.content")} className="flex items-center gap-1">
              <Button size="sm" variant={mode === "preview" ? "secondary" : "ghost"} aria-pressed={mode === "preview"} onClick={() => setMode("preview")}>{t("files.preview")}</Button>
              <Button size="sm" variant={mode === "source" ? "secondary" : "ghost"} aria-pressed={mode === "source"} onClick={() => setMode("source")}>{t("files.source")}</Button>
            </div>
          ) : null}
          <div className="ml-auto flex items-center gap-1">
            {file?.kind === "text" ? <CopyButton text={file.text} aria-label={t("files.copyContent")} /> : null}
            <Button variant="ghost" size="icon-sm" onClick={refresh} title={t("files.refresh")} aria-label={t("files.refresh")}>
              <RefreshCw />
            </Button>
            {canReveal ? (
              <Button variant="ghost" size="icon-sm" onClick={() => onReveal(path)} title={t("files.revealFile")} aria-label={t("files.revealFile")}>
                <FolderOpen />
              </Button>
            ) : null}
            <Button variant="ghost" size="sm" className="gap-1.5" asChild>
              <a href={api.downloadURL(threadId, path)} download><Download />{t("files.download")}</a>
            </Button>
          </div>
        </div>
        <section aria-label={t("files.content")} aria-busy={loading} className={`thin-scrollbar min-h-0 min-w-0 flex-1 ${file?.kind === "image" ? "overflow-hidden" : "overflow-auto p-5"}`}>
          {loading ? <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />{t("files.loading")}</p> : null}
          {error ? <p role="alert" className="break-words text-sm text-destructive">{error}</p> : null}
          {file?.kind === "download" ? <p className="text-sm text-muted-foreground">{t(file.reason === "too-large" ? "files.tooLarge" : "files.unsupported")}</p> : null}
          {file?.kind === "image" && url ? <FilePreviewImage key={url} src={url} name={name} /> : null}
          {file?.kind === "text" ? (
            file.text === "" ? <p className="text-sm text-muted-foreground">{t("files.emptyFile")}</p> : markdown && mode === "preview" ? (
              <article className="md break-words [overflow-wrap:anywhere] [&>:first-child]:mt-0"><MemoMarkdown text={file.text} /></article>
            ) : <FileSource path={path} text={file.text} />
          ) : null}
        </section>
      </DialogContent>
    </Dialog>
  )
}

function FileSource({ path, text }: { path: string; text: string }) {
  const tokens = useMemo(() => {
    const lang = languageFromPath(path)
    return lang && text.length <= MAX_RENDERED_PREVIEW_CHARS
      ? tokenizeSource(text, lang)
      : [{ kind: "text" as const, text }]
  }, [path, text])
  return (
    <pre data-testid="file-source" className="min-w-0 whitespace-pre-wrap break-words font-mono text-sm [overflow-wrap:anywhere]">
      <code>{tokens.map((token, i) => <span key={i} className={SOURCE_KIND_CLASS[token.kind] || undefined}>{token.text}</span>)}</code>
    </pre>
  )
}
