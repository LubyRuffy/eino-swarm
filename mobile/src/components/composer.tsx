import { useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { ArrowUp, Zap } from "lucide-react"

import { cn } from "@/lib/cn"

/** One input for the whole app: starting a conversation and answering inside
 *  one are the same gesture, so they are the same control. */
export function Composer({
  label,
  placeholder,
  sendLabel,
  disabled,
  hint,
  above,
  steerLabel,
  onSteer,
  onSubmit,
}: {
  label: string
  placeholder?: string
  sendLabel: string
  disabled?: boolean
  hint?: string
  above?: ReactNode
  steerLabel?: string
  onSteer?: (text: string) => void
  onSubmit: (text: string) => void
}) {
  const [text, setText] = useState("")
  const box = useRef<HTMLTextAreaElement>(null)
  const ready = text.trim().length > 0 && !disabled

  // Grow with the text instead of scrolling a one-line slot, but stop before
  // the transcript loses its last answer.
  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    el.style.height = "auto"
    el.style.height = Math.min(el.scrollHeight, 132) + "px"
  }, [text])

  const fire = (send: (v: string) => void) => {
    const v = text.trim()
    if (!v || disabled) return
    send(v)
    setText("")
  }

  return (
    <div className="shrink-0 border-t border-border bg-background px-3 pb-3 pt-2">
      {above}
      <form
        className={cn(
          "flex items-end gap-1.5 rounded-3xl bg-muted p-1.5 transition-colors",
          "focus-within:ring-1 focus-within:ring-ring",
        )}
        onSubmit={(e) => {
          e.preventDefault()
          fire(onSubmit)
        }}
      >
        {onSteer && steerLabel ? (
          <button
            type="button"
            aria-label={steerLabel}
            title={steerLabel}
            disabled={!ready}
            className={cn(
              "flex size-9 shrink-0 items-center justify-center rounded-full text-muted-foreground",
              "transition-colors active:bg-accent disabled:opacity-40",
            )}
            onClick={() => fire(onSteer)}
          >
            <Zap className="size-4" aria-hidden />
          </button>
        ) : null}
        <textarea
          ref={box}
          aria-label={label}
          placeholder={placeholder ?? label}
          value={text}
          rows={1}
          disabled={disabled}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key !== "Enter" || e.shiftKey || e.nativeEvent.isComposing) return
            e.preventDefault()
            fire(onSubmit)
          }}
          className={cn(
            "min-h-9 flex-1 resize-none bg-transparent px-2 py-2 text-sm leading-5",
            "placeholder:text-muted-foreground focus-visible:outline-none disabled:opacity-60",
          )}
        />
        <button
          type="submit"
          aria-label={sendLabel}
          title={sendLabel}
          disabled={!ready}
          className={cn(
            "flex size-9 shrink-0 items-center justify-center rounded-full",
            "bg-primary text-primary-foreground transition-opacity",
            "disabled:opacity-30",
          )}
        >
          <ArrowUp className="size-4" aria-hidden />
        </button>
      </form>
      {hint ? (
        <p className="px-3 pt-1.5 text-[11px] text-muted-foreground">{hint}</p>
      ) : null}
    </div>
  )
}
