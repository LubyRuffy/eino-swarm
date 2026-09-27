import { ChevronDown, ChevronRight } from "lucide-react"
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"

import { MemoMarkdown } from "@/components/app/markdown"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { Skeleton } from "@/components/ui/skeleton"
import { api } from "@/lib/api"
import { applyClientPage, type ClientLog } from "@/lib/client-log"
import { clientWorkCounts, foldClientEntries } from "@/lib/client-fold"
import { updateOpenClient } from "@/lib/client-open"
import { chromeTypeClass, contentTypeClass } from "@/lib/chrome-type"
import { useTranscriptFollow } from "@/lib/follow-scroll"
import type { ClientEntry } from "@/lib/local-clients"
import { useT, type Translate } from "@/lib/use-t"
import { cn } from "@/lib/utils"
import { useApp } from "@/store/app"

/** Same cadence as the sidebar poll. The tail is what is live; Earlier
 *  is a separate read and must not be overwritten by this tick. */
const TAIL_POLL_MS = 2000

/** The same column as a conversation. The composer stays mounted above this
 *  and is locked by the shell; this view never sends. */
export function ClientChat({ id }: { id: string }) {
  const t = useT()
  const mode = useApp((s) => s.transcriptMode)
  const scrollerRef = useRef<HTMLDivElement>(null)
  const logRef = useRef<ClientLog | null>(null)
  const paging = useRef(false)
  const tailGen = useRef(0)
  const revealOlder = useRef(false)
  const [log, setLog] = useState<ClientLog | null>(null)
  const [failed, setFailed] = useState(false)
  const [loadingOlder, setLoadingOlder] = useState(false)

  useEffect(() => {
    let stop = false
    let busy = false
    logRef.current = null
    paging.current = false
    tailGen.current += 1
    setLog(null)
    setFailed(false)
    const load = () => {
      if (busy || paging.current) return
      busy = true
      const gen = ++tailGen.current
      api
        .clientTask(id)
        .then((next) => {
          if (stop || gen !== tailGen.current) return
          const applied = applyClientPage(logRef.current, next, "tail")
          logRef.current = applied
          setLog(applied)
          setFailed(false)
          updateOpenClient({ title: next.title, status: next.status })
        })
        .catch(() => {
          if (!stop && !logRef.current) setFailed(true)
        })
        .finally(() => {
          busy = false
        })
    }
    load()
    const timer = window.setInterval(load, TAIL_POLL_MS)
    return () => {
      stop = true
      window.clearInterval(timer)
    }
  }, [id])

  const rows = useMemo(
    () => foldClientEntries(log?.body ?? [], mode),
    [log?.body, mode],
  )
  const last = log?.body.at(-1)
  const growthKey = `${log?.body.length ?? 0}:${last?.text.length ?? 0}`
  const { showJump, jumpToLatest, unpin } = useTranscriptFollow({
    scrollerRef,
    loaded: Boolean(log),
    threadId: id,
    growthKey,
  })

  const loadOlder = () => {
    const cur = logRef.current
    if (!cur?.older || !cur.before || paging.current) return
    paging.current = true
    setLoadingOlder(true)
    revealOlder.current = true
    unpin()
    const gen = ++tailGen.current
    const requested = id
    api
      .clientTask(id, cur.before)
      .then((next) => {
        if (gen !== tailGen.current || requested !== id) return
        const applied = applyClientPage(logRef.current, next, "older")
        logRef.current = applied
        setLog(applied)
      })
      .catch(() => undefined)
      .finally(() => {
        paging.current = false
        setLoadingOlder(false)
      })
  }

  useLayoutEffect(() => {
    if (!revealOlder.current) return
    revealOlder.current = false
    const el = scrollerRef.current
    if (el) el.scrollTop = 0
  }, [log])

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      {log?.older ? (
        <div className="flex shrink-0 justify-center border-b border-border">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="text-muted-foreground"
            data-testid="client-earlier"
            disabled={loadingOlder}
            aria-busy={loadingOlder || undefined}
            onClick={loadOlder}
          >
            {t("transcript.earlier")}
          </Button>
        </div>
      ) : null}
      <div
        ref={scrollerRef}
        data-testid="client-chat"
        className="min-h-0 flex-1 overflow-y-auto pb-composer"
      >
        <div className="content-column content-gutter flex flex-col gap-4 py-6">
          {failed && !log ? (
            <p className={cn(contentTypeClass, "text-muted-foreground")}>{t("sidebar.clientMissing")}</p>
          ) : null}
          {!log && !failed
            ? [0, 1, 2].map((i) => <Skeleton key={i} className="h-12 w-2/3" />)
            : null}
          {(log?.opening.length ?? 0) > 0 ? (
            <div
              data-testid="client-request"
              className="sticky top-0 z-10 -mx-1 flex flex-col gap-2 bg-background px-1 pb-2"
            >
              {log?.opening.map((entry, i) => (
                <ClientLine key={`open-${entry.at ?? i}-${entry.n ?? 0}`} entry={entry} />
              ))}
            </div>
          ) : null}
          {rows.map((row) =>
            row.type === "work" ? (
              <ClientWorkFold key={`work-${row.index}`} entries={row.entries} />
            ) : (
              <ClientLine key={`${row.entry.at ?? row.index}-${row.entry.n ?? 0}`} entry={row.entry} />
            ),
          )}
        </div>
      </div>
      {showJump ? (
        <Button
          type="button"
          variant="outline"
          size="icon"
          data-testid="jump-to-latest"
          aria-label={t("transcript.jumpLatest")}
          className="absolute right-4 z-20 rounded-full bg-background shadow-md bottom-[calc(var(--composer-pad)+0.75rem)]"
          onClick={jumpToLatest}
        >
          <ChevronDown />
        </Button>
      ) : null}
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
