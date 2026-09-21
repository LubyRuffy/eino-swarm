import type { ThreadDetail, WakeView } from "./rpc"

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

export function isLiveThreadWake(payload: ArmedSchedulePayload | undefined): boolean {
  if (!payload) return false
  const kind = (payload.kind ?? "thread").trim() || "thread"
  const status = (payload.status ?? "active").trim() || "active"
  return kind === "thread" && status === "active"
}

export function scheduleHeadline(row: { title?: string; prompt?: string }): string {
  const title = (row.title ?? "").trim()
  if (title) return title
  return (row.prompt ?? "").trim()
}

export function laterScheduleDue(a?: string, b?: string): string {
  const left = (a ?? "").trim()
  const right = (b ?? "").trim()
  const ta = Date.parse(left)
  const tb = Date.parse(right)
  if (Number.isNaN(ta)) return right || left
  if (Number.isNaN(tb)) return left
  return ta >= tb ? left : right
}

export function applyWakeDetail(
  detail: ThreadDetail,
  ev: { kind: string; text?: string },
): ThreadDetail {
  if (ev.kind === "schedule") {
    const payload = parseArmedSchedule(ev.text)
    if (!isLiveThreadWake(payload) || !payload?.id) return detail
    const target = (payload.thread_id ?? "").trim() || detail.id
    if (target && target !== detail.id) return detail
    const existing = detail.wake
    const wake: WakeView = {
      id: payload.id,
      title: payload.title ?? existing?.title ?? "",
      prompt: payload.prompt ?? existing?.prompt ?? "",
      next_run_at: laterScheduleDue(existing?.next_run_at, payload.next_run_at),
    }
    return { ...detail, waiting: true, wake }
  }
  if (ev.kind === "schedule_cancelled") {
    const id = (ev.text ?? "").trim()
    if (id && detail.wake?.id && detail.wake.id !== id) return detail
    return { ...detail, waiting: false, wake: undefined }
  }
  if (ev.kind === "schedule_fired") {
    return { ...detail, waiting: false, wake: undefined }
  }
  return detail
}
