import { X } from "lucide-react"

import { Button } from "@/components/ui/button"
import type { PasteImage } from "@/lib/paste-image"
import { useT } from "@/lib/use-t"

/** Codex-style thumbs in the composer: see the paste, drop it before send. */
export function ComposerImages({
  images,
  onRemove,
}: {
  images: PasteImage[]
  onRemove: (id: string) => void
}) {
  const t = useT()
  if (images.length === 0) return null
  return (
    <ul
      data-testid="composer-images"
      className="flex flex-wrap gap-2 px-3 pt-3"
    >
      {images.map((img) => (
        <li key={img.id} className="relative">
          <img
            src={img.previewUrl}
            alt={img.name}
            className="h-16 w-16 rounded-md border border-border object-cover"
          />
          <Button
            type="button"
            variant="secondary"
            size="icon-sm"
            aria-label={t("composer.removeNamed", { name: img.name })}
            className="absolute -right-1.5 -top-1.5 size-5 rounded-full"
            onClick={() => onRemove(img.id)}
          >
            <X className="size-3" />
          </Button>
        </li>
      ))}
    </ul>
  )
}
