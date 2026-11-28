import { useCallback, useEffect, useRef, useState } from "react"
import { ChevronLeft, RefreshCw } from "lucide-react"
import { GraphBoard } from "../../../frontend/src/components/app/graph/graph-board"
import type { GraphView } from "../../../frontend/src/lib/graph/types"
import { graphEn, graphZh, type GraphTranslate } from "../../../frontend/src/lib/messages-graph"
import { Button } from "./ui/button"
import { installAndroidBack } from "@/lib/android-back"
import { getLocale, t } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import { OpGraph } from "@/lib/rpc"

const translate = (key: keyof typeof graphEn, vars?: Record<string, string | number>) => {
  let text: string = (getLocale() === "zh" ? graphZh : graphEn)[key]
  for (const [name, value] of Object.entries(vars ?? {})) text = text.replaceAll(`{${name}}`, String(value))
  return text
}
// Read at use, not at import: the locale can change while the screen is open.
export const phoneGraphText: GraphTranslate = Object.defineProperty(translate, "locale", { get: getLocale }) as GraphTranslate

/** The phone has no event stream, so it polls with the revision and graph
 *  owner it holds. Both have to match before the PC answers unchanged: two
 *  graphs can share a revision. A running conversation is polled faster
 *  because its to-dos move. */
export const GRAPH_POLL_RUNNING_MS = 3000
export const GRAPH_POLL_IDLE_MS = 10000

type Load = { link: RemoteLink; view?: GraphView; error?: "graph.loadFailed" | "graph.tooLarge" }

export function PhoneWorkRecords({ link, threadId, generation, running, covered, onBack }: {
  link: RemoteLink
  threadId: string
  generation: number
  running: boolean
  covered: boolean
  onBack: () => void
}) {
  const [load, setLoad] = useState<Load>()
  const held = useRef(0)
  const heldOwner = useRef("")
  const inFlight = useRef<AbortController | undefined>(undefined)
  const current = load?.link === link ? load : undefined
  const fatal = current?.error === "graph.tooLarge"

  useEffect(() => {
    if (covered) return
    return installAndroidBack(() => { onBack(); return true })
  }, [covered, onBack])

  const read = useCallback(async () => {
    if (inFlight.current) return
    const controller = new AbortController()
    inFlight.current = controller
    try {
      const reply = await link.rpc({
        op: OpGraph, thread_id: threadId, since: held.current, graph_owner: heldOwner.current || undefined,
      }, { signal: controller.signal, dropOnTimeout: false })
      if (controller.signal.aborted) return
      const graph = reply.graph
      if (reply.ok && reply.data_generation === generation && graph) {
        if (graph.owner) heldOwner.current = graph.owner
        if (graph.unchanged) return
        held.current = graph.revision
        setLoad({ link, view: {
          owner_id: graph.owner || threadId, revision: graph.revision, subjects: graph.subjects ?? [], links: graph.links ?? [],
          todos: graph.todos ?? [], total: graph.total ?? 0, more: Boolean(graph.more),
        } })
        return
      }
      if (reply.code === "graph_too_large") { setLoad({ link, error: "graph.tooLarge" }); return }
      // A failed poll keeps the picture the phone already holds.
      setLoad((previous) => previous?.link === link && previous.view ? previous : { link, error: "graph.loadFailed" })
    } catch {
      if (!controller.signal.aborted) setLoad((previous) => previous?.link === link && previous.view ? previous : { link, error: "graph.loadFailed" })
    } finally {
      if (inFlight.current === controller) inFlight.current = undefined
    }
  }, [link, threadId, generation])

  useEffect(() => {
    held.current = 0
    heldOwner.current = ""
    setLoad(undefined)
    void read()
    return () => { inFlight.current?.abort(); inFlight.current = undefined }
  }, [read])

  useEffect(() => {
    if (fatal || covered) return
    const tick = () => { if (document.visibilityState !== "hidden") void read() }
    const timer = window.setInterval(tick, running ? GRAPH_POLL_RUNNING_MS : GRAPH_POLL_IDLE_MS)
    document.addEventListener("visibilitychange", tick)
    return () => { window.clearInterval(timer); document.removeEventListener("visibilitychange", tick) }
  }, [read, running, fatal, covered])

  const manualRefresh = () => {
    held.current = 0
    heldOwner.current = ""
    setLoad((previous) => previous?.view ? previous : undefined)
    void read()
  }

  return <main inert={covered} aria-hidden={covered} className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-background">
    <header className="flex h-12 shrink-0 items-center gap-1 border-b border-border px-1">
      <Button type="button" variant="ghost" className="size-10 shrink-0 px-0" onClick={onBack} aria-label={t("thread.back")}>
        <ChevronLeft className="size-5" aria-hidden />
      </Button>
      <h1 className="min-w-0 flex-1 truncate text-sm font-medium">{phoneGraphText("graph.tab")}</h1>
      <Button type="button" variant="ghost" className="size-10 shrink-0 px-0"
        disabled={!current} onClick={manualRefresh} aria-label={phoneGraphText("graph.refresh")}>
        <RefreshCw className="size-4" aria-hidden />
      </Button>
    </header>
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      {!current ? <p role="status" className="p-3 text-sm text-muted-foreground">{phoneGraphText("graph.loading")}</p> : null}
      {current?.error ? <p role="alert" className="p-3 text-sm text-destructive">{phoneGraphText(current.error)}</p> : null}
      {current?.view ? <GraphBoard key={threadId} view={current.view} running={running} t={phoneGraphText} /> : null}
    </div>
  </main>
}
