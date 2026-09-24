import { useState } from "react"
import { ChevronRight, Loader2 } from "lucide-react"

import { PhoneMarkdown } from "@/components/markdown"
import { t } from "@/lib/i18n"
import { parseQuotedMessage } from "@/lib/quote"
import {
  foldPhoneItems,
  formatPhoneTicker,
  phonePlanningTail,
  phoneWorkTicker,
  type CompactBlock,
} from "@/lib/transcript"
import { scheduleToolNotice } from "@/lib/inbox-preview"
import {
  dropPackedJson,
  jsonPreview,
  looksPacked,
  rosterCounts,
  rosterLines,
  type RosterCounts,
} from "@/lib/tool-preview"
import { cn } from "@/lib/cn"

export function renderBlock(b: CompactBlock) {
  if (b.kind === "user") {
    const text = dropPackedJson(b.text)
    if (!text && !b.hasImages) return null
    return (
      // A bubble as wide as the screen for three words reads as a banner.
      // What you said hugs its own text and sits on the side you typed from.
      <div className="ml-auto w-fit min-w-0 max-w-[85%] break-words rounded-2xl bg-secondary px-3 py-2 text-sm text-secondary-foreground">
        {text ? <QuotedPhoneText text={text} /> : null}
        {b.hasImages ? (
          <p className="mt-1 text-xs text-muted-foreground">{t("thread.image")}</p>
        ) : null}
      </div>
    )
  }
  if (b.kind === "steer") {
    const text = dropPackedJson(b.text)
    if (!text) return null
    return (
      <div className="ml-auto w-fit min-w-0 max-w-[85%] break-words rounded-2xl bg-accent px-3 py-2 text-sm">
        <QuotedPhoneText text={text} />
      </div>
    )
  }
  if (b.kind === "answer") {
    const text = dropPackedJson(b.text)
    if (!text) return null
    return (
      <div className={cn("mr-4 min-w-0 break-words text-sm leading-snug", b.streaming && "opacity-90")}>
        <PhoneMarkdown text={text} />
      </div>
    )
  }
  if (b.kind === "reasoning") {
    const text = dropPackedJson(b.text)
    if (!text) return null
    return (
      <p data-testid="phone-thought" className="whitespace-pre-wrap text-xs text-muted-foreground">
        {text}
      </p>
    )
  }
  if (b.kind === "tool") {
    if (b.toolName === "close_agent") return null
    const notice = scheduleToolNotice(b.toolName || "", b.args || b.text)
    if (notice !== undefined) {
      if (!notice) return null
      return <p className="text-[11px] text-muted-foreground">{localizeNotice(notice)}</p>
    }
    return <ToolChip block={b} />
  }
  if (b.kind === "spawn") {
    return null
  }
  if (b.kind === "error") {
    return <p className="text-sm text-destructive">{b.text}</p>
  }
  if (b.kind === "notice") {
    const text = localizeNotice(b.text)
    if (!text) return null
    return <p className="text-[11px] text-muted-foreground">{text}</p>
  }
  return null
}

function QuotedPhoneText({ text }: { text: string }) {
  const parsed = parseQuotedMessage(text)
  if (parsed.quotes.length === 0) {
    return <PhoneMarkdown text={text} />
  }
  return (
    <div className="flex flex-col gap-2" data-testid="quoted-message">
      {parsed.quotes.map((q, i) => (
        <p
          key={`${i}-${q.slice(0, 24)}`}
          className="rounded-lg border border-border bg-muted/50 px-2 py-1 text-xs text-muted-foreground"
        >
          {i + 1}. {t("quote.selected")}: {q}
        </p>
      ))}
      {parsed.body ? <PhoneMarkdown text={parsed.body} /> : null}
    </div>
  )
}

function ToolChip({ block }: { block: CompactBlock }) {
  const [open, setOpen] = useState(false)
  const name = block.toolName || t("thread.tool")
  const summary = toolChipSummary(block)
  const body = packedBody(block)
  return (
    <div className="min-w-0 max-w-full">
      <button
        type="button"
        className="flex min-w-0 max-w-full items-center gap-1.5 text-left text-xs text-muted-foreground"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span
          className={cn(
            "size-1.5 shrink-0 rounded-full",
            block.pending
              ? "animate-pulse bg-[hsl(var(--running))]"
              : block.failed
                ? "bg-destructive"
                : "bg-muted-foreground/40",
          )}
          aria-hidden
        />
        <span className="shrink-0 font-mono">{name}</span>
        {summary && !open ? <span className="min-w-0 truncate">{summary}</span> : null}
      </button>
      {open && body ? (
        <pre className="mt-1 max-h-24 overflow-auto whitespace-pre-wrap text-[11px] text-muted-foreground">
          {body}
        </pre>
      ) : null}
    </div>
  )
}

function toolChipSummary(b: CompactBlock): string {
  const roster = formatRoster(rosterCounts(b.text) || rosterCounts(b.args))
  if (roster) return roster
  const line = lastLine(b.text)
  if (line && !looksPacked(line) && line !== b.toolName) return clip(line, 72)
  return clip(jsonPreview(b.text) || jsonPreview(b.args), 72)
}

function packedBody(block: CompactBlock): string {
  const roster = rosterLines(block.text)
  if (roster.length) return roster.join("\n")
  const preview = jsonPreview(block.text) || jsonPreview(block.args)
  if (preview) return clip(preview, 400)
  const raw = block.text.trim()
  if (!raw || raw === block.toolName || rosterCounts(raw)) return ""
  return clip(raw, 400)
}

function formatRoster(counts: RosterCounts | null): string {
  if (!counts) return ""
  const parts: string[] = []
  if (counts.done) parts.push(t("thread.rosterDone", { n: counts.done }))
  if (counts.failed) parts.push(t("thread.rosterFailed", { n: counts.failed }))
  if (counts.running) parts.push(t("thread.rosterRunning", { n: counts.running }))
  if (counts.undelivered) parts.push(t("thread.rosterUndelivered", { n: counts.undelivered }))
  return parts.join(" · ")
}

function lastLine(text: string): string {
  const lines = text.split("\n").map((row) => row.trim()).filter(Boolean)
  return lines[lines.length - 1] ?? ""
}

function clip(s: string, n: number): string {
  if (!s) return ""
  if (s.length <= n) return s
  return s.slice(0, n) + "…"
}

function localizeNotice(text: string): string {
  switch (text) {
    case "A wait is armed.":
      return t("notice.scheduleArmed")
    case "A wait was cancelled.":
      return t("notice.scheduleCancelled")
    case "Scheduled check.":
      return t("notice.scheduleFired")
    default:
      return looksPacked(text) ? "" : text
  }
}

/** Phone default is compact: adjacent thoughts and tools are one row.
 *  Answers stay outside that row. */
export function ThreadLog({
  blocks,
  running = false,
}: {
  blocks: CompactBlock[]
  running?: boolean
}) {
  const items = foldPhoneItems(blocks)
  let lastWork = -1
  items.forEach((item, i) => {
    if (item.type === "work") lastWork = i
  })
  const laterAnswer =
    lastWork >= 0 &&
    items.slice(lastWork + 1).some((item) => item.type === "block" && item.block.kind === "answer")
  const planningTail = phonePlanningTail(items, running)
  return (
    <>
      {items.map((item, i) => {
        if (item.type === "work") {
          return (
            <WorkFold
              key={item.blocks[0]?.id ?? `work-${i}`}
              blocks={item.blocks}
              live={running && i === lastWork && !laterAnswer}
            />
          )
        }
        const node = renderBlock(item.block)
        if (!node) return null
        return (
          <div key={item.block.id} className="min-w-0 max-w-full">
            {node}
          </div>
        )
      })}
      {planningTail ? <PlanningTail /> : null}
    </>
  )
}

function PlanningTail() {
  return (
    <p
      data-testid="planning-tail"
      role="status"
      className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground"
    >
      <Loader2 aria-hidden className="size-3 shrink-0 motion-safe:animate-spin motion-reduce:animate-none" />
      {t("thread.planningMoves")}
    </p>
  )
}

function WorkFold({ blocks, live }: { blocks: CompactBlock[]; live: boolean }) {
  const [open, setOpen] = useState(false)
  const frame = phoneWorkTicker(blocks, live)
  const failed = blocks.some((b) => b.failed)
  const label = frame ? formatPhoneTicker(frame) : foldLabel(blocks)
  return (
    <div className="min-w-0 max-w-full">
      <button
        type="button"
        data-testid="work-fold"
        className={cn(
          "flex min-w-0 max-w-full items-center gap-1 text-left text-xs text-muted-foreground",
          failed && "text-destructive",
        )}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <ChevronRight
          aria-hidden
          className={cn("size-3 shrink-0 transition-transform", open && "rotate-90")}
        />
        <span className="min-w-0 truncate">{label}</span>
      </button>
      {open ? (
        <div className="mt-1 flex flex-col gap-1 pl-4">
          {blocks.map((b) => {
            const node = renderBlock(b)
            if (!node) return null
            return (
              <div key={b.id} className="min-w-0 max-w-full">
                {node}
              </div>
            )
          })}
        </div>
      ) : null}
    </div>
  )
}

function foldLabel(blocks: CompactBlock[]): string {
  let thoughts = 0
  let tools = 0
  for (const b of blocks) {
    if (b.kind === "reasoning") thoughts++
    if (b.kind === "tool") tools++
  }
  const toolLabel =
    tools === 1 ? t("thread.workFoldTool") : t("thread.workFoldTools", { n: tools })
  if (thoughts > 0 && tools > 0) return t("thread.workFoldBoth", { tools: toolLabel })
  if (tools > 0) return toolLabel
  return t("thread.thought")
}
