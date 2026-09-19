import { useCallback, useEffect, useRef, useState } from "react"

import { HomeScreen } from "@/components/home-screen"
import { ScanScreen } from "@/components/scan-screen"
import { ThreadScreen } from "@/components/thread-screen"
import { bindFromURI, DeviceLink, openSaved } from "@/lib/client"
import type { ProjectView, RemoteResponse, RunningView, ThreadDetail, ThreadView } from "@/lib/rpc"
import { OpAnswer, OpList, OpMore, OpOpen, OpSend, OpStart, OpStop } from "@/lib/rpc"
import { clearLink, loadSavedLink } from "@/lib/store"

export function App() {
  const [link, setLink] = useState<DeviceLink | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  const [projects, setProjects] = useState<ProjectView[]>([])
  const [threads, setThreads] = useState<ThreadView[]>([])
  const [running, setRunning] = useState<RunningView[]>([])
  const [more, setMore] = useState(false)
  const [cursor, setCursor] = useState("")
  const [detail, setDetail] = useState<ThreadDetail | null>(null)
  const linkRef = useRef<DeviceLink | null>(null)
  linkRef.current = link

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
  }

  const boot = useCallback(async () => {
    const saved = loadSavedLink()
    if (!saved) return
    setBusy(true)
    try {
      const next = await openSaved(saved)
      setLink(next)
      applyList(await next.rpc({ op: OpList }), false)
    } catch (e) {
      clearLink()
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
    if (!link) return
    const id = window.setInterval(() => {
      void link.rpc({ op: OpList }).then((r) => applyList(r, false))
    }, 2000)
    return () => window.clearInterval(id)
  }, [link])

  const onURI = async (uri: string) => {
    setBusy(true)
    setError(undefined)
    try {
      const bound = await bindFromURI(uri)
      const next = new DeviceLink(bound.identity, bound.redeemed.hostPub, bound.redeemed.sessionID)
      await next.connect(bound.saved.hubURL, bound.saved.ticket)
      setLink(next)
      applyList(await next.rpc({ op: OpList }), false)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (!link) {
    return <ScanScreen onURI={(uri) => void onURI(uri)} busy={busy} error={error} />
  }

  if (detail) {
    return (
      <ThreadScreen
        detail={detail}
        onBack={() => setDetail(null)}
        onSend={async (text) => {
          await link.rpc({ op: OpSend, thread_id: detail.id, text })
          const r = await link.rpc({ op: OpOpen, thread_id: detail.id })
          if (r.detail) setDetail(r.detail)
        }}
        onStop={async () => {
          await link.rpc({ op: OpStop, thread_id: detail.id })
          const r = await link.rpc({ op: OpOpen, thread_id: detail.id })
          if (r.detail) setDetail(r.detail)
        }}
        onAnswer={async (text) => {
          await link.rpc({ op: OpAnswer, thread_id: detail.id, text })
          const r = await link.rpc({ op: OpOpen, thread_id: detail.id })
          if (r.detail) setDetail(r.detail)
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
        projects={projects}
        threads={threads}
        running={running}
        more={more}
        path={link.path}
        onOpen={async (id) => {
          const r = await link.rpc({ op: OpOpen, thread_id: id })
          if (r.detail) setDetail(r.detail)
          else setError(r.error || "open failed")
        }}
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
          if (r.threads?.[0]) {
            const open = await link.rpc({ op: OpOpen, thread_id: r.threads[0].id })
            if (open.detail) setDetail(open.detail)
          }
        }}
        onUnlink={() => {
          link.close()
          clearLink()
          setLink(null)
          setThreads([])
          setProjects([])
          setRunning([])
        }}
      />
    </>
  )
}
