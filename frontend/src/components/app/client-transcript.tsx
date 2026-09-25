import { ChevronRight } from "lucide-react"
import { useEffect, useMemo, useState } from "react"

import { MemoMarkdown } from "@/components/app/markdown"
import { Disclosure } from "@/components/ui/collapsible"
import { Skeleton } from "@/components/ui/skeleton"
import { api } from "@/lib/api"
import { clientWorkCounts, foldClientEntries, splitOpeningRequest } from "@/lib/client-fold"
import { updateOpenClient } from "@/lib/client-open"
import { chromeTypeClass, contentTypeClass } from "@/lib/chrome-type"
import type { ClientEntry, ClientTranscript } from "@/lib/local-clients"
import { useT, type Translate } from "@/lib/use-t"
import { cn } from "@/lib/utils"
import { useApp } from "@/store/app"

/** The same column as a conversation. The composer stays mounted above this
 *  and is locked by the shell; this view never sends. */
export function ClientChat({ id }: { id: string }) {
  const t = useT()
  const mode = useApp((s) => s.transcriptMode)
  const [doc, setDoc] = useState<ClientTranscript | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let stop = false
    let busy = false
    const load = () => {
      if (busy) return
      busy = true
      api
        .clientTask(id)
        .then((next) => {
          if (stop) return
          setDoc(next)
          setFailed(false)
          updateOpenClient({ title: next.title, status: next.status })
        })
        .catch(() => {
          if (!stop) setFailed(true)
        })
        .finally(() => {
          busy = false
        })
    }
    setDoc(null)
    setFailed(false)
    load()
    const timer = window.setInterval(load, 2000)
    return () => {
      stop = true
      window.clearInterval(timer)
    }
  }, [id])

  const split = useMemo(
    () => splitOpeningRequest(doc?.entries ?? []),
    [doc?.entries],
  )
  const rows = useMemo(
    () => foldClientEntries(split.rest, mode),
    [split.rest, mode],
  )

  return (
    <div
      data-testid="client-chat"
      className="min-h-0 flex-1 overflow-y-auto pb-composer"
    >
      <div className="content-column content-gutter flex flex-col gap-4 py-6">
        {failed && !doc ? (
          <p className={cn(contentTypeClass, "text-muted-foreground")}>{t("sidebar.clientMissing")}</p>
        ) : null}
        {!doc && !failed
          ? [0, 1, 2].map((i) => <Skeleton key={i} className="h-12 w-2/3" />)
          : null}
        {split.opening.length > 0 ? (
          <div
            data-testid="client-request"
            className="sticky top-0 z-10 -mx-1 flex flex-col gap-2 bg-background px-1 pb-2"
          >
            {split.opening.map((entry, i) => (
              <ClientLine key={`open-${i}`} entry={entry} />
            ))}
          </div>
        ) : null}
        {rows.map((row) =>
          row.type === "work" ? (
            <ClientWorkFold key={`work-${row.index}`} entries={row.entries} />
          ) : (
            <ClientLine key={`${row.entry.role}-${row.index}`} entry={row.entry} />
          ),
        )}
        {doc?.truncated ? (
          <p className={cn(contentTypeClass, "text-xs text-muted-foreground")}>
            {t("sidebar.clientTruncated")}
          </p>
        ) : null}
      </div>
    </div>
  )
}

function ClientLine({ entry }: { entry: ClientEntry }) {
  if (entry.role === "user") {
    return (
      <div className="flex justify-end">
        <div
          data-testid="user-message"
          className={cn(
            contentTypeClass,
            "max-w-[85%] whitespace-pre-wrap rounded-2xl rounded-br-md bg-secondary px-4 py-2.5 text-secondary-foreground",
          )}
        >
          {entry.text}
        </div>
      </div>
    )
  }
  if (entry.role === "tool") {
    return (
      <p data-testid="client-tool" className={cn(contentTypeClass, "text-xs text-muted-foreground")}>
        {entry.text}
      </p>
    )
  }
  if (entry.role === "thinking") {
    return (
      <p data-testid="client-thought" className={cn(contentTypeClass, "whitespace-pre-wrap text-muted-foreground")}>
        {entry.text}
      </p>
    )
  }
  return (
    <div data-testid="assistant-message" className={contentTypeClass}>
      <MemoMarkdown text={entry.text} />
    </div>
  )
}

function ClientWorkFold({ entries }: { entries: ClientEntry[] }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  return (
    <Disclosure
      open={open}
      onOpenChange={setOpen}
      testId="work-fold"
      summary={
        <>
          <ChevronRight
            aria-hidden
            className={cn("size-3.5 shrink-0 opacity-60 transition-transform", open && "rotate-90")}
          />
          <span className={cn("min-w-0 flex-1 truncate", chromeTypeClass)}>{foldLabel(entries, t)}</span>
        </>
      }
    >
      {open ? (
        <div className="flex flex-col gap-1">
          {entries.map((entry, i) => (
            <ClientLine key={`${entry.role}-${i}`} entry={entry} />
          ))}
        </div>
      ) : null}
    </Disclosure>
  )
}

function foldLabel(entries: ClientEntry[], t: Translate): string {
  const { thoughts, tools } = clientWorkCounts(entries)
  const toolLabel =
    tools === 1 ? t("transcript.workFoldTool") : t("transcript.workFoldTools", { n: String(tools) })
  if (thoughts > 0 && tools > 0) return t("transcript.workFoldBoth", { tools: toolLabel })
  if (tools > 0) return toolLabel
  return t("transcript.thought")
}
