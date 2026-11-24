import { Maximize2 } from "lucide-react"
import { useRef, useState, type ReactNode } from "react"

import { KnowledgeLinkProvider } from "@/components/app/knowledge-link-open"
import { MemoMarkdown } from "@/components/app/markdown"
import { WidePreviewDialog } from "@/components/app/wide-preview-dialog"
import { WorkspaceFileOpenProvider } from "@/components/app/workspace-file-open"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { DialogDescription, DialogTitle } from "@/components/ui/dialog"
import {
  groupKnowledgeConnections,
  knowledgeSourceURL,
  type KnowledgeConnection,
  type KnowledgePage,
  type KnowledgeSource,
} from "@/lib/knowledge"
import { useT } from "@/lib/use-t"
import { errorMessage } from "@/store/toasts"

export interface KnowledgePageActions {
  onPage: (id: string) => void
  onOpenThread: (threadId: string) => Promise<void>
  onTrace: (threadId: string, turnId: string) => Promise<void>
}

export function KnowledgePageView({ page, onPage, onOpenThread, onTrace }: KnowledgePageActions & { page: KnowledgePage }) {
  const t = useT()
  const [preview, setPreview] = useState(false)
  const [linkError, setLinkError] = useState<string>()
  const expand = useRef<HTMLButtonElement>(null)
  const fields = Object.entries(page.fields ?? {})
  const excerpts = new Map<string, Set<string>>()
  for (const { source, quote } of page.wiki_observations ?? []) {
    if (!source.available || !quote) continue
    const key = sourceKey(source)
    const values = excerpts.get(key) ?? new Set<string>()
    values.add(quote)
    excerpts.set(key, values)
  }
  const { updated_thread: updatedThread, updated_turn: updatedTurn } = page
  const historyActions = (thread: string, turn?: string) => {
    setLinkError(undefined)
    void (turn ? onTrace(thread, turn) : onOpenThread(thread)).catch((error: unknown) => setLinkError(errorMessage(error)))
  }
  return (
    <div className="thin-scrollbar min-h-0 min-w-0 flex-1 overflow-y-auto">
      <div className="flex items-start gap-2 border-b p-3">
        <div className="min-w-0 flex-1 space-y-1">
          <h2 className="wrap-break-word font-semibold">{page.title}</h2>
          <Badge variant="outline">
            {page.wiki ? `${t(page.wiki.kind === "event" ? "knowledge.wikiEvent" : "knowledge.wikiEntity")} · ${page.wiki.category}` : page.type}
          </Badge>
        </div>
        <Button
          ref={expand}
          variant="ghost"
          size="icon-sm"
          aria-label={t("knowledge.expand")}
          title={t("knowledge.expand")}
          onClick={() => setPreview(true)}
        ><Maximize2 /></Button>
      </div>
      <div className="space-y-4 p-3 text-sm">
        {page.wiki?.document_pending ? <p role="status">{t("knowledge.wikiDocumentPending")}</p> : null}
        {fields.length || page.as_of ? (
          <div className="space-y-1">
            {fields.length ? (
              <dl className="space-y-1">
                {fields.map(([name, value]) => (
                  <div key={name} className="flex flex-wrap items-baseline gap-x-2 wrap-break-word">
                    <dt className="max-w-full text-muted-foreground">{name}</dt>
                    <dd className="max-w-full">{displayField(value)}</dd>
                  </div>
                ))}
              </dl>
            ) : null}
            {page.as_of ? <p className="text-muted-foreground">{t("knowledge.asOf", { date: page.as_of })}</p> : null}
          </div>
        ) : null}
        <PageMarkdown page={page} onPage={onPage} />
        {page.outgoing?.length ? (
          <PageSection title={t("knowledge.outgoing")}><Relations links={page.outgoing} onPage={onPage} /></PageSection>
        ) : null}
        {page.incoming?.length ? (
          <PageSection title={t("knowledge.incoming")}><Relations links={page.incoming} onPage={onPage} /></PageSection>
        ) : null}
        {page.mentions?.length ? (
          <PageSection title={t("knowledge.mentions")}>
            <ul className="space-y-1">
              {page.mentions.map(({ id, title }) => <li key={id}><PageLink title={title} onOpen={() => onPage(id)} /></li>)}
            </ul>
          </PageSection>
        ) : null}
        {page.sources?.length ? (
          <PageSection title={t("knowledge.sources")}>
            <ul className="space-y-2">
              {page.sources.map((source, index) => (
                <li key={`${source.uri ?? source.thread_id}:${source.kind}:${source.seq}:${index}`}>
                  <SourceItem source={source} excerpts={excerpts.get(sourceKey(source))} onHistory={historyActions} />
                </li>
              ))}
            </ul>
          </PageSection>
        ) : null}
        {page.updated_at || page.updated_turn ? (
          <PageSection title={t("knowledge.lastUpdate")}>
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              {page.updated_at ? <SourceDate value={page.updated_at} /> : null}
              {updatedThread && updatedTurn ? (
                <>
                  <PageLink title={t("knowledge.updatedConversation")} onOpen={() => historyActions(updatedThread)} />
                  <PageLink title={t("knowledge.trace")} onOpen={() => historyActions(updatedThread, updatedTurn)} />
                </>
              ) : null}
            </div>
          </PageSection>
        ) : null}
        {linkError ? <p role="alert" className="wrap-break-word text-destructive">{linkError}</p> : null}
      </div>
      {preview ? (
        <WidePreviewDialog
          testId="knowledge-preview"
          onClose={() => setPreview(false)}
          onReturnFocus={() => expand.current?.focus()}
        >
          <header className="shrink-0 space-y-1 border-b px-5 py-4 pr-12">
            <DialogTitle className="wrap-break-word leading-5">{page.title}</DialogTitle>
            <DialogDescription>{page.type}</DialogDescription>
          </header>
          <div className="thin-scrollbar min-h-0 min-w-0 flex-1 overflow-auto p-5">
            <PageMarkdown page={page} onPage={(id) => { setPreview(false); onPage(id) }} />
          </div>
        </WidePreviewDialog>
      ) : null}
    </div>
  )
}

function displayField(value: unknown): string {
  return typeof value === "string" ? value : JSON.stringify(value) ?? ""
}

function PageMarkdown({ page, onPage }: { page: KnowledgePage; onPage: (id: string) => void }) {
  return (
    <KnowledgeLinkProvider open={onPage} unresolved={page.unresolved ?? []}>
      <WorkspaceFileOpenProvider open={null}>
        <article className="md wrap-break-word wrap-anywhere *:first:mt-0"><MemoMarkdown text={page.body} /></article>
      </WorkspaceFileOpenProvider>
    </KnowledgeLinkProvider>
  )
}

function PageSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-1.5 border-t pt-2">
      <h3 className="text-xs font-semibold text-muted-foreground">{title}</h3>
      {children}
    </section>
  )
}

function Relations({ links, onPage }: { links: KnowledgeConnection[]; onPage: (id: string) => void }) {
  return (
    <dl className="space-y-2">
      {[...groupKnowledgeConnections(links)].map(([id, { label, links }]) => (
        <div key={id} className="space-y-0.5">
          <dt className="wrap-break-word">{label}</dt>
          <dd>
            <ul className="space-y-0.5">
              {links.map((link, index) => (
                <li key={`${link.target}:${index}`}>
                  <PageLink
                    title={link.page?.title ?? link.target}
                    onOpen={() => onPage(link.page?.id ?? link.target)}
                    broken={!link.resolved}
                  />
                </li>
              ))}
            </ul>
          </dd>
        </div>
      ))}
    </dl>
  )
}

function PageLink({ title, onOpen, broken }: { title: string; onOpen: () => void; broken?: boolean }) {
  return (
    <Button
      variant="link"
      size="sm"
      className={`h-auto justify-start whitespace-normal p-0 text-left wrap-anywhere ${broken ? "text-destructive" : ""}`}
      onClick={onOpen}
    >{title}</Button>
  )
}

function sourceKey({ uri, thread_id: threadId, kind, seq }: KnowledgeSource) {
  return JSON.stringify([uri, threadId, kind, seq])
}

function SourceItem({ source, excerpts, onHistory }: {
  source: KnowledgeSource
  excerpts?: Set<string>
  onHistory: (threadId: string, turnId?: string) => void
}) {
  const t = useT()
  const { uri, kind, seq, thread_id: threadId, turn_id: turnId, available } = source
  const url = knowledgeSourceURL(uri)
  const classification = source.classification ?? (uri ? "external" : undefined)
  const label = kind && seq !== undefined ? t(`knowledge.source.${kind}`, { seq }) : undefined
  return (
    <div className="space-y-0.5">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        {classification ? <Badge variant="outline" data-source-classification={classification}>{t(`knowledge.source.${classification}`)}</Badge> : null}
        {source.timestamp ? <SourceDate value={source.timestamp} /> : null}
      </div>
      {uri ? (
        url ? <a className="break-all underline underline-offset-2" href={url} target="_blank" rel="noopener noreferrer">{uri}</a>
          : <span className="break-all">{uri}</span>
      ) : label ? (
        available && threadId ? <PageLink title={label} onOpen={() => onHistory(threadId, turnId)} /> : (
          <p className="text-muted-foreground">{label} · {t("knowledge.source.unavailable")}</p>
        )
      ) : null}
      {available && excerpts?.size ? (
        <details className="mt-1">
          <summary className="cursor-pointer text-primary">{t("knowledge.source.excerpt")}</summary>
          {[...excerpts].map((quote) => (
            <blockquote key={quote} className="mt-1 whitespace-pre-wrap wrap-anywhere border-s-2 ps-2">{quote}</blockquote>
          ))}
        </details>
      ) : null}
    </div>
  )
}

function SourceDate({ value }: { value: string }) {
  const t = useT()
  const date = new Date(value)
  const text = Number.isFinite(date.getTime()) ? date.toLocaleString(t.locale) : value
  return <time dateTime={value} className="text-xs text-muted-foreground">{text}</time>
}
