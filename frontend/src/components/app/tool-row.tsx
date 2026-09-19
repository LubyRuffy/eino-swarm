import {
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  Loader2,
  Terminal,
} from "lucide-react"
import { useMemo, useState } from "react"

import { MarqueeText } from "@/components/app/marquee"
import { ShellCommand } from "@/components/app/shell-command"
import { ToolResultBody } from "@/components/app/tool-result"
import { Disclosure } from "@/components/ui/collapsible"
import { applyCarriageReturns, lastLine } from "@/lib/carriage"
import { execCommand, toolRowSummary, viewTool } from "@/lib/tool-view"
import type { Block } from "@/lib/transcript"

export function ToolRow({ block, reveal }: { block: Block; reveal?: boolean }) {
  const tool = block.tool
  // null until they click. useState(pending) opened a live exec and then
  // left the dump on screen after it returned — a wall nobody asked for.
  // Exec/python_runner stay collapsed; the latest line rides the summary.
  // Other tools still open while pending and fold when the result lands.
  // edit/write stay open: the hunk is the thing they came to review, and the
  // tool result is only a status sentence.
  const [choice, setChoice] = useState<boolean | null>(null)
  const toolName = tool?.name
  const toolArgs = tool?.args
  const toolResult = tool?.result
  const toolFailed = tool?.failed
  const view = useMemo(() => {
    if (toolName === undefined || toolArgs === undefined) return undefined
    return viewTool(toolName, toolArgs, toolResult, toolFailed)
  }, [toolName, toolArgs, toolResult, toolFailed])
  if (!tool || !view) return null
  const command = tool.name === "exec" ? execCommand(tool.args) : ""
  const liveLine = tool.pending && view.body ? lastLine(applyCarriageReturns(view.body)) : ""
  const watchLive =
    Boolean(tool.pending) && tool.name !== "exec" && tool.name !== "python_runner"
  const fileChange = Boolean(view.diff)
  const expanded = (choice ?? (watchLive || fileChange)) || Boolean(reveal)
  return (
    <Disclosure
      open={expanded}
      onOpenChange={setChoice}
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
              text={liveLine || toolRowSummary(view)}
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
        pending={tool.pending}
        view={view}
      />
    </Disclosure>
  )
}
