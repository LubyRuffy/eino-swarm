import { ChevronDown } from "lucide-react"
import { useEffect } from "react"

import { Button } from "@/components/ui/button"
import { ReviewProgressBar } from "./review-progress-bar"
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel,
  DropdownMenuSeparator, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { hasPendingDistillation, isActiveDistillation, type DistillErrorCode, type DistillScope } from "@/lib/distillation"
import { firstByteTimeoutDuration, firstByteTimeoutSettingsKey } from "@/lib/first-byte-timeout"
import { MAX_COMPLETION_SETTINGS_KEY } from "@/lib/output-budget"
import { useT } from "@/lib/use-t"
import { formatBytes } from "@/lib/utils"
import { useApp } from "@/store/app"
import { useDistillation } from "@/store/distillation"
import { openSettings } from "@/store/settings-sheet"
import { useTraceFocus } from "@/store/trace-focus"

export interface ProjectDistillProps {
  projectId: string
  threadId?: string
}

const scopes = [
  ["latest_turn", "distill.latest"],
  ["thread", "distill.task"],
  ["project", "distill.project"],
] as const

const fullTextScopes = [
  ["thread", "distill.taskFullText"],
  ["project", "distill.projectFullText"],
] as const

export function ProjectDistillMenu({ projectId, threadId }: ProjectDistillProps) {
  const t = useT()
  const slot = useDistillation((s) => s.projects[projectId])
  const active = isActiveDistillation(slot?.result?.state)
  const busy = slot?.starting || slot?.controlling || active
  const pending = hasPendingDistillation(slot?.result)
  const reviewId = slot?.result?.review_id

  const start = (scope: DistillScope, options: { full_text?: true } = {}) => {
    void useDistillation.getState().start(projectId, {
      scope,
      ...(scope === "project" ? {} : { thread_id: threadId }),
      ...options,
    })
  }

  if (pending && reviewId && !active && !slot?.starting) {
    return (
      <Button type="button" data-testid="project-distill" variant="outline" size="sm"
        disabled={slot?.controlling} aria-busy={slot?.controlling}
        onClick={() => void useDistillation.getState().start(projectId, { resume_review_id: reviewId })}>
        {t("distill.resume")}
      </Button>
    )
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button data-testid="project-distill" variant="outline" size="sm" disabled={busy || pending} aria-busy={busy}>
          {t("distill.menu")}<ChevronDown aria-hidden />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {scopes.map(([scope, key]) => (
          <DropdownMenuItem key={scope} disabled={scope !== "project" && !threadId} onSelect={() => start(scope)}>{t(key)}</DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuLabel>{t("distill.fullTextOptions")}</DropdownMenuLabel>
        {fullTextScopes.map(([scope, key]) => (
          <DropdownMenuItem key={scope} disabled={scope === "thread" && !threadId} onSelect={() => start(scope, { full_text: true })}>
            {t(key)}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export async function openProjectReviewTrace(threadId: string, turnId: string) {
  if (useApp.getState().activeId !== threadId) await useApp.getState().openThread(threadId)
  if (useApp.getState().activeId !== threadId) return
  if (!await useApp.getState().loadUntilTurn(turnId)) return
  if (useApp.getState().activeId === threadId) useTraceFocus.getState().open(turnId)
}

export function ProjectDistillStatus({ projectId }: { projectId: string }) {
  const t = useT()
  const slot = useDistillation((s) => s.projects[projectId])
  const result = slot?.starting ? undefined : slot?.result
  const active = isActiveDistillation(result?.state)
  useEffect(() => {
    const controller = new AbortController()
    const { signal } = controller
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      await useDistillation.getState().refresh(projectId, signal)
      if (!signal.aborted && active) timer = setTimeout(() => void poll(), 800)
    }
    void poll()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [projectId, active])

  const state = slot?.starting ? "running" : result?.state
  const running = state === "running"
  const scope = result?.scope
  const changes = result?.changes ?? []
  const trace = result?.thread_id && result.turn_id ? { thread: result.thread_id, turn: result.turn_id } : undefined
  const error = slot?.error ?? (state === "interrupted" && result?.error?.trim() === "context canceled" ? undefined : result?.error)
  const errorCode = !slot?.error && error ? result?.error_code ?? legacyDistillError(error) : undefined
  const historyOmitted = result?.history_omitted
  const progress = result?.progress
  const excerpted = progress?.excerpted ?? 0
  const oversizedSkipped = progress?.oversized_skipped ?? 0
  const unavailable = (progress?.skipped ?? 0) - oversizedSkipped
  const segment = progress?.segment_bytes && progress.segment_total_bytes
    ? { done: formatBytes(progress.segment_bytes), total: formatBytes(progress.segment_total_bytes) }
    : undefined
  const percent = progress && progress.total > 0
    ? progress.remaining === 0 ? 100 : Number(Math.min(99.99, 100 * progress.processed / progress.total).toFixed(2))
    : undefined
  const canControl = state !== "pausing" && hasPendingDistillation(result) && result?.review_id !== undefined
  const control = () => {
    const id = result?.review_id
    if (id) void useDistillation.getState().start(projectId, running ? { pause_review_id: id } : { resume_review_id: id })
  }
  const hasChanges = changes.length > 0
  const status = state === "completed" && !hasChanges
    ? t("distill.noChanges")
    : state && state !== "idle" ? t(`distill.state.${state}`) : undefined
  if ((!state || state === "idle") && !error) return null
  return (
    <div className="grid gap-2 rounded-md border border-border bg-muted/40 px-2 py-2 text-xs text-muted-foreground" data-testid="project-distill-status">
      <div className="flex flex-wrap items-center gap-x-2">
        <p role="status">
          {status}{status && scope ? ` · ${t(`distill.scope.${scope}`)}` : ""}
          {status && result?.full_text ? ` · ${t("distill.fullText")}` : ""}
        </p>
        {trace ? (
          <Button variant="link" size="sm" className="h-auto p-0" onClick={() => void openProjectReviewTrace(trace.thread, trace.turn)}>
            {t("distill.trace")}
          </Button>
        ) : null}
      </div>
      {progress || state === "running" ? <ReviewProgressBar label={t("distill.progressLabel")} percent={percent} /> : null}
      {progress ? (
        <div className="flex flex-wrap items-center gap-x-2">
          <p aria-live="polite" data-testid="project-distill-progress">
            {t("distill.progress", { done: progress.processed, total: progress.total, remaining: progress.remaining })}
          </p>
          {canControl ? (
            <Button type="button" variant="link" size="sm" className="h-auto p-0" disabled={slot?.controlling} onClick={control}>
              {t(running ? "distill.pause" : "distill.resume")}
            </Button>
          ) : null}
        </div>
      ) : null}
      {segment ? <p>{t("distill.segmentProgress", segment)}</p> : null}
      {excerpted > 0 ? <p>{t("distill.excerpted", { count: excerpted })}</p> : null}
      {oversizedSkipped > 0 ? <p>{t("distill.oversizedSkipped", { count: oversizedSkipped })}</p> : null}
      {unavailable > 0 ? <p>{t("distill.skipped", { count: unavailable })}</p> : null}
      {historyOmitted && !progress?.coverage_known ? <p>{t(progress ? "distill.clipped" : "distill.omitted")}</p> : null}
      {result?.uncertain && !active ? <p>{t("distill.uncertain")}</p> : null}
      {error ? (
        <div>
          <p role="alert" className="break-words text-destructive">{errorCode ? t(`distill.failure.${errorCode}`) : error}</p>
          {errorCode ? <DistillSettingsAction code={errorCode} providerId={result?.error_provider_id} /> : null}
        </div>
      ) : null}
      {hasChanges ? (
        <details>
          <summary>{t("distill.changes", {
            notes: changes.filter((change) => change.target === "memory").length,
            skills: changes.filter((change) => change.target === "skill_manage").length,
          })}</summary>
          <ul className="list-disc break-words pl-4">
            {changes.map((change, i) => <li key={i}>{change.name ?? change.text}</li>)}
          </ul>
        </details>
      ) : null}
    </div>
  )
}

function legacyDistillError(error: string): DistillErrorCode | undefined {
  if (firstByteTimeoutDuration(error)) return "first_byte_timeout"
  if (error.includes("evidence validation failed")) return "evidence"
  return undefined
}

function DistillSettingsAction({ code, providerId }: { code: DistillErrorCode; providerId?: string }) {
  const t = useT()
  let configure: () => void
  let label = t("distill.openSettings")
  switch (code) {
    case "first_byte_timeout":
      configure = () => openSettings("models", firstByteTimeoutSettingsKey(providerId ?? ""))
      label = t("transcript.firstByteTimeoutSettings")
      break
    case "idle_timeout":
    case "model_settings":
      configure = () => openSettings("models")
      break
    case "context_overflow":
      configure = () => openSettings("swarm", MAX_COMPLETION_SETTINGS_KEY)
      break
    case "agent_timeout":
      configure = () => openSettings("swarm")
      break
    default:
      return null
  }
  return <Button type="button" variant="link" size="sm" className="h-auto p-0" onClick={configure}>{label}</Button>
}
