import { useSyncExternalStore } from "react"

/** The foreign session currently filling the main chat. It is not a zwai thread. */
export type OpenClient = {
  id: string
  title: string
  status: string
}

let current: OpenClient | null = null
const listeners = new Set<() => void>()

function emit() {
  for (const listener of listeners) listener()
}

export function openClient(task: OpenClient) {
  current = { id: task.id, title: task.title, status: task.status }
  emit()
}

export function updateOpenClient(patch: Partial<OpenClient>) {
  if (!current) return
  const next = { ...current, ...patch, id: current.id }
  if (next.title === current.title && next.status === current.status) return
  current = next
  emit()
}

export function closeClient() {
  if (!current) return
  current = null
  emit()
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useOpenClient(): OpenClient | null {
  return useSyncExternalStore(subscribe, () => current, () => null)
}
