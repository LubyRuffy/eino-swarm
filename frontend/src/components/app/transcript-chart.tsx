import { memo, useEffect, useRef, useState, type ReactElement } from "react"
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts"
import type { TooltipProps } from "recharts"

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { chartSpecsEqual, type ChartSpec } from "@/lib/chart-spec"
import { useT } from "@/lib/use-t"

export const CHART_SERIES_PAINT = [
  "hsl(var(--chart-1))",
  "hsl(var(--chart-2))",
  "hsl(var(--chart-3))",
  "hsl(var(--chart-4))",
  "hsl(var(--chart-5))",
] as const

const AXIS = { fill: "hsl(var(--muted-foreground))", fontSize: 12 }
const GRID = "hsl(var(--border))"

function prefersReducedMotion(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) return false
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches
}

export const TranscriptChart = memo(function TranscriptChart({
  spec,
}: {
  spec: ChartSpec
}) {
  const t = useT()
  const label = spec.title || t("chart.untitled")
  const [view, setView] = useState("plot")
  return (
    <figure data-testid="transcript-chart" aria-label={label} className="min-w-0">
      <Tabs value={view} onValueChange={setView} className="min-w-0">
        <div className="mb-2 flex items-end justify-end gap-2">
          {spec.title ? (
            <figcaption className="mr-auto min-w-0 text-sm text-muted-foreground">
              {spec.title}
            </figcaption>
          ) : null}
          <TabsList aria-label={t("chart.views")}>
            <TabsTrigger value="plot">{t("chart.tabPlot")}</TabsTrigger>
            <TabsTrigger value="table">{t("chart.tabTable")}</TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="plot" className="overflow-visible">
          <div className="h-56 w-full">
            {view === "plot" ? <ChartPlot spec={spec} /> : null}
          </div>
        </TabsContent>
        <TabsContent value="table" className="overflow-auto">
          <ChartDataTable spec={spec} caption={label} />
        </TabsContent>
      </Tabs>
    </figure>
  )
}, specPropsEqual)

function specPropsEqual(
  prev: { spec: ChartSpec },
  next: { spec: ChartSpec },
): boolean {
  return chartSpecsEqual(prev.spec, next.spec)
}

/** Recharts restarts its grow animation whenever this subtree re-renders or
 *  ResponsiveContainer remounts (it flashes an empty -1×-1 frame first).
 *  Spec equality is the only reason to paint again; box size is handled
 *  inside ResponsiveContainer, which already ignores unchanged dimensions. */
const ChartPlot = memo(function ChartPlot({ spec }: { spec: ChartSpec }) {
  const animate = useRef(!prefersReducedMotion())
  useEffect(() => {
    animate.current = false
  }, [])
  return (
    <ResponsiveContainer width="100%" height="100%">
      {renderMark(spec, animate.current)}
    </ResponsiveContainer>
  )
}, specPropsEqual)

function renderMark(spec: ChartSpec, animate: boolean): ReactElement {
  if (spec.type === "pie") {
    return (
      <PieChart>
        <Pie
          data={spec.data}
          dataKey={spec.y[0]}
          nameKey={spec.x}
          cx="50%"
          cy="50%"
          innerRadius={0}
          outerRadius="80%"
          paddingAngle={1}
          isAnimationActive={animate}
        >
          {spec.data.map((_, i) => (
            <Cell key={i} fill={CHART_SERIES_PAINT[i % CHART_SERIES_PAINT.length]} />
          ))}
        </Pie>
        <Tooltip content={<ChartTooltip unit={spec.unit} />} />
        <Legend wrapperStyle={{ fontSize: 12 }} />
      </PieChart>
    )
  }

  const axes = cartesianEls(spec)
  const shared = {
    data: spec.data,
    margin: { top: 8, right: 8, bottom: 4, left: 4 },
  }
  if (spec.type === "line") {
    return (
      <LineChart {...shared}>
        {axes}
        {spec.y.map((key, i) => (
          <Line
            key={key}
            type="monotone"
            dataKey={key}
            stroke={CHART_SERIES_PAINT[i % CHART_SERIES_PAINT.length]}
            strokeWidth={2}
            dot={false}
            isAnimationActive={animate}
          />
        ))}
      </LineChart>
    )
  }
  if (spec.type === "area") {
    return (
      <AreaChart {...shared}>
        {axes}
        {spec.y.map((key, i) => (
          <Area
            key={key}
            type="monotone"
            dataKey={key}
            fill={CHART_SERIES_PAINT[i % CHART_SERIES_PAINT.length]}
            stroke={CHART_SERIES_PAINT[i % CHART_SERIES_PAINT.length]}
            fillOpacity={0.28}
            stackId={spec.stacked ? "s" : undefined}
            isAnimationActive={animate}
          />
        ))}
      </AreaChart>
    )
  }
  return (
    <BarChart {...shared}>
      {axes}
      {spec.y.map((key, i) => (
        <Bar
          key={key}
          dataKey={key}
          fill={CHART_SERIES_PAINT[i % CHART_SERIES_PAINT.length]}
          radius={[3, 3, 0, 0]}
          maxBarSize={48}
          stackId={spec.stacked ? "s" : undefined}
          isAnimationActive={animate}
        />
      ))}
    </BarChart>
  )
}

function cartesianEls(spec: ChartSpec): ReactElement[] {
  // Recharts matches children by component type. A fragment is one child
  // that is not an Axis, so the plot would render with no scales.
  const els: ReactElement[] = [
    <CartesianGrid key="grid" stroke={GRID} vertical={false} />,
    <XAxis
      key="x"
      dataKey={spec.x}
      tick={AXIS}
      axisLine={false}
      tickLine={false}
      interval="preserveStartEnd"
    />,
    <YAxis
      key="y"
      tick={AXIS}
      axisLine={false}
      tickLine={false}
      width={40}
      tickFormatter={(v: number) => formatTick(v, spec.unit)}
    />,
    <Tooltip key="tip" content={<ChartTooltip unit={spec.unit} />} />,
  ]
  if (spec.y.length > 1) {
    els.push(<Legend key="legend" wrapperStyle={{ fontSize: 12 }} />)
  }
  return els
}

function ChartTooltip({
  active,
  payload,
  label,
  unit,
}: TooltipProps<number, string> & { unit?: string }) {
  if (!active || !payload?.length) return null
  return (
    <div className="rounded-md border border-border bg-popover px-2 py-1.5 text-xs text-popover-foreground">
      {label != null && label !== "" ? (
        <div className="mb-1 font-medium">{String(label)}</div>
      ) : null}
      <ul className="space-y-0.5">
        {payload.map((row) => (
          <li key={String(row.dataKey)} className="flex items-center gap-2">
            <span
              className="size-2 shrink-0 rounded-sm"
              style={{ background: String(row.color ?? CHART_SERIES_PAINT[0]) }}
            />
            <span className="text-muted-foreground">{row.name}</span>
            <span>{formatTick(Number(row.value), unit ?? "")}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

function ChartDataTable({ spec, caption }: { spec: ChartSpec; caption: string }) {
  return (
    <table className="w-full" aria-label={caption}>
      <thead>
        <tr>
          <th>{spec.x}</th>
          {spec.y.map((key) => (
            <th key={key}>
              {spec.unit && spec.unit !== key ? `${key} (${spec.unit})` : key}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {spec.data.map((row, i) => (
          <tr key={i}>
            <td>{row[spec.x]}</td>
            {spec.y.map((key) => (
              <td key={key} className="tabular-nums">
                {row[key]}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function formatTick(n: number, unit: string): string {
  if (!Number.isFinite(n)) return ""
  const abs = Math.abs(n)
  const text =
    abs >= 100 || Number.isInteger(n) ? String(Math.round(n)) : n.toFixed(1)
  return unit ? `${text} ${unit}` : text
}
