import { useId, useMemo } from "react"
import { Maximize2, Target } from "lucide-react"
import { Button } from "@/components/ui/button"
import { layoutNuwaCollaboration, NUWA_GOAL_ID, type NuwaNode, type NuwaWorker } from "@/lib/nuwa"
import { MANAGER_ID } from "@/lib/transcript-blocks"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"
import { NuwaStatus, NuwaStale, nuwaKind } from "./nuwa-status"

const CARD_HEIGHT = 88
const ROW = 116
const PAD = 16

/** Resolve a record's recorder role and model from the live roster using the
 * same identity the engine binds: conversation, turn, agent and generation. */
function authorKey(w: { thread_id: string; turn_id: string; agent_id: string; generation?: number }) {
  return JSON.stringify([w.thread_id, w.turn_id, w.agent_id, w.generation])
}

/** Buttons overlay the SVG paths so work records remain keyboard accessible. */
export function NuwaGraph({
  nodes,
  workers = [],
  selected,
  onSelect,
  onExpand,
  expanded = false,
}: {
  nodes: NuwaNode[]
  workers?: NuwaWorker[]
  selected?: string
  onSelect: (id: string) => void
  onExpand?: () => void
  expanded?: boolean
}) {
  const t = useT()
  const marker = useId().replace(/:/g, "")
  const layout = useMemo(() => layoutNuwaCollaboration(nodes), [nodes])
  const positions = new Map(layout.positions.map((position) => [position.node.id, position]))
  const roster = useMemo(
    () => new Map(workers.map((worker) => [authorKey(worker), worker])),
    [workers]
  )
  const hasDependencies = layout.edges.some(({ to }) => to !== NUWA_GOAL_ID)
  const compact = !expanded
  const cardWidth = compact ? 148 : 192
  const columnWidth = compact ? 176 : 232
  const columns = 1 + Math.max(0, ...layout.positions.map(({ column }) => column))
  const width = Math.max(280, PAD * 2 + (columns - 1) * columnWidth + cardWidth)
  // Paths and cards share the same horizontal scale. Deeper graphs can scroll.
  const minWidth = Math.min(width, columns * 96 + 20)
  const height = Math.max(
    112,
    ...layout.positions.map(({ row }) => PAD * 2 + row * ROW + CARD_HEIGHT)
  )
  return (
    <section
      className={cn("min-w-0", expanded && "flex min-h-0 flex-1 flex-col")}
      aria-label={t("nuwa.path")}
    >
      <div className="mb-2 flex items-center justify-between gap-2">
        <h3 className="text-xs font-medium text-muted-foreground">{t("nuwa.path")}</h3>
        {onExpand ? (
          <Button variant="ghost" size="icon-xs" aria-label={t("nuwa.expand")} onClick={onExpand}>
            <Maximize2 />
          </Button>
        ) : null}
      </div>
      <div className="mb-2 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
        {hasDependencies ? (
          <span className="inline-flex items-center gap-1.5">
            <span aria-hidden className="w-4 border-t-2 border-muted-foreground" />
            {t("nuwa.dependencyEdge")}
          </span>
        ) : null}
        <span className="inline-flex items-center gap-1.5">
          <span aria-hidden className="w-4 border-t-2 border-dashed border-primary/70" />
          {t("nuwa.goalEdge")}
        </span>
      </div>
      <div
        role="region"
        aria-label={t("nuwa.graph")}
        tabIndex={0}
        className={cn(
          "thin-scrollbar overflow-auto rounded-lg border border-border bg-muted/20",
          expanded ? "min-h-0 flex-1" : "max-h-72"
        )}
      >
        <div className="relative" style={{ width: compact ? `clamp(${minWidth}px, 100%, ${width}px)` : width, height }}>
          <svg
            width="100%"
            height={height}
            viewBox={`0 0 ${width} ${height}`}
            preserveAspectRatio="none"
            className="pointer-events-none absolute inset-0 text-muted-foreground"
            aria-hidden
          >
            <defs>
              <marker
                id={marker}
                viewBox="0 0 10 10"
                refX="9"
                refY="5"
                markerWidth="5"
                markerHeight="5"
                orient="auto-start-reverse"
              >
                <path d="M 0 0 L 10 5 L 0 10 z" fill="currentColor" />
              </marker>
            </defs>
            {layout.edges.map(({ from, to }) => {
              const a = positions.get(from)!,
                b = positions.get(to)!
              const goal = b.node.type === "goal"
              const x1 = PAD + a.column * columnWidth + cardWidth,
                y1 = PAD + a.row * ROW + CARD_HEIGHT / 2
              const x2 = PAD + b.column * columnWidth,
                y2 = PAD + b.row * ROW + CARD_HEIGHT / 2
              return (
                <path
                  key={`${from}:${to}`}
                  data-testid={goal ? "nuwa-goal-edge" : "nuwa-edge"}
                  data-from={from}
                  data-to={to}
                  d={`M ${x1} ${y1} C ${x1 + 24} ${y1}, ${x2 - 24} ${y2}, ${x2} ${y2}`}
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.5"
                  vectorEffect="non-scaling-stroke"
                  strokeDasharray={goal ? "4 3" : undefined}
                  className={goal ? "text-primary/70" : undefined}
                  markerEnd={`url(#${marker})`}
                />
              )
            })}
          </svg>
          {layout.positions.map(({ node, column, row }) => {
            const style = {
              width: `${100 * cardWidth / width}%`,
              height: CARD_HEIGHT,
              left: `${100 * (PAD + column * columnWidth) / width}%`,
              top: PAD + row * ROW,
            }
            if (node.type === "goal") {
              return (
                <div
                  key={node.id}
                  data-testid="nuwa-goal"
                  data-node-id={node.id}
                  className="absolute flex flex-col justify-center gap-1 rounded-lg border border-dashed border-primary/50 bg-primary/5 px-3 py-2"
                  style={style}
                >
                  <span className="inline-flex items-center gap-1 text-[10px] text-muted-foreground">
                    <Target className="size-3" aria-hidden />
                    {t("nuwa.goalNode")}
                  </span>
                  <span className="truncate text-xs font-medium">{t("nuwa.goal")}</span>
                </div>
              )
            }
            const record = node.record
            const worker = record.agent_id && record.turn_id
              ? roster.get(authorKey({
                  thread_id: record.thread_id, turn_id: record.turn_id,
                  agent_id: record.agent_id, generation: record.generation,
                }))
              : undefined
            const manager = record.agent_id === MANAGER_ID
            const author = manager ? t("nuwa.originManager") : (worker?.role || record.agent_id)
            const model = record.model_binding?.model || record.model || worker?.model_binding?.model
            return (
              <button
                key={record.id}
                type="button"
                data-node-id={record.id}
                onClick={() => onSelect(record.id)}
                aria-pressed={selected === record.id}
                aria-description={record.agent_id && record.turn_id
                  ? t("nuwa.originDescription", { agent: author ?? record.agent_id, turn: record.turn_id })
                  : undefined}
                className={cn(
                  "absolute flex flex-col items-start gap-0.5 overflow-hidden rounded-lg border bg-card px-3 py-2 text-left shadow-xs transition-colors hover:border-primary/60 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring",
                  selected === record.id ? "border-primary ring-1 ring-primary/30" : "border-border"
                )}
                style={style}
              >
                <span className="text-[10px] text-muted-foreground">{nuwaKind(record.kind, t)}</span>
                <span className="w-full truncate text-xs font-medium" title={record.title}>
                  {record.title}
                </span>
                <span className="flex max-w-full items-center gap-1">
                  <NuwaStatus status={record.status} />
                  <NuwaStale node={record} />
                </span>
                {author ? (
                  <span className="w-full truncate text-[10px] text-muted-foreground" title={model ? `${author} · ${model}` : author}>
                    {author}{model ? ` · ${model}` : ""}
                  </span>
                ) : null}
              </button>
            )
          })}
        </div>
      </div>
      {layout.cyclic ? (
        <p role="status" className="mt-2 text-xs text-muted-foreground">
          {t("nuwa.cycle")}
        </p>
      ) : null}
      {layout.missing.map((id) => (
        <p key={id} className="mt-1 break-all text-xs text-muted-foreground">
          {t("nuwa.missing", { id })}
        </p>
      ))}
    </section>
  )
}
