import { generationNotice } from "@/lib/transcript-generation"
import type { Block } from "@/lib/transcript"
import { useT } from "@/lib/use-t"

/** Recovery updates this output's receipt, never its worker's failure state. */
export function GenerationNotice({ block }: { block: Block }) {
  const t = useT()
  const generation = block.generation
  if (!generation) return null
  return (
    <p data-testid="generation-notice" role="status" className="px-2 py-1 text-xs text-muted-foreground">
      {generationNotice(generation, t.locale)}
      {generation.detail ? <span className="ml-1 whitespace-pre-wrap break-words">{generation.detail}</span> : null}
    </p>
  )
}
