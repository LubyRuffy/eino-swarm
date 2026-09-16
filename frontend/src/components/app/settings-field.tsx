import { Children, cloneElement, useId, type ReactNode } from "react"

import { Label } from "@/components/ui/label"
import { cn } from "@/lib/utils"

export function settingsMatch(
  query: string,
  ...parts: Array<string | undefined | null>
) {
  const q = query.trim().toLowerCase()
  if (!q) return true
  return parts.some((p) => (p ?? "").toLowerCase().includes(q))
}

/** One settings row: name and hint on the left, the control on the right.
 *  Chat-style sheets read as a list of decisions, not a stacked form. */
export function Field({
  label,
  hint,
  query = "",
  controlClassName,
  children,
}: {
  label: string
  hint?: string
  query?: string
  controlClassName?: string
  children: React.ReactElement<{ id?: string }>
}) {
  const id = useId()
  if (!settingsMatch(query, label, hint)) return null
  return (
    <div className="flex items-start justify-between gap-6 px-4 py-3.5" data-settings-row="">
      <div className="min-w-0 flex-1">
        <Label htmlFor={id}>{label}</Label>
        {hint ? (
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            {hint}
          </p>
        ) : null}
      </div>
      <div className={cn("flex w-56 shrink-0 justify-end", controlClassName)}>
        {cloneElement(children, { id })}
      </div>
    </div>
  )
}

export function SettingsSection({
  title,
  description,
  action,
  children,
}: {
  title?: string
  description?: string
  action?: ReactNode
  children: ReactNode
}) {
  const visible = Children.toArray(children).filter(Boolean)
  if (visible.length === 0) return null
  return (
    <section className="flex flex-col gap-3 [&:not(:has([data-settings-row]))]:hidden">
      {title || action ? (
        <div className="flex items-end justify-between gap-3 px-0.5">
          <div className="min-w-0">
            {title ? <h3 className="text-sm font-medium">{title}</h3> : null}
            {description ? (
              <p className="mt-1 text-sm text-muted-foreground">{description}</p>
            ) : null}
          </div>
          {action}
        </div>
      ) : null}
      <div className="divide-y divide-border overflow-hidden rounded-xl border border-border bg-card">
        {visible}
      </div>
    </section>
  )
}

export function SettingsPage({
  title,
  description,
  children,
}: {
  title: string
  description?: string
  children: ReactNode
}) {
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-8 px-8 py-8">
      <div className="flex flex-col gap-1">
        <h2 className="text-2xl font-semibold tracking-tight">{title}</h2>
        {description ? (
          <p className="text-sm text-muted-foreground">{description}</p>
        ) : null}
      </div>
      {children}
    </div>
  )
}
