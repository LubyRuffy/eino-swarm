import { selectionTranslationAvailable } from "@/lib/model-policy"
import { refreshCapabilities } from "@/lib/refresh-capabilities"
import { Languages, Loader2, MessageSquare, MessageSquarePlus, Square, Volume2 } from "lucide-react"
import { useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from "react"

import { Button } from "@/components/ui/button"
import { ApiError, api } from "@/lib/api"
import {
  QUOTE_SELECTION_EVENT,
  menuPosition,
  nextMenuSelection,
  readTextSelection,
  type PageSelection,
} from "@/lib/selection"
import { englishSelection } from "@/lib/selection-language"
import { speechPlayback } from "@/lib/speech-media"
import { synthesizeSpeech } from "@/lib/speech-api"
import { speechFailureMessage } from "@/lib/speech-error"
import { speechText } from "@/lib/speech-text"
import { useT } from "@/lib/use-t"
import { TERMINAL_SELECTION_EVENT, type TerminalSelection } from "@/lib/terminal"
import { useApp } from "@/store/app"
import { toastError } from "@/store/toasts"

/** One playback owner for the selection menu. Leaving the menu stops it. */
export const selectionSpeechKey = "selection"

type SelectionTranslation = { status: "loading" | "done" | "error"; text: string; error?: string }

/** Offers quote actions after a selection inside a quote source settles.
 *  Reading and translation stay beside that selection; they are not quotes. */
export function SelectionMenu({
  threadId,
  onAdd,
  onAsk,
}: {
  threadId?: string
  onAdd: (text: string) => void
  onAsk?: (text: string) => void
}) {
  const t = useT()
  const speech = useApp((store) => store.meta?.speech)
  const playback = useSyncExternalStore(speechPlayback.subscribe, speechPlayback.getSnapshot)
  const [active, setActive] = useState<PageSelection | null>(null)
  const [clearSelection, setClearSelection] = useState<(() => void) | null>(null)
  const [translation, setTranslation] = useState<SelectionTranslation | null>(null)
  const translateAbort = useRef<AbortController | null>(null)
  const menu = useRef<HTMLDivElement>(null)
  const canAsk = Boolean(onAsk && !clearSelection)
  const selectionText = active?.text ?? ""
  const prose = useMemo(() => speechText(selectionText), [selectionText])
  const canRead = Boolean(speech?.output.enabled && speech.output.ready && prose)
  const translationAllowed = useApp(s => selectionTranslationAvailable(s.meta?.model_policy, s.threads.find(th => th.id === threadId)?.provider_id || s.meta?.default_provider))
  useEffect(() => { if (active) void refreshCapabilities().catch(() => undefined) }, [Boolean(active)])
  const showTranslate = translationAllowed && t.locale === "zh" && Boolean(threadId) && englishSelection(selectionText)
  const reading = playback.key === selectionSpeechKey && playback.state !== "idle"

  useEffect(() => {
    let pressed = false
    let downText = ""
    let dismissedClick = false
    let terminal: TerminalSelection | null = null

    const fromEventTarget = (target: EventTarget | null) =>
      target instanceof Element && target.closest("[data-testid=selection-menu]")

    const apply = (clearIfEmpty: boolean, announce: boolean) => {
      const live = terminal?.selection ?? readTextSelection()
      if (announce && live) {
        document.dispatchEvent(new Event(QUOTE_SELECTION_EVENT))
      }
      setActive((current) => nextMenuSelection(current, live, clearIfEmpty))
    }

    const onMouseDown = (event: MouseEvent) => {
      if (event.button !== 0 || fromEventTarget(event.target)) return
      pressed = true
      dismissedClick = false
      downText = window.getSelection()?.toString() ?? ""
    }
    const onMouseUp = (event: MouseEvent) => {
      if (event.button !== 0) return
      pressed = false
      if (fromEventTarget(event.target)) return
      const insideTerminal = Boolean(
        terminal && event.target instanceof Node && terminal.source.contains(event.target),
      )
      if (terminal && !insideTerminal) {
        terminal = null
        setClearSelection(null)
      }
      const upText = window.getSelection()?.toString() ?? ""
      // A single click that does not paint a new range is leaving the
      // selection. Chromium still reports the old range at mouseup, then
      // collapses it. The later selectionchange must not restore the menu:
      // the same event is how streaming keeps a snapshot.
      if (!insideTerminal && event.detail === 1 && upText !== "" && upText === downText) {
        dismissedClick = true
        setActive(null)
        return
      }
      dismissedClick = false
      apply(true, true)
    }
    const onSelectionChange = () => {
      // While the pointer is down the range is still being painted; wait.
      // A click that did not paint a new range already dismissed the menu.
      if (pressed || dismissedClick) return
      apply(false, true)
    }
    const onKeyUp = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        hide()
        return
      }
      if (!fromEventTarget(event.target)) apply(true, true)
    }
    const hide = () => {
      if (speechPlayback.getSnapshot().key === selectionSpeechKey) speechPlayback.stop()
      translateAbort.current?.abort()
      terminal = null
      dismissedClick = false
      setClearSelection(null)
      setActive(null)
    }
    const onTerminalSelection = (event: Event) => {
      const next = (event as CustomEvent<TerminalSelection>).detail
      if (!next.selection) {
        if (terminal?.source === next.source) hide()
        return
      }
      window.getSelection()?.removeAllRanges()
      terminal = next
      setClearSelection(() => () => {
        hide()
        next.clear()
      })
      if (!pressed) apply(true, true)
    }
    const onUserPan = (event: Event) => {
      if (fromEventTarget(event.target)) return
      hide()
    }
    // Streaming output may scroll a different pane. Keep the settled snapshot;
    // wheel/touch gestures below already dismiss deliberate user movement.
    const onScroll = () => apply(false, false)

    document.addEventListener("mousedown", onMouseDown)
    document.addEventListener("mouseup", onMouseUp)
    document.addEventListener("selectionchange", onSelectionChange)
    document.addEventListener("keyup", onKeyUp)
    document.addEventListener("scroll", onScroll, true)
    document.addEventListener("wheel", onUserPan, { capture: true, passive: true })
    document.addEventListener("touchmove", onUserPan, { capture: true, passive: true })
    window.addEventListener("resize", hide)
    document.addEventListener(TERMINAL_SELECTION_EVENT, onTerminalSelection)
    return () => {
      document.removeEventListener("mousedown", onMouseDown)
      document.removeEventListener("mouseup", onMouseUp)
      document.removeEventListener("selectionchange", onSelectionChange)
      document.removeEventListener("keyup", onKeyUp)
      document.removeEventListener("scroll", onScroll, true)
      document.removeEventListener("wheel", onUserPan, true)
      document.removeEventListener("touchmove", onUserPan, true)
      window.removeEventListener("resize", hide)
      document.removeEventListener(TERMINAL_SELECTION_EVENT, onTerminalSelection)
    }
  }, [])

  useEffect(() => {
    setTranslation(null)
  }, [selectionText])

  useEffect(() => {
    if (translationAllowed) return
    translateAbort.current?.abort()
    setTranslation(null)
  }, [translationAllowed])

  useEffect(() => () => {
    translateAbort.current?.abort()
    if (speechPlayback.getSnapshot().key === selectionSpeechKey) speechPlayback.stop()
  }, [selectionText])

  useEffect(() => {
    const hidden = () => {
      if (document.hidden && speechPlayback.getSnapshot().key === selectionSpeechKey) speechPlayback.stop()
    }
    document.addEventListener("visibilitychange", hidden)
    return () => document.removeEventListener("visibilitychange", hidden)
  }, [])

  useEffect(() => {
    if (!canRead && speechPlayback.getSnapshot().key === selectionSpeechKey) speechPlayback.stop()
  }, [canRead])

  useLayoutEffect(() => {
    const el = menu.current
    if (!active || !el) return
    const { left } = menuPosition(active.rect, windowViewport(), el.offsetWidth)
    el.style.left = `${left}px`
  }, [active, canAsk, canRead, showTranslate, t.locale, translation?.status])

  if (!active) return null
  const pos = menuPosition(active.rect, windowViewport())
  const choose = (action: (text: string) => void) => {
    if (speechPlayback.getSnapshot().key === selectionSpeechKey) speechPlayback.stop()
    translateAbort.current?.abort()
    clearSelection?.()
    window.getSelection()?.removeAllRanges()
    setActive(null)
    action(active.text)
  }
  const read = () => {
    if (!speech || !canRead) return
    if (reading) {
      speechPlayback.stop()
      return
    }
    void speechPlayback.play(selectionSpeechKey, active.text, {
      chunkChars: speech.limits.chunk_chars,
      synthesize: (part, signal) => synthesizeSpeech(part, signal, speech.limits.output_bytes),
    }).catch((error: unknown) => {
      const message = speechFailureMessage(error, t)
      if (message) toastError(message, { title: t("speech.output") })
    })
  }
  const translate = () => {
    if (!threadId) return
    translateAbort.current?.abort()
    const controller = new AbortController()
    translateAbort.current = controller
    const source = active.text
    setTranslation({ status: "loading", text: "" })
    void api.translateSelection(threadId, source, controller.signal).then(
      (result) => {
        if (controller.signal.aborted) return
        if (!result.text.trim()) {
          setTranslation({ status: "error", text: "", error: t("selection.translateFailed") })
          return
        }
        setTranslation({ status: "done", text: result.text })
      },
      (error: unknown) => {
        if (controller.signal.aborted) return
        const tooLong = error instanceof ApiError && error.code === "selection_translation_too_long"
        setTranslation({
          status: "error",
          text: "",
          error: t(tooLong ? "selection.translateTooLong" : "selection.translateFailed"),
        })
      },
    )
  }
  const readLabel = reading ? t("speech.stopReading") : t("speech.read")

  return (
    <div
      ref={menu}
      data-testid="selection-menu"
      role="menu"
      aria-label={t("selection.menu")}
      className="fixed z-50 flex flex-col rounded-md border border-border bg-popover p-0.5 text-popover-foreground shadow-md"
      style={{
        top: pos.top,
        left: pos.left,
        transform: pos.place === "above" ? "translate(-50%, -100%)" : "translate(-50%, 0)",
      }}
    >
      <div className="flex">
        <Button
          role="menuitem"
          variant="ghost"
          size="sm"
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => choose(onAdd)}
        >
          <MessageSquarePlus />
          {t("selection.add")}
        </Button>
        {onAsk && canAsk ? (
          <Button
            role="menuitem"
            variant="ghost"
            size="sm"
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => choose(onAsk)}
          >
            <MessageSquare />
            {t("selection.sideChat")}
          </Button>
        ) : null}
        {canRead ? (
          <Button
            role="menuitem"
            variant="ghost"
            size="sm"
            aria-label={readLabel}
            onMouseDown={(event) => event.preventDefault()}
            onClick={read}
          >
            {reading && playback.state === "loading" ? <Loader2 className="animate-spin" /> : reading ? <Square className="fill-current" /> : <Volume2 />}
            {readLabel}
          </Button>
        ) : null}
        {showTranslate ? (
          <Button
            role="menuitem"
            variant="ghost"
            size="sm"
            aria-busy={translation?.status === "loading"}
            onMouseDown={(event) => event.preventDefault()}
            onClick={translate}
          >
            {translation?.status === "loading" ? <Loader2 className="animate-spin" /> : <Languages />}
            {t("selection.translate")}
          </Button>
        ) : null}
      </div>
      {translation ? (
        <div
          role="status"
          aria-live="polite"
          aria-label={t("selection.translation")}
          className="max-h-48 max-w-sm overflow-auto border-t border-border px-2.5 py-2 text-(length:--chrome-font-size) whitespace-pre-wrap wrap-anywhere"
        >
          {translation.status === "loading" ? t("selection.translating")
            : translation.status === "error" ? <span className="text-destructive">{translation.error}</span>
            : translation.text}
        </div>
      ) : null}
    </div>
  )
}

function windowViewport() {
  return { width: window.innerWidth, height: window.innerHeight }
}
