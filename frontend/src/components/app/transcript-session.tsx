import { ChevronRight } from "lucide-react"
import { useEffect, useState, type ReactNode } from "react"
import { Disclosure } from "@/components/ui/collapsible"
import { cn, formatDuration, formatTime } from "@/lib/utils"
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
  renderBlock,
}: {
  turnId: string
  blocks: Block[]
  turn?: TurnState
  renderBlock: (b: Block) => ReactNode
}) {
  const t = useT()
  const finishedSession = Boolean(turn?.session) && turn?.status !== "running"
  const [open, setOpen] = useState(!finishedSession)
  useEffect(() => {
    if (turn?.status === "running") setOpen(true)
    else if (finishedSession) setOpen(false)
  }, [finishedSession, turn?.status])

  if (!finishedSession) {
    return (
      <div className="flex scroll-mt-6 flex-col gap-1" data-turn-nav={turnId}>
        {blocks.map((b) => (
          <div key={b.id}>{renderBlock(b)}</div>
        ))}
        <TurnFooter turn={turn} />
      </div>
    )
  }

  // Folded work stays unmounted. An 18h /goal is dozens of sessions; creating
  // every BlockView on each token is what froze the composer mid-IME.
  const { leading, work } = splitSessionBlocks(blocks)
  const ms =
    turn?.endedAt && turn.startedAt
      ? new Date(turn.endedAt).getTime() - new Date(turn.startedAt).getTime()
      : 0
  const preview = open ? "" : sessionPreview(work)
  return (
    <div className="flex min-w-0 scroll-mt-6 flex-col gap-1" data-turn-nav={turnId}>
      {leading.map((b) => (
        <div key={b.id}>{renderBlock(b)}</div>
      ))}
      <Disclosure
        open={open}
        onOpenChange={setOpen}
        testId="goal-session"
        summaryClassName="gap-1.5 py-0.5 text-xs"
        summary={
          <>
            <ChevronRight
              className={cn("size-3 shrink-0 opacity-60 transition-transform", open && "rotate-90")}
            />
            <span data-find-ignore="" className="shrink-0 whitespace-nowrap tabular-nums">
              {t("transcript.workedFor")} {formatDuration(Math.max(ms, 0))}
            </span>
            {!open && preview ? (
              <>
                <span className="shrink-0 opacity-40">·</span>
                <span className="min-w-0 flex-1 truncate text-muted-foreground/80">
                  {preview}
                </span>
              </>
            ) : null}
          </>
        }
      >
        {open ? (
          <div className="flex flex-col gap-1">
            {work.map((b) => (
              <div key={b.id}>{renderBlock(b)}</div>
            ))}
          </div>
        ) : null}
      </Disclosure>
    </div>
  )
}

/** User / steer stay visible. The rest is the work session being folded. */
export function splitSessionBlocks(blocks: Block[]): { leading: Block[]; work: Block[] } {
  const leading: Block[] = []
  const work: Block[] = []
  for (const b of blocks) {
    if (work.length === 0 && (b.kind === "user" || b.kind === "steer")) leading.push(b)
    else work.push(b)
  }
  return { leading, work }
}

/** Collapsed session rows are one line. A raw answer with newlines would
 *  otherwise blow the header into a paragraph. */
export function sessionPreview(blocks: Block[]): string {
  for (let i = blocks.length - 1; i >= 0; i--) {
    const row = blocks[i]
    if (row?.kind === "answer") return row.text.replace(/\s+/g, " ").trim()
  }
  return ""
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
