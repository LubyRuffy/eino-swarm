import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { ChevronDown, ChevronLeft, ChevronRight, Loader2 } from "lucide-react"

import { Composer } from "@/components/composer"
import { Button } from "@/components/ui/button"
import { installAndroidBack } from "@/lib/android-back"
import { applyClientPage, type ClientLog } from "@/lib/client-log"
import { cn } from "@/lib/cn"
import { t } from "@/lib/i18n"
import { clientToolTitle } from "@/lib/local-clients"
import type { ClientEntry, ClientTool, ClientView } from "@/lib/rpc"

/** Same cadence as the desktop client tail. */
const TAIL_POLL_MS = 2000

type Reading = {
  id: string
  title: string
  status: string
  opening: ClientEntry[]
  body: ClientEntry[]
  older: boolean
  before: number
  failed: boolean
  loading: boolean
  loadingOlder: boolean
}

/** A row opens the same chat column as a PC thread. The composer is on
 *  screen and cannot send. */
export function ClientGroups({
  tools,
  loadingMore = "",
  onMore,
  onRead,
}: {
  tools: ClientTool[]
  loadingMore?: string
  onMore: (id: string, next?: string) => void
  onRead?: (id: string, before?: number) => Promise<ClientView | null>
}) {
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const [view, setView] = useState<Reading | null>(null)
  const [behind, setBehind] = useState(false)
  const readGeneration = useRef(0)
  const logRef = useRef<ClientLog | null>(null)
  const paging = useRef(false)
  const onReadRef = useRef(onRead)
  const scroller = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const wantTop = useRef(false)
  onReadRef.current = onRead
  const viewVisible = Boolean(view) && tools.length > 0
  const closeView = () => {
    readGeneration.current++
    setView(null)
  }
  useEffect(() => {
    if (!viewVisible) return
    return installAndroidBack(() => {
      closeView()
      return true
    })
  }, [viewVisible])
  useEffect(() => {
    const id = view?.id
    if (!id || view?.loading) return
    const timer = window.setInterval(() => {
      if (paging.current) return
      void onReadRef.current?.(id).then((doc) => {
        if (!doc) return
        setView((cur) => {
          if (!cur || cur.id !== id) return cur
          const next = applyClientPage(logRef.current, doc, "tail")
          logRef.current = next
          return paint(cur, doc, next)
        })
      }).catch(() => undefined)
    }, TAIL_POLL_MS)
    return () => window.clearInterval(timer)
  }, [view?.id, view?.loading])
  useLayoutEffect(() => {
    const el = scroller.current
    if (!el || !view || view.loading) return
    if (wantTop.current && !view.loadingOlder) {
      wantTop.current = false
      stick.current = false
      setBehind(true)
      el.scrollTop = 0
      return
    }
    if (stick.current) {
      el.scrollTop = el.scrollHeight
      setBehind(false)
    }
  }, [view])
  const loadOlder = () => {
    const cur = view
    if (!cur?.older || !cur.before || paging.current || cur.loadingOlder) return
    paging.current = true
    wantTop.current = true
    setView({ ...cur, loadingOlder: true })
    void onReadRef.current?.(cur.id, cur.before).then((doc) => {
      if (!doc) return
      setView((prev) => {
        if (!prev || prev.id !== cur.id) return prev
        const next = applyClientPage(logRef.current, doc, "older")
        logRef.current = next
        return paint(prev, doc, next)
      })
    }).catch(() => undefined).finally(() => {
      paging.current = false
      setView((prev) => (prev && prev.id === cur.id ? { ...prev, loadingOlder: false } : prev))
    })
  }
  if (tools.length === 0) return null
  return (
    <section className="flex flex-col gap-2" data-testid="client-groups">
      <h2 className="px-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
        {t("home.clients")}
      </h2>
      {tools.map((tool) => (
        <div key={tool.id} data-testid={`client-tool-${tool.id}`} className="flex flex-col gap-1">
          <h3 className="px-1 text-[11px] font-medium text-muted-foreground">
            <button
              type="button"
              aria-expanded={open[tool.id] !== false}
              className="flex w-full items-center gap-1 text-left"
              onClick={() => setOpen((cur) => ({ ...cur, [tool.id]: cur[tool.id] === false }))}
            >
              <ChevronRight
                className={cn(
                  "size-3 shrink-0",
                  open[tool.id] !== false && "rotate-90",
                )}
                aria-hidden
              />
              {t(clientToolTitle(tool.id))}
            </button>
          </h3>
          {open[tool.id] === false ? null : (
          <ul className="overflow-hidden rounded-2xl border border-border bg-card">
            {tool.tasks.length === 0 ? (
              <li className="px-3 py-2 text-xs text-muted-foreground">{t("home.clientEmpty")}</li>
            ) : (
              tool.tasks.map((task) => (
                <li
                  key={task.id}
                  data-testid="client-task"
                  className="flex items-center gap-2 border-b border-border px-3 py-2 last:border-b-0"
                >
                  <span
                    data-testid="client-status"
                    data-status={task.status}
                    role="img"
                    aria-label={task.status === "running" ? t("home.clientRunning") : t("home.clientDone")}
                    className={cn(
                      "size-2 shrink-0 rounded-full",
                      task.status === "running"
                        ? "bg-primary motion-safe:animate-pulse motion-reduce:animate-none"
                        : "bg-muted-foreground",
                    )}
                  />
                  <button
                    type="button"
                    className="min-w-0 flex-1 truncate text-left text-sm"
                    onClick={() => {
                      const request = ++readGeneration.current
                      logRef.current = null
                      stick.current = true
                      setBehind(false)
                      const shell: Reading = {
                        id: task.id,
                        title: task.title,
                        status: task.status,
                        opening: [],
                        body: [],
                        older: false,
                        before: 0,
                        failed: false,
                        loading: true,
                        loadingOlder: false,
                      }
                      setView(shell)
                      void onRead?.(task.id).then((doc) => {
                        if (readGeneration.current !== request) return
                        if (!doc) {
                          setView({ ...shell, loading: false, failed: true })
                          return
                        }
                        const next = applyClientPage(logRef.current, doc, "tail")
                        logRef.current = next
                        setView(paint(shell, doc, next))
                      }).catch(() => {
                        if (readGeneration.current === request) setView({ ...shell, loading: false, failed: true })
                      })
                    }}
                  >
                    {task.title}
                  </button>
                </li>
              ))
            )}
            {tool.more ? (
              <li>
                <Button
                  variant="ghost"
                  className={cn(
                    "h-9 w-full gap-2 rounded-none text-muted-foreground",
                    loadingMore === tool.id && "disabled:opacity-100",
                  )}
                  data-testid={`client-more-${tool.id}`}
                  onClick={() => onMore(tool.id, tool.next)}
                  disabled={Boolean(loadingMore)}
                  aria-busy={loadingMore === tool.id || undefined}
                >
                  {loadingMore === tool.id ? (
                    <Loader2 className="size-4 motion-safe:animate-spin motion-reduce:animate-none" aria-hidden />
                  ) : null}
                  {loadingMore === tool.id ? t("home.loadingMore") : t("home.more")}
                </Button>
              </li>
            ) : null}
          </ul>
          )}
        </div>
      ))}
      {/* Pull-to-refresh transforms its scroller; a fixed child there would
          move with it and lose the title/Back button above the viewport. */}
      {view ? createPortal(
        <section
          className="fixed inset-0 z-40 flex flex-col bg-background"
          style={{ padding: "env(safe-area-inset-top) env(safe-area-inset-right) env(safe-area-inset-bottom) env(safe-area-inset-left)" }}
          data-testid="client-transcript"
          onTouchStart={(event) => event.stopPropagation()}
          onTouchMove={(event) => event.stopPropagation()}
          onTouchEnd={(event) => event.stopPropagation()}
          onTouchCancel={(event) => event.stopPropagation()}
        >
          <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-2">
            <Button
              type="button"
              variant="ghost"
              className="size-9 px-0"
              aria-label={t("thread.back")}
              onClick={closeView}
            >
              <ChevronLeft className="size-5" />
            </Button>
            <span
              className={cn(
                "size-1.5 shrink-0 rounded-full",
                view.status === "running"
                  ? "bg-primary motion-safe:animate-pulse motion-reduce:animate-none"
                  : "bg-muted-foreground",
              )}
            />
            <h1 className="min-w-0 flex-1 truncate text-sm font-medium">{view.title}</h1>
          </header>
          {view.older ? (
            <div className="flex shrink-0 justify-center border-b border-border">
              <Button
                type="button"
                variant="ghost"
                className="h-8 text-xs text-muted-foreground"
                data-testid="client-earlier"
                disabled={view.loadingOlder}
                aria-busy={view.loadingOlder || undefined}
                onClick={loadOlder}
              >
                {view.loadingOlder ? t("thread.loading") : t("thread.earlier")}
              </Button>
            </div>
          ) : null}
          <div className="relative flex min-h-0 flex-1 flex-col">
          <div
            ref={scroller}
            className="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 py-4"
            onScroll={(e) => {
              const el = e.currentTarget
              const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 48
              stick.current = atBottom
              setBehind(!atBottom)
            }}
          >
            {view.loading ? <p className="text-sm text-muted-foreground">{t("thread.loading")}</p> : null}
            {view.failed ? <p className="text-sm text-muted-foreground">{t("home.clientMissing")}</p> : null}
            {view.opening.map((entry, i) => (
              <div key={`open-${entry.at ?? i}`} data-testid="client-request" className="sticky top-0 z-10 bg-background pb-2">
                <ClientLine entry={entry} />
              </div>
            ))}
            {foldClientEntries(view.body).map((row) =>
              row.type === "work" ? (
                <ClientWorkFold key={`work-${row.index}`} entries={row.entries} />
              ) : (
                <ClientLine key={`${row.entry.at ?? row.index}-${row.entry.n ?? 0}`} entry={row.entry} />
              ),
            )}
          </div>
          {behind ? (
            <Button
              type="button"
              variant="outline"
              data-testid="jump-to-latest"
              aria-label={t("thread.toLatest")}
              className="absolute bottom-3 right-3 rounded-full bg-background shadow-md"
              onClick={() => {
                const el = scroller.current
                if (!el) return
                stick.current = true
                setBehind(false)
                el.scrollTop = el.scrollHeight
              }}
            >
              <ChevronDown />
            </Button>
          ) : null}
          </div>
          <Composer
            label={t("thread.message")}
            sendLabel={t("thread.send")}
            placeholder={t("home.clientReadOnly")}
            disabled
            onSubmit={() => undefined}
          />
        </section>,
        document.body,
      ) : null}
    </section>
  )
}

type ClientLineEntry = ClientEntry

type ClientRow =
  | { type: "entry"; entry: ClientLineEntry; index: number }
  | { type: "work"; entries: ClientLineEntry[]; index: number }

function paint(cur: Reading, doc: ClientView, log: ClientLog): Reading {
  return {
    ...cur,
    title: doc.title || cur.title,
    status: doc.status || cur.status,
    opening: log.opening,
    body: log.body,
    older: log.older,
    before: log.before,
    failed: false,
    loading: false,
  }
}

function foldClientEntries(entries: ClientLineEntry[]): ClientRow[] {
  const rows: ClientRow[] = []
  let group: ClientLineEntry[] = []
  let groupAt = 0
  const flush = () => {
    if (group.length === 0) return
    rows.push({ type: "work", entries: group, index: groupAt })
    group = []
  }
  entries.forEach((entry, index) => {
    if (entry.role === "thinking" || entry.role === "tool") {
      if (group.length === 0) groupAt = index
      group.push(entry)
      return
    }
    flush()
    rows.push({ type: "entry", entry, index })
  })
  flush()
  return rows
}

function ClientLine({ entry }: { entry: ClientLineEntry }) {
  if (entry.role === "user") {
    return (
      <div className="flex justify-end">
        <p className="max-w-[85%] whitespace-pre-wrap rounded-2xl rounded-br-md bg-secondary px-3 py-2 text-sm text-secondary-foreground">
          {entry.text}
        </p>
      </div>
    )
  }
  if (entry.role === "tool") {
    return <p className="text-xs text-muted-foreground">{entry.text}</p>
  }
  if (entry.role === "thinking") {
    return <p className="whitespace-pre-wrap text-sm text-muted-foreground">{entry.text}</p>
  }
  return <p className="whitespace-pre-wrap text-sm">{entry.text}</p>
}

function ClientWorkFold({ entries }: { entries: ClientLineEntry[] }) {
  const [open, setOpen] = useState(false)
  let thoughts = 0
  let tools = 0
  for (const e of entries) {
    if (e.role === "thinking") thoughts++
    if (e.role === "tool") tools++
  }
  const toolLabel = tools === 1 ? t("thread.workFoldTool") : t("thread.workFoldTools", { n: tools })
  const label =
    thoughts > 0 && tools > 0
      ? t("thread.workFoldBoth", { tools: toolLabel })
      : tools > 0
        ? toolLabel
        : t("thread.thought")
  return (
    <div className="min-w-0">
      <button
        type="button"
        data-testid="work-fold"
        className="flex min-w-0 items-center gap-1 text-left text-xs text-muted-foreground"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <ChevronRight aria-hidden className={cn("size-3 shrink-0 transition-transform", open && "rotate-90")} />
        <span className="min-w-0 truncate">{label}</span>
      </button>
      {open ? (
        <div className="mt-1 flex flex-col gap-1 pl-4">
          {entries.map((entry, i) => (
            <ClientLine key={`${entry.role}-${i}`} entry={entry} />
          ))}
        </div>
      ) : null}
    </div>
  )
}
