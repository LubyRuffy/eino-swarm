import { useCallback, useEffect, useRef, useState } from "react"

import { AddHostSheet } from "@/components/add-host-sheet"
import type { ComposerExtra } from "@/components/composer"
import { DirectChatScreen } from "@/components/direct-chat-screen"
import { HomeScreen } from "@/components/home-screen"
import { ProviderSheet } from "@/components/provider-sheet"
import { LinkBanner } from "@/components/link-banner"
import { NewChatScreen } from "@/components/new-chat-screen"
import { ScanScreen } from "@/components/scan-screen"
import { ThreadScreen } from "@/components/thread-screen"
import { UpdateNotice } from "@/components/update-notice"
import {
  bindFromURI,
  bindError,
  linkFromBind,
  type DeviceLink,
  LinkFault,
  linkError,
  remoteError,
} from "@/lib/client"
import { openLink, type RemoteLink } from "@/lib/link"
import { getLocale, t, toggleLocale } from "@/lib/i18n"
import { emptyInbox, inboxThreads, reduceInbox, type InboxGroupState } from "@/lib/inbox-window"
import type {
  ModelChoice,
  ProjectView,
  RemoteResponse,
  RunningView,
  ThreadView,
} from "@/lib/rpc"
import {
  OpAnswer,
  OpCancelWait,
  OpCatalog,
  OpList,
  OpLog,
  OpMore,
  OpOpen,
  OpReady,
  OpResumeGoal,
  OpRunNow,
  OpFollowupDrop,
  OpFollowupSteer,
  OpPreempt,
  OpSend,
  OpStart,
  OpSteer,
  OpStop,
  OpTune,
  OpUnwatch,
  OpWatch,
} from "@/lib/rpc"
import { androidBackLayer, installAndroidBack } from "@/lib/android-back"
import { loadProviders, type DirectProvider } from "@/lib/direct-provider"
import { phoneShell } from "@/lib/phone-shell"
import { pickResumeThread, detailFromListing, rosterFingerprint } from "@/lib/resume"
import {
  applyOpenDetail,
  applyPush,
  emptyView,
  markRunning,
  openView,
  prependOlder,
  type PhoneView,
} from "@/lib/session"
import {
  clearLastThreadId,
  clearLink,
  loadActiveFingerprint,
  loadLastThreadId,
  loadSavedLink,
  loadSavedLinks,
  removeLink,
  saveActiveFingerprint,
  saveHostLabel,
  saveLastThreadId,
  saveLink,
} from "@/lib/store"
import { sendPhoneQueue, sendPhoneTurn } from "@/lib/phone-turn"
import { sendComposed } from "@/lib/turn-send"

export function App() {
  const [locale, setLocaleTick] = useState(getLocale())
  const [hosts, setHosts] = useState(() => loadSavedLinks())
  const [providers, setProviders] = useState(() => loadProviders())
  const [surface, setSurface] = useState<"hosts" | "chat">(() =>
    loadProviders().length > 0 && loadSavedLinks().length === 0 ? "chat" : "hosts",
  )
  const [modelsOpen, setModelsOpen] = useState(false)
  const [activeFp, setActiveFp] = useState(
    () => loadActiveFingerprint() || loadSavedLinks()[0]?.fingerprint || "",
  )
  const [adding, setAdding] = useState(false)
  const [composing, setComposing] = useState(false)
  const [composeProject, setComposeProject] = useState("")
  const [addError, setAddError] = useState<string>()
  const [link, setLink] = useState<RemoteLink | null>(null)
  const [busy, setBusy] = useState(() => loadSavedLinks().length > 0)
  const [bindBusy, setBindBusy] = useState(false)
  const [reconnecting, setReconnecting] = useState(false)
  const [error, setError] = useState<string>()
  const [projects, setProjects] = useState<ProjectView[]>([])
  const [threads, setThreads] = useState<ThreadView[]>([])
  const [running, setRunning] = useState<RunningView[]>([])
  const [more, setMore] = useState(false)
  const [groups, setGroups] = useState<InboxGroupState[]>([])
  const [loadingGroup, setLoadingGroup] = useState("")
  const [loadingMore, setLoadingMore] = useState(false)
  const [models, setModels] = useState<ModelChoice[]>([])
  const [levels, setLevels] = useState<string[]>([])
  const [catalogBusy, setCatalogBusy] = useState(false)
  const [composerPending, setComposerPending] = useState(false)
  const [view, setView] = useState<PhoneView>(emptyView())
  const [loadingOlder, setLoadingOlder] = useState(false)
  const inboxRef = useRef(emptyInbox())
  const moreBusy = useRef(false)
  const linkRef = useRef<RemoteLink | null>(null)
  const viewRef = useRef<PhoneView>(view)
  const addingRef = useRef(false)
  const composingRef = useRef(false)
  const resumedRef = useRef(false)
  const olderBusy = useRef(false)
  const loadGen = useRef(0)
  const recoveringRef = useRef(false)
  const unlinkingRef = useRef(false)
  const bindGen = useRef(0)
  const modelsRef = useRef(false)
  const surfaceRef = useRef(surface)
  const directOpenRef = useRef(false)
  const directCloseRef = useRef<() => void>(() => undefined)
  const recoverAttempt = useRef(0)
  const retryTimer = useRef(0)
  const rosterFp = useRef("")
  const recoverRef = useRef<(force?: boolean) => Promise<void>>(async () => undefined)
  linkRef.current = link
  viewRef.current = view
  addingRef.current = adding
  composingRef.current = composing
  modelsRef.current = modelsOpen
  surfaceRef.current = surface

  const commitView = (next: PhoneView) => {
    viewRef.current = next
    setView(next)
  }

  const takeHostName = (resp: RemoteResponse) => {
    const name = resp.host?.trim()
    if (!name) return
    const fp = loadActiveFingerprint()
    if (!fp) return
    saveHostLabel(fp, name)
    setHosts(loadSavedLinks())
  }

  const resetRoster = () => {
    inboxRef.current = emptyInbox()
    rosterFp.current = ""
    moreBusy.current = false
    setLoadingMore(false)
    setThreads([])
    setProjects([])
    setRunning([])
    setMore(false)
    setGroups([])
    setLoadingGroup("")
    setModels([])
    setLevels([])
  }

  const applyList = (resp: RemoteResponse, append: boolean, target?: RemoteLink, group?: string) => {
    if (!resp.ok) {
      setError(remoteError(resp.error || resp.code || ""))
      return
    }
    // Head is page one. Tail is what More already loaded. A quiet poll
    // must not put those rows back, and a row that left page one is not
    // kept just because it was there last time.
    const next = reduceInbox(inboxRef.current, resp, append ? "append" : "replace", group)
    const merged = inboxThreads(next)
    const groupSig = next.groups.map((g) => `${g.id}:${g.more ? 1 : 0}:${g.cursor}`).join(",")
    const fp = rosterFingerprint(next.projects, merged, next.running, next.more, `${next.cursor}|${groupSig}`)
    const cur = target ?? linkRef.current
    if (resp.path && cur) cur.path = resp.path
    takeHostName(resp)
    if (!append && fp === rosterFp.current) return
    rosterFp.current = fp
    inboxRef.current = next
    setError(undefined)
    setProjects(next.projects)
    setThreads(merged)
    setRunning(next.running)
    setMore(next.more)
    setGroups(next.groups)
  }

  const attachPush = (next: RemoteLink) => {
    next.onPush = (resp) => {
      if (resp.path) next.path = resp.path
      commitView(applyPush(viewRef.current, resp))
    }
    next.onDisconnect = (err) => {
      if (linkRef.current !== next || unlinkingRef.current) return
      setError(linkError(err))
      void recoverRef.current()
    }
    void next.announceDevice().then((resp) => {
      if (resp) takeHostName(resp)
    })
  }

  const openThreadOn = async (target: RemoteLink, id: string) => {
    const staying = viewRef.current.threadId === id && Boolean(viewRef.current.detail)
    // A cleared threadId means Back already left. A check that only bails
    // when some other id is current treats "left" as still here, and the
    // late open paints the conversation back over the inbox.
    const stillThisThread = () => viewRef.current.threadId === id
    try {
      const r = await target.rpc({ op: OpOpen, thread_id: id })
      if (!stillThisThread()) return false
      if (!r.detail) {
        setError(r.error ? remoteError(r.error) : t("err.open"))
        if (id === loadLastThreadId()) clearLastThreadId()
        if (!staying) viewRef.current = emptyView()
        return false
      }
      saveLastThreadId(id)
      loadGen.current += 1
      olderBusy.current = false
      setLoadingOlder(false)
      commitView(applyOpenDetail(viewRef.current, r.detail))
      const w = await target.rpc({ op: OpWatch, thread_id: id })
      if (!stillThisThread()) return false
      if (!w.ok) {
        setError(w.error || w.code ? remoteError(w.error || w.code || "") : t("err.watch"))
        if (!staying) viewRef.current = emptyView()
        return false
      }
      commitView(applyPush(viewRef.current, { ...w, op: w.op || OpReady }))
      return true
    } catch (e) {
      if (!stillThisThread()) return false
      setError(linkError(e))
      if (!staying) viewRef.current = emptyView()
      return false
    }
  }

  const consumeFirstList = async (target: RemoteLink, resp: RemoteResponse) => {
    applyList(resp, false, target)
    if (!resp.ok || resumedRef.current || viewRef.current.detail) return
    if (surfaceRef.current === "chat") return
    const id = pickResumeThread(loadLastThreadId(), resp.running ?? [], resp.threads ?? [])
    if (!id) return
    commitView(openView(detailFromListing(id, resp.running ?? [], resp.threads ?? [])))
    if (await openThreadOn(target, id)) resumedRef.current = true
  }

  const recover = async (force = false) => {
    if (unlinkingRef.current || recoveringRef.current) return
    if (!force && linkRef.current?.alive()) return
    const saved = loadSavedLink()
    if (!saved) return
    const gen = bindGen.current
    recoveringRef.current = true
    setReconnecting(true)
    try {
      let next = linkRef.current
      if (force || !next?.alive()) {
        next = await openLink(saved)
        if (gen !== bindGen.current) {
          next.close()
          return
        }
        attachPush(next)
        const prev = linkRef.current
        linkRef.current = next
        setLink(next)
        if (prev && prev !== next) prev.close()
      }
      if (gen !== bindGen.current) return
      recoverAttempt.current = 0
      setError(undefined)
      const id = viewRef.current.threadId
      if (id) await openThreadOn(next, id)
      else await consumeFirstList(next, await next.rpc({ op: OpList }))
    } catch (e) {
      if (gen !== bindGen.current) return
      setError(linkError(e))
      recoverAttempt.current += 1
      const delay = Math.min(15_000, 1000 * 2 ** Math.min(recoverAttempt.current - 1, 4))
      window.clearTimeout(retryTimer.current)
      retryTimer.current = window.setTimeout(() => {
        void recoverRef.current()
      }, delay)
    } finally {
      recoveringRef.current = false
      setReconnecting(false)
    }
  }
  recoverRef.current = recover

  const boot = useCallback(async () => {
    const saved = loadSavedLink()
    if (!saved || unlinkingRef.current) {
      setBusy(false)
      return
    }
    const gen = bindGen.current
    setBusy(true)
    try {
      const next = await openLink(saved)
      if (gen !== bindGen.current) {
        next.close()
        return
      }
      attachPush(next)
      linkRef.current = next
      setLink(next)
      await consumeFirstList(next, await next.rpc({ op: OpList }))
    } catch (e) {
      if (gen !== bindGen.current) return
      setError(bindError(e))
      window.clearTimeout(retryTimer.current)
      retryTimer.current = window.setTimeout(() => {
        void recoverRef.current()
      }, 1000)
    } finally {
      if (gen === bindGen.current) setBusy(false)
    }
  }, [])

  useEffect(() => {
    void boot()
    return () => {
      window.clearTimeout(retryTimer.current)
      linkRef.current?.close()
    }
  }, [boot])

  useEffect(() => {
    const wake = () => {
      if (document.visibilityState && document.visibilityState !== "visible") return
      if (!loadSavedLink() || linkRef.current?.alive()) return
      window.clearTimeout(retryTimer.current)
      void recoverRef.current()
    }
    document.addEventListener("visibilitychange", wake)
    window.addEventListener("online", wake)
    window.addEventListener("pageshow", wake)
    return () => {
      document.removeEventListener("visibilitychange", wake)
      window.removeEventListener("online", wake)
      window.removeEventListener("pageshow", wake)
    }
  }, [])

  useEffect(() => {
    if (!link || view.detail) return
    const id = window.setInterval(() => {
      if (!link.alive()) return
      void link
        .rpc({ op: OpList })
        .then((r) => applyList(r, false))
        .catch((e) => setError(linkError(e)))
    }, 2000)
    return () => window.clearInterval(id)
  }, [link, view.detail])

  useEffect(() => {
    if (!link?.alive()) return
    let gone = false
    setCatalogBusy(true)
    void link
      .rpc({ op: OpCatalog })
      .then((r) => {
        if (gone || linkRef.current !== link || !r.ok) return
        setModels(r.models ?? [])
        setLevels(r.reasoning_levels ?? [])
      })
      .catch(() => undefined)
      .finally(() => {
        if (!gone) setCatalogBusy(false)
      })
    return () => {
      gone = true
    }
  }, [link])

  const onURI = async (uri: string) => {
    const isAdd = adding
    setBindBusy(true)
    if (isAdd) setAddError(undefined)
    else setError(undefined)
    let next: DeviceLink | undefined
    let attached = false
    try {
      const bound = await bindFromURI(uri)
      next = linkFromBind(bound)
      await next.connect(bound.saved.hubURL, bound.saved.ticket)
      bindGen.current += 1
      const gen = bindGen.current
      window.clearTimeout(retryTimer.current)
      saveLink(bound.saved)
      const prev = linkRef.current
      if (prev && prev !== next) {
        prev.close()
        linkRef.current = null
      }
      setLink(null)
      resumedRef.current = false
      loadGen.current += 1
      resetRoster()
      commitView(emptyView())
      setHosts(loadSavedLinks())
      setActiveFp(bound.saved.fingerprint)
      setAdding(false)
      setAddError(undefined)
      setBusy(true)
      if (gen !== bindGen.current) {
        next.close()
        return
      }
      attachPush(next)
      attached = true
      linkRef.current = next
      setLink(next)
      await consumeFirstList(next, await next.rpc({ op: OpList }))
      if (gen === bindGen.current) setBusy(false)
    } catch (e) {
      if (!attached) next?.close()
      const msg = bindError(e)
      if (isAdd) setAddError(msg)
      else setError(msg)
    } finally {
      setBindBusy(false)
    }
  }

  const flipLocale = () => {
    toggleLocale()
    setLocaleTick(getLocale())
  }

  const fail = (e: unknown) => setError(linkError(e))

  const openThread = async (id: string) => {
    const target = linkRef.current
    if (!target?.alive()) {
      setError(linkError(new LinkFault("offline", "network")))
      void recover()
      return
    }
    if (viewRef.current.threadId !== id) {
      commitView(openView(detailFromListing(id, running, threads)))
    }
    await openThreadOn(target, id)
  }

  const startConversation = async (text: string, projectId: string, extra?: ComposerExtra) => {
    const target = linkRef.current
    if (!target?.alive()) {
      setError(linkError(new LinkFault("offline", "network")))
      void recover()
      throw new Error("offline")
    }
    setComposerPending(true)
    try {
      const r = await sendComposed(
        (req) => target.rpc(req),
        {
          op: OpStart,
          text,
          project_id: projectId || undefined,
        },
        extra,
      )
      const started = r.ok ? r.threads?.[0] : undefined
      // A refusal has to leave the compose screen up: the text the user
      // typed only exists in that box.
      if (!started) {
        setError(r.error || r.code ? remoteError(r.error || r.code || "") : t("err.start"))
        throw new Error("refused")
      }
      setComposing(false)
      setComposeProject("")
      await openThread(started.id)
      applyList(await target.rpc({ op: OpList }), false)
    } catch (e) {
      if (e instanceof Error && (e.message === "refused" || e.message === "offline")) throw e
      fail(e)
      throw e
    } finally {
      setComposerPending(false)
    }
  }

  const turnDeps = {
    link: () => linkRef.current,
    view: () => viewRef.current,
    commit: commitView,
    fail,
    setError,
    setPending: setComposerPending,
  }
  const queueFollowup = (op: string, threadId: string, followupId?: string) =>
    sendPhoneQueue(turnDeps, op, threadId, followupId)
  const postTurn = (op: string, threadId: string, text: string, extra?: ComposerExtra) =>
    sendPhoneTurn(turnDeps, op, threadId, text, extra)

  const tuneThread = async (
    threadId: string,
    next: { providerId: string; model: string; reasoning: string },
  ) => {
    const target = linkRef.current
    if (!target) return
    try {
      const r = await target.rpc({
        op: OpTune,
        thread_id: threadId,
        provider_id: next.providerId,
        model: next.model,
        reasoning: next.reasoning,
      })
      if (!r.ok) {
        setError(remoteError(r.error || r.code || ""))
        return
      }
      const cur = viewRef.current
      if (cur.detail?.id !== threadId) return
      commitView({
        ...cur,
        detail: {
          ...cur.detail,
          provider_id: next.providerId,
          model: next.model,
          reasoning: next.reasoning,
        },
      })
    } catch (e) {
      fail(e)
    }
  }

  const loadMore = async (group?: string) => {
    const target = linkRef.current
    if (!target?.alive() || moreBusy.current) return
    const section = group ? inboxRef.current.groups.find((g) => g.id === group) : undefined
    if (group) {
      if (!section?.more) return
    } else if (!inboxRef.current.more) {
      return
    }
    moreBusy.current = true
    if (group) setLoadingGroup(group)
    else setLoadingMore(true)
    try {
      applyList(
        await target.rpc({
          op: OpMore,
          cursor: group ? section?.cursor : inboxRef.current.cursor,
          group,
        }),
        true,
        undefined,
        group,
      )
    } catch (e) {
      fail(e)
    } finally {
      moreBusy.current = false
      setLoadingMore(false)
      setLoadingGroup("")
    }
  }

  const loadOlder = async () => {
    const target = linkRef.current
    if (!target || olderBusy.current) return
    const cur = viewRef.current
    const before = cur.oldestSeq > 0 ? cur.oldestSeq : cur.lastSeq
    if (!cur.hasMore || !cur.threadId) return
    const gen = loadGen.current
    const threadId = cur.threadId
    olderBusy.current = true
    setLoadingOlder(true)
    try {
      const r = await target.rpc({
        op: OpLog,
        thread_id: threadId,
        before,
      })
      if (loadGen.current !== gen || viewRef.current.threadId !== threadId) return
      if (!r.ok) {
        setError(remoteError(r.error || r.code || ""))
        return
      }
      commitView(prependOlder(viewRef.current, r.events ?? [], Boolean(r.more), r.seq ?? 0))
    } catch (e) {
      fail(e)
    } finally {
      if (loadGen.current === gen) {
        olderBusy.current = false
        setLoadingOlder(false)
      }
    }
  }

  const closeThread = async () => {
    const id = viewRef.current.threadId
    loadGen.current += 1
    olderBusy.current = false
    setLoadingOlder(false)
    commitView(emptyView())
    const target = linkRef.current
    if (!target?.alive()) return
    try {
      if (id) await target.rpc({ op: OpUnwatch, thread_id: id })
      applyList(await target.rpc({ op: OpList }), false)
    } catch (e) {
      fail(e)
    }
  }

  const backRef = useRef<() => boolean>(() => false)
  backRef.current = () => {
    const layer = androidBackLayer({
      sheet: addingRef.current || modelsRef.current,
      compose: composingRef.current && surfaceRef.current !== "chat",
      thread: Boolean(viewRef.current.detail) && surfaceRef.current !== "chat",
      chat: surfaceRef.current === "chat" && directOpenRef.current,
    })
    if (layer === "sheet") {
      if (modelsRef.current) {
        modelsRef.current = false
        setModelsOpen(false)
        return true
      }
      addingRef.current = false
      setAdding(false)
      setAddError(undefined)
      return true
    }
    if (layer === "chat") {
      directOpenRef.current = false
      directCloseRef.current()
      return true
    }
    if (layer === "compose") {
      composingRef.current = false
      setComposing(false)
      setComposeProject("")
      return true
    }
    if (layer === "thread") {
      void closeThread()
      return true
    }
    return false
  }

  useEffect(() => installAndroidBack(() => backRef.current()), [])

  const unlink = () => {
    bindGen.current += 1
    unlinkingRef.current = true
    window.clearTimeout(retryTimer.current)
    const cur = linkRef.current
    if (cur) {
      void cur.rpc({ op: OpUnwatch }).catch(() => undefined)
      cur.close()
    }
    const fp = loadSavedLink()?.fingerprint
    if (fp) removeLink(fp)
    else clearLink()
    const rest = loadSavedLinks()
    setHosts(rest)
    resumedRef.current = false
    loadGen.current += 1
    linkRef.current = null
    setLink(null)
    resetRoster()
    setError(undefined)
    setAddError(undefined)
    setBindBusy(false)
    setReconnecting(false)
    setAdding(false)
    setComposing(false)
    setComposeProject("")
    commitView(emptyView())
    unlinkingRef.current = false
    if (rest[0]) {
      saveActiveFingerprint(rest[0].fingerprint)
      setActiveFp(rest[0].fingerprint)
      setBusy(true)
      void boot()
      return
    }
    setActiveFp("")
    setBusy(false)
  }

  const selectHost = (fp: string) => {
    setSurface("hosts")
    if (fp === activeFp && linkRef.current?.alive()) return
    bindGen.current += 1
    window.clearTimeout(retryTimer.current)
    linkRef.current?.close()
    linkRef.current = null
    setLink(null)
    resumedRef.current = false
    loadGen.current += 1
    resetRoster()
    setError(undefined)
    setReconnecting(false)
    commitView(emptyView())
    saveActiveFingerprint(fp)
    setActiveFp(fp)
    setBusy(true)
    void boot()
  }

  const commitProviders = (next: DirectProvider[]) => {
    const appeared = providers.length === 0 && next.length > 0
    setProviders(next)
    if (next.length === 0) setSurface("hosts")
    else if (appeared) {
      setSurface("chat")
      setComposing(false)
    }
  }

  const openChat = () => {
    setSurface("chat")
    setComposing(false)
  }

  const modelSheet = modelsOpen ? (
    <ProviderSheet
      providers={providers}
      onClose={() => setModelsOpen(false)}
      onChange={commitProviders}
    />
  ) : null

  if (phoneShell(hosts.length, providers.length) === "scan") {
    return (
      <div className="flex h-full min-h-0 flex-col overflow-hidden">
        <UpdateNotice />
        <div className="min-h-0 flex-1">
          <ScanScreen
            key={locale}
            onURI={(uri) => void onURI(uri)}
            busy={bindBusy}
            error={error}
            onToggleLocale={flipLocale}
            onAddModel={() => setModelsOpen(true)}
          />
        </div>
        {modelSheet}
      </div>
    )
  }

  const sheet = adding ? (
    <AddHostSheet
      onURI={(uri) => void onURI(uri)}
      busy={bindBusy}
      error={addError}
      onClose={() => {
        setAdding(false)
        setAddError(undefined)
      }}
    />
  ) : null

  const banner = (
    <LinkBanner
      reconnecting={reconnecting}
      error={error}
      onRetry={() => void recover(true)}
    />
  )

  if (composing && surface !== "chat") {
    return (
      <div className="flex h-full min-w-0 flex-col overflow-hidden">
        <UpdateNotice />
        {link ? banner : null}
        <div className="screen-push flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          {/* Keyed on the locale only: switching PC re-picks the project,
              and remounting would also throw away the typed message. */}
          <NewChatScreen
            key={locale}
            hosts={hosts}
            activeFingerprint={activeFp}
            projects={projects}
            initialProject={composeProject}
            connected={Boolean(link?.alive())}
            connecting={busy || reconnecting}
            models={models}
            reasoningLevels={levels}
            catalogBusy={catalogBusy}
            pending={composerPending}
            onSelectHost={selectHost}
            onBack={() => {
              setComposing(false)
              setComposeProject("")
            }}
            onStart={(text, projectId, extra) => startConversation(text, projectId, extra)}
          />
        </div>
        {sheet}
        {modelSheet}
      </div>
    )
  }

  if (view.detail && link && surface !== "chat") {
    const detail = view.detail
    return (
      <div className="flex h-full min-w-0 flex-col overflow-hidden">
        <UpdateNotice />
        {banner}
        <div className="screen-push flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          <ThreadScreen
            key={locale + detail.id}
            detail={detail}
            blocks={view.blocks}
            followups={view.followups}
            onSteerFollowup={(id) => void queueFollowup(OpFollowupSteer, detail.id, id)}
            onDropFollowup={(id) => void queueFollowup(OpFollowupDrop, detail.id, id)}
            onInterrupt={() => void queueFollowup(OpPreempt, detail.id)}
            hasMore={view.hasMore}
            loadingOlder={loadingOlder}
            caughtUp={view.caughtUp}
            composerPending={composerPending}
            models={models}
            reasoningLevels={levels}
            catalogBusy={catalogBusy}
            onTune={(next) => void tuneThread(detail.id, next)}
            onBack={() => void closeThread()}
            onOlder={() => void loadOlder()}
            onSend={(text, extra) => postTurn(OpSend, detail.id, text, extra)}
            onSteer={(text, extra) => postTurn(OpSteer, detail.id, text, extra)}
            onStop={async () => {
              try {
                await link.rpc({ op: OpStop, thread_id: detail.id })
                const r = await link.rpc({ op: OpOpen, thread_id: detail.id })
                if (r.detail) commitView({ ...viewRef.current, detail: r.detail })
              } catch (e) {
                fail(e)
              }
            }}
            onAnswer={async (text) => {
              try {
                await link.rpc({ op: OpAnswer, thread_id: detail.id, text })
              } catch (e) {
                fail(e)
              }
            }}
            onAnswerStructured={async (callId, answers) => {
              try {
                await link.rpc({
                  op: OpAnswer,
                  thread_id: detail.id,
                  call_id: callId,
                  answers,
                })
              } catch (e) {
                fail(e)
              }
            }}
            onRunNow={async () => {
              try {
                await link.rpc({ op: OpRunNow, thread_id: detail.id })
                commitView(markRunning(viewRef.current))
              } catch (e) {
                fail(e)
              }
            }}
            onCancelWait={async () => {
              try {
                await link.rpc({ op: OpCancelWait, thread_id: detail.id })
                const r = await link.rpc({ op: OpOpen, thread_id: detail.id })
                if (r.detail) commitView({ ...viewRef.current, detail: r.detail })
              } catch (e) {
                fail(e)
              }
            }}
            onResumeGoal={async () => {
              try {
                await link.rpc({ op: OpResumeGoal, thread_id: detail.id })
                commitView(markRunning(viewRef.current))
              } catch (e) {
                fail(e)
              }
            }}
          />
        </div>
        {sheet}
        {modelSheet}
      </div>
    )
  }

  if (surface === "chat") {
    return (
      <div className="flex h-full min-w-0 flex-col overflow-hidden">
        <UpdateNotice />
        {link ? banner : null}
        <div className="min-h-0 flex-1">
          <DirectChatScreen
            key={locale}
            hosts={hosts}
            activeFingerprint={activeFp}
            path={link?.path ?? "relay"}
            connected={Boolean(link?.alive())}
            reconnecting={reconnecting}
            providers={providers}
            onSelectHost={selectHost}
            onAddHost={() => {
              setAddError(undefined)
              setAdding(true)
            }}
            onUnlink={unlink}
            onToggleLocale={flipLocale}
            onModels={() => setModelsOpen(true)}
            onOpenChange={(open) => {
              directOpenRef.current = open
            }}
            onBindClose={(close) => {
              directCloseRef.current = close
            }}
          />
        </div>
        {sheet}
        {modelSheet}
      </div>
    )
  }

  return (
    <div className="flex h-full min-w-0 flex-col overflow-hidden">
      <UpdateNotice />
      {link ? banner : null}
      <div className="min-h-0 flex-1">
        <HomeScreen
          key={locale}
          hosts={hosts}
          activeFingerprint={activeFp}
          projects={projects}
          threads={threads}
          running={running}
          more={more}
          loadingMore={loadingMore}
          groups={groups}
          loadingGroup={loadingGroup}
          path={link?.path ?? "relay"}
          connected={Boolean(link?.alive())}
          reconnecting={reconnecting}
          connecting={busy || reconnecting}
          error={!link ? error : undefined}
          onOpen={(id) => void openThread(id)}
          onNewChat={(projectId) => {
            setComposeProject(projectId)
            setComposing(true)
          }}
          onSelectHost={selectHost}
          onAddHost={() => {
            setAddError(undefined)
            setAdding(true)
          }}
          onRetry={() => void boot()}
          onRefresh={async () => {
            const target = linkRef.current
            if (!target?.alive()) {
              await recover(true)
              return
            }
            try {
              applyList(await target.rpc({ op: OpList }), false)
            } catch (e) {
              fail(e)
            }
          }}
          onMore={(group) => void loadMore(group)}
          onUnlink={unlink}
          onToggleLocale={flipLocale}
          showChat={providers.length > 0}
          onSelectChat={openChat}
          onModels={() => setModelsOpen(true)}
        />
      </div>
      {sheet}
      {modelSheet}
    </div>
  )
}
