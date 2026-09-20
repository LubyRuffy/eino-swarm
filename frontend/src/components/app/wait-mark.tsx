import { Clock } from "lucide-react"

import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"

/** Breathing clock for a parked wait. A still glyph next to Idle looks like
 *  the conversation died; this is the next turn sitting on next_run_at. */
export function WaitMark({ className }: { className?: string }) {
  const t = useT()
  return (
    <span
      data-testid="wait-mark"
      aria-label={t("status.waiting")}
      className={cn("inline-flex size-3 shrink-0 text-running", className)}
    >
      <Clock className="size-full animate-breathe" aria-hidden />
    </span>
  )
}
