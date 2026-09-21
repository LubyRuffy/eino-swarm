import { Trash2 } from "lucide-react"

import { InputThumbs } from "@/components/app/input-thumbs"
import { QuotedMessageBody } from "@/components/app/quoted-message"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import type { Block } from "@/lib/transcript"
import { useT } from "@/lib/use-t"

/** Unread steering sits under the working line until the manager's next
 *  model round. Interrupt aborts the current manager tool so every bubble
 *  here lands now; Delete retracts one bubble so the model never sees it. */
export function QueuedSteers({
  blocks,
  threadId,
  onPreempt,
  onRetract,
}: {
  blocks: Block[]
  threadId?: string
  onPreempt: () => void
  onRetract: (seq: number) => void
}) {
  const t = useT()
  if (blocks.length === 0) return null
  return (
    <div
      data-testid="queued-steers"
      role="status"
      aria-label={t("transcript.queuedSteering")}
      className="mt-1 flex flex-col gap-1"
    >
      <div className="flex justify-end">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-7 px-2"
          data-testid="queued-steer-interrupt"
          aria-label={t("transcript.interruptSteerNamed")}
          onClick={onPreempt}
        >
          {t("transcript.interruptSteer")}
        </Button>
      </div>
      {blocks.map((block) => (
        <QueuedSteerRow
          key={block.id}
          block={block}
          threadId={threadId}
          onRetract={block.seq > 0 ? () => onRetract(block.seq) : undefined}
        />
      ))}
    </div>
  )
}

function QueuedSteerRow({
  block,
  threadId,
  onRetract,
}: {
  block: Block
  threadId?: string
  onRetract?: () => void
}) {
  const t = useT()
  return (
    <div className="mb-2 flex justify-end" data-testid="steer">
      <div className="flex max-w-[85%] items-start gap-2 rounded-2xl rounded-br-md border border-dashed border-border px-3 py-2 text-sm text-muted-foreground">
        <Badge variant="outline" className="mt-0.5 shrink-0">
          {t("transcript.steer")}
        </Badge>
        <div className="min-w-0 flex-1">
          <InputThumbs
            threadId={threadId}
            images={block.images}
            className={block.text ? "mb-1" : undefined}
          />
          {block.text ? <QuotedMessageBody text={block.text} /> : null}
        </div>
        {onRetract ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            data-testid="queued-steer-delete"
            aria-label={t("transcript.deleteSteerNamed")}
            onClick={onRetract}
          >
            <Trash2 />
          </Button>
        ) : null}
      </div>
    </div>
  )
}
