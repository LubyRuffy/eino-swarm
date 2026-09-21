import { useState } from "react"

import { PhoneMarkdown } from "@/components/markdown"
import { t } from "@/lib/i18n"
import type { CompactBlock } from "@/lib/transcript"
import { jsonPreview, looksPacked } from "@/lib/tool-preview"
import { cn } from "@/lib/cn"

export function renderBlock(b: CompactBlock) {
  if (b.kind === "user") {
    return (
      <div className="ml-6 break-words rounded-2xl bg-secondary px-3 py-2 text-sm text-secondary-foreground">
        <PhoneMarkdown text={b.text} />
        {b.hasImages ? (
          <p className="mt-1 text-xs text-muted-foreground">{t("thread.image")}</p>
        ) : null}
      </div>
    )
  }
  if (b.kind === "steer") {
    return (
      <div className="ml-6 break-words rounded-2xl bg-accent px-3 py-2 text-sm">
        <PhoneMarkdown text={b.text} />
      </div>
    )
  }
  if (b.kind === "answer") {
    return (
      <div className={cn("mr-4 text-sm leading-relaxed", b.streaming && "opacity-90")}>
        <PhoneMarkdown text={b.text} />
      </div>
    )
  }
  if (b.kind === "tool") {
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

function ToolChip({ block }: { block: CompactBlock }) {
  const [open, setOpen] = useState(false)
  const name = block.toolName || t("thread.tool")
  const summary = toolChipSummary(block)
  const body = packedBody(block)
  return (
    <div>
      <button
        type="button"
        className="flex max-w-full items-center gap-1.5 text-left text-xs text-muted-foreground"
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
  const line = lastLine(b.text)
  if (line && !looksPacked(line) && line !== b.toolName) return clip(line, 72)
  return clip(jsonPreview(b.text) || jsonPreview(b.args), 72)
}

function packedBody(block: CompactBlock): string {
  const preview = jsonPreview(block.text) || jsonPreview(block.args)
  if (preview) return clip(preview, 400)
  const raw = block.text.trim()
  if (!raw || raw === block.toolName) return ""
  return clip(raw, 400)
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
      return text
  }
}
