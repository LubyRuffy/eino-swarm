import { Loader2, Maximize, ZoomIn, ZoomOut } from "lucide-react"

import { Button } from "@/components/ui/button"
import { useImagePreview } from "@/lib/use-image-preview"
import { useT } from "@/lib/use-t"

export function FilePreviewImage({ src, name }: { src: string; name: string }) {
  const t = useT()
  const view = useImagePreview()
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div ref={view.viewport} data-testid="image-preview-viewport" tabIndex={0}
        role="region" aria-label={t("image.viewport")} aria-busy={!view.ready && !view.failed}
        className="thin-scrollbar min-h-0 min-w-0 flex-1 overflow-auto bg-muted/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
        {view.failed ? <p role="alert" className="p-5 text-sm text-destructive">{t("files.invalidImage")}</p> : (
          <>
            {!view.ready ? <p role="status" className="flex items-center gap-2 p-5 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />{t("image.loading")}</p> : null}
            <div className="flex min-h-full w-max min-w-full items-center justify-center">
              <img src={src} alt={name} onLoad={(event) => view.onLoad(event.currentTarget)} onError={view.onError}
                draggable={false} style={{ width: view.width, height: view.height }}
                className={`block max-w-none shrink-0 ${view.ready ? "" : "invisible absolute"}`} />
            </div>
          </>
        )}
      </div>
      <div role="group" aria-label={t("image.controls")} className="flex shrink-0 flex-wrap items-center justify-center gap-1 border-t px-3 py-2">
        <Button variant="ghost" size="icon-sm" aria-label={t("image.zoomOut")} title={t("image.zoomOut")}
          disabled={!view.ready || !view.canZoomOut} onClick={view.zoomOut}><ZoomOut /></Button>
        <output aria-label={t("image.zoomLevel")} className="min-w-14 text-center text-xs tabular-nums text-muted-foreground">
          {view.ready ? `${Number((view.scale * 100).toFixed(1))}%` : "—"}
        </output>
        <Button variant="ghost" size="icon-sm" aria-label={t("image.zoomIn")} title={t("image.zoomIn")}
          disabled={!view.ready || !view.canZoomIn} onClick={view.zoomIn}><ZoomIn /></Button>
        <Button variant="ghost" size="sm" aria-label={t("image.actualSize")} title={t("image.actualSize")}
          disabled={!view.ready} onClick={view.actualSize}>100%</Button>
        <Button variant={view.fitting ? "secondary" : "ghost"} size="sm" aria-pressed={view.fitting}
          disabled={!view.ready} onClick={view.fit}><Maximize />{t("image.fit")}</Button>
      </div>
    </div>
  )
}
