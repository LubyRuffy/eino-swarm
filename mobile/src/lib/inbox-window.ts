import { GROUP_ATTENTION, type ProjectView, type RunningView, type ThreadView } from "./rpc"

/** The inbox is a loaded window, not a single page snapshot. `head` is the
 *  latest first page. `tail` is what More already brought in. A later first
 *  page replaces `head` only — otherwise the 2s poll puts those rows back. */
/** One inbox section the host pages on its own. `recent` is conversations
 *  with no project. A project id is that folder. */
export type InboxGroupState = {
  id: string
  more: boolean
  cursor: string
}

export const GROUP_RECENT = "recent"
export const INBOX_PREVIEW = 5

export type InboxWindow = {
  projects: ProjectView[]
  head: ThreadView[]
  tail: ThreadView[]
  attention: ThreadView[]
  attentionHead: string[]
  confirmedReads: Record<string, number>
  running: RunningView[]
  more: boolean
  cursor: string
  /** Set when the host sent per-section pages. The global More button stays
   *  off; each section carries its own. */
  grouped: boolean
  groups: InboxGroupState[]
}

export type InboxPage = {
  projects?: ProjectView[]
  threads?: ThreadView[]
  attention?: ThreadView[]
  attention_more?: boolean
  attention_cursor?: string
  running?: RunningView[]
  more?: boolean
  next?: string
  groups?: InboxGroupPage[]
}

export type InboxGroupPage = {
  id: string
  threads?: ThreadView[]
  more?: boolean
  next?: string
}

export function emptyInbox(): InboxWindow {
  return {
    projects: [],
    head: [],
    tail: [],
    running: [],
    attention: [],
    attentionHead: [],
    confirmedReads: {},
    more: false,
    cursor: "",
    grouped: false,
    groups: [],
  }
}

export function inboxThreads(window: InboxWindow): ThreadView[] {
  return window.head.concat(window.tail)
}

export function reduceInbox(
  prev: InboxWindow,
  page: InboxPage,
  mode: "replace" | "append",
  group?: string,
): InboxWindow {
  const attention = reduceAttention(prev, page, mode, group === GROUP_ATTENTION)
  const next = mode === "append" && group === GROUP_ATTENTION ? prev : reduceThreadInbox(prev, page, mode, group)
  return protectConfirmedReads({ ...next, confirmedReads: prev.confirmedReads, attention: attention.rows, attentionHead: attention.head,
    groups: [...next.groups.filter((g) => g.id !== GROUP_ATTENTION), attention.group] })
}

function reduceAttention(prev: InboxWindow, page: InboxPage, mode: "replace" | "append", attentionPage: boolean) {
  const prior = prev.groups.find((g) => g.id === GROUP_ATTENTION) ?? { id: GROUP_ATTENTION, more: false, cursor: "" }
  const incoming = dedupe(page.attention ?? []).filter((row) => row.unread)
  const group = { id: GROUP_ATTENTION, more: Boolean(page.attention_more), cursor: page.attention_cursor ?? "" }
  if (mode === "replace") return { rows: incoming, head: incoming.map((row) => row.id), group }
  const observed = [...(page.attention ?? []), ...(page.threads ?? [])]
  const rows = prev.attention.map((row) => observed.find((next) => next.id === row.id) ?? row).filter((row) => row.unread)
  return { rows: attentionPage ? dedupe([...rows, ...incoming]) : rows, head: prev.attentionHead,
    group: attentionPage ? group : prior }
}

function reduceThreadInbox(
  prev: InboxWindow, page: InboxPage, mode: "replace" | "append", group?: string,
): InboxWindow {
  const projects = page.projects ?? prev.projects
  const attention = page.attention ?? (mode === "append" ? prev.attention : [])
  const running = page.running ?? (mode === "append" ? prev.running : [])
  if (page.groups && mode === "replace") {
    return replaceGroups(prev, projects, running, attention, page.groups)
  }
  if (mode === "append" && group) {
    return appendGroup(prev, projects, running, attention, group, page)
  }
  const incoming = dedupe(page.threads ?? [])
  const plain = { grouped: false, groups: [] as InboxGroupState[], confirmedReads: prev.confirmedReads }
  if (mode === "append") {
    const known = new Set(inboxThreads(prev).map((row) => row.id))
    const tail = prev.tail.map((row) => incoming.find((next) => next.id === row.id) ?? row)
    for (const row of incoming) {
      if (!row.id || known.has(row.id)) continue
      tail.push(row)
      known.add(row.id)
    }
    return {
      ...plain,
      projects,
      head: prev.head,
      tail,
      running,
      attention,
      attentionHead: prev.attentionHead,
      more: Boolean(page.more),
      cursor: page.next ?? "",
    }
  }
  const headIds = new Set(incoming.map((row) => row.id))
  if (prev.tail.length === 0) {
    return {
      ...plain,
      projects,
      head: incoming,
      tail: [],
      running,
      attention,
      attentionHead: prev.attentionHead,
      more: Boolean(page.more),
      cursor: page.next ?? "",
    }
  }
  return {
    ...plain,
    projects,
    head: incoming,
    tail: prev.tail.filter((row) => row.id && !headIds.has(row.id)),
    running,
    attention,
    attentionHead: prev.attentionHead,
    more: prev.more,
    cursor: prev.cursor,
  }
}

function replaceGroups(
  prev: InboxWindow,
  projects: ProjectView[],
  running: RunningView[],
  attention: ThreadView[],
  pages: InboxGroupPage[],
): InboxWindow {
  const head = dedupe(pages.flatMap((g) => g.threads ?? []))
  const headIds = new Set(head.map((row) => row.id))
  const tail = prev.tail.filter((row) => row.id && !headIds.has(row.id))
  const groups = pages.map((g) => {
    const kept = prev.grouped && tail.some((row) => inGroup(row, g.id))
    const prior = prev.groups.find((row) => row.id === g.id)
    if (kept && prior) return prior
    return { id: g.id, more: Boolean(g.more), cursor: g.next ?? "" }
  })
  return {
    projects,
    head,
    tail,
    running,
    attention,
    attentionHead: prev.attentionHead,
    confirmedReads: prev.confirmedReads,
    more: false,
    cursor: "",
    grouped: true,
    groups,
  }
}

function appendGroup(
  prev: InboxWindow,
  projects: ProjectView[],
  running: RunningView[],
  attention: ThreadView[],
  group: string,
  page: InboxPage,
): InboxWindow {
  const incoming = dedupe(page.threads ?? [])
  const known = new Set(inboxThreads(prev).map((row) => row.id))
  const tail = prev.tail.slice()
  for (const row of incoming) {
    if (!row.id || known.has(row.id)) continue
    tail.push(row)
    known.add(row.id)
  }
  const groups = prev.groups.map((g) =>
    g.id === group ? { id: g.id, more: Boolean(page.more), cursor: page.next ?? "" } : g,
  )
  return {
    projects,
    head: prev.head,
    tail,
    running,
    attention,
    attentionHead: prev.attentionHead,
    confirmedReads: prev.confirmedReads,
    more: false,
    cursor: "",
    grouped: true,
    groups,
  }
}

function inGroup(row: ThreadView, group: string): boolean {
  if (group === GROUP_RECENT) return !row.project_id
  return row.project_id === group
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

/** A successful read is host-scoped and monotone for that completion identity. */
export function protectConfirmedReads(window: InboxWindow): InboxWindow {
  const protect = (row: ThreadView): ThreadView => row.unread && (row.completed_seq ?? 0) > 0
    && (window.confirmedReads[row.id] ?? 0) >= (row.completed_seq ?? 0) ? { ...row, unread: false } : row
  return { ...window, head: window.head.map(protect), tail: window.tail.map(protect),
    attention: window.attention.map(protect).filter((row) => row.unread) }
}
