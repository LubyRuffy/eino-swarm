import { useEffect, useState } from "react"
import { Brain, ChevronDown, ChevronRight, Wrench } from "lucide-react"
import { cn } from "@/lib/utils"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { lastLine, firstWords } from "./state"
import type { Block, AgentState } from "./state"

export function TranscriptBlock({ b }: { b: Block }) {
  const [userOverride, setUserOverride] = useState<boolean | null>(null)
  const shown = userOverride ?? b.open
  useEffect(() => { setUserOverride(null) }, [b.kind])

  if (b.kind === "thinking") {
    return (
      <Collapsible open={shown} onOpenChange={setUserOverride} className="my-1.5">
        <CollapsibleTrigger className="flex w-full items-center gap-1.5 rounded-md px-1.5 py-0.5 text-left text-[12px] text-muted-foreground hover:bg-accent/40">
          {shown ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
          <Brain className="size-3.5 opacity-70" />
          {b.live ? <span className="text-primary">thinking…</span> : <span>thought</span>}
          <span className="opacity-50">({b.text.length})</span>
          {!shown && <span className="truncate opacity-70">{firstWords(lastLine(b.text), 14)}</span>}
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className="ml-3 mt-1 mb-1.5 border-l-2 border-border pl-2.5 text-[12px] italic text-muted-foreground whitespace-pre-wrap max-h-48 overflow-y-auto">
            {b.text}
          </div>
        </CollapsibleContent>
      </Collapsible>
    )
  }
  if (b.kind === "tool") {
    return (
      <Collapsible open={shown} onOpenChange={setUserOverride} className="my-1.5">
        <CollapsibleTrigger className={cn(
          "flex w-full items-center gap-1.5 rounded-md px-1.5 py-0.5 text-left text-[12px] hover:bg-accent/40",
          shown ? "text-chart-4" : "text-muted-foreground",
        )}>
          {shown ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
          <Wrench className="size-3.5" />
          <span className="font-medium">{b.name}</span>
          {!shown && b.res && <span className="truncate opacity-70">→ {firstWords(b.res, 12)}</span>}
          {!shown && !b.res && b.args && <span className="truncate opacity-70">({b.args.slice(0, 48)})</span>}
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className="ml-3 mt-1 space-y-1 border-l-2 border-border pl-2.5">
            {b.args && <div className="break-all text-[12px] text-amber-200/80">args: {b.args}</div>}
            {b.res && <div className="break-all text-[12px] text-emerald-300/80">← {b.res}</div>}
          </div>
        </CollapsibleContent>
      </Collapsible>
    )
  }
  // answer
  if (shown) {
    return <div className="my-1.5 whitespace-pre-wrap text-[#d8e4e6]">{b.text}</div>
  }
  return <div className="my-1 text-[#42d392] text-[13px]">▸ {firstWords(lastLine(b.text), 16)}</div>
}

export function AgentStatus({ a }: { a: AgentState }) {
  if (a.finished) {
    return a.finErr ? (
      <span className="inline-flex items-center gap-1 text-[12px] text-destructive">✗ error</span>
    ) : (
      <span className="inline-flex items-center gap-1 text-[12px] text-emerald-400">✓ done</span>
    )
  }
  if (a.curTool) {
    return (
      <span className="inline-flex items-center gap-1 text-[12px] text-amber-400">
        <Wrench className="size-3" /> {a.curTool.name}
      </span>
    )
  }
  if (a.curThink) {
    return (
      <span className="inline-flex items-center gap-1 text-[12px] text-muted-foreground">
        <Brain className="size-3" /> thinking
      </span>
    )
  }
  return <span className="text-[12px] text-primary">▌ streaming</span>
}
