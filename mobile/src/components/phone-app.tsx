import { usePhoneAccount } from "@/components/gateway-account-shell"
import { useEffect, useRef, useState, type ReactNode } from "react"
import { PcConnectionGate } from "@/components/pc-connection-gate"
import { EngineStartupBanner } from "@/components/engine-startup"
import { useEngineStartup } from "@/lib/use-engine-startup"
import { rosterGenerationReset } from "@/lib/roster-generation"
import { runPhoneAction } from "@/lib/phone-action"
import { AddHostSheet } from "@/components/add-host-sheet"
import type { ComposerExtra } from "@/components/composer"
import { ChatSurface } from "@/components/chat-surface"
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
import { type RemoteLink } from "@/lib/link"
import { sameLinkAuthority } from "@/lib/link-authority"
import { beginAnonymousPairing, gatewayUpgradePending } from "@/lib/gateway-link-upgrade"
import { usePhoneLinkLifecycle } from "@/lib/use-phone-link-lifecycle"
import { getLocale, t, toggleLocale } from "@/lib/i18n"
import { emptyInbox, inboxThreads, reduceInbox, type InboxGroupState, type InboxWindow } from "@/lib/inbox-window"
import type {
  ProjectView,
  RemoteResponse,
  RunningView,
  ThreadView,
} from "@/lib/rpc"
import { parseSlashSubmit } from "../../../frontend/src/lib/slash-core"
import {
  OpCancelWait,
  OpMore,
  OpOpen,
  OpResumeGoal,
  OpRunNow,
  OpFollowupDrop,
  OpFollowupSteer,
  OpDeliver,
  OpSend,
  OpStart,
  OpStop,
  OpTune,
  OpUnwatch,
} from "@/lib/rpc"
import { androidBackLayer, installAndroidBack } from "@/lib/android-back"
import { loadProviders, type DirectProvider } from "@/lib/direct-provider"
import { pcWebSelection } from "@/lib/pc-web-execution"
import { phoneShell } from "@/lib/phone-shell"
import { pickResumeThread, detailFromListing, rosterFingerprint } from "@/lib/resume"
import {
  applyPush,
  emptyView,
  openView,
  type PhoneView,
} from "@/lib/session"
import {
  clearLink,
  loadActiveFingerprint,
  loadLastThreadId,
  loadSavedLink,
  loadSavedLinks,
  removeLink,
  saveActiveFingerprint,
  saveHostLabel,
  saveLink,
  type SavedLink,
} from "@/lib/store"
import { mergeCompletion, readInbox, useCompletionRead } from "@/lib/completion-read"
import { useInboxRefresh } from "@/lib/inbox-refresh"
import { usePhoneVisibility } from "@/lib/phone-visibility"
import { openPhoneThread } from "@/lib/open-thread"
import { readOlderHistory } from "@/lib/read-older-history"
import { answerPhoneQuestion, continuePhoneTurn, deliverPhoneFollowup, sendPhoneQueue, sendPhoneTurn } from "@/lib/phone-turn"
import { sendComposed } from "@/lib/turn-send"
import { phonePrediction } from "@/lib/composer-prediction"
import { useModelCatalog } from "@/lib/use-model-catalog"
import { usePhoneSpeechSync } from "@/lib/phone-speech"
import { usePcModeSwitch } from "@/lib/use-pc-mode-switch"
import { orderSavedPCs } from "@/lib/phone-pcs"

export function PhoneApp() {
  const account = usePhoneAccount()
  const accountRef = useRef(account)
  accountRef.current = account
  const savedHosts = () => orderSavedPCs(loadSavedLinks().filter((row) => accountRef.current.allowsLink?.(row) ?? true), accountRef.current.pcs?.hosts, accountRef.current.pcs?.origin)
  const connectionError = (error: unknown) => linkError(error, Boolean(loadSavedLink()?.gatewayOwnerID))
  const [locale, setLocaleTick] = useState(getLocale())
  const [hosts, setHosts] = useState(() => savedHosts())
  const [personalProviders, setProviders] = useState(() => loadProviders())
  const providers = account.officialOnly ? account.providers : [...account.providers, ...personalProviders]
  const [surface, setSurface] = useState<"hosts" | "chat">(() =>
    !account.officialOnly && loadProviders().length > 0 && savedHosts().length === 0 ? "chat" : "hosts",
  )
  const [modelsOpen, setModelsOpen] = useState(false)
  const [activeFp, setActiveFp] = useState(
    () => savedHosts().find((row) => row.fingerprint === loadActiveFingerprint())?.fingerprint || savedHosts()[0]?.fingerprint || "",
  )
  const [adding, setAdding] = useState(false)
  const [composing, setComposing] = useState(false)
  const [composeProject, setComposeProject] = useState("")
  const [addError, setAddError] = useState<string>()
  const [link, setLink] = useState<RemoteLink | null>(null)
  usePcModeSwitch(link, account.bindPcModeSwitch)
  const [busy, setBusy] = useState(() => savedHosts().length > 0)
  const [bindBusy, setBindBusy] = useState(false)
  const [reconnecting, setReconnecting] = useState(false)
  const [error, setError] = useState<string>()
  const [projects, setProjects] = useState<ProjectView[]>([])
  const [threads, setThreads] = useState<ThreadView[]>([])
  const [attention, setAttention] = useState<ThreadView[]>([])
  const [running, setRunning] = useState<RunningView[]>([])
  const [more, setMore] = useState(false)
  const [groups, setGroups] = useState<InboxGroupState[]>([])
  const [loadingGroup, setLoadingGroup] = useState("")
  const [loadingMore, setLoadingMore] = useState(false)
  const startup = useEngineStartup(link)
  const { policy: catalogPolicy, models, levels, catalogBusy, refreshModels, refreshBusy, refreshError, webExecutionStatus, predictionsEnabled } = useModelCatalog(link, `${startup?.product_mode ?? ""}:${startup?.model_policy?.scope ?? ""}`)
  const speechHost = surface === "chat" && account.officialOnly ? undefined : hosts.find((host) => host.fingerprint === activeFp)
  usePhoneSpeechSync(speechHost && startup?.startup === "ready" ? link : null, speechHost, hosts, surface === "chat" && !account.officialOnly, providers)
  const currentPolicy = catalogPolicy?.scope === startup?.model_policy?.scope ? catalogPolicy : startup?.model_policy ?? catalogPolicy
  const hidePcModel = currentPolicy?.mode === "gateway" && !currentPolicy.capabilities?.capabilities.conversation?.configurable
  const restoring = startup?.startup === "restoring"
  const [composerPending, setComposerPending] = useState(false)
  const [view, setView] = useState<PhoneView>(emptyView())
  const [loadingOlder, setLoadingOlder] = useState(false)
  const managerVisible = useRef(true)
  const inboxRef = useRef(emptyInbox())
  const moreBusy = useRef(false)
  const linkRef = useRef<RemoteLink | null>(null)
  const viewRef = useRef<PhoneView>(view)
  const addingRef = useRef(false)
  const composingRef = useRef(false)
  const resumedRef = useRef(false)
  const olderBusy = useRef(false)
  const loadGen = useRef(0)
  const authorityRef = useRef<SavedLink | null>(null)
  const unlinkingRef = useRef(false)
  const bindGen = useRef(0)
  const activeFpRef = useRef(activeFp)
  activeFpRef.current = activeFp
  const modelsRef = useRef(false)
  const surfaceRef = useRef(surface)
  const directOpenRef = useRef(false)
  const directCloseRef = useRef<() => void>(() => undefined)
  const retryTimer = useRef(0)
  const rosterFp = useRef("")
  const recoverRef = useRef<(force?: boolean) => Promise<void>>(async () => undefined)
  viewRef.current = view
  addingRef.current = adding
  composingRef.current = composing
  modelsRef.current = modelsOpen
  surfaceRef.current = surface

  const commitView = (next: PhoneView) => {
    viewRef.current = next
    setView(next)
  }

  const completionRead = useCompletionRead({
    link: linkRef, view: viewRef, contentVisible: managerVisible,
    visible: Boolean(view.detail) && !adding && !composing && !modelsOpen && surface !== "chat"
      && (account.mode !== "account" || Boolean(link?.alive() && startup?.startup === "ready")),
    onError: setError,
    onRead: (detail, baseline) => {
      setError(undefined)
      const next = readInbox(inboxRef.current, detail, viewRef.current, baseline)
      inboxRef.current = next
      rosterFp.current = ""
      setAttention(next.attention)
      setThreads(inboxThreads(next))
      const current = viewRef.current
      if (current.detail?.id === detail.id) commitView({ ...current, detail: mergeCompletion(current.detail, detail, baseline) })
    },
  })

  const takeHostName = (resp: RemoteResponse) => {
    const name = resp.host?.trim()
    if (!name) return
    const fp = loadActiveFingerprint()
    if (!fp) return
    saveHostLabel(fp, name)
    setHosts(savedHosts())
  }

  const resetRoster = () => {
    inboxRef.current = emptyInbox()
    rosterFp.current = ""
    moreBusy.current = false
    setLoadingMore(false)
    setThreads([])
    setProjects([])
    setRunning([])
    setAttention([])
    setMore(false)
    setGroups([])
    setLoadingGroup("")
  }

  const applyList = (resp: RemoteResponse, append: boolean, target?: RemoteLink, group?: string, refreshed?: InboxWindow) => {
    if (target && target !== linkRef.current) return
    if (!resp.ok) {
      setError(remoteError(resp.error || resp.code || ""))
      return
    }
    // Head is page one. Tail is what More already loaded. A quiet poll
    // must not put those rows back, and a row that left page one is not
    // kept just because it was there last time.
    const next = refreshed ?? reduceInbox(inboxRef.current, resp, append ? "append" : "replace", group)
    const merged = inboxThreads(next)
    const groupSig = next.groups.map((g) => `${g.id}:${g.more ? 1 : 0}:${g.cursor}`).join(",")
    const fp = rosterFingerprint(next.projects, merged, next.running, next.more, `${next.cursor}|${groupSig}`, next.attention)
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
    setAttention(next.attention)
    setMore(next.more)
    setGroups(next.groups)
  }

  const refreshInbox = useInboxRefresh({ link: linkRef, window: inboxRef, onError: setError,
    onPage: (page, next, target) => applyList(page, false, target, undefined, next) })

  const attachPush = (next: RemoteLink) => {
    next.onPush = (resp) => {
      if (linkRef.current !== next) return
      if (resp.path) next.path = resp.path
      completionRead.observe(next, resp)
      commitView(applyPush(viewRef.current, resp))
    }
    next.onDisconnect = (err) => {
      if (linkRef.current !== next || unlinkingRef.current) return
      setError(connectionError(err))
      void recoverRef.current()
    }
    void next.announceDevice().then((resp) => {
      if (resp && linkRef.current === next) takeHostName(resp)
    })
  }

  const openThreadOn = (target: RemoteLink, id: string) =>
    openPhoneThread({
      target,
      id,
      view: () => linkRef.current === target ? viewRef.current : emptyView(),
      setView: (next) => {
        viewRef.current = next
      },
      commitView,
      setError,
      bumpLoad: () => {
        loadGen.current += 1
      },
      clearOlder: () => {
        olderBusy.current = false
        setLoadingOlder(false)
      },
    })

  const consumeFirstList = async (target: RemoteLink) => {
    const resp = await refreshInbox(target)
    if (!resp || !resp.ok || resumedRef.current || viewRef.current.detail) return
    if (surfaceRef.current === "chat") return
    const id = pickResumeThread(loadLastThreadId(), resp.running ?? [], resp.threads ?? [])
    if (!id) return
    commitView(openView(detailFromListing(id, resp.running ?? [], resp.threads ?? [])))
    if (await openThreadOn(target, id)) resumedRef.current = true
  }

  const { boot, recover, close: closeLink, detach: detachLink } = usePhoneLinkLifecycle({
    link: linkRef, authority: authorityRef, generation: bindGen, unlinking: unlinkingRef, retryTimer, recoverRef,
    allowsLink: account.allowsLink, install: setLink, attach: attachPush, firstList: consumeFirstList,
    onlineLink: saved => !saved.gatewayHostID || !accountRef.current.pcs ? undefined : accountRef.current.pcs.hosts.some(host => host.id === saved.gatewayHostID && host.online && host.enabled && saved.gatewayOrigin === accountRef.current.pcs?.origin),
    restore: async (next) => { const id = viewRef.current.threadId; if (id) await openThreadOn(next, id); else await consumeFirstList(next) },
    busy: setBusy, reconnecting: setReconnecting, error: setError, message: connectionError,
  })

  usePhoneVisibility(() => {
    if (!loadSavedLink() || linkRef.current?.alive()) return
    window.clearTimeout(retryTimer.current)
    void recoverRef.current()
  })

  useEffect(() => {
    if (!link || view.detail) return
    const id = window.setInterval(() => {
      if (!link.alive()) return
      void refreshInbox(link)
    }, 2000)
    return () => window.clearInterval(id)
  }, [link, view.detail])
  const seenGeneration = useRef<{ link: RemoteLink | null; value: number | null }>({ link: null, value: null })
  const resetRosterRef = useRef(resetRoster)
  const refreshInboxRef = useRef(refreshInbox)
  resetRosterRef.current = resetRoster
  refreshInboxRef.current = refreshInbox
  useEffect(() => {
    const same = seenGeneration.current.link === link
    const decision = rosterGenerationReset(same ? seenGeneration.current.value : null, startup?.data_generation)
    seenGeneration.current = { link, value: decision.seen }
    if (!same || !decision.reset || !link) return
    resetRosterRef.current()
    void refreshInboxRef.current(link)
  }, [link, startup?.data_generation])

  const onURI = async (uri: string) => {
    const isAdd = adding
    setBindBusy(true)
    if (isAdd) setAddError(undefined)
    else setError(undefined)
    let next: DeviceLink | undefined
    let attached = false
    let releasePairing: (() => void) | undefined
    try {
      releasePairing = beginAnonymousPairing(uri)
      const bound = await bindFromURI(uri)
      next = linkFromBind(bound)
      await next.connect(bound.saved.hubURL, bound.saved.ticket)
      bindGen.current += 1
      const gen = bindGen.current
      window.clearTimeout(retryTimer.current)
      saveLink(bound.saved)
      detachLink()
      resumedRef.current = false
      loadGen.current += 1
      resetRoster()
      commitView(emptyView())
      setHosts(savedHosts())
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
      authorityRef.current = bound.saved
      linkRef.current = next
      setLink(next)
      await consumeFirstList(next)
      if (gen === bindGen.current) setBusy(false)
    } catch (e) {
      if (!attached) next?.close()
      const msg = bindError(e)
      if (isAdd) setAddError(msg)
      else setError(msg)
    } finally {
      releasePairing?.()
      setBindBusy(false)
    }
  }

  const flipLocale = () => {
    toggleLocale()
    setLocaleTick(getLocale())
  }

  const fail = (e: unknown) => setError(connectionError(e))

  const openThread = async (id: string) => {
    const target = linkRef.current
    if (!target?.alive()) {
      setError(connectionError(new LinkFault("offline", "network")))
      void recover()
      return
    }
    if (viewRef.current.threadId !== id) {
      commitView(openView(detailFromListing(id, running, threads)))
    }
    if (await openThreadOn(target, id)) {
      completionRead.request(target, id, viewRef.current.detail?.completed_seq ?? 0)
    }
  }

  const startConversation = async (text: string, projectId: string, extra?: ComposerExtra) => {
    const target = linkRef.current
    if (!target?.alive()) {
      setError(connectionError(new LinkFault("offline", "network")))
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
          ...await pcWebSelection(target),
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
      await refreshInbox(target)
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
        ...await pcWebSelection(target),
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
        target,
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

  const loadOlder = async (turnId?: string): Promise<number | undefined> => {
    const target = linkRef.current
    if (!target || olderBusy.current) return
    const cur = viewRef.current
    if (!cur.hasMore || !cur.threadId) return
    const gen = loadGen.current
    const threadId = cur.threadId
    olderBusy.current = true
    setLoadingOlder(true)
    const stillHere = () => linkRef.current === target && loadGen.current === gen && viewRef.current.threadId === threadId
    try {
      return await readOlderHistory({ link: target, threadId, turnId, stillHere,
        current: () => viewRef.current, commit: commitView, setError })
    } catch (e) {
      if (stillHere()) fail(e)
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
      await refreshInbox(target)
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
    const rest = savedHosts()
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

  const selectHost = (fp: string, preserveView = false, background = false) => {
    if (!preserveView && !background) setSurface("hosts")
    const saved = savedHosts().find((row) => row.fingerprint === fp)
    if (!saved) return
    const paused = saved && gatewayUpgradePending(saved)
    if (!paused && fp === activeFpRef.current && sameLinkAuthority(saved, authorityRef.current) && (!linkRef.current || linkRef.current.alive())) return
    bindGen.current += 1
    window.clearTimeout(retryTimer.current)
    detachLink()
    if (paused) { setBusy(false); setReconnecting(false); return }
    resumedRef.current = true
    loadGen.current += 1
    resetRoster()
    setError(undefined)
    setReconnecting(false)
    if (!preserveView) commitView(emptyView())
    saveActiveFingerprint(fp)
    setActiveFp(fp)
    setBusy(true)
    if (preserveView) void recover(true)
    else void boot()
  }

  useEffect(() => {
    const select = (event: Event) => {
      setHosts(savedHosts())
      selectHost((event as CustomEvent<string>).detail, false, event.type === "zwai.gateway.auto-connect")
    }
    const sync = () => {
      const next = savedHosts()
      setHosts(next)
      if (!next.some((h) => h.fingerprint === activeFpRef.current)) {
        window.clearTimeout(retryTimer.current); bindGen.current += 1
        closeLink()
        resetRoster(); commitView(emptyView()); setActiveFp(""); setBusy(false)
      } else {
        const saved = next.find((row) => row.fingerprint === activeFpRef.current)
        if (saved && (gatewayUpgradePending(saved) || !sameLinkAuthority(saved, authorityRef.current))) selectHost(saved.fingerprint, true)
      }
    }
    const scan = () => { if (account.mode !== "account") setAdding(true) }
    const models = () => { if (account.mode !== "account") setModelsOpen(true) }
    window.addEventListener("zwai.gateway.scan", scan)
    window.addEventListener("zwai.gateway.models", models)
    window.addEventListener("zwai.gateway.connect", select)
    window.addEventListener("zwai.gateway.auto-connect", select)
    window.addEventListener("zwai.gateway.account", sync)
    return () => {
      window.removeEventListener("zwai.gateway.scan", scan)
      window.removeEventListener("zwai.gateway.models", models)
      window.removeEventListener("zwai.gateway.connect", select)
      window.removeEventListener("zwai.gateway.auto-connect", select)
      window.removeEventListener("zwai.gateway.account", sync)
    }
  }, [])

  const commitProviders = (next: DirectProvider[]) => {
    const appeared = personalProviders.length === 0 && next.length > 0
    setProviders(next)
    if (next.length === 0 && account.providers.length === 0) setSurface("hosts")
    else if (appeared) {
      setSurface("chat")
      setComposing(false)
    }
  }

  const openChat = () => {
    setSurface("chat")
    setComposing(false)
  }

  useEffect(() => {
    if (!account.officialOnly && account.providers.length > 0 && hosts.length === 0) setSurface("chat")
  }, [account.officialOnly, account.providers.length, hosts.length])

  const modelSheet = modelsOpen && account.mode !== "account" ? (
    <ProviderSheet
      providers={providers}
      hasProfile={account.hasProfile}
      presentation={account.presentation}
      onPresentation={account.setPresentation}
      onClose={() => setModelsOpen(false)}
      onChange={commitProviders}
    />
  ) : null

  const sheet = <>{adding ? (
    <AddHostSheet
      onURI={(uri) => void onURI(uri)}
      busy={bindBusy}
      error={addError}
      onClose={() => {
        setAdding(false)
        setAddError(undefined)
      }}
    />
  ) : null}
  </>

  const gate = (children: ReactNode) => <PcConnectionGate hosts={hosts} activeFingerprint={activeFp} connecting={busy || reconnecting}
    connected={Boolean(link?.alive())} ready={startup?.startup === "ready"} error={error} onRetry={() => void recover(true)}
    onSelectHost={selectHost} onOpenChat={openChat}>{children}</PcConnectionGate>

  if (account.mode !== "account" && phoneShell(hosts.length, providers.length) === "scan" && !(surface === "chat" && account.gatewaySourceID)) {
    if (account.empty) return <>{account.empty}{sheet}{modelSheet}</>
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

  const banner = (<>
    <EngineStartupBanner startup={startup} />
    <LinkBanner
      reconnecting={reconnecting}
      error={error}
      onRetry={() => void recover(true)}
    />
  </>)
  const openPcAccount = account.openAccount
    ? () => account.openAccount?.({ connected: Boolean(link?.alive()), status: webExecutionStatus })
    : undefined

  if (composing && surface !== "chat") {
    return gate(
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
            link={link}
            onProjectsChanged={() => { const target = linkRef.current; if (target) void refreshInbox(target).catch(fail) }}
            initialProject={composeProject}
            connected={Boolean(link?.alive())}
            connecting={busy || reconnecting}
            models={models}
            reasoningLevels={levels}
            catalogBusy={catalogBusy}
            onRefreshModels={refreshModels}
            refreshBusy={refreshBusy}
            refreshError={refreshError}
            hideModel={hidePcModel}
            restoring={restoring}
            pending={composerPending}
            onSelectHost={selectHost}
            onOpenAccount={openPcAccount}
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

  if (view.detail && surface !== "chat") {
    const detail = view.detail
    return gate(
      <div className="flex h-full min-w-0 flex-col overflow-hidden">
        <UpdateNotice />
        {banner}
        <div className="screen-push flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          <ThreadScreen
            key={locale + detail.id}
            detail={detail}
            prediction={phonePrediction(view.events, detail, (startup?.composer_predictions ?? predictionsEnabled) && !catalogBusy)}
            onReadVisibility={(visible) => { managerVisible.current = visible }}
            fileLink={link ?? undefined}
            panelOwnerKey={activeFp ? `${activeFp}:${startup?.data_generation ?? 0}` : ""}
            workRecordsGeneration={startup?.startup === "ready" && startup.graph_available ? startup.data_generation : undefined}
            blocks={view.blocks}
            followups={view.followups}
            onSteerFollowup={(id, mode, turnId) => mode && turnId
              ? void deliverPhoneFollowup(turnDeps, detail.id, id, mode, turnId)
              : void queueFollowup(OpFollowupSteer, detail.id, id)}
            onDropFollowup={(id) => void queueFollowup(OpFollowupDrop, detail.id, id)}
            hasMore={view.hasMore}
            loadingOlder={loadingOlder}
            caughtUp={view.caughtUp}
            restoring={restoring}
            composerPending={composerPending}
            models={models}
            reasoningLevels={levels}
            catalogBusy={catalogBusy}
            onRefreshModels={refreshModels}
            refreshBusy={refreshBusy}
            refreshError={refreshError}
            hideModel={hidePcModel}
            onTune={(next) => void tuneThread(detail.id, next)}
            onBack={() => void closeThread()}
            onOpenAccount={openPcAccount}
            onOlder={loadOlder}
            historyWork={view.historyWork}
            historyBefore={view.oldestSeq || view.lastSeq}
            onSend={(text, extra) => postTurn(parseSlashSubmit(text) ? OpSend : OpDeliver, detail.id, text, extra)}
            onInsert={(mode, turnId, text, extra) => sendPhoneTurn(turnDeps, OpDeliver, detail.id, text, extra, { mode, turnId })}
            onStop={async () => {
              if (!link?.alive()) return
              try {
                await link.rpc({ op: OpStop, thread_id: detail.id })
                const r = await link.rpc({ op: OpOpen, thread_id: detail.id })
                if (r.detail) commitView({ ...viewRef.current, detail: r.detail })
              } catch (e) {
                fail(e)
              }
            }}
            onAnswer={(text) => answerPhoneQuestion(turnDeps, detail.id, { text })}
            onContinue={(proceed) => continuePhoneTurn(link, detail.id, proceed)}
            onAnswerStructured={(callId, answers) =>
              answerPhoneQuestion(turnDeps, detail.id, { call_id: callId, answers })}
            onRunNow={() => void runPhoneAction(turnDeps, OpRunNow, detail.id)}
            onCancelWait={() => void runPhoneAction(turnDeps, OpCancelWait, detail.id)}
            onResumeGoal={() => void runPhoneAction(turnDeps, OpResumeGoal, detail.id)}
          />
        </div>
        {sheet}
        {modelSheet}
      </div>
    )
  }

  if (surface === "chat") {
    return (
      <ChatSurface
        locale={locale}
        hosts={hosts}
        activeFingerprint={activeFp}
        path={link?.path ?? "relay"}
        connected={Boolean(link?.alive())}
        reconnecting={reconnecting}
        providers={providers}
        banner={!account.officialOnly && link ? banner : null}
        sheet={sheet}
        modelSheet={modelSheet}
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
    )
  }

  return gate(
    <div className="flex h-full min-w-0 flex-col overflow-hidden">
      <UpdateNotice />
      {link ? banner : null}
      <div className="min-h-0 flex-1">
        <HomeScreen
          pcLink={link}
          key={locale}
          hosts={hosts}
          activeFingerprint={activeFp}
          projects={projects}
          threads={threads}
          running={running}
          attention={attention}
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
              await refreshInbox(target)
            } catch (e) {
              fail(e)
            }
          }}
          onMore={(group) => void loadMore(group)}
          onUnlink={unlink}
          onToggleLocale={flipLocale}
          showChat={account.officialOnly || providers.length > 0 || Boolean(account.gatewaySourceID)}
          onSelectChat={openChat}
          onModels={() => setModelsOpen(true)}
          onOpenAccount={openPcAccount}
        />
      </div>
      {sheet}
      {modelSheet}
    </div>
  )
}
