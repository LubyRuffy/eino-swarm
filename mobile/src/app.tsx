import { useCallback, useEffect, useRef, useState } from "react"

import { AddHostSheet } from "@/components/add-host-sheet"
import { HomeScreen } from "@/components/home-screen"
import { LinkBanner } from "@/components/link-banner"
import { ScanScreen } from "@/components/scan-screen"
import { ThreadScreen } from "@/components/thread-screen"
import { bindFromURI, bindError, DeviceLink, linkError, openSaved } from "@/lib/client"
import { getLocale, t, toggleLocale } from "@/lib/i18n"
import type {
  ProjectView,
  RemoteResponse,
  RunningView,
  ThreadView,
} from "@/lib/rpc"
import {
  OpAnswer,
  OpCancelWait,
  OpList,
  OpLog,
  OpMore,
  OpOpen,
  OpReady,
  OpResumeGoal,
  OpRunNow,
  OpSend,
  OpStart,
  OpSteer,
  OpStop,
  OpUnwatch,
  OpWatch,
} from "@/lib/rpc"
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

export function App() {
  const [locale, setLocaleTick] = useState(getLocale())
  const [hosts, setHosts] = useState(() => loadSavedLinks())
  const [activeFp, setActiveFp] = useState(
    () => loadActiveFingerprint() || loadSavedLinks()[0]?.fingerprint || "",
  )
  const [adding, setAdding] = useState(false)
  const [addError, setAddError] = useState<string>()
  const [link, setLink] = useState<DeviceLink | null>(null)
  const [busy, setBusy] = useState(() => loadSavedLinks().length > 0)
  const [bindBusy, setBindBusy] = useState(false)
  const [reconnecting, setReconnecting] = useState(false)
  const [error, setError] = useState<string>()
  const [projects, setProjects] = useState<ProjectView[]>([])
  const [threads, setThreads] = useState<ThreadView[]>([])
  const [running, setRunning] = useState<RunningView[]>([])
  const [more, setMore] = useState(false)
  const [cursor, setCursor] = useState("")
  const [view, setView] = useState<PhoneView>(emptyView())
  const [loadingOlder, setLoadingOlder] = useState(false)
  const linkRef = useRef<DeviceLink | null>(null)
  const viewRef = useRef<PhoneView>(view)
  const resumedRef = useRef(false)
  const olderBusy = useRef(false)
  const loadGen = useRef(0)
  const recoveringRef = useRef(false)
  const unlinkingRef = useRef(false)
  const bindGen = useRef(0)
  const recoverAttempt = useRef(0)
  const retryTimer = useRef(0)
  const rosterFp = useRef("")
  const recoverRef = useRef<(force?: boolean) => Promise<void>>(async () => undefined)
  linkRef.current = link
  viewRef.current = view

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

  const applyList = (resp: RemoteResponse, append: boolean, target?: DeviceLink) => {
    if (!resp.ok) {
      setError(resp.error || resp.code || t("err.rpc"))
      return
    }
    const nextThreads = append ? undefined : (resp.threads ?? [])
    if (!append && nextThreads) {
      const fp = rosterFingerprint(
        resp.projects ?? [],
        nextThreads,
        resp.running ?? [],
        Boolean(resp.more),
        resp.next ?? "",
      )
      if (fp === rosterFp.current) {
        const cur = target ?? linkRef.current
        if (resp.path && cur) cur.path = resp.path
        takeHostName(resp)
        return
      }
      rosterFp.current = fp
    } else if (append) {
      rosterFp.current = ""
    }
    setError(undefined)
    if (resp.projects) setProjects(resp.projects)
    setThreads((prev) => (append ? [...prev, ...(resp.threads ?? [])] : (resp.threads ?? [])))
    setRunning(resp.running ?? [])
    setMore(Boolean(resp.more))
    setCursor(resp.next ?? "")
    const cur = target ?? linkRef.current
    if (resp.path && cur) cur.path = resp.path
    takeHostName(resp)
  }

  const attachPush = (next: DeviceLink) => {
    next.onPush = (resp) => {
      if (resp.path) next.path = resp.path
      commitView(applyPush(viewRef.current, resp))
    }
    next.onDisconnect = () => {
      if (linkRef.current !== next || unlinkingRef.current) return
      setError(t("err.reconnect"))
      void recoverRef.current()
    }
    void next.announceDevice().then((resp) => {
      if (resp) takeHostName(resp)
    })
  }

  const openThreadOn = async (target: DeviceLink, id: string) => {
    const staying = viewRef.current.threadId === id && Boolean(viewRef.current.detail)
    try {
      const r = await target.rpc({ op: OpOpen, thread_id: id })
      if (!r.detail) {
        setError(r.error || t("err.open"))
        if (id === loadLastThreadId()) clearLastThreadId()
        if (!staying) viewRef.current = emptyView()
        return false
      }
      if (viewRef.current.threadId && viewRef.current.threadId !== id) return false
      saveLastThreadId(id)
      loadGen.current += 1
      olderBusy.current = false
      setLoadingOlder(false)
      commitView(applyOpenDetail(viewRef.current, r.detail))
      const w = await target.rpc({ op: OpWatch, thread_id: id })
      if (viewRef.current.threadId && viewRef.current.threadId !== id) return false
      if (!w.ok) {
        setError(w.error || w.code || t("err.watch"))
        if (!staying) viewRef.current = emptyView()
        return false
      }
      commitView(applyPush(viewRef.current, { ...w, op: w.op || OpReady }))
      return true
    } catch (e) {
      setError(linkError(e))
      if (!staying) viewRef.current = emptyView()
      return false
    }
  }

  const consumeFirstList = async (target: DeviceLink, resp: RemoteResponse) => {
    applyList(resp, false, target)
    if (!resp.ok || resumedRef.current || viewRef.current.detail) return
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
        next = await openSaved(saved)
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
      const next = await openSaved(saved)
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

  const onURI = async (uri: string) => {
    const isAdd = adding
    setBindBusy(true)
    if (isAdd) setAddError(undefined)
    else setError(undefined)
    let next: DeviceLink | undefined
    let attached = false
    try {
      const bound = await bindFromURI(uri)
      next = new DeviceLink(bound.identity, bound.redeemed.hostPub, bound.redeemed.sessionID)
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
      rosterFp.current = ""
      setThreads([])
      setProjects([])
      setRunning([])
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
      setError(t("err.reconnect"))
      void recover()
      return
    }
    if (viewRef.current.threadId !== id) {
      commitView(openView(detailFromListing(id, running, threads)))
    }
    await openThreadOn(target, id)
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
        setError(r.error || r.code || t("err.rpc"))
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
    rosterFp.current = ""
    linkRef.current = null
    setLink(null)
    setThreads([])
    setProjects([])
    setRunning([])
    setError(undefined)
    setAddError(undefined)
    setBindBusy(false)
    setReconnecting(false)
    setAdding(false)
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
    if (fp === activeFp && linkRef.current?.alive()) return
    bindGen.current += 1
    window.clearTimeout(retryTimer.current)
    linkRef.current?.close()
    linkRef.current = null
    setLink(null)
    resumedRef.current = false
    loadGen.current += 1
    rosterFp.current = ""
    setThreads([])
    setProjects([])
    setRunning([])
    setError(undefined)
    setReconnecting(false)
    commitView(emptyView())
    saveActiveFingerprint(fp)
    setActiveFp(fp)
    setBusy(true)
    void boot()
  }

  if (phoneShell(hosts.length) === "scan") {
    return (
      <ScanScreen
        key={locale}
        onURI={(uri) => void onURI(uri)}
        busy={bindBusy}
        error={error}
        onToggleLocale={flipLocale}
      />
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

  if (view.detail && link) {
    const detail = view.detail
    return (
      <div className="flex h-full min-w-0 flex-col overflow-hidden">
        {banner}
        <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          <ThreadScreen
            key={locale + detail.id}
            detail={detail}
            blocks={view.blocks}
            hasMore={view.hasMore}
            loadingOlder={loadingOlder}
            caughtUp={view.caughtUp}
            onBack={() => void closeThread()}
            onOlder={() => void loadOlder()}
            onSend={async (text) => {
              try {
                await link.rpc({ op: OpSend, thread_id: detail.id, text })
                commitView(markRunning(viewRef.current))
              } catch (e) {
                fail(e)
              }
            }}
            onSteer={async (text) => {
              try {
                await link.rpc({ op: OpSteer, thread_id: detail.id, text })
              } catch (e) {
                fail(e)
              }
            }}
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
      </div>
    )
  }

  return (
    <div className="flex h-full min-w-0 flex-col overflow-hidden">
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
          path={link?.path ?? "relay"}
          connected={Boolean(link?.alive())}
          reconnecting={reconnecting}
          connecting={busy || reconnecting}
          error={!link ? error : undefined}
          onOpen={(id) => void openThread(id)}
          onSelectHost={selectHost}
          onAddHost={() => {
            setAddError(undefined)
            setAdding(true)
          }}
          onRetry={() => void boot()}
          onMore={async () => {
            if (!link) return
            try {
              applyList(await link.rpc({ op: OpMore, cursor }), true)
            } catch (e) {
              fail(e)
            }
          }}
          onStart={async (text, projectId) => {
            if (!link) return
            try {
              const r = await link.rpc({
                op: OpStart,
                text,
                project_id: projectId || undefined,
              })
              applyList(await link.rpc({ op: OpList }), false)
              if (r.threads?.[0]) await openThread(r.threads[0].id)
            } catch (e) {
              fail(e)
            }
          }}
          onUnlink={unlink}
          onToggleLocale={flipLocale}
        />
      </div>
      {sheet}
    </div>
  )
}
