import {
  AlertTriangle,
  Brain,
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  Loader2,
  Terminal,
  Users,
} from "lucide-react"
import { useEffect, useMemo, useRef, useState } from "react"
import Markdown from "react-markdown"
import remarkGfm from "remark-gfm"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { Skeleton } from "@/components/ui/skeleton"
import { formatDuration, formatTime } from "@/lib/utils"
import type { AgentState, Block, TranscriptState, TurnState } from "@/lib/transcript"
import { MANAGER_ID } from "@/lib/transcript"

/** The middle pane: the manager's conversation, with sub-agent activity folded
 *  in where it happened. */
export function Transcript({
  state,
  loaded,
  onSelectAgent,
}: {
  state: TranscriptState
  loaded: boolean
  onSelectAgent: (id: string) => void
}) {
  const manager = state.agents[MANAGER_ID]
  const endRef = useRef<HTMLDivElement>(null)
  const scrollerRef = useRef<HTMLDivElement>(null)
  const [pinned, setPinned] = useState(true)

  // Follow the stream, but stop the moment the reader scrolls up: yanking
  // someone back to the bottom while they are reading is the worst thing a
  // streaming UI can do.
  useEffect(() => {
    const el = scrollerRef.current
    if (!el) return
    const onScroll = () => {
      const distance = el.scrollHeight - el.scrollTop - el.clientHeight
      setPinned(distance < 80)
    }
    el.addEventListener("scroll", onScroll, { passive: true })
    return () => el.removeEventListener("scroll", onScroll)
  }, [])

  const blockCount = manager?.blocks.length ?? 0
  const lastText = manager?.blocks.at(-1)?.text.length ?? 0
  useEffect(() => {
    if (pinned) endRef.current?.scrollIntoView({ block: "end" })
  }, [blockCount, lastText, pinned])

  if (!loaded) {
    return (
      <div className="flex-1 space-y-4 overflow-hidden px-6 py-8">
        <Skeleton className="ml-auto h-12 w-64" />
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-24 w-full max-w-2xl" />
      </div>
    )
  }

  return (
    <div
      ref={scrollerRef}
      data-testid="transcript"
      className="thin-scrollbar flex-1 overflow-y-auto px-4 py-6 sm:px-8"
    >
      <div className="mx-auto flex max-w-3xl flex-col gap-1">
        {groupByTurn(manager?.blocks ?? []).map(([turnId, blocks]) => (
          <div key={turnId} className="flex flex-col gap-1">
            {blocks.map((b) => (
              <BlockView
                key={b.id}
                block={b}
                agents={state.agents}
                onSelectAgent={onSelectAgent}
              />
            ))}
            {/* Each turn ends with its own "Worked for …", so a conversation
                reads as a sequence rather than one long block. */}
            <TurnFooter turn={state.turns.find((t) => t.id === turnId)} />
          </div>
        ))}
        <div ref={endRef} className="h-4" />
      </div>
    </div>
  )
}

function BlockView({
  block,
  agents,
  onSelectAgent,
}: {
  block: Block
  agents: Record<string, AgentState>
  onSelectAgent: (id: string) => void
}) {
  switch (block.kind) {
    case "user":
      return (
        <div className="mb-2 mt-6 flex justify-end first:mt-0">
          <div className="max-w-[85%] rounded-2xl rounded-br-md bg-secondary px-4 py-2.5 text-[15px] leading-6 text-secondary-foreground">
            <p className="stream-text">{block.text}</p>
          </div>
        </div>
      )

    case "steer":
      return (
        <div className="mb-2 flex justify-end">
          <div className="flex max-w-[85%] items-start gap-2 rounded-2xl rounded-br-md border border-dashed border-border px-3 py-2 text-sm text-muted-foreground">
            <Badge variant="outline" className="mt-0.5 shrink-0">
              steer
            </Badge>
            <p className="stream-text">{block.text}</p>
          </div>
        </div>
      )

    case "reasoning":
      return <Reasoning block={block} />

    case "answer":
      return <Answer block={block} />

    case "tool":
      // A spawn shows up twice — once as the call, once as the sub-agent row
      // that follows it. The row is the better of the two, so the call is
      // dropped here; the Trace tab still lists it.
      if (block.tool?.name === "spawn_agent") return null
      return <ToolRow block={block} />

    case "spawn":
      return (
        <SpawnRow
          block={block}
          agent={block.spawn ? agents[block.spawn.agentId] : undefined}
          onSelect={onSelectAgent}
        />
      )

    case "error":
      return (
        <div className="my-2 flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          <p className="stream-text">{block.text}</p>
        </div>
      )

    default:
      return (
        <p className="px-2 py-1 text-xs text-muted-foreground">{block.text}</p>
      )
  }
}

function Reasoning({ block }: { block: Block }) {
  const [open, setOpen] = useState(false)
  // While it is streaming the thought is the only thing happening, so show it;
  // once the answer starts it collapses to a single line.
  const expanded = open || Boolean(block.streaming)
  return (
    <Disclosure
      open={expanded}
      onOpenChange={setOpen}
      summary={
        <>
          {block.streaming ? (
            <Loader2 className="size-3.5 shrink-0 animate-spin" />
          ) : (
            <Brain className="size-3.5 shrink-0" />
          )}
          <span className="text-[13px]">
            {block.streaming ? "Thinking" : "Thought"}
          </span>
          {expanded ? (
            <ChevronDown className="size-3.5 opacity-60" />
          ) : (
            <ChevronRight className="size-3.5 opacity-60" />
          )}
        </>
      }
    >
      <p className="stream-text border-l-2 border-border pl-3 text-[13px] leading-6 text-muted-foreground">
        {block.text}
      </p>
    </Disclosure>
  )
}

function Answer({ block }: { block: Block }) {
  return (
    <div className="group/answer relative py-2">
      <div className="md">
        {block.streaming ? (
          // Markdown of a half-finished document reflows on every token, so
          // streamed text stays plain until it is complete.
          <p className="stream-text">
            {block.text}
            <span className="ml-0.5 inline-block h-4 w-1.5 translate-y-0.5 animate-breathe bg-foreground/70" />
          </p>
        ) : (
          <Markdown remarkPlugins={[remarkGfm]}>{block.text}</Markdown>
        )}
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

function ToolRow({ block }: { block: Block }) {
  const [open, setOpen] = useState(false)
  const tool = block.tool
  if (!tool) return null
  return (
    <Disclosure
      open={open}
      onOpenChange={setOpen}
      summary={
        <>
          {tool.pending ? (
            <Loader2 className="size-3.5 shrink-0 animate-spin" />
          ) : tool.failed ? (
            <AlertTriangle className="size-3.5 shrink-0 text-destructive" />
          ) : (
            <Terminal className="size-3.5 shrink-0" />
          )}
          <span className="font-mono text-[13px] text-foreground">{tool.name}</span>
          <span className="truncate text-[13px] text-muted-foreground">
            {summariseArgs(tool.args)}
          </span>
          {open ? (
            <ChevronDown className="ml-auto size-3.5 shrink-0 opacity-60" />
          ) : (
            <ChevronRight className="ml-auto size-3.5 shrink-0 opacity-60" />
          )}
        </>
      }
    >
      <div className="space-y-2 border-l-2 border-border pl-3 text-[12px]">
        {tool.args ? (
          <pre className="thin-scrollbar max-h-48 overflow-auto rounded bg-muted p-2 font-mono">
            {pretty(tool.args)}
          </pre>
        ) : null}
        {tool.result !== undefined ? (
          <pre
            className={`thin-scrollbar max-h-72 overflow-auto rounded bg-muted p-2 font-mono ${
              tool.failed ? "text-destructive" : ""
            }`}
          >
            {tool.result || "(no output)"}
          </pre>
        ) : (
          <p className="text-muted-foreground">running…</p>
        )}
      </div>
    </Disclosure>
  )
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
  const id = block.spawn?.agentId
  if (!id) return null
  return (
    <button
      type="button"
      onClick={() => onSelect(id)}
      className="my-0.5 flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent/60"
    >
      <Users className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="text-[13px]">
        Started <span className="font-medium">{block.spawn?.role}</span>
      </span>
      <StatusDot status={agent?.status ?? "running"} />
      <span className="truncate text-[13px] text-muted-foreground">
        {agent?.activity}
      </span>
    </button>
  )
}

export function StatusDot({ status }: { status: AgentState["status"] }) {
  const color =
    status === "running" ? "bg-running" : status === "failed" ? "bg-failed" : "bg-done"
  return (
    <span
      aria-label={status}
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
  if (!turn || turn.status === "running" || !turn.endedAt || !turn.startedAt) return null
  const ms = new Date(turn.endedAt).getTime() - new Date(turn.startedAt).getTime()
  return (
    <p className="mt-2 flex items-center gap-2 px-2 text-xs text-muted-foreground">
      <span>
        {turn.status === "done" ? "Worked for" : "Stopped after"} {formatDuration(ms)}
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
}: {
  text: string
  className?: string
  label?: string
}) {
  const [copied, setCopied] = useState(false)
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
      title={label ?? "Copy"}
    >
      {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
      {label ? <span>{copied ? "Copied" : label}</span> : null}
    </Button>
  )
}

/** One agent's own transcript, for the right-hand panel. */
export function AgentTranscript({ agent }: { agent: AgentState }) {
  const blocks = useMemo(
    () => agent.blocks.filter((b) => b.kind !== "user"),
    [agent.blocks],
  )
  if (blocks.length === 0) {
    return (
      <p className="px-3 py-6 text-center text-xs text-muted-foreground">
        This agent has not produced anything yet.
      </p>
    )
  }
  return (
    <div className="space-y-1 px-1 py-2">
      {blocks.map((b) =>
        b.kind === "answer" ? (
          <div key={b.id} className="md px-2 text-[13px]">
            {b.streaming ? (
              <p className="stream-text">{b.text}</p>
            ) : (
              <Markdown remarkPlugins={[remarkGfm]}>{b.text}</Markdown>
            )}
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

function summariseArgs(args: string): string {
  if (!args) return ""
  const flat = args.replace(/\s+/g, " ").trim()
  // Show the first value rather than the JSON scaffolding: "read a.md" says
  // more at a glance than `read {"file_path":…`.
  const first = /"[^"]+"\s*:\s*"([^"]{1,60})"/.exec(flat)
  const text = first ? first[1] : flat
  return text.length > 60 ? `${text.slice(0, 59)}…` : text
}

function pretty(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}
