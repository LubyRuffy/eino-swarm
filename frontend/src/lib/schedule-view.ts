import type { Schedule } from "@/lib/types"

export function scheduleHeadline(row: Pick<Schedule, "title" | "prompt">): string {
  const title = (row.title ?? "").trim()
  if (title) return title
  return (row.prompt ?? "").trim()
}

/** Live waits first so a just-armed row is not buried under done fires. */
export function inboxScheduleOrder(rows: Schedule[]): Schedule[] {
  const live = (row: Schedule) => row.status === "active" || row.status === "paused"
  return [...rows].sort((a, b) => {
    const rank = (live(a) ? 0 : 1) - (live(b) ? 0 : 1)
    if (rank !== 0) return rank
    return (a.next_run_at || "").localeCompare(b.next_run_at || "")
  })
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
    next_run_at: payload.next_run_at || existing?.next_run_at || "",
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
