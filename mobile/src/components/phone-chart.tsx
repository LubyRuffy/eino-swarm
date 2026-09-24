import { memo, useState, type ReactNode } from "react"

import { chartSpecsEqual, type ChartSpec } from "@/lib/chart-spec"
import { t } from "@/lib/i18n"
import { cn } from "@/lib/cn"

const PAINT = [
  "hsl(var(--chart-1))",
  "hsl(var(--chart-2))",
  "hsl(var(--chart-3))",
  "hsl(var(--chart-4))",
  "hsl(var(--chart-5))",
] as const

const AXIS = "hsl(var(--muted-foreground))"
const GRID = "hsl(var(--border))"

/** A closed `chart` fence. Same rows as the PC plot, drawn in the webview
 *  without pulling the desktop chart library into the phone bundle. */
export const PhoneChart = memo(function PhoneChart({ spec }: { spec: ChartSpec }) {
  const label = spec.title || t("chart.untitled")
  const [view, setView] = useState<"plot" | "table">("plot")
  return (
    <figure data-testid="phone-chart" aria-label={label} className="my-2 min-w-0">
      <div className="mb-2 flex items-end gap-2">
        {spec.title ? (
          <figcaption className="mr-auto min-w-0 text-sm text-muted-foreground">
            {spec.title}
          </figcaption>
        ) : (
          <span className="mr-auto" />
        )}
        <div role="tablist" aria-label={t("chart.views")} className="flex shrink-0 rounded-md bg-muted p-0.5">
          <ChartTab selected={view === "plot"} onSelect={() => setView("plot")}>
            {t("chart.tabPlot")}
          </ChartTab>
          <ChartTab selected={view === "table"} onSelect={() => setView("table")}>
            {t("chart.tabTable")}
          </ChartTab>
        </div>
      </div>
      {view === "plot" ? <ChartPlot spec={spec} /> : <ChartTable spec={spec} caption={label} />}
    </figure>
  )
}, (prev, next) => chartSpecsEqual(prev.spec, next.spec))

export function PhoneChartPending() {
  return (
    <div
      data-testid="phone-chart-pending"
      className="my-2 h-40 animate-pulse rounded-md bg-muted"
      aria-busy="true"
    >
      <span className="sr-only">{t("chart.pending")}</span>
    </div>
  )
}

function ChartTab({
  selected,
  onSelect,
  children,
}: {
  selected: boolean
  onSelect: () => void
  children: ReactNode
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      className={cn(
        "h-7 rounded px-2 text-xs",
        selected ? "bg-background text-foreground" : "text-muted-foreground",
      )}
      onClick={onSelect}
    >
      {children}
    </button>
  )
}

function ChartPlot({ spec }: { spec: ChartSpec }) {
  if (spec.type === "pie") return <PiePlot spec={spec} />
  return <CartesianPlot spec={spec} />
}

const VB_W = 360
const VB_H = 200
const PAD_L = 46
const PAD_R = 8
const PAD_T = 8
const PAD_B = 28

function CartesianPlot({ spec }: { spec: ChartSpec }) {
  const { lo, hi } = domain(spec)
  const innerW = VB_W - PAD_L - PAD_R
  const innerH = VB_H - PAD_T - PAD_B
  const ticks = 4
  const yAt = (v: number) => PAD_T + (1 - (v - lo) / (hi - lo || 1)) * innerH
  const xAt = (i: number) =>
    PAD_L + (spec.data.length <= 1 ? innerW / 2 : (i / (spec.data.length - 1)) * innerW)
  const baseline = yAt(Math.min(Math.max(lo, 0), hi))
  const xLabels = labelIndexes(spec.data.length)

  return (
    <svg
      data-testid="phone-chart-plot"
      viewBox={`0 0 ${VB_W} ${VB_H}`}
      className="h-52 w-full"
      role="img"
    >
      {Array.from({ length: ticks + 1 }, (_, i) => {
        const v = lo + ((hi - lo) * i) / ticks
        const y = yAt(v)
        return (
          <g key={i}>
            <line x1={PAD_L} x2={VB_W - PAD_R} y1={y} y2={y} stroke={GRID} />
            <text x={PAD_L - 4} y={y + 3} textAnchor="end" fill={AXIS} fontSize="10">
              {formatTick(v, spec.unit)}
            </text>
          </g>
        )
      })}
      {spec.type === "bar" ? (
        <Bars spec={spec} xAt={xAt} yAt={yAt} baseline={baseline} innerW={innerW} />
      ) : (
        spec.y.map((key, series) => (
          <SeriesMark
            key={key}
            spec={spec}
            seriesKey={key}
            series={series}
            xAt={xAt}
            yAt={yAt}
            baseline={baseline}
          />
        ))
      )}
      {xLabels.map((i) => (
        <text
          key={i}
          x={xAt(i)}
          y={VB_H - 8}
          textAnchor="middle"
          fill={AXIS}
          fontSize="10"
        >
          {String(spec.data[i][spec.x])}
        </text>
      ))}
      {spec.y.length > 1 ? <SeriesLegend keys={spec.y} /> : null}
    </svg>
  )
}

function SeriesMark({
  spec,
  seriesKey,
  series,
  xAt,
  yAt,
  baseline,
}: {
  spec: ChartSpec
  seriesKey: string
  series: number
  xAt: (i: number) => number
  yAt: (v: number) => number
  baseline: number
}) {
  const color = PAINT[series % PAINT.length]
  const pts = spec.data.map((row, i) => `${xAt(i)},${yAt(Number(row[seriesKey]))}`)
  if (spec.type === "area") {
    const first = xAt(0)
    const last = xAt(spec.data.length - 1)
    return (
      <path
        d={`M ${first} ${baseline} L ${pts.join(" L ")} L ${last} ${baseline} Z`}
        fill={color}
        fillOpacity={0.28}
        stroke={color}
        strokeWidth={2}
      />
    )
  }
  return <polyline points={pts.join(" ")} fill="none" stroke={color} strokeWidth={2} />
}

function Bars({
  spec,
  xAt,
  yAt,
  baseline,
  innerW,
}: {
  spec: ChartSpec
  xAt: (i: number) => number
  yAt: (v: number) => number
  baseline: number
  innerW: number
}) {
  const slot = spec.data.length <= 1 ? innerW : innerW / (spec.data.length - 1)
  const group = Math.min(28, slot * 0.55)
  return (
    <>
      {spec.data.map((row, i) => {
        if (spec.stacked) {
          let acc = 0
          return spec.y.map((key, series) => {
            const v = Number(row[key])
            const y0 = yAt(acc)
            acc += v
            const y1 = yAt(acc)
            const top = Math.min(y0, y1)
            return (
              <rect
                key={key}
                x={xAt(i) - group / 2}
                y={top}
                width={group}
                height={Math.max(1, Math.abs(y1 - y0))}
                fill={PAINT[series % PAINT.length]}
                rx={2}
              />
            )
          })
        }
        const each = group / spec.y.length
        return spec.y.map((key, series) => {
          const y = yAt(Number(row[key]))
          const top = Math.min(y, baseline)
          return (
            <rect
              key={key}
              x={xAt(i) - group / 2 + series * each}
              y={top}
              width={Math.max(2, each - 1)}
              height={Math.max(1, Math.abs(baseline - y))}
              fill={PAINT[series % PAINT.length]}
              rx={2}
            />
          )
        })
      })}
    </>
  )
}

function SeriesLegend({ keys }: { keys: string[] }) {
  return (
    <g>
      {keys.map((key, i) => (
        <g key={key} transform={`translate(${PAD_L + i * 72}, 4)`}>
          <rect width="8" height="8" fill={PAINT[i % PAINT.length]} rx="1" />
          <text x="12" y="8" fill={AXIS} fontSize="10">
            {key}
          </text>
        </g>
      ))}
    </g>
  )
}

function PiePlot({ spec }: { spec: ChartSpec }) {
  const key = spec.y[0]
  const total = spec.data.reduce((sum, row) => sum + Number(row[key]), 0) || 1
  const cx = 140
  const cy = 100
  const r = 68
  let angle = -Math.PI / 2
  return (
    <svg data-testid="phone-chart-plot" viewBox={`0 0 ${VB_W} ${VB_H}`} className="h-52 w-full" role="img">
      {spec.data.map((row, i) => {
        const sweep = (Number(row[key]) / total) * Math.PI * 2
        const start = angle
        angle += sweep
        return (
          <path
            key={i}
            d={slicePath(cx, cy, r, start, angle)}
            fill={PAINT[i % PAINT.length]}
          />
        )
      })}
      {spec.data.map((row, i) => (
        <text key={i} x={230} y={36 + i * 16} fill={AXIS} fontSize="11">
          {String(row[spec.x])}
        </text>
      ))}
    </svg>
  )
}

function slicePath(cx: number, cy: number, r: number, a0: number, a1: number): string {
  const sweep = a1 - a0
  if (sweep >= Math.PI * 2 - 0.001) {
    return `M ${cx - r} ${cy} A ${r} ${r} 0 1 1 ${cx + r} ${cy} A ${r} ${r} 0 1 1 ${cx - r} ${cy} Z`
  }
  const large = sweep > Math.PI ? 1 : 0
  const x0 = cx + r * Math.cos(a0)
  const y0 = cy + r * Math.sin(a0)
  const x1 = cx + r * Math.cos(a1)
  const y1 = cy + r * Math.sin(a1)
  return `M ${cx} ${cy} L ${x0} ${y0} A ${r} ${r} 0 ${large} 1 ${x1} ${y1} Z`
}

function ChartTable({ spec, caption }: { spec: ChartSpec; caption: string }) {
  return (
    <div className="min-w-0 max-w-full overflow-x-auto">
      <table className="w-full text-sm" aria-label={caption}>
        <thead>
          <tr>
            <th className="px-2 py-1 text-left font-medium">{spec.x}</th>
            {spec.y.map((key) => (
              <th key={key} className="px-2 py-1 text-left font-medium">
                {spec.unit && spec.unit !== key ? `${key} (${spec.unit})` : key}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {spec.data.map((row, i) => (
            <tr key={i}>
              <td className="px-2 py-1">{row[spec.x]}</td>
              {spec.y.map((key) => (
                <td key={key} className="px-2 py-1 tabular-nums">
                  {row[key]}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function domain(spec: ChartSpec): { lo: number; hi: number } {
  const nums = plottedNumbers(spec)
  let lo = Math.min(...nums)
  let hi = Math.max(...nums)
  if (lo >= 0) lo = 0
  if (hi <= 0) hi = 0
  if (lo === hi) hi = lo + 1
  return { lo, hi: niceCeil(hi) }
}

function plottedNumbers(spec: ChartSpec): number[] {
  const stacked = spec.stacked && spec.type !== "line"
  const nums: number[] = []
  for (const row of spec.data) {
    if (stacked) {
      nums.push(spec.y.reduce((sum, key) => sum + Number(row[key]), 0))
    } else {
      for (const key of spec.y) nums.push(Number(row[key]))
    }
  }
  return nums.length ? nums : [0, 1]
}

function niceCeil(n: number): number {
  if (n <= 0) return 0
  const exp = 10 ** Math.floor(Math.log10(n))
  const frac = n / exp
  const nice = frac <= 1 ? 1 : frac <= 2 ? 2 : frac <= 5 ? 5 : 10
  return nice * exp
}

function labelIndexes(count: number): number[] {
  if (count <= 6) return Array.from({ length: count }, (_, i) => i)
  const step = Math.ceil((count - 1) / 5)
  const out: number[] = []
  for (let i = 0; i < count; i += step) out.push(i)
  if (out[out.length - 1] !== count - 1) out.push(count - 1)
  return out
}

function formatTick(n: number, unit: string): string {
  if (!Number.isFinite(n)) return ""
  const abs = Math.abs(n)
  const text = abs >= 100 || Number.isInteger(n) ? String(Math.round(n)) : n.toFixed(1)
  return unit ? `${text} ${unit}` : text
}
