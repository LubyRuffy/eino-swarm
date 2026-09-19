import { useCallback, useEffect, useRef, useState } from "react"

import { HomeScreen } from "@/components/home-screen"
import { ScanScreen } from "@/components/scan-screen"
import { ThreadScreen } from "@/components/thread-screen"
import { bindFromURI, DeviceLink, openSaved } from "@/lib/client"
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
  OpMore,
  OpOpen,
  OpSend,
  OpStart,
  OpSteer,
  OpStop,
  OpUnwatch,
  OpWatch,
} from "@/lib/rpc"
import {
  applyPush,
  emptyView,
  markRunning,
  openView,
  type PhoneView,
} from "@/lib/session"
import { clearLink, loadSavedLink } from "@/lib/store"

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
  const linkRef = useRef<DeviceLink | null>(null)
  const viewRef = useRef<PhoneView>(view)
  linkRef.current = link
  viewRef.current = view

  const commitView = (next: PhoneView) => {
    viewRef.current = next
    setView(next)
  }

  const applyList = (resp: RemoteResponse, append: boolean) => {
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
    if (resp.path && linkRef.current) linkRef.current.path = resp.path
  }

  const attachPush = (next: DeviceLink) => {
    next.onPush = (resp) => {
      if (resp.path) next.path = resp.path
      commitView(applyPush(viewRef.current, resp))
    }
  }

  const boot = useCallback(async () => {
    const saved = loadSavedLink()
    if (!saved) return
    setBusy(true)
    try {
      const next = await openSaved(saved)
      attachPush(next)
      setLink(next)
      applyList(await next.rpc({ op: OpList }), false)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
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
      applyList(await next.rpc({ op: OpList }), false)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
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
    const r = await link.rpc({ op: OpOpen, thread_id: id })
    if (!r.detail) {
      setError(r.error || "open failed")
      return
    }
    commitView(openView(r.detail))
    await link.rpc({ op: OpWatch, thread_id: id })
  }

  const closeThread = async () => {
    const id = viewRef.current.threadId
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
        onBack={() => void closeThread()}
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
