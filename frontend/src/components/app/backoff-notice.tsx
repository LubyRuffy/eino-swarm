import { ChevronRight, Sparkles } from "lucide-react"
import { useId, useState } from "react"

import { Button } from "@/components/ui/button"
import { localizeNotice } from "@/lib/i18n"
import type { Block } from "@/lib/transcript"
import { useT, type Translate } from "@/lib/use-t"

function callerLabel(caller: { agentId: string; role?: string }, t: Translate): string {
  const stage = {
    manager: "backoff.manager",
    "title-namer": "backoff.title",
    "compact-summarizer": "backoff.compact",
    "memory-reviewer": "backoff.memory",
    "schedule-answer-policy": "backoff.schedule",
    "thread-wake-policy": "backoff.wake",
  } as const
  const key = stage[caller.agentId as keyof typeof stage]
  if (key) return `${t(key)} · ${caller.agentId}`
  const role = caller.role && caller.role !== caller.agentId ? `${caller.role} · ` : ""
  return `${t("backoff.model")} · ${role}${caller.agentId}`
}

export function BackoffNotice({ block }: { block: Block }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const id = useId()
  const context = block.backoff
  if (block.quiet || !context) return null
  return (
    <div data-testid="backoff-notice" className="my-2 rounded-lg border border-border bg-muted/50 px-3 py-2 text-[13px] text-muted-foreground">
      <div className="flex items-start gap-2">
        <Sparkles aria-hidden className="mt-0.5 size-3.5 shrink-0" />
        <p className="min-w-0 flex-1 stream-text whitespace-pre-wrap leading-5">{localizeNotice(block.text, t.locale)}</p>
        <Button type="button" variant="ghost" size="sm" className="h-auto shrink-0 gap-1 px-1 py-0 text-xs font-normal leading-5"
          aria-expanded={open} aria-controls={id} onClick={() => setOpen((value) => !value)}>
          <ChevronRight aria-hidden className={open ? "size-3 rotate-90" : "size-3"} />
          {t(open ? "backoff.hide" : "backoff.details")}
        </Button>
      </div>
      <div id={id} hidden={!open} role="region" aria-label={t("backoff.details")}>
        <div className="mt-2 space-y-2 pl-5 leading-relaxed">
          <p>{t(context.scope === "origin" ? "backoff.origin" : context.scope === "schedule_answer" ? "backoff.scheduleReason" : "backoff.unknown")}</p>
          <div>
            <p>{t("backoff.steps")}</p>
            <ul className="list-disc pl-5">{context.callers.map((caller) => <li key={caller.agentId} className="break-words">{callerLabel(caller, t)}</li>)}</ul>
          </div>
          {context.attempt && context.scope !== "unknown" ? <p>{t(context.scope === "origin" ? "backoff.failures" : "backoff.attempt", { count: context.attempt })}</p> : null}
          {context.code ? <p>{t(`backoff.${context.code}`)}</p> : null}
        </div>
      </div>
    </div>
  )
}
