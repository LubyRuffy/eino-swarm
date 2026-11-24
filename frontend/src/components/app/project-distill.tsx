import { ChevronDown } from "lucide-react"
import { useEffect, useState } from "react"

import { Button } from "@/components/ui/button"
import { ReviewProgressBar } from "./review-progress-bar"
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel,
  DropdownMenuSeparator, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { api } from "@/lib/api"
import { hasPendingDistillation, isActiveDistillation, type DistillErrorCode, type DistillScope } from "@/lib/distillation"
import { firstByteTimeoutDuration, firstByteTimeoutSettingsKey } from "@/lib/first-byte-timeout"
import { MAX_COMPLETION_SETTINGS_KEY } from "@/lib/output-budget"
import { useT } from "@/lib/use-t"
import { formatBytes } from "@/lib/utils"
import { useApp } from "@/store/app"
import { useDistillation, useWikiBuild } from "@/store/distillation"
import { useKnowledge } from "@/store/knowledge"
import { openSettings } from "@/store/settings-sheet"
import { useTraceFocus } from "@/store/trace-focus"

export interface ProjectDistillProps {
  projectId: string
  threadId?: string
  knowledgeEnabled?: boolean
  wikiGraph?: boolean
}

const scopes = [
  ["latest_turn", "knowledge.distillLatest"],
  ["thread", "knowledge.distillTask"],
  ["project", "knowledge.distillProject"],
] as const

const fullTextScopes = [
  ["thread", "knowledge.distillTaskFullText"],
  ["project", "knowledge.distillProjectFullText"],
] as const

export function ProjectDistillMenu({ projectId, threadId, knowledgeEnabled, wikiGraph = false }: ProjectDistillProps) {
  const t = useT()
  const store = wikiGraph ? useWikiBuild : useDistillation
  const slot = store((s) => s.projects[projectId])
  const active = isActiveDistillation(slot?.result?.state)
  const busy = slot?.starting || slot?.controlling || active
  const pending = hasPendingDistillation(slot?.result)
  const reviewId = slot?.result?.review_id
  const refreshKey = useKnowledge((s) => s.refreshByProject[projectId])
  const [knowledge, setKnowledge] = useState<{ projectId: string; enabled: boolean }>()
  const knowledgeAvailable = useApp((s) => s.meta?.swarm?.knowledge !== false)
  const enabled = knowledgeEnabled ?? (knowledge?.projectId === projectId ? knowledge.enabled : undefined)

  useEffect(() => {
    if (!knowledgeAvailable || wikiGraph || knowledgeEnabled !== undefined) return
    const controller = new AbortController()
    void api.knowledgeStatus(projectId, controller.signal).then((status) => {
      if (!controller.signal.aborted) setKnowledge({ projectId, enabled: status.enabled })
    }, () => undefined)
    return () => controller.abort()
  }, [projectId, knowledgeAvailable, knowledgeEnabled, refreshKey, wikiGraph])

  const start = (scope: DistillScope, enableKnowledge: boolean, fullText = false) => {
    void store.getState().start(projectId, {
      scope,
      ...(scope === "project" ? {} : { thread_id: threadId }),
      ...(enableKnowledge ? { enable_knowledge: true } : {}),
      ...(fullText ? { full_text: true } : {}),
    })
  }
  const testId = wikiGraph ? "knowledge-build" : "knowledge-distill"
  if (wikiGraph && !knowledgeAvailable) return null

  if (pending && reviewId && !active && !slot?.starting) {
    return (
      <Button type="button" data-testid={testId} variant="outline" size="sm"
        disabled={slot?.controlling} aria-busy={slot?.controlling}
        onClick={() => void store.getState().start(projectId, { resume_review_id: reviewId })}>
        {t(wikiGraph ? "knowledge.wikiResume" : "knowledge.distillResume")}
      </Button>
    )
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button data-testid={testId} variant="outline" size="sm" disabled={busy || pending} aria-busy={busy}>
          {t(wikiGraph ? "knowledge.wikiBuild" : "knowledge.distill")}<ChevronDown aria-hidden />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {scopes.filter(([scope]) => !wikiGraph || scope !== "latest_turn").map(([scope, key]) => (
          <DropdownMenuItem key={scope} disabled={scope !== "project" && !threadId} onSelect={() => start(scope, false)}>{t(key)}</DropdownMenuItem>
        ))}
        {wikiGraph ? null : <>
          <DropdownMenuSeparator />
          <DropdownMenuLabel>{t("knowledge.distillFullTextOptions")}</DropdownMenuLabel>
          {fullTextScopes.map(([scope, key]) => (
            <DropdownMenuItem key={scope} disabled={scope === "thread" && !threadId} onSelect={() => start(scope, false, true)}>
              {t(key)}
            </DropdownMenuItem>
          ))}
          {knowledgeAvailable && enabled === false ? <>
            <DropdownMenuSeparator />
            <DropdownMenuLabel>{t("knowledge.enableAndDistill")}</DropdownMenuLabel>
            {scopes.map(([scope, key]) => (
              <DropdownMenuItem
                key={scope}
                aria-label={`${t("knowledge.enableAndDistill")} · ${t(key)}`}
                disabled={scope !== "project" && !threadId}
                onSelect={() => start(scope, true)}
              >{t(key)}</DropdownMenuItem>
            ))}
            {fullTextScopes.map(([scope, key]) => (
              <DropdownMenuItem
                key={key}
                aria-label={`${t("knowledge.enableAndDistill")} · ${t(key)}`}
                disabled={scope === "thread" && !threadId}
                onSelect={() => start(scope, true, true)}
              >{t(key)}</DropdownMenuItem>
            ))}
          </> : null}
        </>}
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

export function ProjectDistillStatus({ projectId, wikiGraph = false }: { projectId: string; wikiGraph?: boolean }) {
  const t = useT()
  const store = wikiGraph ? useWikiBuild : useDistillation
  const slot = store((s) => s.projects[projectId])
  const result = slot?.starting ? undefined : slot?.result
  const active = isActiveDistillation(result?.state)
  useEffect(() => {
    const controller = new AbortController()
    const { signal } = controller
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      await store.getState().refresh(projectId, signal)
      if (!signal.aborted && active) timer = setTimeout(() => void poll(), 800)
    }
    void poll()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [projectId, active, store])

  const state = slot?.starting ? "running" : result?.state
  const running = state === "running"
  const scope = result?.scope
  const changes = result?.changes ?? []
  const pages = result?.knowledge ?? []
  const trace = result?.thread_id && result.turn_id ? { thread: result.thread_id, turn: result.turn_id } : undefined
  const error = slot?.error ?? (state === "interrupted" && result?.error?.trim() === "context canceled" ? undefined : result?.error)
  const errorCode = !slot?.error && error ? result?.error_code ?? legacyDistillError(error) : undefined
  const historyOmitted = result?.history_omitted
  const progress = result?.progress
  const wikiPhase = result?.wiki_phase
  const scanComplete = result?.wiki_scan_complete === true
  const scanProcessed = result?.wiki_scan_processed ?? 0
  const scanSkipped = result?.wiki_scan_skipped ?? 0
  const scanDeferred = result?.wiki_scan_deferred ?? 0
  const showScan = wikiGraph && (wikiPhase === "scan" || scanComplete || result?.wiki_scan_processed !== undefined)
  const scanning = showScan && !scanComplete
  const scanPercent = scanComplete ? 100 : progress?.total
    ? Math.min(99.99, Number((100 * scanProcessed / progress.total).toFixed(2)))
    : undefined
  const phaseLabel = scanning ? "knowledge.wikiScanning" : scanComplete && wikiPhase === "graph"
    ? "knowledge.wikiGraphReady"
    : undefined
  const reviewLabel = showScan ? "knowledge.wikiReviewProgressLabel" : "knowledge.wikiGraphProgressLabel"
  const documentTotal = result?.wiki_documents ?? 0
  const documentCount = result?.wiki_documented ?? 0
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
    if (id) void store.getState().start(projectId, running ? { pause_review_id: id } : { resume_review_id: id })
  }
  const hasChanges = changes.length + pages.length > 0
  const status = wikiGraph && state && state !== "idle"
    ? t(running && phaseLabel ? phaseLabel : `knowledge.wikiState.${state}`)
    : state === "completed" && !hasChanges
    ? t("knowledge.distillNoChanges")
    : state && state !== "idle" ? t(`knowledge.distillState.${state}`) : undefined
  if ((!state || state === "idle") && !error) return null
  return (
    <div className="grid gap-2 rounded-md border border-border bg-muted/40 px-2 py-2 text-xs text-muted-foreground" data-testid={wikiGraph ? "project-wiki-build-status" : "project-distill-status"}>
      <div className="flex flex-wrap items-center gap-x-2">
        <p role="status">
          {status}{status && scope ? ` · ${t(`knowledge.distillScope.${scope}`)}` : ""}
          {status && !wikiGraph && result?.full_text ? ` · ${t("knowledge.distillFullText")}` : ""}
        </p>
        {trace ? (
          <Button variant="link" size="sm" className="h-auto p-0" onClick={() => void openProjectReviewTrace(trace.thread, trace.turn)}>
            {t("knowledge.trace")}
          </Button>
        ) : null}
      </div>
      {showScan && progress ? (
        <>
          <ReviewProgressBar label={t("knowledge.wikiScanProgressLabel")} percent={scanPercent} />
          <p aria-live="polite" data-testid="wiki-scan-progress">
            {t("knowledge.wikiScanProgress", { done: scanProcessed, total: progress.total })}
            {scanSkipped ? ` · ${t("knowledge.wikiScanSkipped", { count: scanSkipped })}` : ""}
            {scanDeferred ? ` · ${t("knowledge.wikiScanDeferred", { count: scanDeferred })}` : ""}
          </p>
        </>
      ) : null}
      {!scanning && (progress || state === "running") ? (
        <ReviewProgressBar label={t(wikiGraph ? reviewLabel : "knowledge.distillProgressLabel")} percent={percent} />
      ) : null}
      {progress ? (
        <div className="flex flex-wrap items-center gap-x-2">
          {!scanning ? (
            <p aria-live="polite" data-testid="project-distill-progress">
              {t(showScan ? "knowledge.wikiReviewProgress" : "knowledge.distillProgress",
                { done: progress.processed, total: progress.total, remaining: progress.remaining })}
            </p>
          ) : null}
          {canControl ? (
            <Button type="button" variant="link" size="sm" className="h-auto p-0" disabled={slot?.controlling} onClick={control}>
              {t(wikiGraph ? (running ? "knowledge.wikiPause" : "knowledge.wikiResume") :
                (running ? "knowledge.distillPause" : "knowledge.distillResume"))}
            </Button>
          ) : null}
        </div>
      ) : null}
      {wikiGraph && result?.wiki_phase === "graph" && progress && (progress.batches > 0 || running) ? (
        <p aria-live="polite" data-testid="wiki-source-batches">
          {progress.batches > 0 ? t("knowledge.wikiSourceBatches", { count: progress.batches }) : t("knowledge.wikiFirstBatch")}
        </p>
      ) : null}
      {wikiPhase === "documents" || (wikiGraph && documentTotal > 0) ? (
        <div className="grid gap-1">
          <ReviewProgressBar label={t("knowledge.wikiDocumentProgressLabel")}
            percent={documentTotal ? Math.floor(100 * documentCount / documentTotal) : 100} />
          <p aria-live="polite" data-testid="wiki-document-progress">
            {t("knowledge.wikiDocumentProgress", { done: documentCount, total: documentTotal })}
          </p>
        </div>
      ) : null}
      {segment ? <p>{t("knowledge.distillSegmentProgress", segment)}</p> : null}
      {excerpted > 0 ? <p>{t("knowledge.distillExcerpted", { count: excerpted })}</p> : null}
      {oversizedSkipped > 0 ? <p>{t("knowledge.distillOversizedSkipped", { count: oversizedSkipped })}</p> : null}
      {unavailable > 0 ? <p>{t("knowledge.distillSkipped", { count: unavailable })}</p> : null}
      {historyOmitted && !progress?.coverage_known ? <p>{t(progress ? "knowledge.distillClipped" : "knowledge.distillOmitted")}</p> : null}
      {result?.uncertain && !active ? <p>{t("knowledge.distillUncertain")}</p> : null}
      {error ? (
        <div>
          <p role="alert" className="break-words text-destructive">{errorCode ? t(`knowledge.distillFailure.${errorCode}`) : error}</p>
          {errorCode ? <DistillSettingsAction code={errorCode} providerId={result?.error_provider_id} /> : null}
        </div>
      ) : null}
      {hasChanges ? (
        <details>
          <summary>{t("knowledge.distillChanges", {
            notes: changes.filter((change) => change.target === "memory").length,
            skills: changes.filter((change) => change.target === "skill_manage").length,
            pages: pages.length,
          })}</summary>
          <ul className="list-disc break-words pl-4">
            {changes.map((change, i) => <li key={i}>{change.name ?? change.text}</li>)}
            {pages.map((page) => <li key={page.revision}>{page.title ?? t("knowledge.distillSchema")}</li>)}
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
  let label = t("knowledge.distillOpenSettings")
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
