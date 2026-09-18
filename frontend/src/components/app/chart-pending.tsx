import { Skeleton } from "@/components/ui/skeleton"
import { useT } from "@/lib/use-t"

/** Shown while a `chart` fence is still truncated. Kept out of the Recharts
 *  module so a live answer does not download the plotter to display a pulse. */
export function ChartPending() {
  const t = useT()
  return (
    <div data-testid="transcript-chart-pending">
      <Skeleton className="h-56 w-full" />
      <span className="sr-only">{t("chart.pending")}</span>
    </div>
  )
}
