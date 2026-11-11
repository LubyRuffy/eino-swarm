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

/** The latest failed human turn can continue. Older retry_turn_id values are
 *  historical and do not choose a different input. */
function continuableFailure(state: RetryState, turnId: string): boolean {
  const latest = state.transcript.turns.at(-1)
  if (!state.activeId || latest?.id !== turnId || latest.status !== "error") return false
  const failed = state.turns.find((turn) => turn.id === turnId)
  if (!failed) return false
  if (failed.thread_id !== state.activeId || failed.seq <= 0) return false
  if (failed.goal_continue || failed.schedule_continue || failed.schedule_run_id) return false
  if (state.turns.some((turn) => turn.thread_id === state.activeId && turn.seq > failed.seq)) return false
  return Boolean(originalInput(state, turnId) || state.historyHasMore)
}

function available(state: RetryState) {
  return state.loaded && !state.status.running && !state.transcript.running &&
    !state.status.awaiting_answer && state.meta?.startup !== "restoring" && !useSettingsSheet.getState().open
}

/** Retry continues the failed turn from the context it already had. It does
 *  not resend the user message or rewind the tool progress below it. */
export function useRetryInput({ enabled, continueFailed }: {
  enabled: boolean
  continueFailed: (turnId: string) => Promise<boolean>
}) {
  const state = useApp(useShallow((s) => ({
    activeId: s.activeId, loaded: s.loaded, transcript: s.transcript, turns: s.turns,
    status: s.status, meta: s.meta, historyHasMore: s.historyHasMore,
  })))
  const [retryingTurn, setRetryingTurn] = useState<string>()
  const pending = useRef<symbol | undefined>(undefined)
  const mounted = useRef(true)
  const current = useRef({ enabled, continueFailed })
  current.current = { enabled, continueFailed }
  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false }
  }, [])

  const canRetryInput = useCallback(
    (turnId: string) => enabled && continuableFailure(state, turnId),
    [enabled, state],
  )
  const retryInput = useCallback(async (turnId: string) => {
    const start = useApp.getState()
    if (!mounted.current || !current.current.enabled || pending.current || !available(start) || !continuableFailure(start, turnId)) return
    const request = Symbol()
    pending.current = request
    setRetryingTurn(turnId)
    try {
      if (!originalInput(start, turnId)) {
        try {
          if (!await start.loadUntilTurn(turnId)) return
        } catch {
          return
        }
      }
      const latest = useApp.getState()
      if (!mounted.current || !current.current.enabled || latest.activeId !== start.activeId ||
        latest.completionVisit !== start.completionVisit || !available(latest) || !continuableFailure(latest, turnId)) return
      if (!originalInput(latest, turnId)) return
      await current.current.continueFailed(turnId)
    } finally {
      if (pending.current === request) {
        pending.current = undefined
        if (mounted.current) setRetryingTurn(undefined)
      }
    }
  }, [])

  return { canRetryInput, retryInput, retryingTurn }
}
