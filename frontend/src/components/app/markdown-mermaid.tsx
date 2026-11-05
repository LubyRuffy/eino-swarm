import { Maximize, ZoomIn, ZoomOut } from "lucide-react"
import { memo, useEffect, useLayoutEffect, useRef, useState } from "react"

import { MarkdownCodeBlock, type Fence } from "@/components/app/markdown-code"
import { MarkdownCopy } from "@/components/app/markdown-copy"
import { Button } from "@/components/ui/button"
import {
  MERMAID_ZOOM_MAX,
  MERMAID_ZOOM_MIN,
  mermaidFitScale,
  mermaidSVGDataURL,
  mermaidZoomBy,
  readMermaidAppearance,
  readMermaidFrame,
  renderMermaid,
  type MermaidBox,
} from "@/lib/markdown-mermaid"
import { useT } from "@/lib/use-t"

export const MarkdownMermaid = memo(function MarkdownMermaid({ source }: { source: string }) {
  const [appearance, setAppearance] = useState(() => JSON.stringify(readMermaidAppearance()))
  const [result, setResult] = useState<{ source: string; appearance: string; src: string } | null>(null)
  useEffect(() => {
    const update = () => setAppearance(JSON.stringify(readMermaidAppearance()))
    update()
    const observer = new MutationObserver(update)
    observer.observe(document.documentElement, { attributes: true })
    return () => observer.disconnect()
  }, [])
  useEffect(() => {
    let cancelled = false
    void renderMermaid(source, JSON.parse(appearance), () => cancelled).then(
      (svg) => {
        if (cancelled || !svg) return
        const src = mermaidSVGDataURL(svg)
        if (src) setResult({ source, appearance, src })
      },
      () => { /* Invalid diagrams retain their copyable source below. */ },
    )
    return () => { cancelled = true }
  }, [source, appearance])

  const fence: Fence = { lang: "mermaid", text: source, className: "language-mermaid" }
  if (result?.source !== source || result.appearance !== appearance) {
    return <MarkdownCodeBlock fence={fence} />
  }
  return (
    <figure className="md-mermaid" data-testid="markdown-mermaid" aria-label="Mermaid">
      <MermaidFigure source={source} src={result.src} onError={() => setResult(null)} />
    </figure>
  )
})

function diagramSize(image: HTMLImageElement): MermaidBox | null {
  if (image.naturalWidth > 0 && image.naturalHeight > 0) {
    return { width: image.naturalWidth, height: image.naturalHeight }
  }
  return null
}

function MermaidFigure({ source, src, onError }: { source: string; src: string; onError: () => void }) {
  const t = useT()
  const viewport = useRef<HTMLDivElement>(null)
  const diagram = useRef<HTMLImageElement>(null)
  const [natural, setNatural] = useState<MermaidBox | null>(null)
  const [frame, setFrame] = useState<MermaidBox>({ width: 0, height: 0 })
  const [manual, setManual] = useState<number | null>(null)
  useEffect(() => { setManual(null) }, [source])
  useLayoutEffect(() => {
    // A data-URL image can finish before React attaches onLoad.
    const image = diagram.current
    setNatural(image ? diagramSize(image) : null)
  }, [src])
  useLayoutEffect(() => {
    const element = viewport.current
    if (!element) return
    const measure = () => setFrame(readMermaidFrame(element))
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  const fit = natural ? mermaidFitScale(natural, frame) : 1
  const scale = manual ?? fit
  const ready = !!natural && frame.width > 0
  const percent = ready ? `${Number((scale * 100).toFixed(1))}%` : "—"
  return (
    <>
      <div className="flex flex-wrap items-center gap-2 px-3 pt-1.5" data-find-ignore="">
        <span className="min-w-0 flex-1 truncate font-mono text-[0.6875rem] text-muted-foreground">mermaid</span>
        <div role="group" aria-label={t("markdown.diagramControls")} className="flex shrink-0 items-center gap-0.5">
          <Button variant="ghost" size="icon-sm" aria-label={t("image.zoomOut")} title={t("image.zoomOut")}
            disabled={!ready || scale <= MERMAID_ZOOM_MIN} onClick={() => setManual(mermaidZoomBy(scale, -1))}><ZoomOut /></Button>
          <output aria-label={t("image.zoomLevel")} className="min-w-12 text-center text-xs tabular-nums text-muted-foreground">{percent}</output>
          <Button variant="ghost" size="icon-sm" aria-label={t("image.zoomIn")} title={t("image.zoomIn")}
            disabled={!ready || scale >= MERMAID_ZOOM_MAX} onClick={() => setManual(mermaidZoomBy(scale, 1))}><ZoomIn /></Button>
          <Button variant="ghost" size="sm" aria-label={t("image.actualSize")} title={t("image.actualSize")}
            disabled={!ready} onClick={() => setManual(1)}>100%</Button>
          <Button variant={manual === null ? "secondary" : "ghost"} size="sm" aria-pressed={manual === null}
            disabled={!ready} onClick={() => setManual(null)}><Maximize />{t("image.fit")}</Button>
        </div>
        <MarkdownCopy text={source} kind="code" />
      </div>
      <div ref={viewport} data-testid="mermaid-viewport" tabIndex={0} role="region" aria-label={t("markdown.diagramViewport")}
        className="md-mermaid-view thin-scrollbar min-w-0 overflow-auto p-3 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
        <img ref={diagram} alt="Mermaid" src={src} draggable={false}
          onLoad={(event) => {
            const size = diagramSize(event.currentTarget)
            if (size) setNatural(size)
          }}
          onError={onError}
          style={ready && natural ? { width: natural.width * scale, height: natural.height * scale, maxWidth: "none" } : undefined} />
      </div>
      <details className="px-3 pb-2">
        <summary className="cursor-pointer text-xs text-muted-foreground">{t("markdown.source")}</summary>
        <pre><code>{source}</code></pre>
      </details>
    </>
  )
}
