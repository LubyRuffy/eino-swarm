export const PROTOCOL_V = 1

export const OpList = "list"
export const OpMore = "more"
export const GROUP_ATTENTION = "attention"
export const OpOpen = "open"
export const OpRead = "read"
export const OpStart = "start"
export const OpSend = "send"
export const OpSteer = "steer"
export const OpStop = "stop"
export const OpAnswer = "answer"
export const OpContinue = "continue"
export const OpWatch = "watch"
export const OpUnwatch = "unwatch"
export const OpLog = "log"
export const OpEvent = "event"
export const OpReady = "ready"
export const OpLagged = "lagged"
export const OpRunNow = "run_now"
export const OpCancelWait = "cancel_wait"
export const OpResumeGoal = "resume_goal"
export const OpHello = "hello"
export const OpMeta = "meta"
export const OpProjectGet = "project_get"
export const OpProjectCreate = "project_create"
export const OpProjectUpdate = "project_update"
export const OpCatalog = "catalog"
export const OpRefreshCatalog = "refresh_catalog"
export const OpGetTimeouts = "get_timeouts"
export const OpSetTimeouts = "set_timeouts"
export const OpTune = "tune"
export const OpPut = "put"
export const OpFollowupDrop = "followup_drop"
export const OpFollowupSteer = "followup_steer"
export const OpPreempt = "preempt"
export const OpClients = "clients"
export const OpClientRead = "client_read"
export const OpFiles = "files"
export const OpFileChunk = "file_chunk"
export const GROUP_RECENT = "recent"

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
  continue?: boolean
  completed_seq?: number
  since?: number
  include_detail?: boolean
  before?: number
  provider_id?: string
  model?: string
  reasoning?: string
  web_execution?: "" | "auto" | "local" | "cloud"
  puts?: string[]
  put_id?: string
  name?: string
  mime?: string
  part?: number
  parts?: number
  data?: string
  group?: string
  followup_id?: string
  task_id?: string
  file_path?: string
  project_patch?: ProjectPatch
  timeout_settings?: TimeoutSettingsPatch
}

export type TimeoutSettings = {
  agent_timeout_seconds: number
  providers: {
    id: string
    label: string
    timeout_seconds: number
    first_byte_timeout_seconds: number
    effective_first_byte_timeout_seconds: number
  }[]
}

export type TimeoutSettingsPatch = {
  agent_timeout_seconds?: number
  providers?: { id: string; timeout_seconds?: number; first_byte_timeout_seconds?: number }[]
}

export type FollowupView = {
  id: string
  seq: number
  text: string
}

export type ThreadGroup = {
  id: string
  threads?: ThreadView[]
  more?: boolean
  next?: string
}

export type ModelChoice = {
  provider_id: string
  provider_label?: string
  /** Shown instead of model when the server named the row. */
  label?: string
  model: string
  default?: boolean
  default_follows_setting?: boolean
  ready?: boolean
  /** The only row while official presentation hides the list. */
  locked?: boolean
}

export type ProjectSettings = {
  id: string
  name: string
  system_prompt: string
  workdir: string
  resolved_workdir?: string
  memory_enabled: boolean
  nuwa_enabled: boolean
  nuwa_planner_provider_id?: string
  nuwa_planner_model?: string
  nuwa_executor_provider_id?: string
  nuwa_executor_model?: string
}
export type ProjectPatch = Partial<Omit<ProjectSettings, "id" | "resolved_workdir">>

export type ProjectView = { id: string; name: string }

export type ThreadView = {
  id: string
  title: string
  project_id?: string
  running: boolean
  waiting?: boolean
  last_active_at: string
  unread?: boolean
  completed_seq?: number
  web_execution?: "auto" | "local" | "cloud"
  summary?: string
}

export type RunningView = {
  thread_id: string
  title: string
  turn_id?: string
  action?: string
  ask_user?: boolean
  awaiting_continue?: boolean
  waiting?: boolean
  last_active_at?: string
  // Which project folder this live row belongs to. list omits it from
  // threads; the inbox still paints it under that project.
  project_id?: string
}

export type TurnView = {
  id: string
  status: string
  text?: string
  web_execution?: "auto" | "local" | "cloud"
}

export type WatchStatus = {
  running?: boolean
  turn_id?: string
  awaiting_answer?: boolean
  awaiting_continue?: boolean
  waiting?: boolean
  wake?: WakeView
  followups?: FollowupView[]
}

export type WakeView = {
  id: string
  title?: string
  prompt?: string
  next_run_at?: string
}

export type RemoteEvent = {
  message_id?: string
  source_thread_id?: string
  source_turn_id?: string
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
  unread?: boolean
  completed_seq?: number
  id: string
  title: string
  goal?: string
  goal_on?: boolean
  goal_complete?: boolean
  goal_blocked?: boolean
  goal_block_reason?: string
  goal_capped?: boolean
  goal_idle?: boolean
  goal_started_at?: string
  plan_on?: boolean
  provider_id?: string
  model?: string
  reasoning?: string
  web_execution?: "auto" | "local" | "cloud"
  waiting?: boolean
  wake?: WakeView
  running?: RunningView
  turns?: TurnView[]
}

export type EngineStartup = {
  startup?: "restoring" | "ready"
  restore?: { completed: number; total: number; failed?: number }
  data_generation?: number
}

export type PcWebExecutionStatus = {
  preference: "auto" | "local" | "cloud"
  execution: "local" | "cloud"
  /** PC-owned account presence; tool grants remain live per invocation. */
  cloud_account: "signed_in" | "sign_in_required" | "unavailable"
}

export type RemoteResponse = EngineStartup & {
  v: number
  id: string
  ok: boolean
  error?: string
  code?: string
  path?: string
  session_id?: string
  host?: string
  project?: ProjectSettings
  memory_available?: boolean
  composer_predictions?: boolean
  projects?: ProjectView[]
  threads?: ThreadView[]
  attention?: ThreadView[]
  attention_more?: boolean
  attention_cursor?: string
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
  models?: ModelChoice[]
  reasoning_levels?: string[]
  web_execution_supported?: boolean
  web_execution_status?: PcWebExecutionStatus
  put?: { id: string; name?: string; ready: boolean; kind?: string }
  groups?: ThreadGroup[]
  followups?: FollowupView[]
  clients?: ClientCatalog
  client_view?: ClientView
  timeout_settings?: TimeoutSettings
  files?: WorkspaceFile[]
  file_chunk?: FileChunk
}

export type WorkspaceFile = {
  path: string
  name: string
  size: number
  dir: boolean
  modified: string
  uploaded: boolean
}

export type FileChunk = {
  path: string
  name: string
  size: number
  mime: string
  offset: number
  next_offset: number
  data: string
  more: boolean
}

export type ClientEntry = { role: string; text: string; at?: number; n?: number }

export type ClientView = {
  id: string
  title: string
  status: string
  entries: ClientEntry[]
  older?: boolean
  before?: number
}

export type ClientTask = {
  id: string
  title: string
  cwd?: string
  status: "running" | "done" | string
  updated_at: string
  older?: boolean
}

export type ClientTool = {
  id: string
  tasks: ClientTask[]
  more: boolean
  next?: string
}

export type ClientCatalog = {
  enabled: boolean
  pending?: boolean
  tools?: ClientTool[]
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
