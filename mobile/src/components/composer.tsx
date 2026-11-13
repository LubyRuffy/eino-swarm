import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { ArrowUp, Loader2, Paperclip, Square, X, Zap } from "lucide-react"

import { cn } from "@/lib/cn"
import { t } from "@/lib/i18n"
import { formatQuotedMessage } from "@/lib/quote"
import type { ComposerPrediction } from "../../../frontend/src/lib/composer-prediction"
import { parseSlashSubmit } from "../../../frontend/src/lib/slash-core"
import { isVisionFile } from "@/lib/put-chunks"
import type { ModelChoice } from "@/lib/rpc"
import { ModelPicker } from "./model-picker"

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
  prediction,
  sendLabel,
  disabled,
  pending,
  hint,
  above,
  quotes = [],
  onQuotesChange,
  steerLabel,
  onSteer,
  stopLabel,
  onStop,
  onSubmit,
  models = noModels,
  reasoningLevels = noLevels,
  providerId = "",
  model = "",
  reasoning = "",
  catalogBusy = false,
  onRefreshModels,
  refreshBusy,
  refreshError,
  onTune,
  hideModel = false,
}: {
  label: string
  prediction?: ComposerPrediction
  placeholder?: string
  sendLabel: string
  disabled?: boolean
  pending?: boolean
  hint?: string
  above?: ReactNode
  quotes?: string[]
  onQuotesChange?: (quotes: string[]) => void
  steerLabel?: string
  onSteer?: Send
  stopLabel?: string
  onStop?: () => void | Promise<void>
  onSubmit: Send
  models?: ModelChoice[]
  reasoningLevels?: string[]
  providerId?: string
  model?: string
  reasoning?: string
  catalogBusy?: boolean
  onRefreshModels?: () => void | Promise<void>
  refreshBusy?: boolean
  refreshError?: string
  onTune?: (next: { providerId: string; model: string; reasoning: string }) => void
  hideModel?: boolean
}) {
  const [dismissedPrediction, setDismissedPrediction] = useState<string>()
  const [text, setText] = useState("")
  const [commandRejected, setCommandRejected] = useState(false)
  const [attached, setAttached] = useState<File[]>([])
  const [pick, setPick] = useState(() => chosen(models, providerId, model))
  const explicitPick = useRef(false)
  const [level, setLevel] = useState(reasoning)
  const [modelPickerOpen, setModelPickerOpen] = useState(false)
  const modelTextId = useId()
  const box = useRef<HTMLTextAreaElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const images = attached.filter((file) => isVisionFile(file))
  const files = attached.filter((file) => !isVisionFile(file))
  const hasDraft = text.trim().length > 0 || attached.length > 0 || quotes.some((quote) => quote.trim().length > 0)
  const suggestion = !hasDraft && !disabled && !pending && prediction?.turnId !== dismissedPrediction ? prediction : undefined
  const insertSuggestion = () => { if (suggestion) { setText(suggestion.text); box.current?.focus() } }
  const command = quotes.some((quote) => quote.trim()) ? parseSlashSubmit(text) : null
  const quotedCommand = command?.id === "goal" || command?.id === "plan"
  const ready = hasDraft && !disabled && !pending
  const showStop = Boolean(onStop && stopLabel && (pending || !hasDraft))
  const showModel = !hideModel && (models.length > 0 || catalogBusy || Boolean(onRefreshModels))
  const showTools = showModel || reasoningLevels.length > 0
  const lockedModel = models.length === 1 && Boolean(models[0]?.locked)
  const modelText = choiceLabel(models, pick) || t("composer.model")

  // The conversation's selection arrives after catalog. A local edit is not
  // overwritten until those props themselves change.
  useEffect(() => {
    explicitPick.current = false
    setPick(chosen(models, providerId, model))
  }, [providerId, model])
  useEffect(() => {
    if (!explicitPick.current) setPick((current) => chosen(models, providerId, model, current))
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
    // The quote envelope hides the command from the PC. Refuse before clearing
    // or uploading any part of this draft, as the desktop composer does.
    if (quotedCommand) {
      setCommandRejected(true)
      return
    }
    setCommandRejected(false)
    const payload = formatQuotedMessage(quotes, text)
    if ((!payload && attached.length === 0) || disabled || pending) return
    const shot = attached
    const selected = quotes
    const more = extra()
    setText("")
    setAttached([])
    onQuotesChange?.([])
    try {
      await (more ? send(payload, more) : send(payload))
    } catch {
      setText(text)
      setAttached(shot)
      onQuotesChange?.(selected)
    }
  }

  const addFiles = (list: File[]) => {
    if (list.length === 0) return
    setAttached((prev) => [...prev, ...list])
  }

  const tune = (next: { providerId: string; model: string; reasoning: string }) => {
    explicitPick.current = true
    setPick({ providerId: next.providerId, model: next.model })
    setLevel(next.reasoning)
    onTune?.(next)
  }

  return (
    <div className="min-w-0 shrink-0 border-t border-border bg-background px-3 pb-2 pt-1.5">
      {above}
      {suggestion ? <div className="mb-1 flex min-w-0 items-start gap-1" data-testid="composer-prediction">
        <button type="button" className="line-clamp-2 min-w-0 flex-1 wrap-break-word rounded-lg px-2 py-1 text-left text-sm text-muted-foreground active:bg-accent" aria-label={`${t("prediction.insert")}: ${suggestion.text}`} onClick={insertSuggestion}>{suggestion.text}</button>
        <button type="button" className="flex size-9 shrink-0 items-center justify-center text-muted-foreground" aria-label={t("prediction.dismiss")} onClick={() => setDismissedPrediction(suggestion.turnId)}><X className="size-4" /></button>
      </div> : null}
      {quotes.length > 0 ? (
        <ul className="mb-2 flex flex-col gap-1" data-testid="quote-drafts">
          {quotes.map((quote, i) => (
            <li key={i} className="flex min-w-0 items-center gap-2 rounded-lg border border-border bg-muted px-3 py-1 text-xs">
              <textarea
                aria-label={t("quote.edit", { n: i + 1 })}
                className="min-h-12 max-h-24 min-w-0 flex-1 resize-y overflow-auto bg-transparent text-base focus-visible:outline-hidden"
                rows={2}
                value={quote}
                data-testid="quote-draft"
                onChange={(event) => onQuotesChange?.(quotes.map((current, j) => j === i ? event.target.value : current))}
              />
              <button
                type="button"
                aria-label={t("quote.remove", { n: i + 1 })}
                className="text-muted-foreground"
                onClick={() => onQuotesChange?.(quotes.filter((_, j) => j !== i))}
              >
                <X className="size-3.5" aria-hidden />
              </button>
            </li>
          ))}
        </ul>
      ) : null}
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
      {commandRejected && quotedCommand ? (
        <p role="alert" className="mb-2 text-sm text-destructive">{t("composer.commandTextOnly")}</p>
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
            if (e.key === "Tab" && !e.shiftKey && !e.nativeEvent.isComposing && suggestion) { e.preventDefault(); insertSuggestion(); return }
            if (e.key === "Escape" && suggestion) { e.preventDefault(); setDismissedPrediction(suggestion.turnId); return }
            if (e.key !== "Enter" || e.shiftKey || e.nativeEvent.isComposing) return
            e.preventDefault()
            void fire(onSubmit)
          }}
          className={cn(
            "min-h-9 min-w-0 flex-1 resize-none bg-transparent px-2 py-2 text-base leading-5",
            "placeholder:text-muted-foreground focus-visible:outline-hidden disabled:opacity-60",
          )}
        />
        <button
          type={showStop ? "button" : "submit"}
          aria-label={showStop ? stopLabel : sendLabel}
          title={showStop ? stopLabel : sendLabel}
          disabled={!showStop && !ready}
          onClick={showStop ? () => void onStop?.() : undefined}
          className={cn(
            "flex size-9 shrink-0 items-center justify-center rounded-full",
            "bg-primary text-primary-foreground transition-opacity",
            "disabled:opacity-30",
          )}
        >
          {showStop ? (
            <Square className="size-4" fill="currentColor" aria-hidden />
          ) : pending ? (
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
        {showModel && catalogBusy && models.length === 0 ? (
          <div aria-busy="true" className="h-8 min-w-24 flex-1 animate-pulse rounded-full bg-muted motion-reduce:animate-none">
            <span className="sr-only">{t("composer.loading")}</span>
          </div>
        ) : null}
        {showModel && (models.length > 0 || onRefreshModels) ? (
          <button
            type="button"
            aria-label={t("composer.model")}
            aria-describedby={modelTextId}
            title={!lockedModel && modelUnavailable(models, pick) ? t("composer.modelUnavailable") : undefined}
            data-unavailable={!lockedModel && modelUnavailable(models, pick) ? "true" : undefined}
            className={`h-8 w-0 min-w-0 flex-1 truncate rounded-full bg-muted px-2 text-left text-xs ${!lockedModel && modelUnavailable(models, pick) ? "text-muted-foreground" : "text-foreground"}`}
            disabled={disabled || pending}
            onClick={() => { if (!lockedModel) setModelPickerOpen(true) }}
          >
            <span id={modelTextId}>{modelText}</span>
          </button>
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
      {showModel && modelPickerOpen && !lockedModel ? (
        <ModelPicker
          models={models}
          providerId={pick.providerId}
          model={pick.model}
          onSelect={(nextProviderId, nextModel) => {
            tune({ providerId: nextProviderId, model: nextModel, reasoning: level })
            setModelPickerOpen(false)
          }}
          onRefresh={onRefreshModels}
          refreshBusy={refreshBusy}
          refreshError={refreshError}
          onClose={() => setModelPickerOpen(false)}
        />
      ) : null}
      {hint ? <p className="px-3 pt-1.5 text-[11px] text-muted-foreground">{hint}</p> : null}
    </div>
  )
}

function chosen(
  models: ModelChoice[],
  providerId: string,
  model: string,
  current?: { providerId: string; model: string },
) {
  const rows = models.filter((row) => row.model)
  if (rows.length === 1 && rows[0].locked) return { providerId: rows[0].provider_id, model: rows[0].model }
  if (providerId && model) return { providerId, model }
  // A known provider that left the catalog was switched off. Keep the
  // conversation on it instead of silently sending the next endpoint.
  const stillListed = !providerId || rows.some((row) => row.provider_id === providerId)
  if (!stillListed) return { providerId, model }
  const settings =
    rows.find((row) => row.provider_id === providerId && row.default && row.default_follows_setting) ??
    rows.find((row) => row.default && row.default_follows_setting)
  if (settings && (!providerId || settings.provider_id === providerId)) {
    return { providerId: settings.provider_id, model: settings.model }
  }
  // A catalog-order fallback already on the button stays there when the
  // list is reordered. A settings default above is allowed to move it.
  if (current?.model && (!providerId || !current.providerId || current.providerId === providerId)) {
    return { providerId: current.providerId || providerId, model: current.model }
  }
  const hit =
    rows.find((row) => row.provider_id === providerId && row.default) ??
    rows.find((row) => row.default) ??
    rows[0]
  return { providerId: hit?.provider_id ?? providerId, model: hit?.model ?? model }
}

function choiceLabel(models: ModelChoice[], pick: { providerId: string; model: string }) {
  const row = models.find((item) => item.provider_id === pick.providerId && item.model === pick.model)
    ?? (models.length === 1 ? models[0] : undefined)
  return row?.label || pick.model
}

function modelUnavailable(models: ModelChoice[], pick: { providerId: string; model: string }) {
  if (!pick.model) return false
  const row = models.find((item) => item.provider_id === pick.providerId && item.model === pick.model)
  return !row || row.ready === false
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
