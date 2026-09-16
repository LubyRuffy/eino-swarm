import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { ImageRef } from "@/lib/types"

/** Thumbnails for pasted vision input. The pixels are fetched from the
 *  conversation's input-images endpoint — they are not workspace files. */
export function InputThumbs({
  threadId,
  images,
  className,
}: {
  threadId?: string
  images?: ImageRef[]
  className?: string
}) {
  if (!threadId || !images?.length) return null
  return (
    <ul
      data-testid="input-images"
      className={cn("flex flex-wrap gap-2", className)}
    >
      {images.map((img) => (
        <li key={img.id}>
          <img
            src={api.inputImageURL(threadId, img.id)}
            alt={img.name || "Pasted image"}
            className="max-h-40 max-w-full rounded-md border border-border"
          />
        </li>
      ))}
    </ul>
  )
}
