import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { ArrowUp, Loader2, Paperclip, X, Zap } from "lucide-react"

import { cn } from "@/lib/cn"
import { t } from "@/lib/i18n"
import { isVisionFile } from "@/lib/put-chunks"
import type { ModelChoice } from "@/lib/rpc"

const noModels: ModelChoice[] = []
const noLevels: string[] = []

export type ComposerExtra = {
  files: File[]
  images: File[]
  providerId: string
  model: string
  reasoning: string
}

type Send = (text: string, extra?: ComposerExtra) => void | Promise<void>

/** One input for the whole app: starting a conversation and answering inside
 *  one are the same gesture, so they are the same control. */
export function Composer({
  label,
  placeholder,
  sendLabel,
  disabled,
  pending,
  hint,
  above,
  steerLabel,
  onSteer,
  onSubmit,
  models = noModels,
  reasoningLevels = noLevels,
  providerId = "",
  model = "",
  reasoning = "",
  catalogBusy = false,
  onTune,
}: {
  label: string
  placeholder?: string
  sendLabel: string
  disabled?: boolean
  pending?: boolean
  hint?: string
  above?: ReactNode
  steerLabel?: string
  onSteer?: Send
  onSubmit: Send
  models?: ModelChoice[]
  reasoningLevels?: string[]
  providerId?: string
  model?: string
  reasoning?: string
  catalogBusy?: boolean
  onTune?: (next: { providerId: string; model: string; reasoning: string }) => void
}) {
  const [text, setText] = useState("")
  const [attached, setAttached] = useState<File[]>([])
  const [pick, setPick] = useState(() => chosen(models, providerId, model))
  const [level, setLevel] = useState(reasoning)
  const box = useRef<HTMLTextAreaElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const images = attached.filter((file) => isVisionFile(file))
  const files = attached.filter((file) => !isVisionFile(file))
  const ready =
    (text.trim().length > 0 || attached.length > 0) && !disabled && !pending
  const showTools = models.length > 0 || reasoningLevels.length > 0 || catalogBusy

  // The conversation's selection arrives after catalog. A local edit is not
  // overwritten until those props themselves change.
  useEffect(() => {
    setPick(chosen(models, providerId, model))
  }, [models, providerId, model])
  useEffect(() => {
    setLevel(reasoning)
  }, [reasoning])

  // Grow with the text instead of scrolling a one-line slot, but stop before
  // the transcript loses its last answer.
  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    el.style.height = "auto"
    el.style.height = Math.min(el.scrollHeight, 132) + "px"
  }, [text])

  const extra = (): ComposerExtra | undefined => {
    if (attached.length === 0 && models.length === 0 && reasoningLevels.length === 0) return undefined
    return {
      files,
      images,
      providerId: pick.providerId,
      model: pick.model,
      reasoning: level,
    }
  }

  const fire = async (send: Send) => {
    const payload = text.trim()
    if ((!payload && attached.length === 0) || disabled || pending) return
    const shot = attached
    const more = extra()
    setText("")
    setAttached([])
    try {
      await (more ? send(payload, more) : send(payload))
    } catch {
      setText(payload)
      setAttached(shot)
    }
  }

  const addFiles = (list: File[]) => {
    if (list.length === 0) return
    setAttached((prev) => [...prev, ...list])
  }

  const tune = (next: { providerId: string; model: string; reasoning: string }) => {
    setPick({ providerId: next.providerId, model: next.model })
    setLevel(next.reasoning)
    onTune?.(next)
  }

  return (
    <div className="min-w-0 shrink-0 border-t border-border bg-background px-3 pb-3 pt-2">
      {above}
      {attached.length > 0 ? (
        <ul className="mb-2 flex flex-col gap-1">
          {attached.map((file, i) => (
            <li
              key={file.name + ":" + i}
              className="flex items-center gap-2 rounded-full bg-muted px-3 py-1 text-xs"
            >
              <span className="min-w-0 flex-1 truncate">{file.name || putFallback(file)}</span>
              <button
                type="button"
                aria-label={t("composer.remove", { name: file.name || putFallback(file) })}
                className="text-muted-foreground"
                onClick={() => setAttached((prev) => prev.filter((_, j) => j !== i))}
              >
                <X className="size-3.5" aria-hidden />
              </button>
            </li>
          ))}
        </ul>
      ) : null}
      <form
        className={cn(
          "flex min-w-0 items-end gap-1.5 rounded-3xl bg-muted p-1.5 transition-colors",
          "focus-within:ring-1 focus-within:ring-ring",
        )}
        aria-busy={pending || undefined}
        onSubmit={(e) => {
          e.preventDefault()
          void fire(onSubmit)
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
            onClick={() => void fire(onSteer)}
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
          disabled={disabled || pending}
          onChange={(e) => setText(e.target.value)}
          onPaste={(e) => {
            const pasted = Array.from(e.clipboardData?.files ?? []).filter((file) => isVisionFile(file))
            if (pasted.length === 0) return
            e.preventDefault()
            addFiles(pasted)
          }}
          onKeyDown={(e) => {
            if (e.key !== "Enter" || e.shiftKey || e.nativeEvent.isComposing) return
            e.preventDefault()
            void fire(onSubmit)
          }}
          className={cn(
            "min-h-9 min-w-0 flex-1 resize-none bg-transparent px-2 py-2 text-sm leading-5",
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
          {pending ? (
            <Loader2 className="size-4 motion-safe:animate-spin motion-reduce:animate-none" aria-hidden />
          ) : (
            <ArrowUp className="size-4" aria-hidden />
          )}
        </button>
      </form>
      <div className="mt-1.5 flex min-w-0 items-center gap-1.5">
        <input
          ref={fileRef}
          type="file"
          multiple
          className="hidden"
          data-testid="file-input"
          onChange={(e) => {
            addFiles(Array.from(e.target.files ?? []))
            e.target.value = ""
          }}
        />
        <button
          type="button"
          aria-label={t("composer.attach")}
          disabled={disabled || pending}
          className="flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground active:bg-accent disabled:opacity-40"
          onClick={() => fileRef.current?.click()}
        >
          <Paperclip className="size-4" aria-hidden />
        </button>
        {catalogBusy && models.length === 0 ? (
          <div aria-busy="true" className="h-8 min-w-24 flex-1 animate-pulse rounded-full bg-muted motion-reduce:animate-none">
            <span className="sr-only">{t("composer.loading")}</span>
          </div>
        ) : null}
        {models.length > 0 ? (
          <select
            aria-label={t("composer.model")}
            className="h-8 w-0 min-w-0 flex-1 truncate rounded-full bg-muted px-2 text-xs text-foreground"
            value={choiceValue(pick.providerId, pick.model)}
            disabled={disabled || pending}
            onChange={(e) => {
              const [providerIdNext, modelNext] = e.target.value.split("\t")
              tune({ providerId: providerIdNext, model: modelNext, reasoning: level })
            }}
          >
            {groups(models).map((group) => (
              <optgroup key={group.label} label={group.label}>
                {group.rows.map((row) => (
                  <option key={choiceValue(row.provider_id, row.model)} value={choiceValue(row.provider_id, row.model)}>
                    {row.model}
                  </option>
                ))}
              </optgroup>
            ))}
          </select>
        ) : null}
        {reasoningLevels.length > 0 ? (
          <select
            aria-label={t("composer.thinking")}
            className="h-8 min-w-0 max-w-[46%] shrink truncate rounded-full bg-muted px-2 text-xs text-foreground"
            value={level}
            disabled={disabled || pending}
            onChange={(e) => tune({ providerId: pick.providerId, model: pick.model, reasoning: e.target.value })}
          >
            <option value="">{t("composer.thinkingDefault")}</option>
            {reasoningLevels.map((item) => (
              <option key={item} value={item}>
                {thinkingLabel(item)}
              </option>
            ))}
          </select>
        ) : null}
        {showTools ? null : <span className="flex-1" />}
      </div>
      {hint ? <p className="px-3 pt-1.5 text-[11px] text-muted-foreground">{hint}</p> : null}
    </div>
  )
}

function chosen(models: ModelChoice[], providerId: string, model: string) {
  const ready = models.filter((row) => row.model)
  const hit =
    ready.find((row) => row.provider_id === providerId && row.model === model) ??
    ready.find((row) => row.provider_id === providerId && row.default) ??
    ready.find((row) => row.default) ??
    ready[0]
  return { providerId: hit?.provider_id ?? providerId, model: hit?.model ?? model }
}

function choiceValue(providerId: string, model: string) {
  return providerId + "\t" + model
}

function groups(models: ModelChoice[]) {
  const out: { label: string; rows: ModelChoice[] }[] = []
  for (const row of models) {
    if (!row.model) continue
    const label = row.provider_label || row.provider_id
    const group = out.find((item) => item.label === label)
    if (group) group.rows.push(row)
    else out.push({ label, rows: [row] })
  }
  return out
}

function thinkingLabel(level: string) {
  if (level === "low") return t("reason.low")
  if (level === "medium") return t("reason.medium")
  if (level === "high") return t("reason.high")
  return level
}

function putFallback(file: File) {
  return file.type.startsWith("image/") ? "image" : "file"
}
