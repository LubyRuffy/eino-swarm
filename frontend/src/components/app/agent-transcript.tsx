import { useMemo } from "react"
import { MemoMarkdown } from "@/components/app/markdown"
import { Reasoning } from "@/components/app/transcript"
import { ToolRow } from "@/components/app/tool-row"
import { useT } from "@/lib/use-t"
import type { AgentState } from "@/lib/transcript"

/** One agent's own transcript, for the right-hand panel. */
export function AgentTranscript({ agent }: { agent: AgentState }) {
  const t = useT()
  const blocks = useMemo(
    () => agent.blocks.filter((b) => b.kind !== "user"),
    [agent.blocks],
  )
  if (blocks.length === 0) {
    if (agent.result?.trim()) {
      return (
        <div className="md px-3 py-2 text-[13px]">
          <MemoMarkdown text={agent.result} />
        </div>
      )
    }
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
