import type { ReactNode } from "react"

import { cn } from "@/lib/cn"

export type Choice = { id: string; name: string; icon?: ReactNode }

/** One row of chips that answers "which one of these am I on". A radiogroup
 *  rather than a select: the options are few and the answer has to be
 *  readable without opening anything. */
export function ChoiceRail({
  label,
  value,
  choices,
  onChange,
}: {
  label: string
  value: string
  choices: Choice[]
  onChange: (id: string) => void
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <h2 className="px-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
        {label}
      </h2>
      <div role="radiogroup" aria-label={label} className="rail flex gap-1.5 overflow-x-auto pb-0.5">
        {choices.map((c) => (
          <button
            key={c.id || "default"}
            type="button"
            role="radio"
            aria-checked={c.id === value}
            className={cn(
              "inline-flex h-9 shrink-0 items-center gap-1.5 rounded-full px-3 text-sm transition-colors",
              c.id === value
                ? "bg-primary text-primary-foreground"
                : "bg-muted text-muted-foreground",
            )}
            onClick={() => onChange(c.id)}
          >
            {c.icon}
            <span className="max-w-40 truncate">{c.name}</span>
          </button>
        ))}
      </div>
    </div>
  )
}
