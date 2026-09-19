import {
  createContext,
  lazy,
  memo,
  Suspense,
  useContext,
  useRef,
  type ComponentPropsWithoutRef,
  type ReactNode,
} from "react"
import Markdown from "react-markdown"
import type { Components } from "react-markdown"
import remarkGfm from "remark-gfm"
import remarkMath from "remark-math"

import { ChartPending } from "@/components/app/chart-pending"
import { MarkdownCodeBlock, readFence } from "@/components/app/markdown-code"
import { classifyHref } from "@/lib/external-links"
import { isMathFence } from "@/lib/math-fence"
import { chartSpecsEqual, parseChartSpec, type ChartSpec } from "@/lib/chart-spec"
import { closeIncompleteMarkdown } from "@/lib/stream-markdown"

const TranscriptChart = lazy(async () => {
  const mod = await import("@/components/app/transcript-chart")
  return { default: mod.TranscriptChart }
})

const MarkdownMath = lazy(async () => {
  const mod = await import("@/components/app/markdown-math")
  return { default: mod.MarkdownMath }
})

/** remark plugins are module-level so a memoised markdown block is not
 *  invalidated by a new array on every parent render. */
const PLUGINS = [remarkGfm, remarkMath]

const MarkdownStreaming = createContext(false)

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
    <MarkdownStreaming.Provider value={streaming}>
      <Markdown remarkPlugins={PLUGINS} components={COMPONENTS}>
        {source}
      </Markdown>
      {streaming ? (
        <span
          aria-hidden
          className="ml-0.5 inline-block h-4 w-1.5 translate-y-0.5 animate-breathe bg-foreground/70"
        />
      ) : null}
    </MarkdownStreaming.Provider>
  )
})

function MarkdownPre({
  children,
  node: _node,
  ...props
}: ComponentPropsWithoutRef<"pre"> & { node?: unknown }) {
  const streaming = useContext(MarkdownStreaming)
  const specRef = useRef<ChartSpec | null>(null)
  const fence = readFence(children)
  if (fence !== null && fence.lang === "chart") {
    const parsed = parseChartSpec(fence.text)
    if (parsed.ok) {
      if (!specRef.current || !chartSpecsEqual(specRef.current, parsed.spec)) {
        specRef.current = parsed.spec
      }
      return (
        <Suspense fallback={<ChartPending />}>
          <TranscriptChart spec={specRef.current} />
        </Suspense>
      )
    }
    specRef.current = null
    if (streaming || parsed.incomplete) return <ChartPending />
  } else {
    specRef.current = null
  }
  if (fence !== null && isMathFence(fence.lang)) {
    return (
      <Suspense fallback={<code>{fence.text}</code>}>
        <MarkdownMath display tex={fence.text} />
      </Suspense>
    )
  }
  if (fence !== null) {
    return <MarkdownCodeBlock fence={fence} {...props} />
  }
  return <pre {...props}>{children}</pre>
}

function MarkdownInlineCode({
  className,
  children,
  node: _node,
  ...props
}: ComponentPropsWithoutRef<"code"> & { node?: unknown }) {
  if (className?.includes("math-display")) {
    const tex = codeText(children)
    return (
      <Suspense fallback={<code>{tex}</code>}>
        <MarkdownMath display tex={tex} />
      </Suspense>
    )
  }
  if (
    className?.includes("math-inline") ||
    /(^|\s)language-math(\s|$)/.test(className ?? "")
  ) {
    const tex = codeText(children)
    return (
      <Suspense fallback={<code>{tex}</code>}>
        <MarkdownMath tex={tex} />
      </Suspense>
    )
  }
  return (
    <code className={className} {...props}>
      {children}
    </code>
  )
}

function codeText(children: ReactNode): string {
  if (children == null || typeof children === "boolean") return ""
  if (typeof children === "string" || typeof children === "number") {
    return String(children).replace(/\n$/, "")
  }
  if (Array.isArray(children)) {
    return children.map((c) => codeText(c as ReactNode)).join("").replace(/\n$/, "")
  }
  return String(children ?? "").replace(/\n$/, "")
}

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

/** react-markdown does createElement(components.pre). A new function here
 *  is a new component type, so every later token unmounted a finished
 *  chart and Recharts painted from an empty box again. */
const COMPONENTS: Components = {
  a: MarkdownLink,
  pre: MarkdownPre,
  code: MarkdownInlineCode,
}
