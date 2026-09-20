export const PROTOCOL_V = 1

export const OpList = "list"
export const OpMore = "more"
export const OpOpen = "open"
export const OpStart = "start"
export const OpSend = "send"
export const OpSteer = "steer"
export const OpStop = "stop"
export const OpAnswer = "answer"
export const OpWatch = "watch"
export const OpUnwatch = "unwatch"
export const OpLog = "log"
export const OpEvent = "event"
export const OpReady = "ready"
export const OpLagged = "lagged"

export type RemoteRequest = {
  v: number
  id: string
  op: string
  cursor?: string
  thread_id?: string
  project_id?: string
  text?: string
  call_id?: string
  answers?: unknown
  since?: number
  before?: number
}

export type ProjectView = { id: string; name: string }

export type ThreadView = {
  id: string
  title: string
  project_id?: string
  running: boolean
  last_active_at: string
  summary?: string
}

export type RunningView = {
  thread_id: string
  title: string
  turn_id?: string
  action?: string
  ask_user?: boolean
}

export type TurnView = {
  id: string
  status: string
  text?: string
}

export type WatchStatus = {
  running?: boolean
  turn_id?: string
  awaiting_answer?: boolean
}

export type RemoteEvent = {
  thread_id: string
  turn_id?: string
  seq: number
  kind: string
  agent_id?: string
  role?: string
  text: string
  tool_call_id?: string
  err?: string
  has_images?: boolean
  created_at: string
}

export type ThreadDetail = {
  id: string
  title: string
  goal?: string
  goal_on?: boolean
  plan_on?: boolean
  running?: RunningView
  turns?: TurnView[]
}

export type RemoteResponse = {
  v: number
  id: string
  ok: boolean
  error?: string
  code?: string
  path?: string
  session_id?: string
  projects?: ProjectView[]
  threads?: ThreadView[]
  running?: RunningView[]
  more?: boolean
  next?: string
  detail?: ThreadDetail
  op?: string
  thread_id?: string
  seq?: number
  event?: RemoteEvent
  events?: RemoteEvent[]
  status?: WatchStatus
}

export function encodeRequest(req: RemoteRequest): Uint8Array {
  return new TextEncoder().encode(JSON.stringify(req))
}

export function decodeResponse(raw: Uint8Array): RemoteResponse {
  const text = new TextDecoder().decode(raw)
  const v = JSON.parse(text) as RemoteResponse
  if (typeof v !== "object" || v == null) throw new Error("not json")
  return v
}

let seq = 0
export function nextRPCId(): string {
  seq += 1
  return "m" + String(seq)
}

export function slimListBytes(resp: RemoteResponse): number {
  return new TextEncoder().encode(JSON.stringify(resp)).length
}
