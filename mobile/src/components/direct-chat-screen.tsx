import { useEffect, useRef, useState } from "react"
import { ChevronLeft } from "lucide-react"

import { Composer, type ComposerExtra } from "@/components/composer"
import { HostChrome } from "@/components/host-chrome"
import { PhoneMarkdown } from "@/components/markdown"
import { ThreadLog } from "@/components/thread-blocks"
import { Button } from "@/components/ui/button"
import {
  AttachmentTooBig,
  readComposerFiles,
  turnsFromMessages,
} from "@/lib/attachments"
import { findProvider, mintID, providerChoices, type DirectProvider } from "@/lib/direct-provider"
import {
  loadThreads,
  newThread,
  saveThreads,
  threadTitle,
  type DirectMessage,
  type DirectThread,
} from "@/lib/direct-threads"
import { t } from "@/lib/i18n"
import type { CompactBlock } from "@/lib/transcript"
import { streamCompletion } from "@/lib/openai-client"
import { ModelCallError, redact } from "@/lib/openai-wire"
import type { SavedLink } from "@/lib/store"
import { timeAgo } from "@/lib/when"

const thinking = ["low", "medium", "high"]

/** A conversation that lives on the phone. Same composer as a PC thread:
 *  model, thinking level, image or file. The call does not go through the PC. */
export function DirectChatScreen({
  hosts,
  activeFingerprint,
  path,
  connected,
  reconnecting,
  providers,
  onSelectHost,
  onAddHost,
  onUnlink,
  onToggleLocale,
  onModels,
  onOpenChange,
  onBindClose,
  complete = streamCompletion,
}: {
  hosts: SavedLink[]
  activeFingerprint: string
  path: string
  connected?: boolean
  reconnecting?: boolean
  providers: DirectProvider[]
  onSelectHost: (fingerprint: string) => void
  onAddHost: () => void
  onUnlink: () => void
  onToggleLocale?: () => void
  onModels: () => void
  onOpenChange?: (open: boolean) => void
  onBindClose?: (close: () => void) => void
  complete?: typeof streamCompletion
}) {
  const [threads, setThreads] = useState(loadThreads)
  const [openID, setOpenID] = useState<string | null>(null)
  const [pending, setPending] = useState(false)
  const [notice, setNotice] = useState("")
  const [pick, setPick] = useState(() => firstPick(providers, loadThreads()[0]))
  const stopRef = useRef<AbortController | null>(null)
  const busyRef = useRef(false)
  const open = threads.find((row) => row.id === openID) ?? null
  const models = providerChoices(providers)

  useEffect(() => {
    onOpenChange?.(Boolean(openID))
  }, [openID, onOpenChange])

  useEffect(() => {
    onBindClose?.(() => {
      stopRef.current?.abort("stop")
      setOpenID(null)
    })
  }, [onBindClose])

  useEffect(() => {
    return () => {
      stopRef.current?.abort("stop")
    }
  }, [])

  const remember = (next: DirectThread[]) => {
    setThreads(next)
    saveThreads(next)
  }

  const stop = () => {
    stopRef.current?.abort("stop")
  }

  const leave = () => {
    stop()
    setOpenID(null)
  }

  const send = async (text: string, extra?: ComposerExtra) => {
    if (busyRef.current) throw new Error("busy")
    const providerId = extra?.providerId || pick.providerId
    const model = extra?.model || pick.model
    const reasoning = extra?.reasoning ?? pick.reasoning
    const provider = findProvider(providers, providerId)
    if (!provider || !model) throw new Error("no-model")
    busyRef.current = true
    setPending(true)
    const ctrl = new AbortController()
    stopRef.current = ctrl
    let committed = false
    try {
      const files = [...(extra?.images ?? []), ...(extra?.files ?? [])]
      let chips: DirectMessage["attachments"]
      try {
        chips = (await readComposerFiles(files)).chips
        setNotice("")
      } catch (err) {
        if (err instanceof AttachmentTooBig) setNotice(t("chat.tooBig"))
        throw err
      }
      if (ctrl.signal.aborted) return
      const user: DirectMessage = {
        id: mintID("m"),
        role: "user",
        text,
        attachments: chips,
      }
      const assistant: DirectMessage = { id: mintID("m"), role: "assistant", text: "" }
      const prev = open
      const base: DirectThread = prev
        ? { ...prev, messages: prev.messages.slice() }
        : newThread({ providerId, model, reasoning })
      base.title = base.title || threadTitle(text, chips?.[0]?.name ?? "") || t("chat.tab")
      base.providerId = providerId
      base.model = model
      base.reasoning = reasoning
      base.updatedAt = Date.now()
      base.messages = [...base.messages, user, assistant]
      const next = [base, ...threads.filter((row) => row.id !== base.id)]
      remember(next)
      setOpenID(base.id)
      setPick({ providerId, model, reasoning })
      committed = true
      const latest: DirectMessage = { ...assistant }
      const patch = (message: DirectMessage) => {
        setThreads((cur) => {
          const updated = cur.map((row) =>
            row.id !== base.id
              ? row
              : {
                  ...row,
                  messages: row.messages.map((item) => (item.id === assistant.id ? message : item)),
                },
          )
          saveThreads(updated)
          return updated
        })
      }
      try {
        // Chips already carry the bytes. Rebuilding from them sends a file once.
        const history = turnsFromMessages(base.messages.filter((item) => item.id !== assistant.id))
        await complete({
          provider,
          model,
          reasoning,
          turns: history,
          signal: ctrl.signal,
          onDelta: (piece) => {
            latest.text = piece.text
            latest.reasoning = piece.reasoning
            patch({ ...latest })
          },
        })
      } catch (err) {
        if (err instanceof ModelCallError && err.kind === "abort") return
        patch({ ...latest, error: explain(err, provider.apiKey) })
      }
    } catch (err) {
      if (!committed) throw err
    } finally {
      if (stopRef.current === ctrl) stopRef.current = null
      busyRef.current = false
      setPending(false)
    }
  }

  return (
    <main className="mx-auto flex h-full max-w-lg flex-col overflow-hidden">
      {open ? (
        <header className="flex h-12 shrink-0 items-center gap-1 border-b border-border px-1">
          <Button
            variant="ghost"
            className="size-10 shrink-0 px-0"
            aria-label={t("thread.back")}
            onClick={leave}
          >
            <ChevronLeft className="size-5" aria-hidden />
          </Button>
          <h1 className="min-w-0 flex-1 truncate text-sm font-medium">{open.title || t("chat.tab")}</h1>
          {pending ? (
            <Button variant="ghost" className="h-8 shrink-0 px-2 text-destructive" onClick={stop}>
              {t("thread.stop")}
            </Button>
          ) : null}
        </header>
      ) : (
        <HostChrome
          hosts={hosts}
          activeFingerprint={activeFingerprint}
          path={path}
          connected={connected}
          reconnecting={reconnecting}
          showChat
          chatSelected
          onSelect={onSelectHost}
          onSelectChat={() => setOpenID(null)}
          onAdd={onAddHost}
          onNewChat={() => setOpenID(null)}
          onUnlink={onUnlink}
          onModels={onModels}
          onToggleLocale={onToggleLocale}
        />
      )}
      {open ? (
        <div data-testid="transcript" className="min-h-0 flex-1 overflow-y-auto px-3 py-3">
          <div className="flex flex-col gap-3">
            {open.messages.map((message) => (
              <MessageRow key={message.id} message={message} live={pending && message.id === open.messages.at(-1)?.id} />
            ))}
          </div>
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto px-3 py-3">
          {threads.length === 0 ? (
            <div className="px-1">
              <p className="text-sm font-medium">{t("chat.emptyTitle")}</p>
              <p className="pt-1 text-[13px] text-muted-foreground">{t("chat.emptyHint")}</p>
            </div>
          ) : (
            <ul className="flex flex-col gap-2">
              {threads.map((row) => (
                <li key={row.id}>
                  <button
                    type="button"
                    className="flex w-full flex-col rounded-xl bg-muted px-3 py-2 text-left"
                    aria-label={t("home.open", { title: row.title || t("chat.tab") })}
                    onClick={() => {
                      setPick({
                        providerId: row.providerId || pick.providerId,
                        model: row.model || pick.model,
                        reasoning: row.reasoning,
                      })
                      setOpenID(row.id)
                    }}
                  >
                    <span className="truncate text-sm font-medium">{row.title || t("chat.tab")}</span>
                    <span className="truncate text-xs text-muted-foreground">
                      {[row.model, timeAgo(new Date(row.updatedAt).toISOString())].filter(Boolean).join(" · ")}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
          {models.length === 0 ? (
            <p className="px-1 pt-3 text-[13px] text-muted-foreground">{t("chat.needModel")}</p>
          ) : null}
        </div>
      )}
      <Composer
        key={openID ?? "new"}
        label={t("thread.message")}
        sendLabel={t("thread.send")}
        disabled={models.length === 0}
        pending={pending}
        hint={notice || undefined}
        models={models}
        reasoningLevels={models.length ? thinking : []}
        providerId={open?.providerId || pick.providerId}
        model={open?.model || pick.model}
        reasoning={open?.reasoning ?? pick.reasoning}
        onTune={(next) => {
          setPick(next)
          if (!open) return
          const updated = threads.map((row) =>
            row.id === open.id
              ? { ...row, providerId: next.providerId, model: next.model, reasoning: next.reasoning }
              : row,
          )
          remember(updated)
        }}
        onSubmit={(text, extra) => send(text, extra)}
      />
    </main>
  )
}

function MessageRow({ message, live }: { message: DirectMessage; live: boolean }) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="max-w-[85%] rounded-2xl bg-muted px-3 py-2">
          <AttachmentChips items={message.attachments} />
          {message.text ? <PhoneMarkdown text={message.text} /> : null}
        </div>
      </div>
    )
  }
  return (
    <div className="min-w-0 max-w-full">
      <AttachmentChips items={message.attachments} />
      <ThreadLog blocks={assistantBlocks(message, live)} running={live && !message.error} />
      {message.error ? (
        <p className="text-sm text-destructive" role="alert">
          {message.error}
        </p>
      ) : null}
    </div>
  )
}

function assistantBlocks(message: DirectMessage, live: boolean): CompactBlock[] {
  const blocks: CompactBlock[] = []
  const thought = message.reasoning ?? ""
  const thinking = live && !message.text && !message.error
  if (thought || thinking) {
    blocks.push({
      id: `${message.id}:thought`,
      kind: "reasoning",
      text: thought,
      streaming: thinking,
    })
  }
  if (message.text) {
    blocks.push({
      id: `${message.id}:answer`,
      kind: "answer",
      text: message.text,
      streaming: live && !message.error,
    })
  }
  return blocks
}

function AttachmentChips({ items }: { items: DirectMessage["attachments"] }) {
  if (!items?.length) return null
  return (
    <ul className="mb-1 flex flex-col gap-1">
      {items.map((item) => (
        <li key={item.name} className="truncate text-xs text-muted-foreground">
          {item.name}
        </li>
      ))}
    </ul>
  )
}

function firstPick(
  providers: DirectProvider[],
  thread: DirectThread | undefined,
): { providerId: string; model: string; reasoning: string } {
  if (thread?.model) {
    return { providerId: thread.providerId, model: thread.model, reasoning: thread.reasoning }
  }
  const choice = providerChoices(providers)[0]
  return {
    providerId: choice?.provider_id ?? "",
    model: choice?.model ?? "",
    reasoning: "",
  }
}

function explain(err: unknown, secret: string): string {
  if (err instanceof AttachmentTooBig) return t("chat.tooBig")
  if (err instanceof ModelCallError) {
    if (err.kind === "timeout") return t("chat.timedOut")
    if (err.kind === "abort") return ""
    const message = redact(err.message, secret)
    if (message === "network" || message === "parse") return t("chat.quiet")
    if (!message || message === "bad-url") {
      return t("chat.failed")
    }
    return message
  }
  return t("chat.failed")
}
