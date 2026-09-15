// SSE store bridging swarm notifications into React state.
import { useSyncExternalStore } from "react"
import { apply, newSwarmState, type SwarmState, type Notification } from "./state"

export interface RunStatus {
  connected: boolean
  running: boolean
}

let state: SwarmState = newSwarmState()
let connected = false
let listeners = new Set<() => void>()
let es: EventSource | null = null
let snapshot = 0

function bump() {
  snapshot++
  listeners.forEach(l => l())
}

export function connect() {
  if (es) return
  es = new EventSource("/events")
  es.onopen = () => { connected = true; bump() }
  es.onerror = () => { connected = false; bump() }
  es.onmessage = e => {
    try {
      const n = JSON.parse(e.data) as Notification
      apply(state, n)
      bump()
    } catch { /* ignore malformed */ }
  }
}

export async function startTask(task: string) {
  await fetch("/run", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ task }),
  })
}

export function useSwarm(): RunStatus & { state: SwarmState } {
  useSyncExternalStore(
    l => { listeners.add(l); return () => listeners.delete(l) },
    () => snapshot,
    () => snapshot,
  )
  return { state, connected, running: false }
}
