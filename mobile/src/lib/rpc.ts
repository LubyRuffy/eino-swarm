import type { ModelPolicy } from "../../../frontend/src/lib/model-policy"
export const PROTOCOL_V = 1

export const OpList = "list"
export const OpMore = "more"
export const GROUP_ATTENTION = "attention"
export const OpOpen = "open"
export const OpRead = "read"
export const OpStart = "start"
export const OpSend = "send"
export const OpEditInput = "edit_input"
export const OpDeliver = "deliver"
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
export const OpSetProductMode = "set_product_mode"
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
export const OpGeneratedImageChunk = "generated_image_chunk"
export const OpInputImageChunk = "input_image_chunk"
export const OpTaskPanels = "task_panels"
export const OpGraph = "graph"
export const OpTaskPanelRead = "task_panel_read"
export const OpTaskPanelCancel = "task_panel_cancel"
export const OpTaskPanelClose = "task_panel_close"
export const OpTranslateSelection = "translate_selection"
export const OpCancelTranslation = "cancel_translation"
export const OpSpeechConfig = "speech_config"
export const OpAudioTranscribe = "audio_transcribe"
export const OpAudioSpeech = "audio_speech"
export const OpAudioChunk = "audio_chunk"
export const OpAudioCancel = "audio_cancel"
export const GROUP_RECENT = "recent"

export type RemoteRequest = {
  product_mode?: "local" | "fofa"
  v: number
  id: string
  op: string
  cursor?: string
  thread_id?: string
  project_id?: string
  text?: string
  from_event_seq?: number
  replacement?: string
  mode?: import("../../../frontend/src/lib/delivery-mode").DeliveryMode
  expected_turn_id?: string
  call_id?: string
  answers?: unknown
  continue?: boolean
  completed_seq?: number
  since?: number
  graph_owner?: string
  include_detail?: boolean
  before?: number
  compact_history?: boolean
  work_from?: number
  work_through?: number
  provider_id?: string
  model?: string
  reasoning?: string
  web_execution?: "" | "auto" | "local" | "cloud"
  puts?: string[]
  put_id?: string
  purpose?: "audio"
  audio_id?: string
  offset?: number
  name?: string
  mime?: string
  part?: number
  parts?: number
  data?: string
  group?: string
  followup_id?: string
  task_id?: string
  file_path?: string
  file_scope?: "task" | "project"
  file_root?: string
  image_id?: string
  panel_id?: string
  easm_panel_version?: string
  base_revision?: number
  transfer_id?: string
  /** OpFiles: one workspace directory. Empty or omitted is the root. */
  directory?: string
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
  attachments?: number
}

export type ThreadGroup = {
  id: string
  threads?: ThreadView[]
  more?: boolean
  next?: string
}

export const OpRefreshCapabilities = "refresh_capabilities"

export type ModelChoice = {
  capabilities?: import("../../../frontend/src/lib/capabilities").SourceCapabilities
  catalog_group?: "original" | "third_party"
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
}
export type ProjectPatch = Partial<Omit<ProjectSettings, "id" | "resolved_workdir">>

export type ProjectView = { id: string; name: string }

export type ScheduleSource = {
  schedule_id: string
  origin_thread_id?: string
  origin_title?: string
  origin_available?: boolean
}

export type ThreadView = {
  schedule_source?: ScheduleSource
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
  schedule_source?: ScheduleSource
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
  user_text?: string
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
  image_ids?: string[]
  generated_image?: import("../../../frontend/src/lib/types").ImageRef
  created_at: string
}

export type ThreadDetail = {
  unread?: boolean
  completed_seq?: number
  id: string
  project_id?: string
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
  graph_available?: boolean
  composer_predictions?: boolean
  model_policy?: ModelPolicy
  product_mode?: "fofa" | "local"
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
  /** Live-edge watch only. Running workers whose spawn is outside this page. */
  roster?: RemoteEvent[]
  history_work?: HistoryWork[]
  focus_seq?: number
  history_before?: number
  status?: WatchStatus
  models?: ModelChoice[]
  reasoning_levels?: string[]
  web_execution_supported?: boolean
  web_execution_status?: PcWebExecutionStatus
  put?: { id: string; name?: string; ready: boolean; kind?: string }
  groups?: ThreadGroup[]
  followups?: FollowupView[]
  delivery?: { turn_id?: string; steer_seq?: number; preempted?: boolean }
  clients?: ClientCatalog
  client_view?: ClientView
  timeout_settings?: TimeoutSettings
  files?: WorkspaceFile[]
  file_view?: import("../../../frontend/src/lib/types").FileView
  file_chunk?: FileChunk
  task_panels?: TaskPanelManifest[]
  graph?: GraphReply
  panel_transfer?: TaskPanelTransfer
  translation?: string
  speech?: import("../../../frontend/src/lib/speech-types").SpeechSnapshot
  audio?: { audio_id: string; text?: string; mime?: string; size?: number; offset?: number; next?: number; data?: string; more?: boolean }
}

export type HistoryWork = { from: number; through: number; agent_ids?: string[] }

/** A small discovery view; plugin definitions and data use a frozen transfer. */
export type TaskPanelManifest = {
  id: string
  thread_id: string
  plugin_id: string
  plugin_version: string
  revision: number
  source_status?: string
}

export type TaskPanelTransfer = {
  id: string
  thread_id: string
  panel_id: string
  revision: number
  size: number
  sha256: string
  offset: number
  next_offset: number
  data: string
  more: boolean
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

/** A phone that sends the revision and owner it holds gets unchanged and no rows. */
export type GraphReply = {
  owner?: string
  revision: number
  unchanged?: boolean
  subjects?: import("../../../frontend/src/lib/graph/types").GraphSubject[]
  links?: import("../../../frontend/src/lib/graph/types").GraphLink[]
  todos?: import("../../../frontend/src/lib/graph/types").GraphTodo[]
  total?: number
  more?: boolean
}
