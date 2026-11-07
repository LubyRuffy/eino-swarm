import { useMemo, useRef } from "react"

import { PhoneMarkdown } from "@/components/markdown"
import { ReadingFontControls, useReadingFont } from "@/components/reading-font"
import { usePinch } from "@/lib/use-pinch"
import { isMarkdownPath } from "../../../frontend/src/lib/read-result"
import { languageFromPath, tokenizeSource, type SourceTokenKind } from "../../../frontend/src/lib/source-highlight"

const tokenClass: Record<SourceTokenKind, string> = {
  text: "",
  keyword: "text-syntax-keyword",
  command: "text-syntax-command",
  string: "text-syntax-string",
  comment: "text-syntax-comment",
  operator: "text-syntax-operator",
  flag: "text-syntax-flag",
  variable: "text-syntax-variable",
}

// Large readable files still show every character without building a huge AST
// or one React span per token on a phone. Downloads always retain the bytes.
const maxRenderedChars = 256 * 1024

export function TextFilePreview({ path, text }: { path: string; text: string }) {
  const font = useReadingFont(14)
  const content = useRef<HTMLDivElement>(null)
  usePinch(content, ({ distance }) => (point) => font.setSize(font.size * point.distance / distance))
  const render = text.length <= maxRenderedChars
  const tokens = useMemo(() => {
    const lang = languageFromPath(path)
    return render && lang ? tokenizeSource(text, lang) : [{ kind: "text" as const, text }]
  }, [path, text, render])
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-3 overflow-hidden">
      <ReadingFontControls font={font} />
      <div ref={content} data-testid="phone-text-content" style={{ fontSize: `${font.size}px` }}
        className="file-text-content min-h-0 min-w-0 flex-1 overflow-auto wrap-anywhere [touch-action:pan-x_pan-y]">
        {render && isMarkdownPath(path) ? <PhoneMarkdown text={text} /> : (
          <pre className="min-w-0 whitespace-pre-wrap wrap-break-word rounded-md bg-muted p-3 [font-size:inherit]">
            <code>{tokens.map((token, i) => <span key={i} className={tokenClass[token.kind]}>{token.text}</span>)}</code>
          </pre>
        )}
      </div>
    </div>
  )
}
