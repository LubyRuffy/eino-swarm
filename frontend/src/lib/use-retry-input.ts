import { useCallback, useEffect, useRef, useState } from "react"
import { useShallow } from "zustand/react/shallow"

import { useApp, type AppState } from "@/store/app"
import { useSettingsSheet } from "@/store/settings-sheet"
import { MANAGER_ID } from "./transcript"

type RetryState = Pick<AppState, "activeId" | "loaded" | "transcript" | "turns" | "status" | "meta" | "historyHasMore">

function originalInput(state: RetryState, turnId: string) {
  return state.transcript.agents[MANAGER_ID]?.blocks.find(
    (block) => block.kind === "user" && block.turnId === turnId && block.seq > 0 &&
      (block.text.trim() !== "" || Boolean(block.images?.length)),
  )
}

/** Retry ancestry is explicit storage identity. Similar words never prove that
 *  an input came from the old continuation button rather than from a person. */
function inputSource(state: RetryState, turnId: string): string | undefined {
  const latest = state.transcript.turns.at(-1)
  if (!state.activeId || latest?.id !== turnId || latest.status !== "error") return
  const failed = state.turns.find((turn) => turn.id === turnId)
  if (failed && state.turns.some((turn) => turn.thread_id === state.activeId && turn.seq > failed.seq)) return
  const seen = new Set<string>()
  let sourceId = turnId
  let childSeq: number | undefined
  for (;;) {
    if (seen.has(sourceId)) return
    seen.add(sourceId)
    const source = state.turns.find((turn) => turn.id === sourceId)
    // Tail events can arrive before turn metadata. A user-shaped block may
    // belong to a legacy retry, so it cannot prove human input ancestry alone.
    if (!source) return
    if (source.thread_id !== state.activeId || source.seq <= 0 || source.goal_continue || source.schedule_continue || source.schedule_run_id) return
    if (childSeq !== undefined && source.seq >= childSeq) return
    if (source.retry_turn_id) {
      childSeq = source.seq
      sourceId = source.retry_turn_id
      continue
    }
    if (originalInput(state, sourceId) || state.historyHasMore) return sourceId
    return
  }
}

function available(state: RetryState) {
  return state.loaded && !state.status.running && !state.transcript.running &&
    !state.status.awaiting_answer && state.meta?.startup !== "restoring" && !useSettingsSheet.getState().open
}

/** Retry is the human's confirmation to resend the original request. Reuse its
 *  recorded sequence so the existing resend path retains the original images. */
export function useRetryInput({ enabled, jumpTo, resend }: {
  enabled: boolean
  jumpTo: (turnId: string) => void
  resend: (text: string, seq: number) => Promise<boolean>
}) {
  const state = useApp(useShallow((s) => ({
    activeId: s.activeId, loaded: s.loaded, transcript: s.transcript, turns: s.turns,
    status: s.status, meta: s.meta, historyHasMore: s.historyHasMore,
  })))
  const [retryingTurn, setRetryingTurn] = useState<string>()
  const pending = useRef<symbol | undefined>(undefined)
  const mounted = useRef(true)
  const current = useRef({ enabled, jumpTo, resend })
  current.current = { enabled, jumpTo, resend }
  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false }
  }, [])

  const canRetryInput = useCallback(
    (turnId: string) => enabled && Boolean(inputSource(state, turnId)),
    [enabled, state],
  )
  const retryInput = useCallback(async (turnId: string) => {
    const start = useApp.getState()
    const sourceId = inputSource(start, turnId)
    if (!mounted.current || !current.current.enabled || pending.current || !available(start) || !sourceId) return
    const request = Symbol()
    pending.current = request
    setRetryingTurn(turnId)
    try {
      if (!originalInput(start, sourceId)) {
        // Missing pages cannot authorize resending API prose with an invented seq.
        try {
          if (!await start.loadUntilTurn(sourceId)) return
        } catch {
          return
        }
      }
      const latest = useApp.getState()
      if (!mounted.current || !current.current.enabled || latest.activeId !== start.activeId ||
        latest.completionVisit !== start.completionVisit || !available(latest) || inputSource(latest, turnId) !== sourceId) return
      const input = originalInput(latest, sourceId)
      if (!input) return
      current.current.jumpTo(sourceId)
      await current.current.resend(input.text, input.seq)
    } finally {
      if (pending.current === request) {
        pending.current = undefined
        if (mounted.current) setRetryingTurn(undefined)
      }
    }
  }, [])

  return { canRetryInput, retryInput, retryingTurn }
}
