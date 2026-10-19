import { useEffect, useRef, useState } from "react"
import { api } from "@/lib/api"
import type { Thread } from "@/lib/types"
import { MANAGER_ID } from "@/lib/transcript"
import { useApp } from "@/store/app"

/** An old read response must not clear a result that arrived after the click. */
export function applyCompletionRead(threads: Thread[], read: Thread): Thread[] {
  return threads.map((thread) => thread.id === read.id &&
    (thread.completed_seq ?? 0) === (read.completed_seq ?? 0) &&
    !(thread.unread === false && read.unread)
    ? { ...thread, unread: read.unread } : thread)
}

/** Mounted only beside the visible main transcript, after its history rendered. */
export function CompletionReader() {
  const id = useApp((s) => s.activeId)
  const loaded = useApp((s) => s.loaded)
  const displayed = useApp((s) => {
    const seq = s.threads.find((thread) => thread.id === s.activeId)?.completed_seq
    const turn = s.transcript.turns.find((row) => row.completedSeq === seq && !row.quiet)
    return Boolean(turn && turn.status !== "running" &&
      s.transcript.agents[MANAGER_ID]?.blocks.some((block) => block.turnId === turn.id && !block.quiet && block.kind !== "spawn"))
  })
  const visit = useApp((s) => s.completionVisit ?? 0)
  const completion = useApp((s) => s.threads.find((thread) => thread.id === s.activeId))
  const [visible, setVisible] = useState(document.visibilityState !== "hidden")
  const attempted = useRef("")
  useEffect(() => {
    const change = () => setVisible(document.visibilityState !== "hidden")
    document.addEventListener("visibilitychange", change)
    return () => document.removeEventListener("visibilitychange", change)
  }, [])
  const seq = completion?.completed_seq ?? 0
  useEffect(() => {
    if (!id || !loaded || !visible || !displayed || !completion?.unread || seq <= 0) return
    const key = `${id}:${seq}:${visit}`
    if (attempted.current === key) return
    attempted.current = key
    void api.readThread(id, seq).then(({ thread }) => {
      useApp.setState((s) => ({ threads: applyCompletionRead(s.threads, thread) }))
    }).catch((error: unknown) => {
      if (useApp.getState().activeId === id) {
        useApp.getState().setError(error instanceof Error ? error.message : String(error))
      }
    })
  }, [id, loaded, visible, displayed, completion?.unread, seq, visit])
  return null
}
