import { Loader2, Maximize, ZoomIn, ZoomOut } from "lucide-react"

import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"
import { useImagePreview } from "../../../frontend/src/lib/use-image-preview"

export function ImageFilePreview({ src, name }: { src: string; name: string }) {
  const view = useImagePreview()
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      <div ref={view.viewport} data-testid="phone-image-viewport" tabIndex={0} role="region" aria-label={t("files.imageViewport")}
        aria-busy={!view.ready && !view.failed}
        className="min-h-0 min-w-0 flex-1 overflow-auto rounded-md bg-muted/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
        {view.failed ? <p role="alert" className="p-3 text-sm text-destructive">{t("files.invalidImage")}</p> : (
          <>
            {!view.ready ? <p role="status" className="flex items-center gap-2 p-3 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />{t("files.imageLoading")}</p> : null}
            <div className="flex min-h-full w-max min-w-full items-center justify-center">
              <img src={src} alt={name} onLoad={(event) => view.onLoad(event.currentTarget)} onError={view.onError}
                draggable={false} style={{ width: view.width, height: view.height }}
                className={`block max-w-none shrink-0 ${view.ready ? "" : "invisible absolute"}`} />
            </div>
          </>
        )}
      </div>
      <div role="group" aria-label={t("files.imageControls")} className="flex shrink-0 flex-wrap items-center justify-center gap-1 border-t border-border py-2">
        <Button type="button" variant="ghost" className="size-11 shrink-0 px-0" aria-label={t("files.zoomOut")}
          title={t("files.zoomOut")} disabled={!view.ready || !view.canZoomOut} onClick={view.zoomOut}><ZoomOut className="size-5" /></Button>
        <output aria-label={t("files.zoomLevel")} className="min-w-12 text-center text-xs tabular-nums text-muted-foreground">
          {view.ready ? `${Number((view.scale * 100).toFixed(1))}%` : "—"}
        </output>
        <Button type="button" variant="ghost" className="size-11 shrink-0 px-0" aria-label={t("files.zoomIn")}
          title={t("files.zoomIn")} disabled={!view.ready || !view.canZoomIn} onClick={view.zoomIn}><ZoomIn className="size-5" /></Button>
        <Button type="button" variant="ghost" className="h-11 min-w-11 px-2" aria-label={t("files.actualSize")}
          title={t("files.actualSize")} disabled={!view.ready} onClick={view.actualSize}>100%</Button>
        <Button type="button" variant={view.fitting ? "outline" : "ghost"} className="h-11 min-w-11 gap-1 px-2"
          aria-pressed={view.fitting} disabled={!view.ready} onClick={view.fit}><Maximize className="size-4" />{t("files.fit")}</Button>
      </div>
    </div>
  )
}
