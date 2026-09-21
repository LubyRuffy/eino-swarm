import {
  Children,
  isValidElement,
  useMemo,
  useState,
  type ComponentPropsWithoutRef,
  type ReactElement,
  type ReactNode,
} from "react"
import katex from "katex"
import { Check, Copy } from "lucide-react"
import Markdown from "react-markdown"
import type { Components } from "react-markdown"
import remarkGfm from "remark-gfm"
import remarkMath from "remark-math"
import "katex/dist/katex.min.css"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/cn"
import { copyText } from "@/lib/copy-text"
import { t } from "@/lib/i18n"

const PLUGINS = [remarkGfm, remarkMath]
const MATH_LANGS = new Set(["math", "latex", "tex", "katex"])

export function PhoneMarkdown({ text, className }: { text: string; className?: string }) {
  return (
    <div className={cn("md-body break-words text-sm", className)}>
      <Markdown remarkPlugins={PLUGINS} components={COMPONENTS} urlTransform={safeUrl}>
        {text}
      </Markdown>
    </div>
  )
}

function PhoneLink({
  href,
  children,
  node: _node,
  ...props
}: ComponentPropsWithoutRef<"a"> & { node?: unknown }) {
  const origin = typeof window === "undefined" ? "http://127.0.0.1" : window.location.origin
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

function PhoneTable({
  node: _node,
  ...props
}: ComponentPropsWithoutRef<"table"> & { node?: unknown }) {
  return (
    <div className="overflow-x-auto">
      <table {...props} />
    </div>
  )
}

/** Hash fragments stay. javascript/file/data never navigate. http(s)
 *  leaves the webview. Same-origin paths are blocked: the phone has no
 *  router, and a local path in a markdown link would replace the transcript. */
function classifyHref(href: string | null | undefined, origin: string): "stay" | "leave" | "block" {
  if (href == null) return "stay"
  const raw = href.trim()
  if (raw.startsWith("#")) return "stay"
  if (raw === "") return "block"
  let url: URL
  try {
    url = new URL(raw, origin)
  } catch {
    return "block"
  }
  const proto = url.protocol.toLowerCase()
  if (proto === "javascript:" || proto === "data:" || proto === "vbscript:" || proto === "file:") {
    return "block"
  }
  // The phone is one webview. A same-origin path would replace the transcript.
  if (url.origin === origin) return "block"
  if (proto === "http:" || proto === "https:" || proto === "mailto:") return "leave"
  return "block"
}

function safeUrl(url: string): string {
  return classifyHref(url, "http://127.0.0.1") === "block" ? "" : url
}

function PhonePre({
  children,
  node: _node,
  ...props
}: ComponentPropsWithoutRef<"pre"> & { node?: unknown }) {
  const fence = readFence(children)
  if (fence && MATH_LANGS.has(fence.lang.toLowerCase())) {
    return <PhoneMath tex={fence.text} display />
  }
  if (fence) {
    return (
      <div className="md-code" data-testid="markdown-code">
        <div className="flex items-center gap-2 px-3 pt-1.5">
          <span className="min-w-0 flex-1 truncate font-mono text-[0.6875rem] text-muted-foreground">
            {fence.lang}
          </span>
          <PhoneCopy text={fence.text} kind="code" />
        </div>
        <pre {...props}>
          <code className={fence.className}>{fence.text}</code>
        </pre>
      </div>
    )
  }
  return <pre {...props}>{children}</pre>
}

function PhoneInlineCode({
  className,
  children,
  node: _node,
  ...props
}: ComponentPropsWithoutRef<"code"> & { node?: unknown }) {
  if (className?.includes("math-display")) {
    return <PhoneMath tex={codeText(children)} display />
  }
  if (
    className?.includes("math-inline") ||
    /(^|\s)language-math(\s|$)/.test(className ?? "")
  ) {
    return <PhoneMath tex={codeText(children)} />
  }
  return (
    <code className={className} {...props}>
      {children}
    </code>
  )
}

function PhoneMath({ tex, display = false }: { tex: string; display?: boolean }) {
  const html = useMemo(() => renderKatex(tex, display), [tex, display])
  if (display) {
    return (
      <div className="md-math" data-testid="markdown-math">
        <div className="mb-1 flex justify-end">
          <PhoneCopy text={tex} kind="formula" />
        </div>
        {html ? (
          <div className="overflow-x-auto" dangerouslySetInnerHTML={{ __html: html }} />
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

function PhoneCopy({ text, kind }: { text: string; kind: "code" | "formula" }) {
  const [copied, setCopied] = useState(false)
  const name = kind === "formula" ? t("markdown.copyFormula") : t("markdown.copyCode")
  return (
    <Button
      type="button"
      variant="ghost"
      className="size-7 px-0"
      aria-label={name}
      title={copied ? t("markdown.copied") : name}
      onClick={() => {
        void copyText(text).then((ok) => {
          if (!ok) return
          setCopied(true)
          setTimeout(() => setCopied(false), 1200)
        })
      }}
    >
      {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
    </Button>
  )
}

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

function readFence(children: ReactNode): { lang: string; text: string; className?: string } | null {
  const codes: ReactElement<{ className?: string; children?: ReactNode }>[] = []
  Children.forEach(children, (child) => {
    if (!isValidElement(child)) return
    codes.push(child as ReactElement<{ className?: string; children?: ReactNode }>)
  })
  if (codes.length !== 1) return null
  const el = codes[0]
  const className = el.props.className
  const lang = /language-([^\s]+)/.exec(className ?? "")?.[1] ?? ""
  const text = codeText(el.props.children)
  return { lang, text, className }
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

const COMPONENTS: Components = {
  a: PhoneLink,
  pre: PhonePre,
  code: PhoneInlineCode,
  table: PhoneTable,
}
