/** Integrated terminal: one PTY per click, cwd chosen by the server. */

export const MAX_TERMINALS = 8

export type TerminalTarget = {
  threadId?: string
  projectId?: string
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
