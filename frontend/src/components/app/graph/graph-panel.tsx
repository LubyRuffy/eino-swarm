import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { useT } from "@/lib/use-t"
import type { GraphView } from "@/lib/graph/types"
import { useApp } from "@/store/app"
import { useGraph, type GraphOwner } from "@/store/graph"
import { GraphBoard } from "./graph-board"

type ExtractGraph = (threadId: string, clear: boolean, ownerId?: string) => Promise<boolean>

/** The work-record panel: the graph of what the agents found on top, the
 *  ordered to-dos below. The panel does not edit rows. Update starts a turn
 *  that records what this conversation has already established. */
export function GraphPanel({ threadId, projectId, running = false, visible = true, onExtract }: GraphOwner & {
  running?: boolean
  visible?: boolean
  onExtract?: ExtractGraph
}) {
  const t = useT()
  const { view, loading, connected, error, connect, refresh } = useGraph()
  useEffect(() => {
    if (threadId || projectId) return connect({ threadId, projectId })
  }, [threadId, projectId, connect])
  const extract = (
    <ExtractButton key={JSON.stringify([threadId, view?.owner_id])} view={view} threadId={threadId} running={running}
      label={t("graph.extract")} hint={t("graph.extractHint")}
      onExtract={onExtract ?? ((id, clear, ownerId) => useApp.getState().extractGraph(id, clear, ownerId))} />
  )

  if (!view) {
    return (
      <div className="flex flex-col items-start gap-2 p-3 text-xs text-muted-foreground" role={error ? "alert" : "status"}>
        {extract}
        {error ? <>
          <span>{t("graph.loadFailed")}</span>
          <Button variant="outline" size="sm" onClick={() => void refresh()}>{t("graph.retry")}</Button>
        </> : <span>{loading ? t("graph.loading") : t("graph.empty")}</span>}
      </div>
    )
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center justify-end gap-2 px-3 pt-3">
        {running && <span className="mr-auto text-xs text-muted-foreground">{t("graph.extracting")}</span>}
        {extract}
      </div>
      {error && <div className="flex items-center gap-2 px-3 pt-2 text-xs text-muted-foreground" role="alert">
        <span>{t("graph.loadFailed")}</span>
        <Button variant="outline" size="sm" onClick={() => void refresh()}>{t("graph.retry")}</Button>
      </div>}
      <GraphBoard key={`${threadId ?? ""}|${projectId ?? ""}`} view={view} running={running} visible={visible}
        connected={connected || !threadId} t={t} />
    </div>
  )
}

function ExtractButton({ threadId, view, running, label, hint, onExtract }: {
  threadId?: string
  view?: GraphView
  running: boolean
  label: string
  hint: string
  onExtract: ExtractGraph
}) {
  const t = useT()
  const [pending, setPending] = useState(false)
  const [ask, setAsk] = useState(false)
  if (!threadId) return null
  const occupied = !!view && (view.subjects.length > 0 || view.todos.length > 0 || view.total > 0)
  const start = (ownerId?: string) => {
    setAsk(false)
    setPending(true)
    const request = ownerId ? onExtract(threadId, true, ownerId) : onExtract(threadId, false)
    void request.finally(() => setPending(false))
  }
  return (
    <>
      <Button type="button" variant="outline" size="sm" title={hint} disabled={running || pending || !view}
        onClick={() => { if (occupied) setAsk(true); else start() }}>
        {label}
      </Button>
      <Dialog open={ask} onOpenChange={setAsk}>
        <DialogContent>
          <DialogTitle>{t("graph.extractAsk")}</DialogTitle>
          <DialogDescription>
            {t("graph.extractAskBody")}
            {view && view.owner_id !== threadId ? ` ${t("graph.extractAskShared")}` : ""}
          </DialogDescription>
          <div className="flex flex-wrap justify-end gap-2">
            <Button type="button" variant="outline" size="sm" onClick={() => setAsk(false)}>{t("graph.extractCancel")}</Button>
            <Button type="button" variant="outline" size="sm" onClick={() => start()}>{t("graph.extractKeep")}</Button>
            <Button type="button" size="sm" disabled={!view?.owner_id}
              onClick={() => { if (view?.owner_id) start(view.owner_id) }}>{t("graph.extractClear")}</Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
