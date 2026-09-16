import { Loader2, Paperclip, X } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { formatBytes } from "@/lib/utils"
import { useT } from "@/lib/use-t"

/** Pending workspace files sitting on the composer until send. */
export function ComposerAttachments({
  files,
  uploading,
  onRemove,
}: {
  files: File[]
  uploading?: boolean
  onRemove: (index: number) => void
}) {
  const t = useT()
  if (files.length === 0) return null
  return (
    <div
      data-testid="composer-attachments"
      aria-busy={uploading || undefined}
      className="mb-2 flex flex-wrap gap-1.5"
    >
      {files.map((f, i) => (
        <Badge key={`${f.name}-${i}`} variant="outline" className="gap-1.5 py-1">
          {uploading ? (
            <Loader2 className="size-3 animate-spin" aria-hidden />
          ) : (
            <Paperclip className="size-3" aria-hidden />
          )}
          <span className="max-w-48 truncate">{f.name}</span>
          <span className="text-muted-foreground">{formatBytes(f.size)}</span>
          <button
            type="button"
            aria-label={t("composer.removeNamed", { name: f.name })}
            disabled={uploading}
            onClick={() => onRemove(i)}
            className="ml-0.5 rounded hover:text-foreground disabled:pointer-events-none"
          >
            <X className="size-3" />
          </button>
        </Badge>
      ))}
    </div>
  )
}
