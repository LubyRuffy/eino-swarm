import { useId, useMemo } from "react"
import { Maximize2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { layoutNuwaCollaboration, type NuwaNode, type NuwaWorker } from "@/lib/nuwa"
import { MANAGER_ID } from "@/lib/transcript-blocks"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"
import { NuwaStatus, NuwaStale, nuwaKind } from "./nuwa-status"

const CARD_HEIGHT = 80
const ROW = 108
const PAD = 16

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
  const layout = useMemo(() => layoutNuwaCollaboration(nodes, workers), [nodes, workers])
  const positions = new Map(layout.positions.map((position) => [position.node.id, position]))
  const hasOrigins = layout.positions.some(({ node }) => node.type !== "work")
  const hasDependencies = layout.edges.some(({ from }) => positions.get(from)?.node.type === "work")
  const compact = hasOrigins && !expanded
  const cardWidth = compact ? 132 : 176
  const columnWidth = compact ? 160 : 216
  const columns = 1 + Math.max(0, ...layout.positions.map(({ column }) => column))
  const width = Math.max(280, PAD * 2 + (columns - 1) * columnWidth + cardWidth)
  // In the sidebar, keep the three source columns in view at normal widths.
  // Paths and cards share the same horizontal scale. Deeper graphs can scroll.
  const minWidth = Math.min(width, columns * 84 + 20)
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
      {hasOrigins ? (
        <div className="mb-2 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            <span aria-hidden className="w-4 border-t-2 border-dashed border-primary/70" />
            {t("nuwa.originEdge")}
          </span>
          {hasDependencies ? (
            <span className="inline-flex items-center gap-1.5">
              <span aria-hidden className="w-4 border-t-2 border-muted-foreground" />
              {t("nuwa.dependencyEdge")}
            </span>
          ) : <span>{t("nuwa.noDependencies")}</span>}
        </div>
      ) : null}
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
              const origin = a.node.type !== "work"
              const x1 = PAD + a.column * columnWidth + cardWidth,
                y1 = PAD + a.row * ROW + CARD_HEIGHT / 2
              const x2 = PAD + b.column * columnWidth,
                y2 = PAD + b.row * ROW + CARD_HEIGHT / 2
              return (
                <path
                  key={`${from}:${to}`}
                  data-testid={origin ? "nuwa-origin-edge" : "nuwa-edge"}
                  data-from={from}
                  data-to={to}
                  d={`M ${x1} ${y1} C ${x1 + 24} ${y1}, ${x2 - 24} ${y2}, ${x2} ${y2}`}
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.5"
                  vectorEffect="non-scaling-stroke"
                  strokeDasharray={origin ? "4 3" : undefined}
                  className={origin ? "text-primary/70" : undefined}
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
            if (node.type !== "work") {
              const turn = node.type === "turn"
              const label = turn
                ? node.turnId
                : node.worker?.role || (node.agentId === MANAGER_ID ? t("nuwa.originManager") : node.agentId)
              return (
                <div
                  key={node.id}
                  data-testid="nuwa-origin"
                  data-node-id={node.id}
                  data-origin-kind={node.type}
                  data-turn-id={turn ? node.turnId : undefined}
                  data-agent-id={turn ? undefined : node.agentId}
                  className="absolute flex flex-col justify-center gap-1 rounded-lg border border-dashed border-primary/40 bg-card px-3 py-2"
                  style={style}
                >
                  <span className="text-[10px] text-muted-foreground">{t(turn ? "nuwa.originTurn" : "nuwa.originAuthor")}</span>
                  <span className="truncate text-xs font-medium" title={label}>{label}</span>
                  {!turn && node.worker?.model_binding?.model ? (
                    <span className="truncate text-[10px] text-muted-foreground" title={node.worker.model_binding.model}>
                      {node.worker.model_binding.model}
                    </span>
                  ) : null}
                </div>
              )
            }
            const record = node.record
            const author = record.agent_id === MANAGER_ID ? t("nuwa.originManager") : record.agent_id
            return (
              <button
                key={record.id}
                type="button"
                data-node-id={record.id}
                onClick={() => onSelect(record.id)}
                aria-pressed={selected === record.id}
                aria-description={author && record.turn_id
                  ? t("nuwa.originDescription", { agent: author, turn: record.turn_id })
                  : undefined}
                className={cn(
                  "absolute flex flex-col items-start gap-1 overflow-hidden rounded-lg border bg-card px-3 py-2 text-left shadow-xs transition-colors hover:border-primary/60 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring",
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
