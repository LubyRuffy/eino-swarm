import { Cloud } from "lucide-react"

import { cloudToolSummary } from "@/lib/cloud-tool"
import type { Block } from "@/lib/transcript"
import { useT } from "@/lib/use-t"

export function CloudToolNotice({ block }: { block: Block }) {
  const t = useT()
  if (block.quiet) return null
  const tool = block.cloudTool ?? {}
  return (
    <div data-testid="cloud-tool-notice" className="my-2 flex items-start gap-2 rounded-lg border border-border bg-muted/50 px-3 py-2 text-[13px] text-muted-foreground">
      <Cloud aria-hidden className="mt-0.5 size-3.5 shrink-0" />
      <p className="min-w-0 flex-1 whitespace-pre-wrap">
        {cloudToolSummary(tool, t.locale)}
        {typeof tool.provider_id === "string" ? <span className="ml-2">{tool.provider_id}</span> : null}
      </p>
    </div>
  )
}
