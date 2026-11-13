import { useId, useLayoutEffect, useRef } from "react"
import { Button } from "@/components/ui/button"
import { HighlightedLines } from "@/lib/highlighted-lines"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"
import { settingsHintClass } from "./settings-field"

// The colored layer and the field must wrap on the same boundaries. A
// different padding, font or scrollbar gutter puts the caret on the wrong glyph.
const jsonPaint =
  "absolute inset-0 m-0 box-border overflow-auto border-0 bg-transparent p-6 font-mono text-xs leading-5 whitespace-pre-wrap wrap-break-word [font-variant-ligatures:none] [scrollbar-gutter:stable] [tab-size:2] scrollbar-thin"

/** The settings page's right column. The connection list is locked while this
 *  document is open, so the column is the editor instead of a short box under
 *  the list. */
export function MCPJSONEditor({ text, saving, error, onChange, onApply, onCancel }: {
  text: string
  saving: boolean
  error: string
  onChange: (text: string) => void
  onApply: () => void
  onCancel: () => void
}) {
  const t = useT()
  const id = useId()
  const hintId = useId()
  const areaRef = useRef<HTMLTextAreaElement>(null)
  const preRef = useRef<HTMLPreElement>(null)
  const sync = () => {
    const area = areaRef.current
    const pre = preRef.current
    if (!area || !pre) return
    pre.scrollTop = area.scrollTop
    pre.scrollLeft = area.scrollLeft
  }
  useLayoutEffect(() => {
    sync()
  }, [text])
  return (
    <fieldset disabled={saving} className="flex min-h-0 min-w-0 flex-1 flex-col border-0 p-0">
      <div className="flex shrink-0 items-start justify-between gap-4 border-b px-6 py-4">
        <div className="min-w-0">
          <h2 className="text-lg font-semibold tracking-tight">{t("mcp.jsonTitle")}</h2>
          <label htmlFor={id} className="sr-only">{t("mcp.jsonLabel")}</label>
          <p id={hintId} className={settingsHintClass}>{t("mcp.jsonHint")}</p>
        </div>
        <div className="flex shrink-0 gap-2">
          <Button type="button" variant="ghost" onClick={onCancel}>{t("mcp.cancel")}</Button>
          <Button type="button" onClick={onApply}>{t(saving ? "mcp.jsonApplying" : "mcp.jsonApply")}</Button>
        </div>
      </div>
      {error ? <p role="alert" className="shrink-0 px-6 py-3 text-(length:--chrome-font-size) text-destructive">{error}</p> : null}
      <div className={cn("relative min-h-0 flex-1", saving && "opacity-50")}>
        <pre ref={preRef} aria-hidden className={cn(
          jsonPaint,
          "pointer-events-none text-foreground select-none [scrollbar-color:transparent_transparent] [&::-webkit-scrollbar-thumb]:bg-transparent",
        )}>
          <HighlightedLines text={text} lang="json" />
        </pre>
        <textarea
          ref={areaRef}
          id={id}
          value={text}
          aria-describedby={hintId}
          autoComplete="off"
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
          className={cn(jsonPaint, "resize-none text-transparent outline-none selection:bg-primary/25")}
          style={{ caretColor: "hsl(var(--foreground))", WebkitTextFillColor: "transparent" }}
          onChange={(event) => onChange(event.target.value)}
          onScroll={sync}
        />
      </div>
    </fieldset>
  )
}
