import { Loader2, X } from "lucide-react"
import { useState } from "react"

import { Button } from "@/components/ui/button"
import { Dialog, DialogClose, DialogContent, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"
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
        <li key={`${threadId}:${img.id}`} className="min-w-0">
          <InputImageThumb src={api.inputImageURL(threadId, img.id)} name={img.name} />
        </li>
      ))}
    </ul>
  )
}

function InputImageThumb({ src, name }: { src: string; name?: string }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const label = name || t("image.pasted")
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          className="h-auto max-w-full cursor-zoom-in p-0 hover:bg-transparent"
          aria-label={t("image.enlarge", { name: label })}
        >
          <img src={src} alt={label} className="max-h-40 max-w-full rounded-md border border-border" />
        </Button>
      </DialogTrigger>
      {open ? (
        <DialogContent
          className="input-image-preview data-[state=open]:slide-in-from-left-1/2 data-[state=open]:slide-in-from-top-1/2"
          hideClose
          aria-describedby={undefined}
        >
          <DialogTitle className="truncate pr-10 text-sm">{label}</DialogTitle>
          <DialogClose asChild>
            <Button type="button" variant="ghost" size="icon-sm"
              className="absolute right-2 top-2" aria-label={t("image.close")}>
              <X />
            </Button>
          </DialogClose>
          <ImagePreview src={src} name={label} />
        </DialogContent>
      ) : null}
    </Dialog>
  )
}

function ImagePreview({ src, name }: { src: string; name: string }) {
  const t = useT()
  const [status, setStatus] = useState<"loading" | "loaded" | "error">("loading")
  return (
    <>
      {status !== "loaded" ? (
        <div className="input-image-preview-status" role={status === "error" ? "alert" : "status"}>
          {status === "loading" ? <Loader2 className="size-4 animate-spin" /> : null}
          {t(status === "error" ? "image.failed" : "image.loading")}
        </div>
      ) : null}
      <img
        src={src}
        alt={name}
        className={cn("input-image-preview-image", status !== "loaded" && "hidden")}
        onLoad={() => setStatus("loaded")}
        onError={() => setStatus("error")}
      />
    </>
  )
}
