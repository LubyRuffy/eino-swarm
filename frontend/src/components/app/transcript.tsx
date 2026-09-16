import {
  AlertTriangle,
  Brain,
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  Loader2,
  Pencil,
  Sparkles,
  Terminal,
  Users,
} from "lucide-react"
import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import { MemoMarkdown } from "@/components/app/markdown"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { Skeleton } from "@/components/ui/skeleton"
import { ToolResultBody } from "@/components/app/tool-result"
import { ShellCommand } from "@/components/app/shell-command"
import { MarqueeText } from "@/components/app/marquee"
import { TurnNav } from "@/components/app/turn-nav"
import { InputThumbs } from "@/components/app/input-thumbs"
import { execCommand, toolRowSummary, viewTool } from "@/lib/tool-view"
import {
  thoughtExpanded,
  thoughtFollowsStream,
  thoughtHasHiddenPrefix,
} from "@/lib/thought-scroll"
import { FIND_REPAINT_MS, revealBlockIds } from "@/lib/find"
import {
  applyFindHighlights,
  clearFindHighlights,
  scrollRangeIntoView,
} from "@/lib/find-dom"
import { useTranscriptFollow } from "@/lib/follow-scroll"
import { localizeNotice } from "@/lib/i18n"
import { TURN_NAV_MIN, scrollTurnIntoView, turnNavItems } from "@/lib/turn-nav"
import { cn, formatDuration, formatMessageTime, formatTime } from "@/lib/utils"
import { afterImeSettles, enterSendsMessage } from "@/lib/ime"
import { useT } from "@/lib/use-t"
import { Textarea } from "@/components/ui/textarea"
import type { AgentState, Block, Pulse, TranscriptState, TurnState } from "@/lib/transcript"
import { MANAGER_ID, liveWorkers, splitQueuedSteers } from "@/lib/transcript"
import { useApp } from "@/store/app"

const EMPTY_BLOCKS: Block[] = []

/** The middle pane: the manager's conversation, with sub-agent activity folded
 *  in where it happened. */
export function Transcript({
  state,
  loaded,
  onSelectAgent,
  onResendUser,
  findQuery = "",
  findIndex = 0,
  onFindCount,
}: {
  state: TranscriptState
  loaded: boolean
  onSelectAgent: (id: string) => void
  /** Edit-and-resend from this user_message seq: the bubble below is cleared
   *  and the turn starts again at that position. */
  onResendUser?: (text: string, seq: number) => void
  /** Live find query. Empty means the bar is closed or idle. */
  findQuery?: string
  findIndex?: number
  onFindCount?: (total: number) => void
}) {
  const t = useT()
  const [editingSeq, setEditingSeq] = useState<number | null>(null)
  const manager = state.agents[MANAGER_ID]
  const endRef = useRef<HTMLDivElement>(null)
  const scrollerRef = useRef<HTMLDivElement>(null)
  const blocks = manager?.blocks ?? EMPTY_BLOCKS
  const navItems = turnNavItems(blocks)
  const { body, queued } = splitQueuedSteers(blocks, state.running)
  const revealIds = useMemo(
    () => new Set(revealBlockIds(blocks, findQuery)),
    [blocks, findQuery],
  )
  const scrolledFind = useRef("")
  const threadId = useApp((s) => s.activeId)
  const blockCount = manager?.blocks.length ?? 0
  const lastText = manager?.blocks.at(-1)?.text.length ?? 0
  const lastUserId = useMemo(() => {
    for (let i = blocks.length - 1; i >= 0; i--) {
      if (blocks[i]?.kind === "user") return blocks[i].id
    }
    return undefined
  }, [blocks])
  const { showJump, jumpToLatest, unpin } = useTranscriptFollow({
    scrollerRef,
    loaded,
    threadId,
    growthKey: `${blockCount}:${lastText}`,
    lastUserId,
  })

  const jumpTo = useCallback(
    (id: string) => {
      // Follow uses a rAF; a jump must cancel it in the same click, not after
      // setState, or the next token yanks the viewport back to the bottom.
      unpin()
      const el = scrollerRef.current
      if (el) scrollTurnIntoView(el, id)
    },
    [unpin],
  )

  const paintFind = useCallback(
    (scroll: boolean) => {
      const root = scrollerRef.current
      if (!root || !findQuery.trim()) {
        clearFindHighlights()
        onFindCount?.(0)
        scrolledFind.current = ""
        return
      }
      const painted = applyFindHighlights(root, findQuery, findIndex)
      onFindCount?.(painted.total)
      const key = `${findQuery}\0${findIndex}`
      const moved = scrolledFind.current !== key
      scrolledFind.current = key
      if (scroll && moved && painted.current) {
        unpin()
        scrollRangeIntoView(root, painted.current)
      }
    },
    [findQuery, findIndex, onFindCount, unpin],
  )

  // Query / index / a newly revealed row: paint before the browser samples
  // layout so the current hit is already on screen.
  useLayoutEffect(() => {
    paintFind(true)
    return () => clearFindHighlights()
  }, [paintFind, revealIds])

  // Streamed tokens replace text nodes. Doing that work on every delta with
  // find open is what froze a 25-minute turn; coalesce until the stream pauses.
  useEffect(() => {
    if (!findQuery.trim()) return
    const id = window.setTimeout(() => paintFind(false), FIND_REPAINT_MS)
    return () => window.clearTimeout(id)
  }, [blockCount, lastText, findQuery, paintFind])

  if (!loaded) {
    return (
      <div className="flex-1 space-y-4 overflow-hidden px-6 pt-8 pb-composer">
        <Skeleton className="ml-auto h-12 w-64" />
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-24 w-full max-w-2xl" />
      </div>
    )
  }

  return (
    <div className="relative flex min-h-0 flex-1">
      <TurnNav items={navItems} scrollerRef={scrollerRef} onJump={jumpTo} />
      <div
        ref={scrollerRef}
        data-testid="transcript"
        data-quote-source=""
        className={cn(
          "thin-scrollbar min-h-0 flex-1 overflow-y-auto [overflow-anchor:none] px-4 pt-6 sm:px-8 pb-composer",
          navItems.length >= TURN_NAV_MIN && "pl-10 sm:pl-12",
        )}
      >
        <div className="mx-auto flex max-w-3xl flex-col gap-1">
          {groupByTurn(body).map(([turnId, blocks]) => (
            <div key={turnId} className="flex scroll-mt-6 flex-col gap-1" data-turn-nav={turnId}>
              {blocks.map((b) => (
                <BlockView
                  key={b.id}
                  block={b}
                  threadId={threadId}
                  reveal={revealIds.has(b.id)}
                  onSelectAgent={onSelectAgent}
                  onResendUser={onResendUser}
                  editing={editingSeq === b.seq}
                  onBeginEdit={() => setEditingSeq(b.seq)}
                  onCancelEdit={() => setEditingSeq(null)}
                />
              ))}
              {/* Each turn ends with its own "Worked for …", so a conversation
                  reads as a sequence rather than one long block. */}
              <TurnFooter turn={state.turns.find((t) => t.id === turnId)} />
            </div>
          ))}
          <Heartbeat pulse={state.pulse} running={state.running} workers={liveWorkers(state)} />
          {queued.length > 0 ? (
            <div
              data-testid="queued-steers"
              role="status"
              aria-label={t("transcript.queuedSteering")}
              className="mt-1 flex flex-col gap-1"
            >
              {queued.map((b) => (
                <BlockView
                  key={b.id}
                  block={b}
                  threadId={threadId}
                  reveal={revealIds.has(b.id)}
                  onSelectAgent={onSelectAgent}
                  onResendUser={onResendUser}
                  editing={editingSeq === b.seq}
                  onBeginEdit={() => setEditingSeq(b.seq)}
                  onCancelEdit={() => setEditingSeq(null)}
                />
              ))}
            </div>
          ) : null}
          <div ref={endRef} className="h-4" />
        </div>
      </div>
      {showJump ? (
        <Button
          type="button"
          variant="outline"
          size="icon"
          data-testid="jump-to-latest"
          aria-label={t("transcript.jumpLatest")}
          className="absolute right-4 z-20 rounded-full bg-background shadow-md bottom-[calc(var(--composer-pad)+0.75rem)]"
          onClick={jumpToLatest}
        >
          <ChevronDown />
        </Button>
      ) : null}
    </div>
  )
}

const BlockView = memo(function BlockView({
  block,
  threadId,
  reveal,
  onSelectAgent,
  onResendUser,
  editing,
  onBeginEdit,
  onCancelEdit,
}: {
  block: Block
  threadId?: string
  reveal?: boolean
  onSelectAgent: (id: string) => void
  onResendUser?: (text: string, seq: number) => void
  editing?: boolean
  onBeginEdit?: () => void
  onCancelEdit?: () => void
}) {
  const t = useT()
  switch (block.kind) {
    case "user":
      return (
        <UserMessage
          block={block}
          threadId={threadId}
          editing={editing}
          onBeginEdit={onResendUser ? onBeginEdit : undefined}
          onCancelEdit={onCancelEdit}
          onResend={onResendUser}
        />
      )

    case "steer":
      return (
        <div className="mb-2 flex justify-end" data-testid="steer">
          <div className="flex max-w-[85%] items-start gap-2 rounded-2xl rounded-br-md border border-dashed border-border px-3 py-2 text-sm text-muted-foreground">
            <Badge variant="outline" className="mt-0.5 shrink-0">
              {t("transcript.steer")}
            </Badge>
            <div className="min-w-0">
              <InputThumbs
                threadId={threadId}
                images={block.images}
                className={block.text ? "mb-1" : undefined}
              />
              {block.text ? <p className="stream-text">{block.text}</p> : null}
            </div>
          </div>
        </div>
      )

    case "reasoning":
      return <Reasoning block={block} reveal={reveal} />

    case "answer":
      return <Answer block={block} />

    case "tool":
      // A spawn shows up twice — once as the call, once as the sub-agent row
      // that follows it. The row is the better of the two, so the call is
      // dropped here; the Trace tab's Full log still lists it.
      if (block.tool?.name === "spawn_agent") return null
      // A pending wait is the moment a swarm looks frozen from the outside, so
      // it shows the sub-agents it is waiting on and what each is doing right
      // now rather than a bare "running…".
      if (block.tool?.name === "wait_agents" && block.tool.pending) {
        return <WaitProgress block={block} onSelect={onSelectAgent} />
      }
      return <ToolRow block={block} reveal={reveal} />

    case "spawn":
      return <SpawnRow block={block} onSelect={onSelectAgent} />

    case "error":
      return (
        <div className="my-2 flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          <p className="stream-text">{block.text}</p>
        </div>
      )

    case "confirm":
      return <IterationLimitCard block={block} />

    case "notice":
      if (block.quiet || !block.text) return null
      return (
        <div
          data-testid="memory-notice"
          className="my-2 flex items-start gap-2 rounded-lg border border-border bg-muted/50 px-3 py-2 text-[13px] text-muted-foreground"
        >
          <Sparkles className="mt-0.5 size-3.5 shrink-0" />
          <p className="stream-text whitespace-pre-wrap">{localizeNotice(block.text, t.locale)}</p>
        </div>
      )

    case "title":
      // Sidebar metadata. The Trace tab's Full log lists it; the chat does not.
      return null

    default:
      return (
        <p className="px-2 py-1 text-xs text-muted-foreground">{block.text}</p>
      )
  }
})

function IterationLimitCard({ block }: { block: Block }) {
  const t = useT()
  const extendTurn = useApp((s) => s.extendTurn)
  const running = useApp((s) => s.transcript.running)
  const limit = block.confirm?.limit ?? 0
  const extendBy = block.confirm?.extendBy ?? 0
  const pending = Boolean(block.confirm?.pending && running)
  const continued = block.confirm?.continued
  return (
    <div
      data-testid="iteration-limit"
      className="my-2 rounded-lg border border-border bg-muted/50 px-3 py-3 text-sm"
    >
      <p className="text-foreground">
        {t("transcript.limitAsk", { limit, extendBy })}
      </p>
      {pending ? (
        <div className="mt-3 flex gap-2">
          <Button
            size="sm"
            aria-label={t("transcript.continueAria")}
            onClick={() => void extendTurn(true)}
          >
            {t("transcript.continue")}
          </Button>
          <Button
            size="sm"
            variant="outline"
            aria-label={t("transcript.stopAria")}
            onClick={() => void extendTurn(false)}
          >
            {t("transcript.stop")}
          </Button>
        </div>
      ) : (
        <p className="mt-2 text-[13px] text-muted-foreground">
          {continued
            ? t("transcript.limitContinued", { extendBy })
            : t("transcript.limitStopped", { limit })}
        </p>
      )}
    </div>
  )
}

function Reasoning({ block, reveal }: { block: Block; reveal?: boolean }) {
  const t = useT()
  const [choice, setChoice] = useState<boolean | null>(null)
  // Live thoughts start open so you can watch them. The click has to win
  // though — `open || streaming` locked the chevron until the model stopped.
  // Find keeps a matching thought open so the highlight has a node to paint.
  const expanded = thoughtExpanded(Boolean(block.streaming), choice) || Boolean(reveal)
  return (
    <Disclosure
      open={expanded}
      onOpenChange={(next) => setChoice(next)}
      testId="thought-toggle"
      summary={
        <>
          {block.streaming ? (
            <Loader2 className="size-3.5 shrink-0 animate-spin" />
          ) : (
            <Brain className="size-3.5 shrink-0" />
          )}
          {block.streaming ? (
            <MarqueeText text={t("transcript.thinking")} active className="flex-none w-fit text-[13px]" />
          ) : (
            <span className="text-[13px]">{t("transcript.thought")}</span>
          )}
          {expanded ? (
            <ChevronDown className="size-3.5 opacity-60" />
          ) : (
            <ChevronRight className="size-3.5 opacity-60" />
          )}
        </>
      }
    >
      <ThoughtBody text={block.text} streaming={block.streaming} />
    </Disclosure>
  )
}

/** Live thoughts follow the newest tokens inside a 10-line window. A finished
 *  thought that the user opens starts at the top, because they came back to
 *  read it, not to watch it grow. */
function ThoughtBody({ text, streaming }: { text: string; streaming?: boolean }) {
  const scrollerRef = useRef<HTMLDivElement>(null)
  const pinnedRef = useRef(Boolean(streaming))
  const followRaf = useRef(0)
  const [fadeTop, setFadeTop] = useState(false)

  const sync = useCallback(() => {
    const el = scrollerRef.current
    if (!el) return
    setFadeTop(thoughtHasHiddenPrefix(el.scrollTop))
    pinnedRef.current = thoughtFollowsStream(el.scrollHeight, el.scrollTop, el.clientHeight)
  }, [])

  useEffect(() => {
    const el = scrollerRef.current
    if (!el) return
    el.addEventListener("scroll", sync, { passive: true })
    sync()
    return () => el.removeEventListener("scroll", sync)
  }, [sync])

  useEffect(() => {
    if (streaming) pinnedRef.current = true
  }, [streaming])

  useEffect(() => {
    if (!pinnedRef.current) return
    const el = scrollerRef.current
    if (!el) return
    followRaf.current = window.requestAnimationFrame(() => {
      if (!pinnedRef.current) return
      el.scrollTop = el.scrollHeight
      sync()
    })
    return () => window.cancelAnimationFrame(followRaf.current)
  }, [text, streaming, sync])

  return (
    <div className="relative isolate">
      {fadeTop ? (
        <div
          aria-hidden
          data-testid="thought-fade"
          className="thought-fade pointer-events-none absolute inset-x-0 top-0 z-[1] h-10"
        />
      ) : null}
      <div
        ref={scrollerRef}
        data-testid="thought-scroll"
        data-overflow-top={fadeTop ? "true" : undefined}
        role="region"
        aria-label={streaming ? "Thinking" : "Thought"}
        className="thought-scroll thin-scrollbar border-l-2 border-border pl-3"
      >
        <p className="stream-text text-[13px] leading-6 text-muted-foreground">{text}</p>
      </div>
    </div>
  )
}

function Answer({ block }: { block: Block }) {
  return (
    <div className="group/answer relative py-2">
      <div className="md">
        <MemoMarkdown text={block.text} streaming={block.streaming} />
      </div>
      {!block.streaming && block.text.length > 0 ? (
        <CopyButton
          text={block.text}
          className="absolute -top-1 right-0 opacity-0 transition-opacity group-hover/answer:opacity-100"
        />
      ) : null}
    </div>
  )
}

function ToolRow({ block, reveal }: { block: Block; reveal?: boolean }) {
  const [open, setOpen] = useState(false)
  const tool = block.tool
  if (!tool) return null
  const view = viewTool(tool.name, tool.args, tool.result, tool.failed)
  const command = tool.name === "exec" ? execCommand(tool.args) : ""
  const expanded = open || Boolean(reveal)
  return (
    <Disclosure
      open={expanded}
      onOpenChange={setOpen}
      failed={view.failed}
      summary={
        <>
          {tool.pending ? (
            <Loader2 className="size-3.5 shrink-0 animate-spin" />
          ) : view.failed ? (
            <AlertTriangle className="size-3.5 shrink-0 text-destructive" />
          ) : (
            <Terminal className="size-3.5 shrink-0" />
          )}
          <span className="shrink-0 font-mono text-[13px] text-foreground">{tool.name}</span>
          {tool.pending ? (
            <MarqueeText
              text={toolRowSummary(view)}
              active
              className={view.failed ? "text-[13px] text-destructive" : "text-[13px] text-muted-foreground"}
            />
          ) : command ? (
            <span className="min-w-0 flex-1 overflow-hidden">
              <ShellCommand command={command} compact />
            </span>
          ) : (
            <MarqueeText
              text={toolRowSummary(view)}
              className={view.failed ? "text-[13px] text-destructive" : "text-[13px] text-muted-foreground"}
            />
          )}
          {expanded ? (
            <ChevronDown className="ml-auto size-3.5 shrink-0 opacity-60" />
          ) : (
            <ChevronRight className="ml-auto size-3.5 shrink-0 opacity-60" />
          )}
        </>
      }
    >
      <ToolResultBody
        name={tool.name}
        args={tool.args}
        result={tool.result}
        failed={tool.failed}
      />
    </Disclosure>
  )
}

export function WaitProgress({
  block,
  agents,
  pulse,
  onSelect,
}: {
  block: Block
  agents?: Record<string, AgentState>
  pulse?: Pulse
  onSelect: (id: string) => void
}) {
  const t = useT()
  const storedAgents = useApp((s) => s.transcript.agents)
  const storedPulse = useApp((s) => s.transcript.pulse)
  const liveAgents = agents ?? storedAgents
  const livePulse = pulse ?? storedPulse
  const ids = waitAgentIds(block.tool?.args ?? "")
  const watched = ids.map((id) => liveAgents[id]).filter(Boolean) as AgentState[]
  const running = watched.filter((a) => a.status === "running").length
  const ages = new Map(livePulse?.agents.map((a) => [a.agentId, a.elapsedMs]))
  const waiting = running > 0 ? running : watched.length
  return (
    <div className="my-1 rounded-lg border border-border bg-muted/40 px-3 py-2">
      <div className="flex items-center gap-2 text-[13px] text-muted-foreground">
        <Loader2 className="size-3.5 shrink-0 animate-spin" />
        <MarqueeText
          active
          text={t("transcript.waitingFor", {
            n: waiting,
            s: waiting === 1 ? "" : "s",
          })}
        />
      </div>
      <div className="mt-1.5 flex flex-col gap-0.5">
        {watched.map((a) => (
          <button
            key={a.id}
            type="button"
            onClick={() => onSelect(a.id)}
            className="flex items-center gap-2 rounded-md px-1.5 py-1 text-left text-[13px] transition-colors hover:bg-accent/60"
          >
            <StatusDot status={a.status} />
            <span className="shrink-0 font-medium">{a.role}</span>
            <MarqueeText
              text={a.status === "running" ? a.activity || t("transcript.workingEllipsis") : a.status}
              active={a.status === "running"}
              className="text-muted-foreground"
            />
            {/* The age is the honest part of a silent row: it says the work is
                still being done, and how long it has been going. */}
            {ages.has(a.id) ? (
              <span className="ml-auto shrink-0 tabular-nums text-muted-foreground">
                {formatDuration(ages.get(a.id) ?? 0)}
              </span>
            ) : null}
          </button>
        ))}
      </div>
    </div>
  )
}

/** Read the agent ids a wait_agents call is blocking on out of its raw args. */
export function waitAgentIds(args: string): string[] {
  try {
    const parsed = JSON.parse(args)
    if (Array.isArray(parsed?.agent_ids)) {
      return parsed.agent_ids.filter((x: unknown): x is string => typeof x === "string")
    }
  } catch {
    // Half-streamed args are not valid JSON yet; the roll-up fills in once the
    // call is complete, which for wait_agents is effectively immediate.
  }
  return []
}

function SpawnRow({
  block,
  agent,
  onSelect,
}: {
  block: Block
  agent?: AgentState
  onSelect: (id: string) => void
}) {
  const t = useT()
  const id = block.spawn?.agentId
  const stored = useApp((s) => (id ? s.transcript.agents[id] : undefined))
  const live = agent ?? stored
  if (!id) return null
  return (
    <button
      type="button"
      onClick={() => onSelect(id)}
      className="my-0.5 flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent/60"
    >
      <Users className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="text-[13px]">
        {t("transcript.started", { role: block.spawn?.role ?? "" })}
      </span>
      <StatusDot status={live?.status ?? "running"} />
      <MarqueeText
        text={live?.activity ?? ""}
        active={live?.status === "running"}
        className="text-[13px] text-muted-foreground"
      />
    </button>
  )
}

/** The bottom-of-transcript pulse: proof the run is alive, and how long it has
 *  been going, for the stretches where nothing streams. The clock is anchored
 *  to each pulse and interpolated locally in between, so it ticks every second
 *  without drifting away from what the server said. */
export function Heartbeat({
  pulse,
  running,
  workers,
}: {
  pulse?: Pulse
  running: boolean
  /** Counted from the agent rows rather than from the pulse: a count that is a
   *  pulse old would claim work is still running next to a row that says it
   *  finished. */
  workers: number
}) {
  const t = useT()
  const [drift, setDrift] = useState(0)
  useEffect(() => {
    if (!running || !pulse) return
    setDrift(0)
    const anchored = Date.now()
    const id = window.setInterval(() => setDrift(Date.now() - anchored), 1000)
    return () => window.clearInterval(id)
  }, [pulse?.at, running])

  if (!running || !pulse) return null
  return (
    <p
      data-testid="heartbeat"
      data-find-ignore=""
      className="mt-2 flex items-center gap-2 px-2 text-xs text-muted-foreground"
    >
      <Loader2 className="size-3 shrink-0 animate-spin" />
      <MarqueeText
        active
        className="text-xs"
        text={
          workers > 0
            ? t("transcript.workingAgents", {
                duration: formatDuration(pulse.elapsedMs + drift),
                n: workers,
                s: workers === 1 ? "" : "s",
              })
            : t("transcript.workingFor", {
                duration: formatDuration(pulse.elapsedMs + drift),
              })
        }
      />
    </p>
  )
}

export function StatusDot({ status }: { status: AgentState["status"] }) {
  const t = useT()
  const color =
    status === "running" ? "bg-running" : status === "failed" ? "bg-failed" : "bg-done"
  const label =
    status === "running"
      ? t("status.running")
      : status === "failed"
        ? t("status.failed")
        : t("status.done")
  return (
    <span
      aria-label={label}
      className={`size-1.5 shrink-0 rounded-full ${color} ${
        status === "running" ? "animate-breathe" : ""
      }`}
    />
  )
}

function groupByTurn(blocks: Block[]): [string, Block[]][] {
  const groups: [string, Block[]][] = []
  for (const b of blocks) {
    const last = groups.at(-1)
    if (last && last[0] === b.turnId) last[1].push(b)
    else groups.push([b.turnId, [b]])
  }
  return groups
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

export function CopyButton({
  text,
  className,
  label,
  "aria-label": ariaLabel,
}: {
  text: string
  className?: string
  label?: string
  "aria-label"?: string
}) {
  const t = useT()
  const [copied, setCopied] = useState(false)
  const name = ariaLabel ?? label ?? t("transcript.copy")
  return (
    <Button
      type="button"
      variant="ghost"
      size={label ? "sm" : "icon-sm"}
      className={className}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
          setCopied(true)
          setTimeout(() => setCopied(false), 1200)
        } catch {
          // Clipboard access can be denied; the button just does nothing
          // rather than throwing an error at the user.
        }
      }}
      title={copied ? t("transcript.copied") : name}
      aria-label={name}
    >
      {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
      {label ? <span>{copied ? t("transcript.copied") : label}</span> : null}
    </Button>
  )
}

function UserMessage({
  block,
  threadId,
  editing,
  onBeginEdit,
  onCancelEdit,
  onResend,
}: {
  block: Block
  threadId?: string
  editing?: boolean
  onBeginEdit?: () => void
  onCancelEdit?: () => void
  onResend?: (text: string, seq: number) => void
}) {
  const t = useT()
  const stamped = formatMessageTime(block.at)
  const [draft, setDraft] = useState(block.text)
  const composingRef = useRef(false)
  const cancelIme = useRef<(() => void) | null>(null)
  useEffect(() => {
    if (editing) setDraft(block.text)
  }, [editing, block.text])
  useEffect(() => () => cancelIme.current?.(), [])

  const submit = () => {
    const text = draft.trim()
    if (!text && !(block.images && block.images.length > 0)) return
    onResend?.(draft, block.seq)
    onCancelEdit?.()
  }

  return (
    <div className="mb-2 mt-6 flex scroll-mt-6 justify-end first:mt-0">
      <div className="flex max-w-[85%] flex-col items-end">
        {editing ? (
          <div
            data-testid="user-message-editor"
            className="w-[min(100%,28rem)] rounded-2xl border border-border bg-secondary px-3 py-2"
          >
            <InputThumbs
              threadId={threadId}
              images={block.images}
              className={draft ? "mb-2" : undefined}
            />
            <Textarea
              autoFocus
              data-edit-draft="true"
              aria-label={t("transcript.editMessage")}
              value={draft}
              rows={4}
              className="min-h-[4.5rem] px-1 py-1 text-[15px] leading-6 text-secondary-foreground"
              onChange={(e) => setDraft(e.target.value)}
              onCompositionStart={() => {
                cancelIme.current?.()
                composingRef.current = true
              }}
              onCompositionEnd={() => {
                cancelIme.current?.()
                cancelIme.current = afterImeSettles(() => {
                  composingRef.current = false
                  cancelIme.current = null
                })
              }}
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  e.preventDefault()
                  onCancelEdit?.()
                  return
                }
                if (!enterSendsMessage(e.nativeEvent, composingRef.current)) return
                e.preventDefault()
                submit()
              }}
            />
            <div className="mt-2 flex justify-end gap-2">
              <Button type="button" variant="outline" size="sm" onClick={onCancelEdit}>
                {t("transcript.cancelEdit")}
              </Button>
              <Button
                type="button"
                size="sm"
                disabled={!draft.trim() && !(block.images && block.images.length > 0)}
                onClick={submit}
              >
                {t("transcript.resend")}
              </Button>
            </div>
          </div>
        ) : (
          <>
            <div
              data-testid="user-message"
              className="rounded-2xl rounded-br-md bg-secondary px-4 py-2.5 text-[15px] leading-6 text-secondary-foreground"
            >
              <InputThumbs
                threadId={threadId}
                images={block.images}
                className={block.text ? "mb-2" : undefined}
              />
              {block.text ? <p className="stream-text whitespace-pre-wrap">{block.text}</p> : null}
            </div>
            <div
              data-find-ignore=""
              className="mt-1 flex items-center gap-0.5 text-[11px] text-muted-foreground"
            >
              {stamped ? (
                <time
                  data-testid="user-message-time"
                  dateTime={block.at}
                  className="px-1 tabular-nums"
                >
                  {stamped}
                </time>
              ) : null}
              {block.text ? (
                <CopyButton
                  text={block.text}
                  aria-label={t("transcript.copyMessage")}
                  className="text-muted-foreground"
                />
              ) : null}
              {onBeginEdit && block.seq > 0 ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t("transcript.editMessage")}
                  title={t("transcript.editMessage")}
                  className="text-muted-foreground"
                  onClick={onBeginEdit}
                >
                  <Pencil className="size-3.5" />
                </Button>
              ) : null}
            </div>
          </>
        )}
      </div>
    </div>
  )
}

/** One agent's own transcript, for the right-hand panel. */
export function AgentTranscript({ agent }: { agent: AgentState }) {
  const t = useT()
  const blocks = useMemo(
    () => agent.blocks.filter((b) => b.kind !== "user"),
    [agent.blocks],
  )
  if (blocks.length === 0) {
    return (
      <p className="px-3 py-6 text-center text-xs text-muted-foreground">
        {t("transcript.agentEmpty")}
      </p>
    )
  }
  return (
    <div className="space-y-1 px-1 py-2">
      {blocks.map((b) =>
        b.kind === "answer" ? (
          <div key={b.id} className="md px-2 text-[13px]">
            <MemoMarkdown text={b.text} streaming={b.streaming} />
          </div>
        ) : b.kind === "reasoning" ? (
          <Reasoning key={b.id} block={b} />
        ) : b.kind === "tool" ? (
          <ToolRow key={b.id} block={b} />
        ) : null,
      )}
    </div>
  )
}
