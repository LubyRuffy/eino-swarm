import { cn } from "@/lib/cn"
import { t } from "@/lib/i18n"
import { timeAgo } from "@/lib/when"

export type RowState = "idle" | "running" | "waiting" | "ask"

/** A row has to answer "does this need me" before it is read. State is a
 *  badge, not a two-pixel dot, and the age sits opposite the title. */
export function InboxRow({
  id,
  title,
  detail,
  state,
  at,
  onOpen,
}: {
  id: string
  title: string
  detail?: string
  state: RowState
  at?: string
  onOpen: (id: string) => void
}) {
  const age = timeAgo(at)
  return (
    <li>
      <button
        type="button"
        aria-label={t("home.open", { title })}
        className={cn(
          "flex w-full flex-col gap-0.5 px-3 py-2 text-left",
          "transition-colors active:bg-accent",
        )}
        onClick={() => onOpen(id)}
      >
        <span className="flex w-full min-w-0 items-baseline gap-2">
          <span className="min-w-0 flex-1 truncate text-sm font-medium leading-5">
            {title}
          </span>
          {age ? (
            <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">
              {age}
            </span>
          ) : null}
        </span>
        <span className="flex w-full min-w-0 items-center gap-2">
          {state === "idle" ? null : <StatusBadge state={state} />}
          {detail ? (
            <span className="min-w-0 flex-1 truncate text-xs leading-4 text-muted-foreground">
              {detail}
            </span>
          ) : null}
        </span>
      </button>
    </li>
  )
}

function StatusBadge({ state }: { state: RowState }) {
  const text =
    state === "ask"
      ? t("home.ask")
      : state === "waiting"
        ? t("home.waiting")
        : t("home.running")
  return (
    <span
      data-testid="row-state"
      data-state={state}
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium",
        state === "ask"
          ? "bg-[hsl(var(--running))] text-background"
          : "bg-[hsl(var(--running)/0.14)] text-[hsl(var(--running))]",
      )}
    >
      {state === "running" ? (
        <span
          className="size-1.5 rounded-full bg-current motion-safe:animate-pulse"
          aria-hidden
        />
      ) : null}
      {text}
    </span>
  )
}
