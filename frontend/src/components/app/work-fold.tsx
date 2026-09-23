import { ChevronRight, Loader2 } from "lucide-react"
import { useMemo, useState, type ReactNode } from "react"

import { SwapLine } from "@/components/app/swap-line"
import { Disclosure } from "@/components/ui/collapsible"
import type { TranscriptModePref } from "@/lib/appearance"
import { chromeTypeClass } from "@/lib/chrome-type"
import type { Block } from "@/lib/transcript"
import { cn } from "@/lib/utils"
import { useT, type Translate } from "@/lib/use-t"
import { useApp } from "@/store/app"
import {
  foldTurnItems,
  formatWorkTicker,
  liveWorkIndex,
  planningTailAfterAnswer,
  workFoldStats,
  workTickerFrames,
  type WorkFoldStats,
} from "@/lib/work-fold"

const EMPTY_REVEAL = new Set<string>()

export function TurnBlockList({
  blocks,
  revealIds = EMPTY_REVEAL,
  renderBlock,
  mode,
  running = false,
}: {
  blocks: Block[]
  revealIds?: Set<string>
  renderBlock: (b: Block) => ReactNode
  mode?: TranscriptModePref
  running?: boolean
}) {
  const stored = useApp((s) => s.transcriptMode)
  const view = mode ?? stored
  const items = useMemo(() => foldTurnItems(blocks, view), [blocks, view])
  const liveAt = liveWorkIndex(items, running)
  const planningTail = planningTailAfterAnswer(items, running)
  return (
    <>
      {items.map((item, i) =>
        item.type === "block" ? (
          <div key={item.block.id}>{renderBlock(item.block)}</div>
        ) : (
          <WorkFold
            key={item.blocks[0]?.id ?? "work"}
            blocks={item.blocks}
            reveal={item.blocks.some((b) => revealIds.has(b.id))}
            running={i === liveAt}
            renderBlock={renderBlock}
          />
        ),
      )}
      {planningTail ? <PlanningTail /> : null}
    </>
  )
}

function PlanningTail() {
  const t = useT()
  return (
    <div
      data-testid="planning-tail"
      role="status"
      className="flex w-full min-w-0 items-center gap-2 overflow-hidden rounded-md px-2 py-1 text-sm text-muted-foreground"
    >
      <Loader2 aria-hidden className="size-3.5 shrink-0 animate-spin" />
      <SwapLine itemKey="planning:tail" text={t("transcript.planningMoves")} active />
    </div>
  )
}

export function WorkFold({
  blocks,
  reveal,
  running = false,
  renderBlock,
}: {
  blocks: Block[]
  reveal?: boolean
  running?: boolean
  renderBlock: (b: Block) => ReactNode
}) {
  const t = useT()
  const stats = workFoldStats(blocks)
  const frames = useMemo(
    () => workTickerFrames(blocks, running),
    [blocks, running],
  )
  const frame = frames[0]
  const live = Boolean(frame)
  const [choice, setChoice] = useState<boolean | null>(null)
  const expanded = (choice ?? false) || Boolean(reveal)
  const tickerText = frame ? formatWorkTicker(frame, t) : ""

  return (
    <Disclosure
      open={expanded}
      onOpenChange={setChoice}
      failed={stats.failed}
      testId="work-fold"
      summary={
        <>
          {live ? (
            <Loader2
              aria-hidden
              className="size-3.5 shrink-0 animate-spin"
            />
          ) : (
            <ChevronRight
              aria-hidden
              className={cn(
                "size-3.5 shrink-0 opacity-60 transition-transform",
                expanded && "rotate-90",
              )}
            />
          )}
          {live && frame ? (
            <SwapLine
              itemKey={frame.id}
              text={tickerText}
              active
              className={stats.failed ? "text-destructive" : undefined}
            />
          ) : (
            <span className={cn("min-w-0 flex-1 truncate", chromeTypeClass)}>
              {foldLabel(stats, t)}
            </span>
          )}
          {live ? (
            <ChevronRight
              aria-hidden
              className={cn(
                "ml-auto size-3.5 shrink-0 opacity-60 transition-transform",
                expanded && "rotate-90",
              )}
            />
          ) : null}
        </>
      }
    >
      {expanded ? (
        <div className="flex flex-col gap-1">
          {blocks.map((b) => (
            <div key={b.id}>{renderBlock(b)}</div>
          ))}
        </div>
      ) : null}
    </Disclosure>
  )
}

function foldLabel(stats: WorkFoldStats, t: Translate): string {
  const tools =
    stats.tools === 1
      ? t("transcript.workFoldTool")
      : t("transcript.workFoldTools", { n: String(stats.tools) })
  if (stats.thoughts > 0 && stats.tools > 0) {
    return t("transcript.workFoldBoth", { tools })
  }
  if (stats.tools > 0) return tools
  return t("transcript.thought")
}
