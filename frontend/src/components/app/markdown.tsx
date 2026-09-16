import { memo, type ComponentPropsWithoutRef } from "react"
import Markdown from "react-markdown"
import remarkGfm from "remark-gfm"

import { classifyHref } from "@/lib/external-links"
import { closeIncompleteMarkdown } from "@/lib/stream-markdown"

/** remark plugins are module-level so a memoised markdown block is not
 *  invalidated by a new array on every parent render. */
const PLUGINS = [remarkGfm]
const COMPONENTS = { a: MarkdownLink }

/** Completed markdown is immutable. Without this memo, every streamed token
 *  would re-parse every finished answer in the conversation. The open
 *  streaming block is a different text each time, so it still re-parses —
 *  that is the live render. */
export const MemoMarkdown = memo(function MemoMarkdown({
  text,
  streaming = false,
}: {
  text: string
  streaming?: boolean
}) {
  const source = streaming ? closeIncompleteMarkdown(text) : text
  return (
    <>
      <Markdown remarkPlugins={PLUGINS} components={COMPONENTS}>
        {source}
      </Markdown>
      {streaming ? (
        <span
          aria-hidden
          className="ml-0.5 inline-block h-4 w-1.5 translate-y-0.5 animate-breathe bg-foreground/70"
        />
      ) : null}
    </>
  )
})

function MarkdownLink({
  href,
  children,
  node: _node,
  ...props
}: ComponentPropsWithoutRef<"a"> & { node?: unknown }) {
  const origin =
    typeof window === "undefined" ? "http://127.0.0.1" : window.location.origin
  const action = classifyHref(href, origin)
  if (action === "block") {
    return <span>{children}</span>
  }
  if (action === "leave") {
    return (
      <a {...props} href={href} target="_blank" rel="noopener noreferrer">
        {children}
      </a>
    )
  }
  return (
    <a {...props} href={href}>
      {children}
    </a>
  )
}
