import { type FormEvent, type ReactNode } from "react"

import { settingsSelectTriggerClass } from "@/components/app/settings-field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { chromeTypeClass } from "@/lib/chrome-type"
import { applyRunsIn, runsInThreadId } from "@/lib/schedule-dest"
import { ScheduleRunsInPicker } from "@/components/app/schedule-runs-in"
import {
  cadenceCreateFields,
  type ScheduleCadenceKind,
} from "@/lib/schedule-view"
import type { ScheduleCreate } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"
import { useProjects } from "@/store/projects"

export type Cadence = ScheduleCadenceKind

function FormRow({
  label,
  htmlFor,
  children,
}: {
  label: string
  htmlFor?: string
  children: ReactNode
}) {
  return (
    <div className="flex items-center justify-between gap-3 py-2.5">
      <label
        htmlFor={htmlFor}
        className={cn(chromeTypeClass, "shrink-0 text-foreground/90")}
      >
        {label}
      </label>
      <div className="flex min-w-0 justify-end">{children}</div>
    </div>
  )
}

function FormSection({
  title,
  children,
}: {
  title: string
  children: ReactNode
}) {
  return (
    <section className="flex shrink-0 flex-col">
      <h3 className={cn(chromeTypeClass, "mb-0.5 font-medium text-foreground")}>
        {title}
      </h3>
      <div className="divide-y divide-border border-y border-border">{children}</div>
    </section>
  )
}

/** Fields for a wait drawer. A live edit can change where the next fire
 *  lands; a finished wait is read-only. Title rides the same save. */
export function ScheduleInboxForm({
  expanded = false,
  title,
  prompt,
  cadence,
  cadenceValue,
  projectId,
  runsIn,
  readOnly = false,
  disabled = false,
  canSubmit,
  submitLabel,
  extraActions,
  onPrompt,
  onCadence,
  onCadenceValue,
  onProjectId,
  onRunsIn,
  onSubmit,
}: {
  expanded?: boolean
  title?: string
  prompt: string
  cadence: Cadence
  cadenceValue: string
  projectId: string
  runsIn: string
  readOnly?: boolean
  disabled?: boolean
  canSubmit?: boolean
  submitLabel?: string
  extraActions?: ReactNode
  onPrompt: (v: string) => void
  onCadence: (v: Cadence) => void
  onCadenceValue: (v: string) => void
  onProjectId: (v: string) => void
  onRunsIn: (v: string) => void
  onSubmit: (body: ScheduleCreate) => void
}) {
  const t = useT()
  const projects = useProjects((s) => s.projects)
  const waking = Boolean(runsInThreadId(runsIn))
  const cadenceLabel =
    cadence === "every"
      ? t("schedule.every")
      : cadence === "cron"
        ? t("schedule.cron")
        : t("schedule.delay")
  const fieldsLocked = readOnly || disabled
  const selectWidth = expanded ? "max-w-[22rem]" : "max-w-[13rem]"

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (readOnly || disabled || canSubmit === false) return
    const body: ScheduleCreate = {
      prompt: prompt.trim(),
      ...cadenceCreateFields(cadence, cadenceValue),
    }
    if (title !== undefined) body.title = title.trim()
    onSubmit(applyRunsIn(body, runsIn, projectId))
  }

  return (
    <form className="flex min-h-0 flex-1 flex-col" onSubmit={submit}>
      <div
        className={cn(
          "thin-scrollbar flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto",
          expanded ? "px-6 py-5" : "px-4 py-4",
        )}
      >
        <Textarea
          id="schedule-prompt"
          required
          readOnly={fieldsLocked}
          value={prompt}
          onChange={(e) => onPrompt(e.target.value)}
          aria-label={t("schedule.prompt")}
          placeholder={t("schedule.promptPlaceholder")}
          className={cn(
            "text-base shadow-none focus-visible:ring-0",
            expanded
              ? "h-[min(32rem,50vh)] min-h-[16rem] resize-none overflow-y-auto rounded-xl border border-border bg-background px-3 py-3"
              : "min-h-[10rem] shrink-0 border-0 bg-transparent px-0",
          )}
        />
        <FormSection title={t("schedule.details")}>
          <FormRow label={t("schedule.runsIn")} htmlFor="schedule-runs-in">
            <ScheduleRunsInPicker
              id="schedule-runs-in"
              value={runsIn}
              disabled={fieldsLocked}
              expanded={expanded}
              onChange={onRunsIn}
            />
          </FormRow>
          {waking ? null : (
            <FormRow label={t("schedule.project")} htmlFor="schedule-project">
              <Select value={projectId} onValueChange={onProjectId} disabled={fieldsLocked}>
                <SelectTrigger
                  id="schedule-project"
                  aria-label={t("schedule.project")}
                  className={cn(settingsSelectTriggerClass, selectWidth)}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">{t("schedule.projectNone")}</SelectItem>
                  {projects.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </FormRow>
          )}
        </FormSection>
        <FormSection title={t("schedule.frequency")}>
          <FormRow label={t("schedule.repeat")} htmlFor="schedule-cadence">
            <Select
              value={cadence}
              onValueChange={(v) => onCadence(v as Cadence)}
              disabled={fieldsLocked}
            >
              <SelectTrigger
                id="schedule-cadence"
                aria-label={t("schedule.repeat")}
                className={cn(settingsSelectTriggerClass, selectWidth)}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="delay">{t("schedule.repeatDelay")}</SelectItem>
                <SelectItem value="every">{t("schedule.repeatEvery")}</SelectItem>
                <SelectItem value="cron">{t("schedule.repeatCron")}</SelectItem>
              </SelectContent>
            </Select>
          </FormRow>
          <FormRow label={cadenceLabel} htmlFor="schedule-cadence-value">
            <Input
              id="schedule-cadence-value"
              type={cadence === "cron" ? "text" : "number"}
              min={cadence === "cron" ? undefined : 1}
              readOnly={fieldsLocked}
              value={cadenceValue}
              onChange={(e) => onCadenceValue(e.target.value)}
              aria-label={cadenceLabel}
              placeholder={cadence === "cron" ? t("schedule.cronPlaceholder") : undefined}
              className={cn(
                chromeTypeClass,
                "h-[32px] shadow-none",
                cadence === "cron"
                  ? "w-40"
                  : "w-[5.75rem] px-2 text-right tabular-nums",
              )}
            />
          </FormRow>
        </FormSection>
      </div>
      {readOnly && !extraActions ? null : (
        <div
          className={cn(
            "flex shrink-0 items-center border-t border-border px-4 py-3",
            extraActions ? "gap-1" : "justify-end",
          )}
        >
          {extraActions}
          {readOnly ? null : (
            <Button
              type="submit"
              className={extraActions ? "ml-auto" : undefined}
              disabled={disabled || canSubmit === false}
            >
              {submitLabel ?? t("schedule.create")}
            </Button>
          )}
        </div>
      )}
    </form>
  )
}
