import { CaseLower, CaseUpper, RotateCcw } from "lucide-react"
import { useMemo, useState } from "react"

import { PhoneMarkdown } from "@/components/markdown"
import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"
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
  const [fontSize, setFontSize] = useState(14)
  const render = text.length <= maxRenderedChars
  const tokens = useMemo(() => {
    const lang = languageFromPath(path)
    return render && lang ? tokenizeSource(text, lang) : [{ kind: "text" as const, text }]
  }, [path, text, render])
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-3 overflow-hidden">
      <div role="group" aria-label={t("files.textControls")} className="flex shrink-0 flex-wrap items-center justify-center gap-1">
        <Button type="button" variant="ghost" className="size-11 px-0" aria-label={t("files.textSmaller")}
          disabled={fontSize <= 12} onClick={() => setFontSize((size) => Math.max(12, size - 2))}><CaseLower className="size-5" /></Button>
        <output aria-label={t("files.textSize")} className="min-w-14 text-center text-xs tabular-nums text-muted-foreground">{fontSize} px</output>
        <Button type="button" variant="ghost" className="size-11 px-0" aria-label={t("files.textLarger")}
          disabled={fontSize >= 28} onClick={() => setFontSize((size) => Math.min(28, size + 2))}><CaseUpper className="size-5" /></Button>
        <Button type="button" variant="ghost" className="h-11 gap-1.5 px-3" aria-label={t("files.textReset")}
          disabled={fontSize === 14} onClick={() => setFontSize(14)}><RotateCcw className="size-4" />{t("files.textReset")}</Button>
      </div>
      <div data-testid="phone-text-content" style={{ fontSize: `${fontSize}px` }}
        className="file-text-content min-h-0 min-w-0 flex-1 overflow-auto wrap-anywhere">
        {render && isMarkdownPath(path) ? <PhoneMarkdown text={text} /> : (
          <pre className="min-w-0 whitespace-pre-wrap wrap-break-word rounded-md bg-muted p-3 [font-size:inherit]">
            <code>{tokens.map((token, i) => <span key={i} className={tokenClass[token.kind]}>{token.text}</span>)}</code>
          </pre>
        )}
      </div>
    </div>
  )
}
