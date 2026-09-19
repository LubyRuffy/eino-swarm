import { useMemo } from "react"
import katex from "katex"
import "katex/dist/katex.min.css"

import { MarkdownCopy } from "@/components/app/markdown-copy"

/** KaTeX HTML is escaped by the library. throwOnError is off so a
 *  streaming fragment stays readable instead of blowing the block up. */
function renderKatex(tex: string, display: boolean): string | null {
  const src = tex.trim()
  if (!src) return null
  try {
    return katex.renderToString(src, {
      displayMode: display,
      throwOnError: false,
      strict: "ignore",
      output: "html",
      errorColor: "hsl(var(--destructive))",
    })
  } catch {
    return null
  }
}

export function MarkdownMath({
  tex,
  display = false,
}: {
  tex: string
  display?: boolean
}) {
  const html = useMemo(() => renderKatex(tex, display), [tex, display])
  if (display) {
    return (
      <div className="md-math" data-testid="markdown-math">
        <div className="mb-1 flex justify-end" data-find-ignore="">
          <MarkdownCopy text={tex} kind="formula" />
        </div>
        {html ? (
          <div
            className="overflow-x-auto"
            dangerouslySetInnerHTML={{ __html: html }}
          />
        ) : (
          <pre>
            <code>{tex}</code>
          </pre>
        )}
      </div>
    )
  }
  if (!html) return <code>{tex}</code>
  return (
    <span
      className="md-math-inline"
      data-testid="markdown-math-inline"
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
}
