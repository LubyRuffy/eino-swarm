import { useId, useMemo } from "react"
import { Maximize2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { layoutNuwa, type NuwaNode } from "@/lib/nuwa"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"
import { NuwaStatus, NuwaStale, nuwaKind } from "./nuwa-status"

const CARD_WIDTH = 176
const CARD_HEIGHT = 80
const COLUMN = 216
const ROW = 108
const PAD = 16

/** Buttons overlay the SVG paths so the same graph works by keyboard and
 * screen reader. The layout is derived solely from recorded dependency IDs. */
export function NuwaGraph({
  nodes,
  selected,
  onSelect,
  onExpand,
  expanded = false,
}: {
  nodes: NuwaNode[]
  selected?: string
  onSelect: (id: string) => void
  onExpand?: () => void
  expanded?: boolean
}) {
  const t = useT()
  const marker = useId().replace(/:/g, "")
  const layout = useMemo(() => layoutNuwa(nodes), [nodes])
  const positions = new Map(layout.positions.map((position) => [position.node.id, position]))
  const width = Math.max(
    280,
    ...layout.positions.map(({ column }) => PAD * 2 + column * COLUMN + CARD_WIDTH)
  )
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
      <div
        role="region"
        aria-label={t("nuwa.graph")}
        tabIndex={0}
        className={cn(
          "thin-scrollbar overflow-auto rounded-lg border border-border bg-muted/20",
          expanded ? "min-h-0 flex-1" : "max-h-72"
        )}
      >
        <div className="relative" style={{ width, height }}>
          <svg
            width={width}
            height={height}
            className="pointer-events-none absolute inset-0 text-muted-foreground/50"
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
              const x1 = PAD + a.column * COLUMN + CARD_WIDTH,
                y1 = PAD + a.row * ROW + CARD_HEIGHT / 2
              const x2 = PAD + b.column * COLUMN,
                y2 = PAD + b.row * ROW + CARD_HEIGHT / 2
              return (
                <path
                  key={`${from}:${to}`}
                  data-testid="nuwa-edge"
                  d={`M ${x1} ${y1} C ${x1 + 24} ${y1}, ${x2 - 24} ${y2}, ${x2} ${y2}`}
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.5"
                  markerEnd={`url(#${marker})`}
                />
              )
            })}
          </svg>
          {layout.positions.map(({ node, column, row }) => (
            <button
              key={node.id}
              type="button"
              onClick={() => onSelect(node.id)}
              aria-pressed={selected === node.id}
              className={cn(
                "absolute flex flex-col items-start gap-1 overflow-hidden rounded-lg border bg-card px-3 py-2 text-left shadow-xs transition-colors hover:border-primary/60 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring",
                selected === node.id ? "border-primary ring-1 ring-primary/30" : "border-border"
              )}
              style={{
                width: CARD_WIDTH,
                height: CARD_HEIGHT,
                left: PAD + column * COLUMN,
                top: PAD + row * ROW,
              }}
            >
              <span className="text-[10px] text-muted-foreground">{nuwaKind(node.kind, t)}</span>
              <span className="w-full truncate text-xs font-medium" title={node.title}>
                {node.title}
              </span>
              <span className="flex max-w-full items-center gap-1">
                <NuwaStatus status={node.status} />
                <NuwaStale node={node} />
              </span>
            </button>
          ))}
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
