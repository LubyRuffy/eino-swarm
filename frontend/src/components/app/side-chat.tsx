import { ArrowUp, Square, X } from "lucide-react"
import { useEffect, useRef, useState } from "react"

import { ModelPicker } from "@/components/app/model-picker"
import { ResizeHandle } from "@/components/app/resize-handle"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { afterImeSettles, enterSendsMessage } from "@/lib/ime"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"
import { SIDE_CHAT_QUESTION_BYTES, sideChatQuestionTooLong, useSideChat } from "@/store/app-side-chat"

export function SideChat({ threadId, onClose }: { threadId: string; onClose: () => void }) {
  const t = useT()
  const session = useSideChat((s) => s.session?.threadId === threadId ? s.session : undefined)
  const models = useApp((s) => s.models)
  const [width, setWidth] = useState(384)
  const input = useRef<HTMLTextAreaElement>(null)
  const log = useRef<HTMLDivElement>(null)
  const composing = useRef(false)
  const settle = useRef<(() => void) | null>(null)
  useEffect(() => () => settle.current?.(), [])

  const focus = session?.focus
  useEffect(() => {
    if (focus) input.current?.focus()
  }, [focus])
  const count = session?.exchanges.length
  const preview = session?.preview
  const pending = Boolean(session?.request)
  useEffect(() => {
    if (log.current) log.current.scrollTop = log.current.scrollHeight
  }, [count, pending, preview])

  if (!session) return null
  const tooLong = sideChatQuestionTooLong(session.question)
  const canSend = !pending && !tooLong && session.question.trim() !== ""
  const ask = () => {
    if (!canSend) return
    input.current?.focus()
    void useSideChat.getState().ask()
  }

  return (
    <aside
      id="side-chat"
      aria-labelledby="side-chat-title"
      className="relative flex h-full max-w-[55%] min-w-0 shrink-0 flex-col border-l border-border bg-card"
      style={{ width }}
      onKeyDown={(event) => {
        if (event.key !== "Escape" || event.nativeEvent.isComposing || composing.current) return
        event.stopPropagation()
        onClose()
      }}
    >
      <ResizeHandle width={width} onWidthChange={setWidth} edge="left" label={t("sideChat.resize")} min={260} max={720} />
      <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border px-3 py-2">
        <h2 id="side-chat-title" className="text-(length:--chrome-font-size) font-medium">{t("sideChat.title")}</h2>
        <Button type="button" variant="ghost" size="icon-sm" aria-label={t("sideChat.close")} onClick={onClose}>
          <X />
        </Button>
      </div>
      <div ref={log} role="log" aria-label={t("sideChat.messages")} className="min-h-0 flex-1 space-y-4 overflow-auto p-3 text-(length:--chrome-font-size)">
        {session.exchanges.length === 0 && !pending ? (
          <p className="text-muted-foreground">{t("sideChat.intro")}</p>
        ) : session.exchanges.map(({ question, answer }, index) => (
          <div key={index} className="space-y-2">
            <p className="ml-5 rounded-lg bg-muted px-3 py-2 wrap-anywhere whitespace-pre-wrap">
              <span className="sr-only">{t("sideChat.you")} </span>
              {question}
            </p>
            <p className="wrap-anywhere whitespace-pre-wrap">
              <span className="sr-only">{t("sideChat.title")} </span>
              {answer}
            </p>
          </div>
        ))}
        {pending ? (
          <div className="space-y-2" aria-live="off">
            <p className="ml-5 rounded-lg bg-muted px-3 py-2 wrap-anywhere whitespace-pre-wrap">
              <span className="sr-only">{t("sideChat.you")} </span>
              {session.question.trim()}
            </p>
            {preview ? <p className="wrap-anywhere whitespace-pre-wrap">{preview}</p> : null}
          </div>
        ) : null}
      </div>
      <form
        className="shrink-0 space-y-2 border-t border-border p-3"
        onSubmit={(event) => {
          event.preventDefault()
          ask()
        }}
      >
        {session.quote ? (
          <div className="flex items-start gap-1 rounded-md bg-muted px-2 py-1">
            <blockquote data-testid="side-chat-quote" className="max-h-24 min-w-0 flex-1 overflow-auto wrap-anywhere whitespace-pre-wrap text-(length:--chrome-font-size) text-muted-foreground">
              {session.quote}
            </blockquote>
            <Button type="button" variant="ghost" size="icon-sm" aria-label={t("sideChat.removeQuote")} onClick={useSideChat.getState().clearQuote}>
              <X />
            </Button>
          </div>
        ) : null}
        <Textarea
          ref={input}
          value={pending ? "" : session.question}
          aria-label={t("sideChat.question")}
          aria-invalid={tooLong || undefined}
          aria-describedby={tooLong ? "side-chat-question-length" : undefined}
          placeholder={t("sideChat.placeholder")}
          maxLength={SIDE_CHAT_QUESTION_BYTES}
          rows={3}
          readOnly={pending}
          onChange={(event) => useSideChat.getState().edit(event.target.value)}
          onCompositionStart={() => {
            settle.current?.()
            composing.current = true
          }}
          onCompositionEnd={() => {
            settle.current?.()
            settle.current = afterImeSettles(() => { composing.current = false })
          }}
          onKeyDown={(event) => {
            if (!enterSendsMessage(event.nativeEvent, composing.current)) return
            event.preventDefault()
            ask()
          }}
        />
        {tooLong ? <p id="side-chat-question-length" className="text-(length:--chrome-font-size) text-destructive">{t("sideChat.questionTooLong")}</p> : null}
        {session.error ? <p role="alert" className="wrap-anywhere text-(length:--chrome-font-size) text-destructive">{session.error}</p> : null}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <ModelPicker
            models={models}
            providerId={session.model?.provider_id}
            model={session.model?.model}
            onChange={useSideChat.getState().selectModel}
            label={t("sideChat.model")}
            defaultLabel={t("sideChat.followModel")}
            disabled={pending}
          />
          {pending ? (
            <div className="ml-auto flex items-center gap-2">
              <span role="status" className="text-(length:--chrome-font-size) text-muted-foreground">{t(preview ? "sideChat.streaming" : "sideChat.thinking")}</span>
              <Button type="button" variant="outline" size="icon-sm" aria-label={t("sideChat.cancel")} onClick={useSideChat.getState().cancel}>
                <Square />
              </Button>
            </div>
          ) : (
            <Button type="submit" size="icon-sm" className="ml-auto" aria-label={t("sideChat.send")} disabled={!canSend}>
              <ArrowUp />
            </Button>
          )}
        </div>
      </form>
    </aside>
  )
}
