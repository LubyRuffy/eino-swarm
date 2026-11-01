import { memo, useEffect, useState } from "react"

import { MarkdownCodeBlock, type Fence } from "@/components/app/markdown-code"
import { MarkdownCopy } from "@/components/app/markdown-copy"
import { mermaidSVGDataURL, readMermaidAppearance, renderMermaid } from "@/lib/markdown-mermaid"
import { useT } from "@/lib/use-t"

export const MarkdownMermaid = memo(function MarkdownMermaid({ source }: { source: string }) {
  const t = useT()
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
      <div className="flex items-center gap-2 px-3 pt-1.5" data-find-ignore="">
        <span className="min-w-0 flex-1 truncate font-mono text-[0.6875rem] text-muted-foreground">mermaid</span>
        <MarkdownCopy text={source} kind="code" />
      </div>
      <div className="thin-scrollbar overflow-x-auto p-3">
        <img alt="Mermaid" src={result.src} onError={() => setResult(null)} />
      </div>
      <details className="px-3 pb-2">
        <summary className="cursor-pointer text-xs text-muted-foreground">{t("markdown.source")}</summary>
        <pre><code>{source}</code></pre>
      </details>
    </figure>
  )
})
