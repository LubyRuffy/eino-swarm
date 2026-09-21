import { ChevronRight } from "lucide-react"
import { useEffect, useState, type ReactNode } from "react"
import { Disclosure } from "@/components/ui/collapsible"
import { TurnBlockList } from "@/components/app/work-fold"
import { cn, formatDuration, formatTime } from "@/lib/utils"
import { isArmedWaitNotice, isGoalHoldNotice } from "@/lib/transcript-notices"
import { useT } from "@/lib/use-t"
import type { Block, TurnState } from "@/lib/transcript"

export function groupByTurn(blocks: Block[]): [string, Block[]][] {
  const groups: [string, Block[]][] = []
  for (const b of blocks) {
    const last = groups.at(-1)
    if (last && last[0] === b.turnId) last[1].push(b)
    else groups.push([b.turnId, [b]])
  }
  return groups
}

export function GoalSessionTurn({
  turnId,
  blocks,
  turn,
  revealIds,
  renderBlock,
}: {
  turnId: string
  blocks: Block[]
  turn?: TurnState
  revealIds?: Set<string>
  renderBlock: (b: Block) => ReactNode
}) {
  const t = useT()
  const failed = turn?.status === "error"
  const finishedSession = Boolean(turn?.session) && turn?.status !== "running"
  const [open, setOpen] = useState(!finishedSession || failed)
  useEffect(() => {
    if (turn?.status === "running") setOpen(true)
    else if (finishedSession && !failed) setOpen(false)
  }, [failed, finishedSession, turn?.status])

  const pinAfter = Boolean(turn?.status && turn.status !== "running")
  const split = pinAfter || finishedSession ? splitSessionBlocks(blocks) : null

  if (!finishedSession) {
    const ordered = split ? [...split.leading, ...split.work, ...split.trailing] : blocks
    return (
      <div className="flex scroll-mt-6 flex-col gap-1" data-turn-nav={turnId}>
        <TurnBlockList
          blocks={ordered}
          revealIds={revealIds}
          renderBlock={renderBlock}
          running={turn?.status === "running"}
        />
        <TurnFooter turn={turn} />
      </div>
    )
  }

  // Folded work stays unmounted. An 18h /goal is dozens of sessions; creating
  // every BlockView on each token is what froze the composer mid-IME.
  const { leading, work, trailing } = splitSessionBlocks(blocks)
  const ms =
    turn?.endedAt && turn.startedAt
      ? new Date(turn.endedAt).getTime() - new Date(turn.startedAt).getTime()
      : 0
  const preview = open ? "" : sessionPreview(work, turn)
  const durationLabel =
    turn?.status === "done" ? t("transcript.workedFor") : t("transcript.stoppedAfter")
  return (
    <div className="flex min-w-0 scroll-mt-6 flex-col gap-1" data-turn-nav={turnId}>
      <TurnBlockList
        blocks={leading}
        revealIds={revealIds}
        renderBlock={renderBlock}
      />
      {work.length > 0 ? (
        <Disclosure
          open={open}
          onOpenChange={setOpen}
          failed={failed}
          testId="goal-session"
          summaryClassName="gap-1.5 py-0.5 text-xs"
          summary={
            <>
              <ChevronRight
                className={cn("size-3 shrink-0 opacity-60 transition-transform", open && "rotate-90")}
              />
              <span data-find-ignore="" className="shrink-0 whitespace-nowrap tabular-nums">
                {durationLabel} {formatDuration(Math.max(ms, 0))}
              </span>
              {!open && preview ? (
                <>
                  <span className="shrink-0 opacity-40">·</span>
                  <span
                    className={cn(
                      "min-w-0 flex-1 truncate",
                      failed ? "" : "text-muted-foreground/80",
                    )}
                  >
                    {preview}
                  </span>
                </>
              ) : null}
            </>
          }
        >
          {open ? (
            <div className="flex flex-col gap-1">
              <TurnBlockList
                blocks={work}
                revealIds={revealIds}
                renderBlock={renderBlock}
              />
            </div>
          ) : null}
        </Disclosure>
      ) : null}
      <TurnBlockList
        blocks={trailing}
        revealIds={revealIds}
        renderBlock={renderBlock}
      />
    </div>
  )
}

/** User / steer stay visible. A cap / idle / block notice after the work
 *  stays visible too: folding it behind Worked-for made a budget pause look
 *  like the turn crashed. */
export function splitSessionBlocks(blocks: Block[]): {
  leading: Block[]
  work: Block[]
  trailing: Block[]
} {
  const leading: Block[] = []
  const rest: Block[] = []
  for (const b of blocks) {
    if (rest.length === 0 && (b.kind === "user" || b.kind === "steer")) leading.push(b)
    else rest.push(b)
  }
  const trailing: Block[] = []
  const work: Block[] = []
  for (const b of rest) {
    if (b.kind === "notice" && isArmedWaitNotice(b.text)) trailing.push(b)
    else work.push(b)
  }
  const holds: Block[] = []
  while (work.length > 0) {
    const last = work[work.length - 1]
    if (last?.kind !== "notice" || !isGoalHoldNotice(last.text)) break
    holds.unshift(work.pop() as Block)
  }
  return { leading, work, trailing: [...trailing, ...holds] }
}

function flattenPreview(text: string): string {
  return text.replace(/\s+/g, " ").trim()
}

/** Collapsed session rows are one line. A raw answer with newlines would
 *  otherwise blow the header into a paragraph. A failed session must not
 *  preview the last answer and look like it finished cleanly. */
export function sessionPreview(blocks: Block[], turn?: TurnState): string {
  for (let i = blocks.length - 1; i >= 0; i--) {
    const row = blocks[i]
    if (row?.kind === "error") return flattenPreview(row.text)
  }
  if (turn?.error?.trim()) return flattenPreview(turn.error)
  for (let i = blocks.length - 1; i >= 0; i--) {
    const row = blocks[i]
    if (row?.kind === "answer") return flattenPreview(row.text)
  }
  return ""
}

/** Hover clock lives under the user request and under the last finished
 *  answer of that turn. Intermediate answers while tools still run are
 *  not their own messages. */
export function responseClockBlockId(blocks: Block[]): string | undefined {
  let last: Block | undefined
  for (const b of blocks) {
    if (b.kind === "answer") last = b
  }
  if (!last || last.streaming) return undefined
  return last.id
}

function TurnFooter({ turn }: { turn?: TurnState }) {
  const t = useT()
  if (!turn || turn.status === "running" || !turn.endedAt || !turn.startedAt) return null
  const ms = new Date(turn.endedAt).getTime() - new Date(turn.startedAt).getTime()
  return (
    <p
      data-find-ignore=""
      className="mt-2 flex items-center gap-2 px-2 text-xs text-muted-foreground"
    >
      <span>
        {turn.status === "done" ? t("transcript.workedFor") : t("transcript.stoppedAfter")}{" "}
        {formatDuration(ms)}
      </span>
      <span className="opacity-50">·</span>
      <span>{formatTime(turn.endedAt)}</span>
    </p>
  )
}
