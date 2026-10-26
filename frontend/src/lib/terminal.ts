import { normalizeSelectedText, type PageSelection, type SelectionRect } from "@/lib/selection"

/** Integrated terminal: one PTY per click, cwd chosen by the server. */

export const MAX_TERMINALS = 8

export type TerminalTarget = {
  threadId?: string
  projectId?: string
}

export const TERMINAL_SELECTION_EVENT = "zwai:terminal-selection"

export interface TerminalSelection {
  source: HTMLElement
  selection: PageSelection | null
  clear: () => void
}

/** Keep provenance in the existing editable quotation, so draft persistence,
 * replay and model input all retain the same source. cwd is only the initial
 * directory: a PTY does not tell us what later cd/ssh commands changed. */
export function terminalQuote(
  text: string,
  source: TerminalTarget & { id: string; cwd?: string; shell?: string },
): string {
  const selected = normalizeSelectedText(text)
  if (!selected) return ""
  const fields = [`Session: ${JSON.stringify(source.id)}`]
  if (source.threadId) fields.push(`Conversation: ${JSON.stringify(source.threadId)}`)
  if (source.projectId) fields.push(`Project: ${JSON.stringify(source.projectId)}`)
  if (source.cwd) fields.push(`Initial directory: ${JSON.stringify(source.cwd)}`)
  if (source.shell) fields.push(`Shell: ${JSON.stringify(source.shell)}`)
  return `Source: integrated terminal\n\n${selected}\n\n${fields.join("\n")}`
}

/** Public xterm buffer positions include scrollback; menu coordinates use only
 * visible cells. An exclusive end at column zero belongs to the preceding row. */
export function terminalSelectionRect(
  range: { start: { x: number; y: number }; end: { x: number; y: number } },
  viewportY: number,
  cols: number,
  rows: number,
  bounds: Pick<DOMRect, "top" | "left" | "width" | "height">,
): SelectionRect | null {
  const first = range.start.y - viewportY
  const last = range.end.y - viewportY - (range.end.x === 0 ? 1 : 0)
  if (last < 0 || first >= rows) return null
  const top = bounds.top + Math.max(0, first) * bounds.height / rows
  const bottom = bounds.top + (Math.min(rows - 1, last) + 1) * bounds.height / rows
  const single = first === last
  return {
    top, bottom, height: bottom - top,
    left: bounds.left + (single ? range.start.x * bounds.width / cols : 0),
    width: single ? ((range.end.x || cols) - range.start.x) * bounds.width / cols : bounds.width,
  }
}

export function canOpenTerminal(target?: TerminalTarget): boolean {
  return Boolean(target?.threadId || target?.projectId)
}

/** Prefer the open conversation: that is where the agents are working. */
export function terminalTarget(
  threadId?: string,
  projectId?: string,
): TerminalTarget | undefined {
  if (threadId) return { threadId }
  if (projectId) return { projectId }
  return undefined
}

export function terminalSocketURL(
  target: TerminalTarget,
  cols: number,
  rows: number,
  loc: Pick<Location, "protocol" | "host"> = window.location,
): string {
  const proto = loc.protocol === "https:" ? "wss:" : "ws:"
  const params = new URLSearchParams({
    cols: String(Math.max(1, cols)),
    rows: String(Math.max(1, rows)),
  })
  if (target.threadId) {
    return `${proto}//${loc.host}/api/threads/${encodeURIComponent(target.threadId)}/terminal?${params}`
  }
  if (target.projectId) {
    return `${proto}//${loc.host}/api/projects/${encodeURIComponent(target.projectId)}/terminal?${params}`
  }
  throw new Error("a conversation or a project is required")
}

export function terminalTabLabel(cwd: string | undefined, untitled: string): string {
  if (!cwd) return untitled
  const trimmed = cwd.replace(/[\\/]+$/, "")
  const base = trimmed.split(/[\\/]/).pop()
  return base || untitled
}

export function terminalShortcut(
  e: Pick<KeyboardEvent, "key" | "metaKey" | "ctrlKey" | "shiftKey" | "altKey">,
): boolean {
  if (e.shiftKey || e.altKey) return false
  if (!(e.metaKey || e.ctrlKey)) return false
  return e.key === "j" || e.key === "J"
}
