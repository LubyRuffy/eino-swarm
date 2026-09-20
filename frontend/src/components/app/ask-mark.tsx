import { CircleHelp } from "lucide-react"

import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"

/** Question mark for a blocked ask_user. The working breathe-dot means
 *  the swarm is busy; this means the next move is the human's. Chrome
 *  (sidebar, title bar) pings so a parked conversation still reads as
 *  "your move". The transcript card is already in the user's face, so
 *  pulse is off there: a whole dialog flashing is noise, not a cue. */
export function AskMark({
  className,
  pulse = true,
}: {
  className?: string
  pulse?: boolean
}) {
  const t = useT()
  return (
    <span
      data-testid="ask-mark"
      aria-label={t("status.asking")}
      className={cn("relative inline-flex size-3 shrink-0 text-ask", className)}
    >
      {pulse ? (
        <span
          aria-hidden
          className="absolute inset-0 rounded-full bg-ask/70 animate-ping"
        />
      ) : null}
      <CircleHelp className="relative size-full" aria-hidden />
    </span>
  )
}
