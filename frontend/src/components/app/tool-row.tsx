import {
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  Loader2,
  Terminal,
} from "lucide-react"
import { useState } from "react"

import { MarqueeText } from "@/components/app/marquee"
import { ShellCommand } from "@/components/app/shell-command"
import { ToolResultBody } from "@/components/app/tool-result"
import { Disclosure } from "@/components/ui/collapsible"
import { applyCarriageReturns, lastLine } from "@/lib/carriage"
import { execCommand, toolRowSummary, viewTool } from "@/lib/tool-view"
import type { Block } from "@/lib/transcript"

export function ToolRow({ block, reveal }: { block: Block; reveal?: boolean }) {
  const tool = block.tool
  const [open, setOpen] = useState(() => Boolean(tool?.pending))
  if (!tool) return null
  const view = viewTool(tool.name, tool.args, tool.result, tool.failed)
  const command = tool.name === "exec" ? execCommand(tool.args) : ""
  const liveLine = tool.pending && view.body ? lastLine(applyCarriageReturns(view.body)) : ""
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
      />
    </Disclosure>
  )
}
