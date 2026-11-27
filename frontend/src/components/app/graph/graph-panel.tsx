import { useEffect } from "react"
import { Button } from "@/components/ui/button"
import { useT } from "@/lib/use-t"
import { useGraph, type GraphOwner } from "@/store/graph"
import { GraphBoard } from "./graph-board"

/** The work-record panel: the graph of what the agents found on top, the
 *  ordered to-dos below. Read-only; agents write it through update_graph. */
export function GraphPanel({ threadId, projectId, running = false, visible = true }: GraphOwner & { running?: boolean; visible?: boolean }) {
  const t = useT()
  const { view, loading, connected, error, connect, refresh } = useGraph()
  useEffect(() => {
    if (threadId || projectId) return connect({ threadId, projectId })
  }, [threadId, projectId, connect])

  if (!view) {
    return (
      <div className="flex flex-col items-start gap-2 p-3 text-xs text-muted-foreground" role={error ? "alert" : "status"}>
        {error ? <>
          <span>{t("graph.loadFailed")}</span>
          <Button variant="outline" size="sm" onClick={() => void refresh()}>{t("graph.retry")}</Button>
        </> : <span>{loading ? t("graph.loading") : t("graph.empty")}</span>}
      </div>
    )
  }
  return <GraphBoard key={`${threadId ?? ""}|${projectId ?? ""}`} view={view} running={running} visible={visible}
    connected={connected || !threadId} t={t} />
}
