import { Maximize, ZoomIn, ZoomOut } from "lucide-react"
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"
import {
  MERMAID_ZOOM_MAX, MERMAID_ZOOM_MIN, mermaidFitScale, mermaidSVGDataURL,
  mermaidZoomBy, readMermaidAppearance, readMermaidFrame, renderMermaid, type MermaidBox,
} from "../../../frontend/src/lib/markdown-mermaid"

// Rendering, theme resolution and resource refusal share desktop's renderer.
// Phone controls stay touch-sized and scroll the diagram inside its own frame.
export function PhoneMermaid({ source, fallback }: { source: string; fallback: ReactNode }) {
  const [appearance, setAppearance] = useState(() => JSON.stringify(readMermaidAppearance()))
  const [result, setResult] = useState<{ source: string; appearance: string; src: string }>()
  useEffect(() => {
    const observer = new MutationObserver(() => setAppearance(JSON.stringify(readMermaidAppearance())))
    observer.observe(document.documentElement, { attributes: true })
    return () => observer.disconnect()
  }, [])
  useEffect(() => {
    let cancelled = false
    void renderMermaid(source, JSON.parse(appearance), () => cancelled).then((svg) => {
      if (cancelled || !svg) return
      const src = mermaidSVGDataURL(svg)
      if (src) setResult({ source, appearance, src })
    }).catch(() => undefined)
    return () => { cancelled = true }
  }, [source, appearance])
  if (result?.source !== source || result.appearance !== appearance) return fallback
  return <figure className="my-2 min-w-0 max-w-full rounded-lg border border-border bg-muted/40" data-testid="phone-mermaid" aria-label="Mermaid">
    <Diagram key={source} src={result.src} onError={() => setResult(undefined)} />
    <details className="px-2 pb-2"><summary className="cursor-pointer text-sm text-muted-foreground">{t("mermaid.source")}</summary>{fallback}</details>
  </figure>
}

function Diagram({ src, onError }: { src: string; onError: () => void }) {
  const viewport = useRef<HTMLDivElement>(null)
  const image = useRef<HTMLImageElement>(null)
  const [natural, setNatural] = useState<MermaidBox>()
  const [frame, setFrame] = useState<MermaidBox>({ width: 0, height: 0 })
  const [manual, setManual] = useState<number>()
  const readImage = (img: HTMLImageElement) => {
    if (img.naturalWidth > 0 && img.naturalHeight > 0) setNatural({ width: img.naturalWidth, height: img.naturalHeight })
  }
  useLayoutEffect(() => { if (image.current) readImage(image.current) }, [src])
  useLayoutEffect(() => {
    const node = viewport.current
    if (!node) return
    const measure = () => setFrame(readMermaidFrame(node))
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(node)
    return () => observer.disconnect()
  }, [])
  const scale = manual ?? (natural ? mermaidFitScale(natural, frame) : 1)
  const ready = Boolean(natural && frame.width > 0)
  return <>
    <div role="group" aria-label={t("mermaid.controls")} className="flex flex-wrap items-center gap-1 p-1">
      <Button type="button" variant="ghost" className="size-10 shrink-0 px-0" aria-label={t("mermaid.zoomOut")} disabled={!ready || scale <= MERMAID_ZOOM_MIN} onClick={() => setManual(mermaidZoomBy(scale, -1))}><ZoomOut className="size-4" /></Button>
      <output className="min-w-10 text-center text-xs" aria-label={t("mermaid.zoomLevel")}>{ready ? `${Number((scale * 100).toFixed(1))}%` : "—"}</output>
      <Button type="button" variant="ghost" className="size-10 shrink-0 px-0" aria-label={t("mermaid.zoomIn")} disabled={!ready || scale >= MERMAID_ZOOM_MAX} onClick={() => setManual(mermaidZoomBy(scale, 1))}><ZoomIn className="size-4" /></Button>
      <Button type="button" variant="ghost" className="shrink-0 px-2" aria-label={t("mermaid.actualSize")} disabled={!ready} onClick={() => setManual(1)}>100%</Button>
      <Button type="button" variant="ghost" className="min-w-0 gap-1 px-2" aria-pressed={manual === undefined} disabled={!ready} onClick={() => setManual(undefined)}><Maximize className="size-4 shrink-0" />{t("mermaid.fit")}</Button>
    </div>
    <div ref={viewport} role="region" aria-label={t("mermaid.viewport")} tabIndex={0} data-testid="mermaid-viewport" className="max-h-[65vh] min-w-0 overflow-auto p-2" style={{ scrollbarGutter: "stable" }}>
      <img ref={image} src={src} alt="Mermaid" draggable={false} onLoad={(event) => readImage(event.currentTarget)} onError={onError}
        style={ready && natural ? { display: "block", margin: "auto", width: natural.width * scale, height: natural.height * scale, maxWidth: "none" } : undefined} />
    </div>
  </>
}
