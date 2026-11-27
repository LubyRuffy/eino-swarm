import { GATEWAY_CAPABILITY_REFRESH_EVENT } from "@/lib/gateway-capabilities"
import { usePairedFeature } from "@/lib/paired-capabilities"
import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react"
import { Languages, Loader2, MessageSquarePlus, Square, Volume2, X } from "lucide-react"
import { englishSelection } from "../../../frontend/src/lib/selection-language"
import { speechPlayback } from "../../../frontend/src/lib/speech-media"
import { speechText } from "../../../frontend/src/lib/speech-text"
import { Button } from "@/components/ui/button"
import { getLocale, t } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import { cancelPhoneSpeech, synthesizePhoneSpeech, usePhoneSpeech } from "@/lib/phone-speech"
import { nextRPCId, OpCancelTranslation, OpTranslateSelection } from "@/lib/rpc"
import { speechFailureMessage } from "./speech-capture"

export function SelectionActions({ text, threadId, link, providerID, onAdd, onDismiss, onExpand }: {
  text: string
  threadId: string
  providerID?: string
  link?: RemoteLink
  onAdd: () => void
  onDismiss: () => void
  onExpand: () => void
}) {
  const speech = usePhoneSpeech()
  useEffect(() => { window.dispatchEvent(new Event(GATEWAY_CAPABILITY_REFRESH_EVENT)) }, [])
  const playback = useSyncExternalStore(speechPlayback.subscribe, speechPlayback.getSnapshot, speechPlayback.getSnapshot)
  const speechKey = `phone-selection:${threadId}`
  const reading = playback.key === speechKey && playback.state !== "idle"
  const prose = useMemo(() => Boolean(speechText(text)), [text])
  const canRead = speech.outputReady && prose
  const translationAllowed = usePairedFeature(link, providerID, "translation")
  const canTranslate = translationAllowed && getLocale() === "zh" && Boolean(link) && englishSelection(text)
  const [translation, setTranslation] = useState<{ status: "loading" | "done" | "error"; text: string } | null>(null)
  const [readError, setReadError] = useState("")
  const request = useRef<AbortController | null>(null)
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    const stop = () => {
      request.current?.abort()
      if (speechPlayback.getSnapshot().key === speechKey) speechPlayback.stop()
    }
    const visibility = () => { if (document.hidden) { stop(); onDismiss() } }
    document.addEventListener("visibilitychange", visibility)
    return () => {
      alive.current = false
      stop()
      document.removeEventListener("visibilitychange", visibility)
    }
    // Parent keys this component by thread and selected text.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [speechKey, link])

  useEffect(() => {
    if (!canRead && speechPlayback.getSnapshot().key === speechKey) speechPlayback.stop()
  }, [canRead, speechKey])

  const read = () => {
    setReadError("")
    if (reading) { speechPlayback.stop(); return }
    if (!canRead || !speech.snapshot) return
    cancelPhoneSpeech()
    void speechPlayback.play(speechKey, text, {
      chunkChars: speech.snapshot.limits.chunk_chars, synthesize: synthesizePhoneSpeech,
    }).catch((error) => { if (alive.current) setReadError(speechFailureMessage(error)) })
  }

  useEffect(() => {
    if (translationAllowed) return
    request.current?.abort()
    setTranslation(null)
  }, [translationAllowed])

  const translate = () => {
    if (!canTranslate || !link?.alive() || translation?.status === "loading") return
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    const id = nextRPCId()
    const cancel = () => {
      void link.rpc({ op: OpCancelTranslation, call_id: id }, { dropOnTimeout: false }).catch(() => {})
    }
    controller.signal.addEventListener("abort", cancel, { once: true })
    setTranslation({ status: "loading", text: "" })
    onExpand()
    void link.rpc({ id, op: OpTranslateSelection, thread_id: threadId, text }, {
      signal: controller.signal, timeoutMs: 65000, dropOnTimeout: false,
    }).then((response) => {
      if (controller.signal.aborted || !alive.current) return
      if (!response.ok || !response.translation?.trim()) {
        setTranslation({ status: "error", text: t(response.code === "unknown_op" ? "selection.updatePC" : response.code === "selection_translation_too_long" ? "selection.tooLong" : "selection.failed") })
      } else setTranslation({ status: "done", text: response.translation })
    }).catch(() => {
      if (!controller.signal.aborted && alive.current) {
        cancel()
        setTranslation({ status: "error", text: t("selection.failed") })
      }
    }).finally(() => {
      controller.signal.removeEventListener("abort", cancel)
      if (request.current === controller) request.current = null
    })
  }

  return <>
    <div role="toolbar" aria-label={t("selection.actions")} className="flex select-none items-center gap-1 p-1">
      <Button className="h-11 flex-1 gap-1 whitespace-nowrap rounded-lg px-1.5 text-xs" onClick={onAdd}>
        <MessageSquarePlus aria-hidden className="size-4 shrink-0" />{t("quote.add")}
      </Button>
      {canRead ? <Button variant="ghost" className="h-11 flex-1 gap-1 whitespace-nowrap rounded-lg px-1.5 text-xs" onClick={read}
        data-playback-state={reading ? playback.state : "idle"}>
        {reading && playback.state === "loading" ? <Loader2 aria-hidden className="size-4 animate-spin motion-reduce:animate-none" /> : reading ? <Square aria-hidden className="size-4" /> : <Volume2 aria-hidden className="size-4" />}
        {t(reading ? "speech.stop" : "speech.read")}
      </Button> : null}
      {canTranslate ? <Button variant="ghost" className="h-11 flex-1 gap-1 whitespace-nowrap rounded-lg px-1.5 text-xs" onClick={translate} disabled={translation?.status === "loading"}>
        {translation?.status === "loading" ? <Loader2 aria-hidden className="size-4 animate-spin motion-reduce:animate-none" /> : <Languages aria-hidden className="size-4" />}
        {t("selection.translate")}
      </Button> : null}
      <Button variant="ghost" className="size-11 shrink-0 rounded-lg px-0" aria-label={t("selection.close")} onClick={onDismiss}><X aria-hidden className="size-4" /></Button>
    </div>
    {readError ? <p role="alert" className="px-3 pb-2 text-sm text-destructive">{readError}</p> : null}
    {translation ? <section aria-label={t("selection.translation")} aria-live="polite" aria-busy={translation.status === "loading"}
      className="max-h-40 overflow-y-auto overscroll-contain border-t border-border px-3 py-2 text-sm leading-relaxed whitespace-pre-wrap break-words select-text">
      <p role={translation.status === "error" ? "alert" : undefined}>{translation.status === "loading" ? t("selection.translating") : translation.text}</p>
    </section> : null}
  </>
}
