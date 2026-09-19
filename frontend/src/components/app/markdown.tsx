import {
  Children,
  createContext,
  isValidElement,
  lazy,
  memo,
  Suspense,
  useContext,
  useRef,
  type ComponentPropsWithoutRef,
  type ReactElement,
  type ReactNode,
} from "react"
import Markdown from "react-markdown"
import type { Components } from "react-markdown"
import remarkGfm from "remark-gfm"

import { ChartPending } from "@/components/app/chart-pending"
import { classifyHref } from "@/lib/external-links"
import { chartSpecsEqual, parseChartSpec, type ChartSpec } from "@/lib/chart-spec"
import { closeIncompleteMarkdown } from "@/lib/stream-markdown"

const TranscriptChart = lazy(async () => {
  const mod = await import("@/components/app/transcript-chart")
  return { default: mod.TranscriptChart }
})

/** remark plugins are module-level so a memoised markdown block is not
 *  invalidated by a new array on every parent render. */
const PLUGINS = [remarkGfm]

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
  const fence = chartFence(children)
  if (fence !== null) {
    const parsed = parseChartSpec(fence)
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
  return <pre {...props}>{children}</pre>
}

/** Only a fenced `chart` block is a spec. Inline `code` and other fences
 *  stay code — a JSON sample in a typescript fence must not become a plot. */
function chartFence(children: ReactNode): string | null {
  const codes: ReactElement<{ className?: string; children?: ReactNode }>[] = []
  Children.forEach(children, (child) => {
    if (!isValidElement(child)) return
    codes.push(child as ReactElement<{ className?: string; children?: ReactNode }>)
  })
  if (codes.length !== 1) return null
  const el = codes[0]
  const lang = /language-([^\s]+)/.exec(el.props.className ?? "")?.[1]
  if (lang !== "chart") return null
  return String(el.props.children ?? "").replace(/\n$/, "")
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
}
