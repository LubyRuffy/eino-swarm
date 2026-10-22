import type { CompactBlock } from "@/lib/transcript"
import { t } from "@/lib/i18n"

export function GenerationNotice({ block }: { block: CompactBlock }) {
  const generation = block.generation
  if (!generation) return null
  return (
    <p data-testid="generation-notice" role="status" className="text-[11px] text-muted-foreground">
      {t("generation.interrupted", { reason: t(`generation.reason.${generation.reason}`) })}{" "}
      {t(`generation.recovery.${generation.recovery}`)}
      {generation.detail ? <span className="ml-1 whitespace-pre-wrap break-words">{generation.detail}</span> : null}
    </p>
  )
}
