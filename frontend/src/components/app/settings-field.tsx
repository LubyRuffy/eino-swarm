import { Children, cloneElement, useId, type ReactNode } from "react"

import {
  Select,
  SelectContent,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { cn } from "@/lib/utils"

export function settingsMatch(
  query: string,
  ...parts: Array<string | undefined | null>
) {
  const q = query.trim().toLowerCase()
  if (!q) return true
  return parts.some((p) => (p ?? "").toLowerCase().includes(q))
}

/** One settings row. Title + hint left, a compact control hugging the right. */
export const settingsRowClass =
  "flex items-start justify-between gap-4 px-4 py-3"

/** Title and hint share a size. Color is the hierarchy, not weight or a
 *  smaller caption — a medium/large chrome size must not turn this into a
 *  bold form. 13px is px so Settings stays this quiet when the window is Large. */
export const settingsLabelClass =
  "text-[13px] font-normal leading-snug text-foreground/90"

export const settingsHintClass =
  "mt-0.5 text-[13px] font-normal leading-snug text-muted-foreground"

/** Light bordered control, same type as the row, not a grey chip. */
export const settingsSelectTriggerClass =
  "h-[32px] w-auto max-w-[14rem] border-border bg-background px-2.5 text-[13px] font-normal text-foreground shadow-none"

function fieldControlClass(wide?: boolean) {
  return cn(
    "[&_input]:h-[32px] [&_input]:text-[13px] [&_input]:shadow-none",
    "[&_input[type=number]]:w-[5.75rem] [&_input[type=number]]:px-2 [&_input[type=number]]:text-right [&_input[type=number]]:tabular-nums",
    wide
      ? "[&_input:not([type=number])]:w-72"
      : "[&_input:not([type=number])]:w-56",
  )
}

export function SettingsCopy({
  label,
  hint,
  badge,
  htmlFor,
}: {
  label: string
  hint?: string
  badge?: ReactNode
  htmlFor?: string
}) {
  const title = htmlFor ? (
    <label htmlFor={htmlFor} className={settingsLabelClass}>
      {label}
    </label>
  ) : (
    <p className={settingsLabelClass}>{label}</p>
  )
  return (
    <div className="min-w-0 flex-1">
      {badge ? (
        <div className="flex flex-wrap items-center gap-2">
          {title}
          {badge}
        </div>
      ) : (
        title
      )}
      {hint ? <p className={settingsHintClass}>{hint}</p> : null}
    </div>
  )
}

export function SettingsRow({
  label,
  hint,
  badge,
  query = "",
  search,
  htmlFor,
  controlClassName,
  children,
}: {
  label: string
  hint?: string
  badge?: ReactNode
  query?: string
  search?: Array<string | undefined | null>
  htmlFor?: string
  controlClassName?: string
  children?: ReactNode
}) {
  if (!settingsMatch(query, label, hint, ...(search ?? []))) return null
  return (
    <div className={settingsRowClass} data-settings-row="">
      <SettingsCopy label={label} hint={hint} badge={badge} htmlFor={htmlFor} />
      {children ? (
        <div
          className={cn(
            "flex shrink-0 items-center justify-end pt-0.5",
            controlClassName,
          )}
        >
          {children}
        </div>
      ) : null}
    </div>
  )
}

/** One settings row: name and hint on the left, the control on the right.
 *  Chat-style sheets read as a list of decisions, not a stacked form. */
export function Field({
  label,
  hint,
  query = "",
  wide,
  controlClassName,
  children,
}: {
  label: string
  hint?: string
  query?: string
  /** URL / token fields. Numbers stay a short box either way. */
  wide?: boolean
  controlClassName?: string
  children: React.ReactElement<{ id?: string }>
}) {
  const id = useId()
  return (
    <SettingsRow
      label={label}
      hint={hint}
      query={query}
      htmlFor={id}
      controlClassName={cn(fieldControlClass(wide), controlClassName)}
    >
      {cloneElement(children, { id })}
    </SettingsRow>
  )
}

export function SettingsChoice({
  label,
  hint,
  badge,
  query = "",
  search,
  value,
  onValueChange,
  placeholder,
  ariaLabel,
  children,
}: {
  label: string
  hint?: string
  badge?: ReactNode
  query?: string
  search?: Array<string | undefined | null>
  value?: string
  onValueChange: (value: string) => void
  placeholder?: string
  ariaLabel?: string
  children: ReactNode
}) {
  return (
    <SettingsRow
      label={label}
      hint={hint}
      badge={badge}
      query={query}
      search={search}
    >
      <Select value={value} onValueChange={onValueChange}>
        <SelectTrigger
          aria-label={ariaLabel ?? label}
          className={settingsSelectTriggerClass}
        >
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>{children}</SelectContent>
      </Select>
    </SettingsRow>
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
    <section className="flex flex-col gap-2 [&:not(:has([data-settings-row]))]:hidden">
      {title || action ? (
        <div className="flex items-end justify-between gap-3 px-0.5">
          <div className="min-w-0">
            {title ? (
              <h3 className="text-[13px] font-medium text-foreground/90">
                {title}
              </h3>
            ) : null}
            {description ? (
              <p className="mt-0.5 text-[13px] font-normal leading-snug text-muted-foreground">
                {description}
              </p>
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
    <div className="flex w-full max-w-3xl flex-col gap-8 px-12 py-8">
      <div className="flex flex-col gap-1">
        <h2 className="text-2xl font-semibold tracking-tight text-foreground">
          {title}
        </h2>
        {description ? (
          <p className="text-[13px] font-normal leading-snug text-muted-foreground">
            {description}
          </p>
        ) : null}
      </div>
      {children}
    </div>
  )
}
