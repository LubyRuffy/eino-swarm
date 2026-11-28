import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/input"
import { installAndroidBack } from "@/lib/android-back"
import { linkError, remoteError } from "@/lib/client"
import { MAX_PAYLOAD } from "@/lib/frame"
import { t } from "@/lib/i18n"
import { formatQuotedMessage, parseQuotedMessage } from "@/lib/quote"
import { encodeRequest, nextRPCId, OpEditInput, PROTOCOL_V, type RemoteResponse } from "@/lib/rpc"
import type { TranscriptImageOwner } from "./transcript-image"

function editError(response: RemoteResponse): string {
  switch (response.code) {
    case "unknown_op": return t("editInput.updatePC")
    case "not_found": return t("editInput.missing")
    case "too_large": return t("editInput.tooLarge")
    case "edit_input_command": return t("editInput.command")
    default: return remoteError(response.error || response.code || "")
  }
}

export function HistoryInputEditor({ owner: { link, threadId }, seq, disabled, onClose }: {
  owner: TranscriptImageOwner
  seq: number
  disabled?: boolean
  onClose: (seq: number) => void
}) {
  const [original, setOriginal] = useState<{ quotes: string[]; hasImages: boolean }>()
  const [draft, setDraft] = useState("")
  const [error, setError] = useState("")
  const [sending, setSending] = useState(false)
  const inFlight = useRef(false)

  useEffect(() => {
    if (original) return
    const abort = new AbortController()
    setError("")
    void link.rpc({ op: OpEditInput, thread_id: threadId, from_event_seq: seq }, {
      signal: abort.signal, dropOnTimeout: false,
    }).then((response) => {
      if (abort.signal.aborted) return
      if (!response.ok) {
        setError(editError(response))
        return
      }
      const event = response.event
      if (!event || event.kind !== "user_message" || event.thread_id !== threadId || event.seq !== seq) {
        setError(t("editInput.missing"))
        return
      }
      const { quotes, body } = parseQuotedMessage(event.text)
      setOriginal({ quotes, hasImages: Boolean(event.has_images) })
      setDraft(body)
    }).catch((cause) => {
      if (!abort.signal.aborted) setError(linkError(cause))
    })
    return () => abort.abort()
  }, [link, threadId, seq, original])

  useEffect(() => installAndroidBack(() => {
    if (!inFlight.current) onClose(seq)
    return true
  }), [onClose, seq])

  const replacement = formatQuotedMessage(original?.quotes ?? [], draft)
  const ready = Boolean(original && (replacement || original.hasImages))
  const submit = async () => {
    if (disabled || !ready || inFlight.current) return
    const request = {
      v: PROTOCOL_V, id: nextRPCId(), op: OpEditInput, thread_id: threadId, from_event_seq: seq, replacement,
    }
    // The sealed frame adds an 8-byte counter and a 16-byte authentication tag.
    if (encodeRequest(request).byteLength + 24 > MAX_PAYLOAD) {
      setError(t("editInput.tooLarge"))
      return
    }
    inFlight.current = true
    setSending(true)
    setError("")
    try {
      // A running turn can take time to stop before the host starts its replacement.
      const result = await link.rpc(request, { timeoutMs: 70_000, dropOnTimeout: false })
      if (result.ok) onClose(seq)
      else setError(editError(result))
    } catch (cause) {
      setError(linkError(cause))
    } finally {
      inFlight.current = false
      setSending(false)
    }
  }

  return (
    <form className="ml-auto flex w-full max-w-md flex-col gap-2 rounded-2xl border border-border bg-secondary p-3"
      aria-label={t("editInput.title")}
      onKeyDown={(event) => {
        if (event.key !== "Escape" || event.nativeEvent.isComposing || inFlight.current) return
        event.stopPropagation()
        onClose(seq)
      }}
      onSubmit={(event) => { event.preventDefault(); void submit() }}>
      {original?.quotes.map((quote, i) => (
        <p key={i} className="line-clamp-2 whitespace-pre-wrap wrap-break-word text-xs text-muted-foreground">
          {t("quote.selected")}: {quote}
        </p>
      ))}
      {original?.hasImages ? <p className="text-xs text-muted-foreground">{t("editInput.keepImages")}</p> : null}
      {original ? <Textarea
        aria-label={t("editInput.title")}
        className="min-h-20 text-base"
        rows={3}
        value={draft}
        readOnly={sending}
        onChange={(event) => setDraft(event.target.value)}
      /> : !error ? <p role="status" className="text-sm text-muted-foreground">{t("editInput.loading")}</p> : null}
      {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
      <p className="text-xs text-muted-foreground">{t("editInput.rewind")}</p>
      <div className="flex flex-wrap justify-end gap-2">
        <Button type="button" variant="outline" className="min-h-11" onClick={() => onClose(seq)} disabled={sending}>{t("editInput.cancel")}</Button>
        <Button type="submit" className="min-h-11" disabled={disabled || sending || !ready}>
          {t(sending ? "editInput.sending" : "editInput.resend")}
        </Button>
      </div>
    </form>
  )
}
