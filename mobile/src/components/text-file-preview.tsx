import { useMemo } from "react"

import { PhoneMarkdown } from "@/components/markdown"
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
  const render = text.length <= maxRenderedChars
  const tokens = useMemo(() => {
    const lang = languageFromPath(path)
    return render && lang ? tokenizeSource(text, lang) : [{ kind: "text" as const, text }]
  }, [path, text, render])
  if (render && isMarkdownPath(path)) return <PhoneMarkdown text={text} />
  return (
    <pre className="min-w-0 flex-1 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-3 text-sm">
      <code>{tokens.map((token, i) => <span key={i} className={tokenClass[token.kind]}>{token.text}</span>)}</code>
    </pre>
  )
}
