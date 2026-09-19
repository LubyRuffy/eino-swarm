import {
  Children,
  isValidElement,
  useMemo,
  type ComponentPropsWithoutRef,
  type ReactElement,
  type ReactNode,
} from "react"

import { MarkdownCopy } from "@/components/app/markdown-copy"
import {
  languageFromFence,
  tokenizeSource,
  type SourceTokenKind,
} from "@/lib/source-highlight"

const KIND_CLASS: Record<SourceTokenKind, string> = {
  text: "",
  comment: "text-syntax-comment",
  string: "text-syntax-string",
  keyword: "text-syntax-keyword",
  command: "text-syntax-command",
  operator: "text-syntax-operator",
  flag: "text-syntax-flag",
  variable: "text-syntax-variable",
}

export type Fence = {
  lang: string
  text: string
  className?: string
}

/** react-markdown wraps a fence in one <code>. Anything else is not a
 *  language-tagged block — leave it to the caller. */
export function readFence(children: ReactNode): Fence | null {
  const codes: ReactElement<{ className?: string; children?: ReactNode }>[] = []
  Children.forEach(children, (child) => {
    if (!isValidElement(child)) return
    codes.push(child as ReactElement<{ className?: string; children?: ReactNode }>)
  })
  if (codes.length !== 1) return null
  const el = codes[0]
  const className = el.props.className
  const lang = /language-([^\s]+)/.exec(className ?? "")?.[1] ?? ""
  const text = childText(el.props.children).replace(/\n$/, "")
  return { lang, text, className }
}

function childText(node: ReactNode): string {
  if (node == null || typeof node === "boolean") return ""
  if (typeof node === "string" || typeof node === "number") return String(node)
  if (Array.isArray(node)) return node.map(childText).join("")
  if (isValidElement(node) && "children" in (node.props as { children?: ReactNode })) {
    return childText((node.props as { children?: ReactNode }).children)
  }
  return ""
}

/** Fenced source: language from the info string, same tokens as a `read`
 *  listing, copy of the original body. Unknown tags stay one uncoloured
 *  span so Find still sees the fence text. */
export function MarkdownCodeBlock({
  fence,
  ...props
}: {
  fence: Fence
} & ComponentPropsWithoutRef<"pre"> & { node?: unknown }) {
  const { node: _node, ...preProps } = props
  return (
    <div className="md-code" data-testid="markdown-code">
      <div
        className="flex items-center gap-2 px-3 pt-1.5"
        data-find-ignore=""
      >
        <span className="min-w-0 flex-1 truncate font-mono text-[0.6875rem] text-muted-foreground">
          {fence.lang}
        </span>
        <MarkdownCopy text={fence.text} kind="code" />
      </div>
      <pre {...preProps}>
        <code className={fence.className}>
          <HighlightedCode source={fence.text} lang={fence.lang} />
        </code>
      </pre>
    </div>
  )
}

function HighlightedCode({ source, lang }: { source: string; lang: string }) {
  const tokens = useMemo(() => {
    const id = languageFromFence(lang)
    if (!id) return [{ kind: "text" as const, text: source }]
    return tokenizeSource(source, id)
  }, [source, lang])
  return (
    <>
      {tokens.map((t, i) => (
        <span key={i} className={KIND_CLASS[t.kind] || undefined}>
          {t.text}
        </span>
      ))}
    </>
  )
}
