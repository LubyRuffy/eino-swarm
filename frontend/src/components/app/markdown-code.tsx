import {
  Children,
  isValidElement,
  type ComponentPropsWithoutRef,
  type ReactElement,
  type ReactNode,
} from "react"

import { MarkdownCopy } from "@/components/app/markdown-copy"
import {
  CodeBlock,
  CodeBlockActions,
  CodeBlockContent,
  CodeBlockHeader,
  CodeBlockTitle,
} from "@/components/ui/code-block"
import { HighlightedLines } from "@/lib/highlighted-lines"

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

/** Fenced source. The header names the language and copies the original
 *  body. Highlighting waits for that language's grammar; an unknown tag
 *  stays uncoloured text so Find still sees the fence. */
export function MarkdownCodeBlock({
  fence,
  ...props
}: {
  fence: Fence
} & ComponentPropsWithoutRef<"pre"> & { node?: unknown }) {
  const { node: _node, ...preProps } = props
  return (
    <CodeBlock data-testid="markdown-code">
      <CodeBlockHeader data-find-ignore="">
        <CodeBlockTitle className="font-mono text-xs font-normal text-muted-foreground">
          {fence.lang}
        </CodeBlockTitle>
        <CodeBlockActions>
          <MarkdownCopy text={fence.text} kind="code" />
        </CodeBlockActions>
      </CodeBlockHeader>
      <CodeBlockContent codeClassName={fence.className} {...preProps}>
        <HighlightedLines text={fence.text} lang={fence.lang} />
      </CodeBlockContent>
    </CodeBlock>
  )
}
