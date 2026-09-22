import type { ProjectView, RunningView, ThreadView } from "./rpc"

/** The inbox is a loaded window, not a single page snapshot. `head` is the
 *  latest first page. `tail` is what More already brought in. A later first
 *  page replaces `head` only — otherwise the 2s poll puts those rows back. */
export type InboxWindow = {
  projects: ProjectView[]
  head: ThreadView[]
  tail: ThreadView[]
  running: RunningView[]
  more: boolean
  cursor: string
}

export type InboxPage = {
  projects?: ProjectView[]
  threads?: ThreadView[]
  running?: RunningView[]
  more?: boolean
  next?: string
}

export function emptyInbox(): InboxWindow {
  return { projects: [], head: [], tail: [], running: [], more: false, cursor: "" }
}

export function inboxThreads(window: InboxWindow): ThreadView[] {
  return window.head.concat(window.tail)
}

export function reduceInbox(
  prev: InboxWindow,
  page: InboxPage,
  mode: "replace" | "append",
): InboxWindow {
  const projects = page.projects ?? prev.projects
  const running = page.running ?? []
  const incoming = dedupe(page.threads ?? [])
  if (mode === "append") {
    const known = new Set(inboxThreads(prev).map((row) => row.id))
    const tail = prev.tail.map((row) => incoming.find((next) => next.id === row.id) ?? row)
    for (const row of incoming) {
      if (!row.id || known.has(row.id)) continue
      tail.push(row)
      known.add(row.id)
    }
    return {
      projects,
      head: prev.head,
      tail,
      running,
      more: Boolean(page.more),
      cursor: page.next ?? "",
    }
  }
  const headIds = new Set(incoming.map((row) => row.id))
  if (prev.tail.length === 0) {
    return {
      projects,
      head: incoming,
      tail: [],
      running,
      more: Boolean(page.more),
      cursor: page.next ?? "",
    }
  }
  return {
    projects,
    head: incoming,
    tail: prev.tail.filter((row) => row.id && !headIds.has(row.id)),
    running,
    more: prev.more,
    cursor: prev.cursor,
  }
}

function dedupe(rows: ThreadView[]): ThreadView[] {
  const seen = new Set<string>()
  const out: ThreadView[] = []
  for (const row of rows) {
    if (!row.id || seen.has(row.id)) continue
    seen.add(row.id)
    out.push(row)
  }
  return out
}
