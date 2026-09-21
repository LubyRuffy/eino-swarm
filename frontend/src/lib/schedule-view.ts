import type { Schedule, ScheduleRun } from "@/lib/types"

export function scheduleHeadline(row: Pick<Schedule, "title" | "prompt">): string {
  const title = (row.title ?? "").trim()
  if (title) return title
  return (row.prompt ?? "").trim()
}

/** Unread findings/errors the inbox can open, newest fire first. */
export function unreadFindings(runs: ScheduleRun[]): ScheduleRun[] {
  return runs
    .filter(
      (run) =>
        run.unread &&
        (run.status === "findings" || run.status === "error") &&
        Boolean(run.thread_id?.trim()),
    )
    .sort((a, b) => {
      const tb = Date.parse(b.created_at)
      const ta = Date.parse(a.created_at)
      if (!Number.isNaN(tb) && !Number.isNaN(ta) && tb !== ta) return tb - ta
      return b.id.localeCompare(a.id)
    })
}

/** A wait the human can still pause, resume, cancel, or run. */
export function isLiveSchedule(row: Pick<Schedule, "status">): boolean {
  const status = (row.status ?? "").trim()
  return status !== "done" && status !== "cancelled"
}

/** Inbox status tabs. Completed is done + cancelled. */
export type InboxFilter = "all" | "active" | "paused" | "completed"

export function inboxStatusTab(
  status: string | undefined,
): Exclude<InboxFilter, "all"> {
  const s = (status ?? "").trim()
  if (s === "paused") return "paused"
  if (s === "done" || s === "cancelled") return "completed"
  return "active"
}

export function inboxFilterMatch(
  row: Pick<Schedule, "status">,
  filter: InboxFilter,
): boolean {
  if (filter === "all") return true
  return inboxStatusTab(row.status) === filter
}

export function inboxSearchMatch(
  row: Pick<Schedule, "title" | "prompt">,
  query: string,
): boolean {
  const needle = query.trim().toLowerCase()
  if (!needle) return true
  const title = (row.title ?? "").toLowerCase()
  const prompt = (row.prompt ?? "").toLowerCase()
  return title.includes(needle) || prompt.includes(needle)
}

/** Live waits first so a just-armed row is not buried under done fires. */
export function inboxScheduleOrder(rows: Schedule[]): Schedule[] {
  return [...rows].sort((a, b) => {
    const rank = (isLiveSchedule(a) ? 0 : 1) - (isLiveSchedule(b) ? 0 : 1)
    if (rank !== 0) return rank
    return (a.next_run_at || "").localeCompare(b.next_run_at || "")
  })
}

export function inboxFilteredSchedules(
  rows: Schedule[],
  filter: InboxFilter,
  query: string,
): Schedule[] {
  return inboxScheduleOrder(
    rows.filter(
      (row) => inboxFilterMatch(row, filter) && inboxSearchMatch(row, query),
    ),
  )
}

export type CadenceSpec =
  | { kind: "every"; seconds: number }
  | { kind: "delay"; seconds: number }
  | { kind: "cron"; expr: string }
  | { kind: "none" }

export function scheduleCadenceSpec(
  row: Pick<Schedule, "delay_s" | "every_s" | "cron">,
): CadenceSpec {
  const cron = (row.cron ?? "").trim()
  if (cron) return { kind: "cron", expr: cron }
  if (row.every_s > 0) return { kind: "every", seconds: row.every_s }
  if (row.delay_s > 0) return { kind: "delay", seconds: row.delay_s }
  return { kind: "none" }
}

export type CadenceUnit = "second" | "minute" | "hour"

export function cadenceAmount(seconds: number): { n: number; unit: CadenceUnit } {
  if (seconds > 0 && seconds % 3600 === 0) {
    return { n: seconds / 3600, unit: "hour" }
  }
  if (seconds > 0 && seconds % 60 === 0) {
    return { n: seconds / 60, unit: "minute" }
  }
  return { n: Math.max(0, seconds), unit: "second" }
}

export function isScheduleDue(nextRunAt: string, nowMs = Date.now()): boolean {
  const t = Date.parse(nextRunAt)
  if (Number.isNaN(t)) return false
  return t <= nowMs
}

export type ArmedSchedulePayload = {
  id?: string
  kind?: string
  title?: string
  prompt?: string
  thread_id?: string
  origin_thread_id?: string
  status?: string
  next_run_at?: string
}

export function parseArmedSchedule(text?: string): ArmedSchedulePayload | undefined {
  const raw = text?.trim() ?? ""
  if (!raw.startsWith("{")) return undefined
  try {
    const body = JSON.parse(raw) as ArmedSchedulePayload
    if (typeof body.id !== "string" || !body.id.trim()) return undefined
    return body
  } catch {
    return undefined
  }
}

/** A `schedule` chip that still owns the next turn on this conversation. */
export function isLiveThreadWake(payload: ArmedSchedulePayload | undefined): boolean {
  if (!payload) return false
  const kind = (payload.kind ?? "thread").trim() || "thread"
  const status = (payload.status ?? "active").trim() || "active"
  return kind === "thread" && status === "active"
}

/** Active thread wake targeting this conversation — the composer banner. */
export function activeWake(
  schedules: Schedule[] | undefined,
  threadId: string | undefined,
): Schedule | undefined {
  if (!threadId) return undefined
  return (schedules ?? []).find(
    (row) =>
      row.kind === "thread" &&
      row.thread_id === threadId &&
      row.status === "active",
  )
}

/** Prefer the later due time so a replayed arm chip cannot rewind the clock. */
export function laterScheduleDue(a?: string, b?: string): string {
  const left = (a ?? "").trim()
  const right = (b ?? "").trim()
  const ta = Date.parse(left)
  const tb = Date.parse(right)
  if (Number.isNaN(ta)) return right || left
  if (Number.isNaN(tb)) return left
  return ta >= tb ? left : right
}

/** Paint the composer banner from the chip payload before GET returns. */
export function applyArmedSchedule(
  rows: Schedule[],
  payload: ArmedSchedulePayload | undefined,
  originThreadId: string,
): Schedule[] {
  if (!payload) return rows
  const id = payload.id?.trim()
  if (!id) return rows
  const kind = payload.kind?.trim() || "thread"
  const threadId =
    payload.thread_id?.trim() || (kind === "thread" ? originThreadId : "")
  const existing = rows.find((row) => row.id === id)
  const next: Schedule = {
    id,
    kind,
    origin_thread_id:
      payload.origin_thread_id?.trim() ||
      existing?.origin_thread_id ||
      originThreadId,
    thread_id: threadId || existing?.thread_id || "",
    project_id: existing?.project_id ?? "",
    provider_id: existing?.provider_id ?? "",
    model: existing?.model ?? "",
    title: payload.title ?? existing?.title ?? "",
    prompt: payload.prompt ?? existing?.prompt ?? "",
    delay_s: existing?.delay_s ?? 0,
    every_s: existing?.every_s ?? 0,
    cron: existing?.cron ?? "",
    status: payload.status?.trim() || existing?.status || "active",
    // The arm event is a snapshot. Replaying it after GET advanced
    // next_run_at used to freeze the banner on the first due slot.
    next_run_at: laterScheduleDue(existing?.next_run_at, payload.next_run_at),
    run_count: existing?.run_count ?? 0,
    max_runs: existing?.max_runs ?? 0,
    created_by: existing?.created_by || "manager",
    created_at: existing?.created_at || new Date().toISOString(),
    updated_at: new Date().toISOString(),
  }
  if (existing) return rows.map((row) => (row.id === id ? next : row))
  return [next, ...rows]
}

export function applyCancelledSchedule(rows: Schedule[], id: string): Schedule[] {
  const target = id.trim()
  if (!target) return rows
  return rows.map((row) =>
    row.id === target ? { ...row, status: "cancelled" } : row,
  )
}

/** A claimed fire is no longer a parked wait. Recurring rows come back on GET. */
export function applyFiredThreadWake(rows: Schedule[], threadId: string): Schedule[] {
  if (!threadId) return rows
  return rows.map((row) =>
    row.kind === "thread" && row.status === "active" && row.thread_id === threadId
      ? { ...row, status: "done" }
      : row,
  )
}

/** A GET that started before the arm must not wipe the composer banner.
 *  Only the open conversation's live wait is kept: a cancelled wait already
 *  cleared `waiting`, so an empty GET is then the truth. */
export function keepArmedWakes(
  local: Schedule[],
  incoming: Schedule[],
  opts: { waiting?: boolean; threadId?: string },
): Schedule[] {
  if (!opts.waiting || !opts.threadId) return incoming
  const got = new Set(incoming.map((row) => row.id))
  const keep = local.filter(
    (row) =>
      row.kind === "thread" &&
      row.status === "active" &&
      row.thread_id === opts.threadId &&
      !got.has(row.id),
  )
  return keep.length ? [...keep, ...incoming] : incoming
}
