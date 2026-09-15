/** The wire contract with the Go server. Kept in one file so a change in the
 *  backend shows up here as a type error rather than as a blank panel. */

export type EventKind =
  | "user_message"
  | "agent_message"
  | "reasoning"
  | "reasoning_delta"
  | "delta"
  | "turn"
  | "spawned"
  | "finished"
  | "tool_call"
  | "tool_result"
  | "steer"
  | "cleanup"
  | "done"
  | "error"

export interface SwarmEvent {
  thread_id: string
  turn_id: string
  /** Gap-free per conversation. 0 means a streamed delta, which is not stored
   *  and therefore cannot be resumed from. */
  seq: number
  kind: EventKind | string
  agent_id: string
  role?: string
  text?: string
  tool_call_id?: string
  err?: string
  created_at: string
}

export interface Thread {
  id: string
  title: string
  provider_id: string
  /** This conversation's thinking level: "" (model default), low, medium,
   *  high. Switchable in the composer, applied from the next turn. */
  reasoning_effort: string
  archived: boolean
  created_at: string
  last_active_at: string
  /** Set by the server from its live runtimes, so the sidebar can show a
   *  conversation working even while another one is on screen. */
  running: boolean
}

export interface ThreadStatus {
  thread_id?: string
  running: boolean
  /** The current turn while running, and the last one afterwards, which is
   *  what the header offers to copy for `zwai trace`. */
  turn_id?: string
  started_at?: string
  workers?: number
}

export interface Turn {
  id: string
  thread_id: string
  seq: number
  status: "running" | "done" | "error" | "cancelled" | string
  user_text: string
  final: string
  error?: string
  provider_id: string
  model: string
  reasoning_effort?: string
  started_at: string
  ended_at?: string
  duration_ms: number
}

export interface FileEntry {
  /** Relative to the conversation's workspace, with `/` separators. */
  path: string
  name: string
  size: number
  dir: boolean
  modified: string
  uploaded?: boolean
}

export interface Attachment {
  id: string
  thread_id: string
  name: string
  rel_path: string
  size: number
  created_at: string
}

export interface ModelInfo {
  id: string
  label: string
  model: string
  ready: boolean
}

export interface ToolDescriptor {
  name: string
  title: string
  summary: string
  group: string
  network: boolean
  default_off: boolean
}

export interface Meta {
  version: string
  mode: "web" | "desktop"
  mock: boolean
  configured: boolean
  default_provider: string
  /** The thinking levels the composer offers, in order. The empty default is
   *  rendered as "Default" and is not in this list. */
  reasoning_levels: string[]
  data_dir: string
  capabilities: { reveal?: boolean }
  swarm: SwarmLimits
}

export interface SwarmLimits {
  max_concurrent: number
  agent_timeout_seconds: number
  max_turns: number
  manager_max_iterations: number
}

export interface ProviderSettings {
  id: string
  label: string
  base_url: string
  model: string
  timeout_seconds: number
  has_api_key: boolean
  ready: boolean
  /** Only ever sent, never received: the server does not hand keys back. */
  api_key?: string
}

export interface Settings {
  server: { addr: string; open_browser: boolean }
  models: { default: string; providers: ProviderSettings[] }
  swarm: SwarmLimits
  tools: {
    disabled: string[]
    enabled: string[]
    proxy: { http: string; https: string; no_proxy: string }
    web_search_max_results: number
  }
  log: { level: string }
}
