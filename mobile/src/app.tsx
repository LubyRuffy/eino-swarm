import { useCallback, useEffect, useRef, useState } from "react"

import { HomeScreen } from "@/components/home-screen"
import { ScanScreen } from "@/components/scan-screen"
import { ThreadScreen } from "@/components/thread-screen"
import { bindFromURI, bindError, DeviceLink, openSaved } from "@/lib/client"
import { getLocale, toggleLocale } from "@/lib/i18n"
import type {
  ProjectView,
  RemoteResponse,
  RunningView,
  ThreadView,
} from "@/lib/rpc"
import {
  OpAnswer,
  OpList,
  OpLog,
  OpMore,
  OpOpen,
  OpSend,
  OpStart,
  OpSteer,
  OpStop,
  OpUnwatch,
  OpWatch,
} from "@/lib/rpc"
import { pickResumeThread } from "@/lib/resume"
import {
  applyPush,
  emptyView,
  markRunning,
  openView,
  prependOlder,
  type PhoneView,
} from "@/lib/session"
import { clearLastThreadId, clearLink, loadLastThreadId, loadSavedLink, saveLastThreadId } from "@/lib/store"

export function App() {
  const [locale, setLocaleTick] = useState(getLocale())
  const [link, setLink] = useState<DeviceLink | null>(null)
  const [busy, setBusy] = useState(false)
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
  linkRef.current = link
  viewRef.current = view

  const commitView = (next: PhoneView) => {
    viewRef.current = next
    setView(next)
  }

  const applyList = (resp: RemoteResponse, append: boolean, target?: DeviceLink) => {
    if (!resp.ok) {
      setError(resp.error || resp.code || "rpc failed")
      return
    }
    setError(undefined)
    if (resp.projects) setProjects(resp.projects)
    setThreads((prev) => (append ? [...prev, ...(resp.threads ?? [])] : (resp.threads ?? [])))
    setRunning(resp.running ?? [])
    setMore(Boolean(resp.more))
    setCursor(resp.next ?? "")
    const link = target ?? linkRef.current
    if (resp.path && link) link.path = resp.path
  }

  const attachPush = (next: DeviceLink) => {
    next.onPush = (resp) => {
      if (resp.path) next.path = resp.path
      commitView(applyPush(viewRef.current, resp))
    }
  }

  const openThreadOn = async (target: DeviceLink, id: string) => {
    const r = await target.rpc({ op: OpOpen, thread_id: id })
    if (!r.detail) {
      setError(r.error || "open failed")
      if (id === loadLastThreadId()) clearLastThreadId()
      return false
    }
    saveLastThreadId(id)
    loadGen.current += 1
    olderBusy.current = false
    setLoadingOlder(false)
    commitView(openView(r.detail))
    await target.rpc({ op: OpWatch, thread_id: id })
    return true
  }

  const consumeFirstList = async (target: DeviceLink, resp: RemoteResponse) => {
    applyList(resp, false, target)
    if (!resp.ok || resumedRef.current || viewRef.current.detail) return
    const id = pickResumeThread(loadLastThreadId(), resp.running ?? [], resp.threads ?? [])
    if (!id) return
    if (await openThreadOn(target, id)) resumedRef.current = true
  }

  const boot = useCallback(async () => {
    const saved = loadSavedLink()
    if (!saved) return
    setBusy(true)
    try {
      const next = await openSaved(saved)
      attachPush(next)
      setLink(next)
      await consumeFirstList(next, await next.rpc({ op: OpList }))
    } catch (e) {
      setError(bindError(e))
    } finally {
      setBusy(false)
    }
  }, [])

  useEffect(() => {
    void boot()
    return () => {
      linkRef.current?.close()
    }
  }, [boot])

  useEffect(() => {
    if (!link || view.detail) return
    const id = window.setInterval(() => {
      void link.rpc({ op: OpList }).then((r) => applyList(r, false))
    }, 2000)
    return () => window.clearInterval(id)
  }, [link, view.detail])

  const onURI = async (uri: string) => {
    setBusy(true)
    setError(undefined)
    try {
      const bound = await bindFromURI(uri)
      const next = new DeviceLink(bound.identity, bound.redeemed.hostPub, bound.redeemed.sessionID)
      await next.connect(bound.saved.hubURL, bound.saved.ticket)
      attachPush(next)
      setLink(next)
      await consumeFirstList(next, await next.rpc({ op: OpList }))
    } catch (e) {
      setError(bindError(e))
    } finally {
      setBusy(false)
    }
  }

  const flipLocale = () => {
    toggleLocale()
    setLocaleTick(getLocale())
  }

  const openThread = async (id: string) => {
    if (!link) return
    await openThreadOn(link, id)
  }

  const loadOlder = async () => {
    if (!link || olderBusy.current) return
    const cur = viewRef.current
    const before = cur.oldestSeq > 0 ? cur.oldestSeq : cur.lastSeq
    if (!cur.hasMore || !cur.threadId || before <= 0) return
    const gen = loadGen.current
    const threadId = cur.threadId
    olderBusy.current = true
    setLoadingOlder(true)
    try {
      const r = await link.rpc({
        op: OpLog,
        thread_id: threadId,
        before,
      })
      if (loadGen.current !== gen || viewRef.current.threadId !== threadId) return
      if (!r.ok) {
        setError(r.error || r.code || "rpc failed")
        return
      }
      commitView(prependOlder(viewRef.current, r.events ?? [], Boolean(r.more), r.seq ?? 0))
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
    if (link && id) {
      await link.rpc({ op: OpUnwatch, thread_id: id })
    }
    if (link) applyList(await link.rpc({ op: OpList }), false)
  }

  if (!link) {
    return (
      <ScanScreen
        key={locale}
        onURI={(uri) => void onURI(uri)}
        busy={busy}
        error={error}
        onRetry={loadSavedLink() ? () => void boot() : undefined}
        onToggleLocale={flipLocale}
      />
    )
  }

  if (view.detail) {
    const detail = view.detail
    return (
      <ThreadScreen
        key={locale + detail.id}
        detail={detail}
        blocks={view.blocks}
        hasMore={view.hasMore}
        loadingOlder={loadingOlder}
        onBack={() => void closeThread()}
        onOlder={() => void loadOlder()}
        onSend={async (text) => {
          await link.rpc({ op: OpSend, thread_id: detail.id, text })
          commitView(markRunning(viewRef.current))
        }}
        onSteer={async (text) => {
          await link.rpc({ op: OpSteer, thread_id: detail.id, text })
        }}
        onStop={async () => {
          await link.rpc({ op: OpStop, thread_id: detail.id })
          const r = await link.rpc({ op: OpOpen, thread_id: detail.id })
          if (r.detail) commitView({ ...viewRef.current, detail: r.detail })
        }}
        onAnswer={async (text) => {
          await link.rpc({ op: OpAnswer, thread_id: detail.id, text })
        }}
        onAnswerStructured={async (callId, answers) => {
          await link.rpc({
            op: OpAnswer,
            thread_id: detail.id,
            call_id: callId,
            answers,
          })
        }}
      />
    )
  }

  return (
    <>
      {error ? (
        <p className="px-4 pt-4 text-sm text-destructive" role="alert">
          {error}
        </p>
      ) : null}
      <HomeScreen
        key={locale}
        projects={projects}
        threads={threads}
        running={running}
        more={more}
        path={link.path}
        onOpen={(id) => void openThread(id)}
        onMore={async () => {
          applyList(await link.rpc({ op: OpMore, cursor }), true)
        }}
        onStart={async (text, projectId) => {
          const r = await link.rpc({
            op: OpStart,
            text,
            project_id: projectId || undefined,
          })
          applyList(await link.rpc({ op: OpList }), false)
          if (r.threads?.[0]) await openThread(r.threads[0].id)
        }}
        onUnlink={() => {
          void link.rpc({ op: OpUnwatch }).catch(() => undefined)
          link.close()
          clearLink()
          resumedRef.current = false
          loadGen.current += 1
          setLink(null)
          setThreads([])
          setProjects([])
          setRunning([])
          commitView(emptyView())
        }}
        onToggleLocale={flipLocale}
      />
    </>
  )
}
