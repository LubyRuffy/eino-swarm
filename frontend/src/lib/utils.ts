import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/** Human-readable byte size for the Files panel. */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ["KB", "MB", "GB", "TB"]
  let value = bytes / 1024
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[i]}`
}

/** Elapsed time the way a person would say it: "12s", "3m 20s". */
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.max(ms, 0)}ms`
  const total = Math.round(ms / 1000)
  if (total < 60) return `${total}s`
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  if (minutes < 60) return seconds ? `${minutes}m ${seconds}s` : `${minutes}m`
  const hours = Math.floor(minutes / 60)
  return `${hours}h ${minutes % 60}m`
}

/** Sidebar grouping: people look for "yesterday", not for a date. */
export function relativeDay(iso: string, now = new Date()): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return "Earlier"
  const startOf = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
  const days = Math.round((startOf(now) - startOf(date)) / 86_400_000)
  if (days <= 0) return "Today"
  if (days === 1) return "Yesterday"
  if (days < 7) return "This week"
  if (days < 30) return "This month"
  return "Earlier"
}

/** Hidden-inset traffic lights only exist on the macOS desktop window. */
export function isMac(): boolean {
  if (typeof navigator === "undefined") return false
  return /Mac/.test(navigator.platform || "") || /Mac OS X/.test(navigator.userAgent)
}

const SIDEBAR_KEY = "zwai.sidebar"

/** Conversation list visibility. Missing or unreadable storage means open —
 *  hiding the list on first launch would look like the app forgot its chrome. */
export function readSidebarOpen(): boolean {
  try {
    return localStorage.getItem(SIDEBAR_KEY) !== "0"
  } catch {
    return true
  }
}

export function writeSidebarOpen(open: boolean): void {
  try {
    localStorage.setItem(SIDEBAR_KEY, open ? "1" : "0")
  } catch {
    // A preference is not worth failing to start over.
  }
}

/** Clock time for a timeline row. */
export function formatTime(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ""
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })
}
