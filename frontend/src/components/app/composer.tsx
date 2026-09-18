import { ArrowUp, Brain, Paperclip, ShieldCheck, Square } from "lucide-react"
import { useEffect, useLayoutEffect, useRef, useState, type DragEvent } from "react"

import { ContextMeter } from "@/components/app/context-meter"
import { ComposerAttachments } from "@/components/app/composer-attachments"
import { ComposerDropOverlay } from "@/components/app/composer-drop-overlay"
import { ComposerImages } from "@/components/app/composer-images"
import { ComposerQuotes } from "@/components/app/composer-quotes"
import { ModelPicker } from "@/components/app/model-picker"
import { GoalBanner } from "@/components/app/goal-banner"
import { PlanBanner } from "@/components/app/plan-banner"
import { ScheduleBanner } from "@/components/app/schedule-banner"
import { QueueTray } from "@/components/app/queue-tray"
import { SlashMenu } from "@/components/app/slash-menu"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { clearComposerPad, resizeComposerArea, syncComposerPad } from "@/lib/composer-chrome"
import {
  filesFromDataTransfer,
  isFileDrag,
  splitDroppedFiles,
} from "@/lib/composer-drop"
import { afterImeSettles, enterSendsMessage } from "@/lib/ime"
import {
  commandNeedsArgument,
  compactHint,
  filterSlashCommands,
  localizedSlashCommands,
  nextSlashIndex,
  parseSlashSubmit,
  slashDraft,
  stripSlashToken,
  normalizeSlashPrefix,
  withSlashHints,
  type SlashCommand,
} from "@/lib/slash"
import {
  addPasteImages,
  dropPasteImage,
  filesFromClipboard,
  revokePasteImages,
  toSendImages,
  type PasteImage,
  type SendImage,
} from "@/lib/paste-image"
import { lastFailedTurnError } from "@/lib/transcript"
import { formatQuotedMessage, type Quote } from "@/lib/quote"
import type { Attachment, Followup, ModelInfo, UsageSnapshot } from "@/lib/types"
import { windowForSelection } from "@/lib/usage"
import { useT, type Translate } from "@/lib/use-t"
import { useApp } from "@/store/app"

/** The composer. Enter while a turn is running queues a follow-up for after
 *  it finishes. ⌘Enter (or Steer on a queued row) injects into this turn. */
export function Composer({
  running,
  models,
  provider,
  model,
  onModelChange,
  reasoning,
  reasoningLevels,
  onReasoningChange,
  onSend,
  onStop,
  onUpload,
  onRefreshModels,
  onEditProviders,
  disabled,
  prefill,
  prefillToken,
  focusSignal,
  quotes,
  onQuotesChange,
  followups,
  onSteerFollowup,
  onDeleteFollowup,
  onRequeueFollowup,
  onClearFollowups,
  usage,
  goal,
  goalComplete,
  goalBlocked,
  goalBlockReason,
  goalCapped,
  goalIdle,
  goalStartedAt,
  contextChars,
  contextBudget,
  onSetGoal,
  onClearGoal,
  onEditGoal,
  onResumeGoal,
  onCompact,
  planMode,
  planMarkdown,
  awaitingAnswer,
  onSetPlan,
  onSavePlan,
  onImplementPlan,
  onLeavePlan,
}: {
  running: boolean
  models: ModelInfo[]
  provider?: string
  model?: string
  onModelChange: (providerId: string, model: string) => void
  /** The conversation's thinking level: "" for the model's default. */
  reasoning?: string
  /** Explicit levels the server offers (low/medium/high), in order. */
  reasoningLevels: string[]
  onReasoningChange: (level: string) => void
  onSend: (text: string, images?: SendImage[], opts?: { steer?: boolean; files?: string[] }) => void
  onStop: () => void
  onUpload: (files: File[]) => Promise<Attachment[]>
  onRefreshModels?: () => Promise<void>
  onEditProviders?: () => void
  disabled?: boolean
  /** Bumped by the empty state (or a shortcut) to drop text into the box
   *  without the composer being a controlled field of the whole app. */
  prefill?: string
  prefillToken?: number
  focusSignal: number
  /** Snippets pulled out of the transcript. Shown as annotations, then
   *  prefixed onto the send so the model sees them. */
  quotes?: Quote[]
  onQuotesChange?: (quotes: Quote[]) => void
  followups?: Followup[]
  onSteerFollowup?: (id: string) => void
  onDeleteFollowup?: (id: string) => void
  onRequeueFollowup?: (id: string, text: string) => void
  onClearFollowups?: () => void
  usage?: UsageSnapshot | null
  goal?: string
  goalComplete?: boolean
  goalBlocked?: boolean
  goalBlockReason?: string
  goalCapped?: boolean
  goalIdle?: boolean
  goalStartedAt?: string
  contextChars?: number
  contextBudget?: number
  onSetGoal?: (text: string) => void
  onClearGoal?: () => void
  onEditGoal?: (text: string) => void
  onResumeGoal?: () => void
  onCompact?: () => void
  planMode?: boolean
  planMarkdown?: string
  awaitingAnswer?: boolean
  onSetPlan?: (text: string) => void
  onSavePlan?: (text: string) => void
  onImplementPlan?: () => void
  onLeavePlan?: () => void
}) {
  const t = useT()
  const lastTurnError = useApp((s) => lastFailedTurnError(s.transcript.turns))
  const [text, setText] = useState("")
  const [pending, setPending] = useState<File[]>([])
  const [pasted, setPasted] = useState<PasteImage[]>([])
  const [uploading, setUploading] = useState(false)
  const [armed, setArmed] = useState(false)
  const [adding, setAdding] = useState(false)
  const [goalDraft, setGoalDraft] = useState(false)
  const [planDraft, setPlanDraft] = useState(false)
  const [slashIndex, setSlashIndex] = useState(0)
  const areaRef = useRef<HTMLTextAreaElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const dockRef = useRef<HTMLDivElement>(null)
  // WebKit fires compositionend, then the Enter that confirmed leftover
  // Latin, with isComposing already false. Stay "composing" until that
  // keydown has run, or leftover English becomes a send.
  const composingRef = useRef(false)
  const cancelImeSettle = useRef<(() => void) | null>(null)
  const pastedRef = useRef<PasteImage[]>([])
  pastedRef.current = pasted
  // CJK IMEs write the committed string back after we clear the box.
  // Swallow that echo so a leftover Enter cannot queue the live turn.
  const echoRef = useRef("")

  useEffect(() => () => cancelImeSettle.current?.(), [])
  useEffect(() => () => revokePasteImages(pastedRef.current), [])

  // The box sits on the transcript. Without this pad the last line hides
  // under it, which is the whole reason the fade exists.
  useLayoutEffect(() => {
    const el = dockRef.current
    if (!el) return
    const apply = () => syncComposerPad(el, el.offsetHeight)
    apply()
    if (typeof ResizeObserver === "undefined") {
      return () => clearComposerPad(el)
    }
    const ro = new ResizeObserver(apply)
    ro.observe(el)
    return () => {
      ro.disconnect()
      clearComposerPad(el)
    }
  }, [])

  useEffect(() => {
    if (!prefillToken) return
    setText(prefill ?? "")
  }, [prefillToken, prefill])

  // Grow with the text, but stop before the composer eats the conversation.
  // IME preedit fires onChange; measuring with height:auto there jumps the
  // candidate window and feels like a stuck key.
  useEffect(() => {
    resizeComposerArea(areaRef.current, { composing: composingRef.current })
  }, [text])

  useEffect(() => {
    const el = areaRef.current
    if (!el) return
    el.focus()
    el.setSelectionRange(el.value.length, el.value.length)
  }, [focusSignal])

  const quoted = quotes ?? []
  const meterWindow = windowForSelection(models, provider, model)
  const draft = slashDraft(text)
  // Hints (the compact %) live in ComposerSlashMenu so a usage pulse cannot
  // re-render this textarea mid-IME. Keyboard handling only needs the ids.
  const slashItems = draft
    ? filterSlashCommands(draft.query, localizedSlashCommands(t.locale))
    : []
  const slashOpen = slashItems.length > 0

  useEffect(() => {
    setSlashIndex(0)
  }, [draft?.query])

  const pickCommand = (cmd: SlashCommand) => {
    const rest = draft ? stripSlashToken(text, draft) : ""
    if (cmd.id === "goal" || cmd.id === "plan" || commandNeedsArgument(cmd.id)) {
      const objective = rest.trim()
      if (objective) {
        setGoalDraft(false)
        setPlanDraft(false)
        setText("")
        if (cmd.id === "plan") onSetPlan?.(objective)
        else onSetGoal?.(objective)
        return
      }
      setGoalDraft(cmd.id === "goal")
      setPlanDraft(cmd.id === "plan")
      setText("")
      return
    }
    if (cmd.id === "compact") {
      setText(rest.trimEnd())
      setGoalDraft(false)
      setPlanDraft(false)
      onCompact?.()
    }
  }

  const submit = async (opts?: { steer?: boolean }) => {
    const slash = parseSlashSubmit(text)
    // A glued `/goal…` argument must not be eaten as a menu pick. Codex
    // dispatches inline args the same way: the rest is the objective, not
    // another keystroke of command name.
    if (slashOpen && !slash?.arg) {
      const cmd = slashItems[Math.min(slashIndex, slashItems.length - 1)]
      if (cmd) pickCommand(cmd)
      return
    }
    if (goalDraft) {
      const next = text.trim()
      if (!next) return
      setGoalDraft(false)
      setText("")
      onSetGoal?.(next)
      return
    }
    if (planDraft) {
      const next = text.trim()
      if (!next) return
      setPlanDraft(false)
      setText("")
      onSetPlan?.(next)
      return
    }
    if (slash) {
      if (slash.id === "compact") {
        setText("")
        onCompact?.()
        return
      }
      if (slash.id === "plan") {
        if (!slash.arg) {
          setPlanDraft(true)
          setGoalDraft(false)
          setText("")
          return
        }
        setText("")
        setPlanDraft(false)
        onSetPlan?.(slash.arg)
        return
      }
      if (!slash.arg) {
        setGoalDraft(true)
        setText("")
        return
      }
      setText("")
      setGoalDraft(false)
      onSetGoal?.(slash.arg)
      return
    }
    const payload = formatQuotedMessage(
      quoted.map((q) => q.text),
      text,
    )
    if (!payload && pending.length === 0 && pasted.length === 0) return
    if (echoRef.current && payload.trim() === echoRef.current) return
    let uploaded: Attachment[] = []
    if (pending.length > 0) {
      setUploading(true)
      try {
        uploaded = (await onUpload(pending)) ?? []
        if (uploaded.length === 0) return
        setPending([])
      } catch {
        return
      } finally {
        setUploading(false)
      }
    }
    const images = pasted.length > 0 ? await toSendImages(pasted) : undefined
    const files = uploaded.map((a) => a.rel_path).filter(Boolean)
    if (!payload && !(images && images.length > 0) && files.length === 0) return
    echoRef.current = payload.trim()
    onSend(payload, images, {
      steer: Boolean(opts?.steer),
      files: files.length > 0 ? files : undefined,
    })
    setText("")
    onQuotesChange?.([])
    revokePasteImages(pasted)
    setPasted([])
  }

  const onFileDragOver = (e: DragEvent<HTMLDivElement>) => {
    if (!isFileDrag(e.dataTransfer)) return
    // Without this the browser navigates to the dropped file.
    e.preventDefault()
    e.stopPropagation()
    if (e.dataTransfer) e.dataTransfer.dropEffect = disabled ? "none" : "copy"
    if (!disabled) setArmed(true)
  }

  const onFileDragEnter = (e: DragEvent<HTMLDivElement>) => {
    if (!isFileDrag(e.dataTransfer)) return
    e.preventDefault()
    e.stopPropagation()
    if (!disabled) setArmed(true)
  }

  const onFileDragLeave = (e: DragEvent<HTMLDivElement>) => {
    const next = e.relatedTarget
    if (next instanceof Node && e.currentTarget.contains(next)) return
    setArmed(false)
  }

  const onFileDrop = (e: DragEvent<HTMLDivElement>) => {
    if (!isFileDrag(e.dataTransfer)) return
    e.preventDefault()
    e.stopPropagation()
    setArmed(false)
    if (disabled) return
    const { images, attachments } = splitDroppedFiles(
      filesFromDataTransfer(e.dataTransfer),
    )
    if (images.length === 0 && attachments.length === 0) return
    setAdding(true)
    try {
      if (images.length > 0) {
        setPasted((prev) => addPasteImages(prev, images).next)
      }
      if (attachments.length > 0) {
        setPending((prev) => [...prev, ...attachments])
      }
    } finally {
      setAdding(false)
    }
  }

  return (
    <div
      data-testid="composer"
      className="pointer-events-none absolute inset-x-0 bottom-0 z-10"
    >
      {/* Codex-style: the box sits on the transcript. A hairline here would
          turn it into a toolbar docked to the bottom, which is the cheap
          look. The fade is the join; pointer-events stay off so the faded
          lines are still selectable and the scrollbar still drags. */}
      <div
        aria-hidden
        data-testid="composer-fade"
        className="composer-fade pointer-events-none absolute inset-x-0 -top-24 bottom-0"
      />
      <div
        ref={dockRef}
        className="pointer-events-auto relative z-10 content-column content-gutter pb-3"
      >
        <PlanBanner
          mode={planMode}
          markdown={planMarkdown}
          running={running}
          onImplement={onImplementPlan}
          onEdit={onSavePlan}
          onLeave={onLeavePlan}
        />
        <GoalBanner
          goal={goal ?? ""}
          complete={goalComplete}
          blocked={goalBlocked}
          blockReason={goalBlockReason}
          turnError={goalBlocked ? lastTurnError : undefined}
          capped={goalCapped}
          idle={goalIdle}
          running={running}
          startedAt={goalStartedAt}
          onClear={() => onClearGoal?.()}
          onEdit={onEditGoal}
          onResume={onResumeGoal}
        />
        <ScheduleBanner />
        <div
          data-testid="composer-drop"
          className="relative"
          onDragEnter={onFileDragEnter}
          onDragOver={onFileDragOver}
          onDragLeave={onFileDragLeave}
          onDrop={onFileDrop}
        >
          <ComposerAttachments
            files={pending}
            uploading={uploading}
            onRemove={(i) => setPending((prev) => prev.filter((_, j) => j !== i))}
          />

          <QueueTray
            items={followups ?? []}
            onSteer={(id) => onSteerFollowup?.(id)}
            onDelete={(id) => onDeleteFollowup?.(id)}
            onRequeue={(id, next) => onRequeueFollowup?.(id, next)}
            onClear={() => onClearFollowups?.()}
          />
          <div className="relative">
            {draft && slashOpen ? (
              <ComposerSlashMenu
                query={draft.query}
                activeIndex={Math.min(slashIndex, slashItems.length - 1)}
                usage={usage}
                window={meterWindow}
                contextChars={contextChars ?? 0}
                contextBudget={contextBudget ?? 0}
                onHover={setSlashIndex}
                onSelect={pickCommand}
              />
            ) : null}
          <div
            className={
              "relative rounded-3xl border border-border bg-card shadow-lg transition-colors focus-within:border-ring" +
              (armed || adding ? " min-h-36" : "")
            }
          >
          {armed || adding ? <ComposerDropOverlay busy={adding} /> : null}
          <ComposerQuotes quotes={quoted} onChange={(next) => onQuotesChange?.(next)} />
          <ComposerImages
            images={pasted}
            onRemove={(id) => setPasted((prev) => dropPasteImage(prev, id))}
          />
          <Textarea
            ref={areaRef}
            value={text}
            rows={1}
            disabled={disabled}
            data-testid="composer-input"
            data-slash-open={slashOpen ? "true" : undefined}
            data-goal-draft={goalDraft ? "true" : undefined}
            data-plan-draft={planDraft ? "true" : undefined}
            aria-expanded={slashOpen}
            aria-controls={slashOpen ? "slash-menu" : undefined}
            placeholder={
              goalDraft
                ? t("composer.placeholderGoal")
                : planDraft
                  ? t("composer.placeholderPlan")
                  : awaitingAnswer
                    ? t("composer.placeholderAsk")
                    : running
                      ? t("composer.placeholderRunning")
                      : t("composer.placeholder")
            }
            className="max-h-[200px] min-h-[44px] rounded-none border-0 bg-transparent px-4 py-3 text-[0.9375rem] shadow-none focus-visible:ring-0"
            onChange={(e) => {
              const next = normalizeSlashPrefix(e.target.value)
              if (echoRef.current && next.trim() === echoRef.current) {
                setText("")
                return
              }
              echoRef.current = ""
              setText(next)
            }}
            onPaste={(e) => {
              const files = filesFromClipboard(e.clipboardData)
              if (files.length === 0) return
              e.preventDefault()
              setPasted((prev) => addPasteImages(prev, files).next)
            }}
            onCompositionStart={() => {
              cancelImeSettle.current?.()
              composingRef.current = true
            }}
            onCompositionEnd={() => {
              cancelImeSettle.current?.()
              cancelImeSettle.current = afterImeSettles(() => {
                composingRef.current = false
                cancelImeSettle.current = null
                resizeComposerArea(areaRef.current)
              })
            }}
            onKeyDown={(e) => {
              if (slashOpen) {
                if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                  e.preventDefault()
                  setSlashIndex((i) =>
                    nextSlashIndex(
                      i,
                      slashItems.length,
                      e.key === "ArrowDown" ? 1 : -1,
                    ),
                  )
                  return
                }
                if (e.key === "Escape") {
                  e.preventDefault()
                  e.stopPropagation()
                  setText(draft ? stripSlashToken(text, draft) : "")
                  return
                }
                if (e.key === "Tab") {
                  e.preventDefault()
                  const cmd = slashItems[Math.min(slashIndex, slashItems.length - 1)]
                  if (cmd) pickCommand(cmd)
                  return
                }
              }
              if (e.key === "Escape" && (goalDraft || planDraft)) {
                e.preventDefault()
                e.stopPropagation()
                setGoalDraft(false)
                setPlanDraft(false)
                setText("")
                return
              }
              // Enter sends; Shift+Enter is a newline. An IME confirm —
              // keep leftover Latin as typed — is not a send, and must
              // not be preventDefault'd or the IME cannot commit.
              if (!enterSendsMessage(e.nativeEvent, composingRef.current)) return
              e.preventDefault()
              void submit({ steer: e.metaKey || e.ctrlKey })
            }}
          />

          <div className="flex items-center gap-1.5 px-2 pb-2">
            <input
              ref={fileRef}
              type="file"
              multiple
              className="hidden"
              data-testid="file-input"
              onChange={(e) => {
                const files = Array.from(e.target.files ?? [])
                if (files.length > 0) setPending((prev) => [...prev, ...files])
                e.target.value = ""
              }}
            />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  disabled={disabled}
                  onClick={() => fileRef.current?.click()}
                  aria-label={t("composer.attach")}
                >
                  <Paperclip />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t("composer.attachHint")}</TooltipContent>
            </Tooltip>

            <ModelPicker
              models={models}
              providerId={provider}
              model={model}
              onChange={onModelChange}
              onRefresh={onRefreshModels}
              onEdit={onEditProviders}
            />

            {reasoningLevels.length > 0 ? (
              <Select
                value={reasoning ? reasoning : "default"}
                onValueChange={(v) => onReasoningChange(v === "default" ? "" : v)}
              >
                <SelectTrigger
                  aria-label={t("composer.thinking")}
                  className="h-7 w-auto gap-1.5 border-none bg-transparent px-2 shadow-none hover:bg-accent"
                >
                  <Brain className="size-3.5 text-muted-foreground" />
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="default">{t("composer.thinkingDefault")}</SelectItem>
                  {reasoningLevels.map((l) => (
                    <SelectItem key={l} value={l}>
                      {thinkingLabel(l, t)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : null}

            <Tooltip>
              <TooltipTrigger asChild>
                <Badge variant="outline" className="gap-1">
                  <ShieldCheck className="size-3" />
                  {t("composer.fullAccess")}
                </Badge>
              </TooltipTrigger>
              <TooltipContent>
                {t("composer.fullAccessHint")}
              </TooltipContent>
            </Tooltip>

            <div className="ml-auto flex items-center gap-1">
              <ComposerContextMeter
                usage={usage}
                window={meterWindow}
                scale={contextBudget ?? 0}
              />
              {running ? (
                <Button
                  size="icon"
                  variant="secondary"
                  onClick={onStop}
                  aria-label={t("composer.stop")}
                  title={t("composer.stop")}
                >
                  <Square className="size-3.5 fill-current" />
                </Button>
              ) : (
                <Button
                  size="icon"
                  disabled={
                    disabled ||
                    uploading ||
                    (!slashOpen &&
                      !goalDraft &&
                      !planDraft &&
                      !text.trim() &&
                      pending.length === 0 &&
                      quoted.length === 0 &&
                      pasted.length === 0) ||
                    (goalDraft && !text.trim()) ||
                    (planDraft && !text.trim())
                  }
                  onClick={() => void submit()}
                  aria-label={t("composer.send")}
                  title={t("composer.sendHint")}
                >
                  <ArrowUp />
                </Button>
              )}
            </div>
          </div>
        </div>
        </div>
        </div>
      </div>
    </div>
  )
}

/** Isolated so a live `usage` pulse (one per model call) cannot rewrite the
 *  controlled textarea while a CJK IME is composing. */
function ComposerContextMeter({
  usage,
  window,
  scale,
}: {
  usage?: UsageSnapshot | null
  window: number
  scale: number
}) {
  const live = useApp((s) => s.usage)
  return <ContextMeter usage={usage ?? live} window={window} scale={scale} />
}

function ComposerSlashMenu({
  query,
  activeIndex,
  usage,
  window,
  contextChars,
  contextBudget,
  onHover,
  onSelect,
}: {
  query: string
  activeIndex: number
  usage?: UsageSnapshot | null
  window: number
  contextChars: number
  contextBudget: number
  onHover: (index: number) => void
  onSelect: (cmd: SlashCommand) => void
}) {
  const t = useT()
  const live = useApp((s) => s.usage)
  const snap = usage ?? live
  const items = withSlashHints(
    filterSlashCommands(query, localizedSlashCommands(t.locale)),
    {
      compact: compactHint(
        snap?.context_tokens,
        snap?.context_window || window,
        contextChars,
        contextBudget,
        t.locale,
      ),
    },
  )
  return (
    <SlashMenu
      items={items}
      activeIndex={Math.min(activeIndex, Math.max(0, items.length - 1))}
      onHover={onHover}
      onSelect={onSelect}
    />
  )
}

function thinkingLabel(level: string, t: Translate): string {
  if (level === "low") return t("reason.low")
  if (level === "medium") return t("reason.medium")
  if (level === "high") return t("reason.high")
  return t("composer.thinkingLevel", { level })
}

