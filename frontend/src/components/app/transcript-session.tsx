import type { ReactNode } from "react"
import { TurnBlockList } from "@/components/app/work-fold"
import { formatDuration, formatTurnTime } from "@/lib/utils"
import { isArmedWaitNotice, isGoalHoldNotice } from "@/lib/transcript-notices"
import { useT } from "@/lib/use-t"
import type { Block, TurnState } from "@/lib/transcript"

export function GoalSessionTurn({
  blocks,
  turn,
  revealIds,
  renderBlock,
  live = false,
  showFooter = true,
}: {
  blocks: Block[]
  turn?: TurnState
  revealIds?: Set<string>
  renderBlock: (b: Block) => ReactNode
  /** Live ticker belongs on the newest running turn only. Older sessions
   *  that never got a `done` still look "running" in the store; they must
   *  not keep Planning next moves after the conversation has moved on. */
  live?: boolean
  showFooter?: boolean
}) {
  // Answer text remains mounted when a session completes. TurnBlockList folds
  // only process rows and keeps their heavier bodies unmounted until opened.
  const split = turn?.status && turn.status !== "running" ? splitSessionBlocks(blocks) : null
  const ordered = split ? [...split.leading, ...split.work, ...split.trailing] : blocks
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <TurnBlockList
        blocks={ordered}
        revealIds={revealIds}
        renderBlock={renderBlock}
        running={live}
      />
      {showFooter ? <TurnFooter turn={turn} /> : null}
    </div>
  )
}

/** Completed turns keep armed waits and completion controls below their
 *  answers, so a long result cannot bury the next actionable state. */
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
    if (b.kind === "notice" && (isArmedWaitNotice(b.text) || b.text?.trim() === "Standing objective completed.")) trailing.push(b)
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
  if (!turn || turn.status === "running" || !turn.endedAt) return null
  const ms = turn.startedAt ? new Date(turn.endedAt).getTime() - new Date(turn.startedAt).getTime() : NaN
  return (
    <p
      data-find-ignore=""
      data-testid="turn-boundary"
      className="mt-3 flex items-center gap-2 border-t border-border px-2 pt-2 text-xs text-muted-foreground"
    >
      {Number.isFinite(ms) && ms >= 0 ? (
        <span>
          {turn.status === "done" ? t("transcript.workedFor") : t("transcript.stoppedAfter")} {formatDuration(ms)}
        </span>
      ) : null}
      <time dateTime={turn.endedAt} className="ml-auto tabular-nums">{formatTurnTime(turn.endedAt)}</time>
    </p>
  )
}
