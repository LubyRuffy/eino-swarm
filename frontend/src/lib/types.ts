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
  | "tool_delta"
  | "tool_call_delta"
  | "steer"
  | "steer_retracted"
  | "steer_preempted"
  | "cleanup"
  | "progress"
  | "memory_review"
  | "max_iterations"
  | "max_iterations_continued"
  | "model_retry"
  | "title"
  | "session_memory"
  | "done"
  | "error"
  | "resumed"
  | "goal"
  | "goal_complete"
  | "goal_continued"
  | "goal_capped"
  | "goal_idle"
  | "goal_blocked"
  | "goal_edited"
  | "goal_resumed"
  | "goal_session"
  | "plan"
  | "plan_updated"
  | "plan_implemented"
  | "plan_cancelled"
  | "compacted"
  | "usage"
  | "rewound"
  | "schedule"
  | "schedule_fired"
  | "schedule_skipped"
  | "schedule_report"
  | "schedule_cancelled"

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
  /** Pasted vision inputs. Handles, not pixels — the bytes live under GET
   *  /input-images/:image_id. */
  images?: ImageRef[]
  created_at: string
}

export interface ImageRef {
  id: string
  name?: string
  mime: string
}

export interface Project {
  id: string
  name: string
  /** Added to the manager's prompt for every conversation in the project. */
  system_prompt: string
  /** What the user chose, empty when zwai manages the directory. */
  workdir: string
  /** Where the agents actually work, so the UI never derives a path itself. */
  resolved_workdir: string
  memory_enabled: boolean
  memory_dir: string
  /** Names and one-line descriptions, for the sidebar listing. Bodies stay
   *  behind `GET /skills/:name`, the same way the prompt does not inline them. */
  skills?: SkillInfo[]
  /** 0 until the user drags the row; then the pinned sidebar order. */
  sort_rank?: number
  created_at: string
  updated_at: string
}

export interface MemoryEntries {
  text: string
  entries: string[]
  chars: number
  /** The prompt budget. Memory rides in every turn's system prompt, so the
   *  panel shows how much of it is spent. */
  limit: number
  /** Identifies this exact content. The editor sends it back on save so a
   *  review that landed in between is refused rather than overwritten. */
  rev: string
}

export interface SkillInfo {
  name: string
  description: string
  updated_at: string
}

export interface Skill extends SkillInfo {
  body: string
}

export interface ProjectMemory {
  dir: string
  /** False when either the project or the global setting has memory off. */
  enabled: boolean
  memory: MemoryEntries
  skills: SkillInfo[]
  /** True when a same-subject family is still on disk. Folding is a click,
   *  not a side effect of opening the tab. */
  needs_tidy?: boolean
}

/** One family a tidy folded: the keeper, the chapter-skills it ate, and
 *  whether the keeper had to be created. */
export interface SkillTidyMerge {
  keep: string
  dropped: string[]
  created: boolean
}

/** One skill write the tidy stream reported before the model finished. */
export interface TidyLiveChange {
  action: string
  name: string
}

/** Prose and writes seen while POST /memory/tidy-skills is still open. */
export interface TidyLive {
  text: string
  changes: TidyLiveChange[]
}

/** What POST /memory/tidy-skills returns besides the refreshed catalog. */
export interface SkillTidyReport {
  scanned: number
  before: number
  after: number
  families: number
  unchanged: number
  created: string[]
  deleted: string[]
  patched: string[]
  merged: SkillTidyMerge[]
  reviewed?: boolean
  err?: string
}

/** The payload of a `memory_review` event: what the post-turn review decided
 *  to keep. `changed: false` is the common case and shows nothing. */
export interface ReviewOutcome {
  changed: boolean
  notes?: Record<string, number>
  skills?: MemoryChange[]
  changes?: MemoryChange[]
  note?: string
  err?: string
  /** How chatty this review is in the transcript: off | on | verbose. */
  notify?: MemoryNotify
}

export interface MemoryChange {
  target: string
  action: string
  name?: string
  text?: string
}

export type MemoryNotify = "off" | "on" | "verbose"

export interface Thread {
  id: string
  title: string
  /** True while the engine still owns the sidebar name. A generated `title`
   *  event or a user rename clears it. */
  title_auto?: boolean
  /** Empty for a conversation that belongs to no project. */
  project_id: string
  provider_id: string
  /** Empty follows this provider's configured default. Set from the composer. */
  model?: string
  /** This conversation's thinking level: "" (model default), low, medium,
   *  high. Switchable in the composer, applied from the next turn. */
  reasoning_effort: string
  /** Standing objective from /goal. Empty means none. */
  goal?: string
  /** True after complete_goal. The text stays so the banner can show it. */
  goal_complete?: boolean
  /** True after block_goal. Auto-continue stops until resume or a human message. */
  goal_blocked?: boolean
  goal_block_reason?: string
  /** True after consecutive auto-continues hit the cap. */
  goal_capped?: boolean
  /** True after a continuation finished with no tool progress. */
  goal_idle?: boolean
  /** True while /plan is open. */
  plan_mode?: boolean
  /** Current plan body while planning (and after, until replaced). */
  plan_markdown?: string
  goal_auto_turns?: number
  /** When the current objective was set. Banner elapsed clock. */
  goal_started_at?: string
  /** True after /compact folded earlier replay into a briefing. */
  compacted?: boolean
  /** Replay size for the /compact hint when no token window is known. */
  context_chars?: number
  context_budget?: number
  archived: boolean
  /** 0 until the user drags the row; then the pinned sidebar order. */
  sort_rank?: number
  /** True when this conversation is tracked in the sidebar Pinned section.
   *  Pinning is for project topics you want to keep an eye on. */
  pinned?: boolean
  /** When it was pinned, so the newest pin sits at the top. Empty when not. */
  pinned_at?: string
  created_at: string
  last_active_at: string
  /** Set by the server from its live runtimes, so the sidebar can show a
   *  conversation working even while another one is on screen. */
  running: boolean
  /** Live only: ask_user is blocked waiting for the human. Still `running`. */
  awaiting_answer?: boolean
  /** Parked thread wake. Not `running`: the next turn is that wait. */
  waiting?: boolean
}

/** A message typed while a turn was already running. It waits for that turn
 *  to finish; Steer pulls it into the current turn instead. */
export interface Followup {
  id: string
  thread_id: string
  seq: number
  text: string
  created_at: string
}

export interface ThreadStatus {
  thread_id?: string
  running: boolean
  /** The current turn while running, and the last one afterwards, which is
   *  what the header offers to copy for `zwai trace`. */
  turn_id?: string
  /** Current turn start. Title-bar Working clock. Absent when idle.
   *  `goal_continued` is a new turn and a new clock; the standing-objective
   *  age is `goal_started_at` on the thread. */
  started_at?: string
  workers?: number
  /** True while the manager is paused at its tool-round cap waiting for the
   *  human to extend the turn. Still `running`. */
  awaiting_continue?: boolean
  /** True while ask_user is blocked waiting for the human. Still `running`. */
  awaiting_answer?: boolean
  /** True while auto-compact is rewriting the next prompt. Still `running`.
   *  Live only: `compacted` with `phase: "start"` sets it, a later compact
   *  result or `done` clears it. */
  compressing?: boolean
  /** True while a thread wake still owns the next turn. Not `running`.
   *  Live `schedule` sets it; `done` must keep it or the title bar says
   *  Idle while /goal is parked on that wait. */
  waiting?: boolean
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
  quiet?: boolean
  /** Engine-started /goal session. Not a human send; the jump rail
   *  stays on the last user_message. */
  goal_continue?: boolean
  schedule_continue?: boolean
  schedule_run_id?: string
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
  provider_id: string
  provider_label: string
  label: string
  model: string
  ready: boolean
  default?: boolean
  /** Token limit for this name. 0 means the endpoint never said. */
  context_window?: number
}

export interface ToolDescriptor {
  name: string
  title: string
  summary: string
  group: string
  network: boolean
  default_off: boolean
}

export interface PresenceClient {
  id: string
  surface: string
  pid?: number
}

export interface Meta {
  version: string
  mode: "web" | "desktop" | "engine"
  mock: boolean
  configured: boolean
  default_provider: string
  /** The thinking levels the composer offers, in order. The empty default is
   *  rendered as "Default" and is not in this list. */
  reasoning_levels: string[]
  data_dir: string
  capabilities: { reveal?: boolean; memory?: boolean; open_url?: boolean }
  swarm: SwarmLimits
  /** Chrome language: system, en, or zh. Kept next to `ui` so older clients
   *  that only read this field still pin the dictionary. */
  locale?: string
  ui?: UISettings
  /** Shells holding a presence connection. Empty when this process has none. */
  clients?: PresenceClient[]
}

export interface SwarmLimits {
  max_concurrent: number
  agent_timeout_seconds: number
  max_turns: number
  manager_max_iterations: number
  progress_interval_seconds: number
  delta_coalesce_ms: number
  auto_title: boolean
  /** Empty follows this conversation's model. */
  title_provider?: string
  title_model?: string
  compact_provider?: string
  compact_model?: string
  /** Rune count treated as 100% full on the /compact hint. */
  context_char_budget?: number
  /** Recent replay messages that stay verbatim after /compact. */
  compact_keep_messages?: number
  /** Prompt tokens that trigger compression when the model window is unknown. */
  auto_compact_tokens?: number
  /** Tokens kept free under a confirmed window for the completion. */
  compact_output_reserve?: number
  /** max_tokens sent on every chat request. */
  max_completion_tokens?: number
  /** Consecutive engine-started turns that may pursue an open /goal. */
  goal_max_auto_turns?: number
  /** Manager tool rounds of one /goal ReAct slice. */
  goal_session_max_iterations?: number
  /** Context fullness (1-100) that compact-before-continue uses. */
  goal_auto_compact_percent?: number
  /** Shortest cadence a wait may use, in seconds. */
  schedule_min_interval_seconds?: number
  /** How often the process looks for due waits, in milliseconds. */
  schedule_tick_ms?: number
  /** Schedule runs that may execute at once. */
  schedule_max_active?: number
}

export interface ProviderSettings {
  id: string
  label: string
  base_url: string
  model: string
  catalog?: string[]
  timeout_seconds: number
  context_window?: number
  model_context?: Record<string, number>
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
  memory: MemorySettings
  personality?: PersonalitySettings
  log: { level: string }
  ui?: UISettings
  remote?: RemoteSettings
  search?: SearchSettings
  clients?: ClientsSettings
}

/** Read-only local agent transcripts. Blank directories are filled on save. */
export interface ClientsSettings {
  enabled: boolean
  claude_dir: string
  codex_dir: string
  cursor_dir: string
  recent_days: number
  running_stale_seconds: number
}

export function defaultClientsSettings(
  clients?: Partial<ClientsSettings> | null,
): ClientsSettings {
  return {
    enabled: clients?.enabled ?? false,
    claude_dir: clients?.claude_dir ?? "",
    codex_dir: clients?.codex_dir ?? "",
    cursor_dir: clients?.cursor_dir ?? "",
    recent_days: clients?.recent_days ?? 3,
    running_stale_seconds: clients?.running_stale_seconds ?? 90,
  }
}

/** Conversation search. Keyword indexing is always on; embeddings are opt-in. */
export interface SearchSettings {
  embedding: boolean
  embedding_provider: string
  embedding_model: string
}

export function defaultSearchSettings(
  search?: Partial<SearchSettings> | null,
): SearchSettings {
  return {
    embedding: search?.embedding ?? false,
    embedding_provider: search?.embedding_provider ?? "",
    embedding_model: search?.embedding_model ?? "",
  }
}

export interface SearchHit {
  thread_id: string
  title: string
  snippet: string
  score: number
  source: "fts" | "semantic" | "hybrid"
}

export interface SearchResult {
  query: string
  embedding: boolean
  hits: SearchHit[]
}

/** Phone pairing. Hub URL is whatever the human typed — never compiled in. */
export interface RemoteSettings {
  enabled: boolean
  hub_url: string
  thread_limit: number
  summary_chars: number
  open_turns: number
  event_chars: number
  watch_events: number
  keep_awake: boolean
  display_name: string
}

export function defaultRemoteSettings(
  remote?: Partial<RemoteSettings> | null,
): RemoteSettings {
  return {
    enabled: remote?.enabled ?? false,
    hub_url: remote?.hub_url ?? "",
    thread_limit: remote?.thread_limit && remote.thread_limit > 0 ? remote.thread_limit : 5,
    summary_chars:
      remote?.summary_chars && remote.summary_chars > 0 ? remote.summary_chars : 280,
    open_turns: remote?.open_turns && remote.open_turns > 0 ? remote.open_turns : 6,
    event_chars: remote?.event_chars && remote.event_chars > 0 ? remote.event_chars : 4000,
    watch_events: remote?.watch_events && remote.watch_events > 0 ? remote.watch_events : 80,
    keep_awake: remote?.keep_awake ?? true,
    display_name: remote?.display_name ?? "",
  }
}

export interface RemoteStatus {
  enabled: boolean
  hub_url: string
  has_token: boolean
  online: boolean
  keep_awake?: boolean
  awake?: boolean
  fingerprint?: string
  error?: string
}

export interface RemoteOffer {
  uri: string
  pairing_id?: string
  png: string
  expires_at?: string
}

export interface RemoteBinding {
  id: string
  device_fp: string
  device?: string
  created_at: string
  last_seen?: string
  session_id: string
}

/** Install-wide personal preferences added to every manager prompt. */
export interface PersonalitySettings {
  instructions: string
}

/** Chrome stored in config.yaml. Tokens, not CSS — the front end maps them. */
export interface UISettings {
  locale: string
  font: string
  ui_font_size: string
  content_font: string
  font_size: string
  code_font: string
  code_font_size: string
  content_width: string
  transcript_mode: string
  palette: string
}

export interface TokenTotals {
  prompt_tokens: number
  completion_tokens: number
  cached_tokens: number
  reasoning_tokens: number
  total_tokens: number
  calls: number
}

/** Live token snapshot for the composer meter. `context_tokens` is the last
 *  manager prompt; the window comes from the selected model. */
export interface UsageSnapshot {
  context_tokens: number
  context_window: number
  turn: TokenTotals
  thread: TokenTotals
}

export interface MemorySettings {
  enabled: boolean
  auto_review: boolean
  char_limit: number
  entry_max: number
  review_max_iterations: number
  skills_index_max: number
  notifications: MemoryNotify | string
}

export type ScheduleKind = "thread" | "standalone"
export type ScheduleStatus = "active" | "paused" | "done" | "cancelled"
export type ScheduleRunStatus =
  | "skipped_busy"
  | "running"
  | "findings"
  | "quiet"
  | "error"
export type ScheduleCreatedBy = "human" | "manager"

/** Wire shape of a wait. Cadence is delay_s / every_s / cron; the engine
 *  keeps exactly one set. `until` is create-body only; the row stores until_at. */
export interface Schedule {
  id: string
  kind: ScheduleKind | string
  origin_thread_id: string
  thread_id: string
  project_id: string
  provider_id: string
  model: string
  reasoning_effort?: string
  title: string
  title_auto?: boolean
  prompt: string
  delay_s: number
  every_s: number
  cron: string
  status: ScheduleStatus | string
  next_run_at: string
  last_run_at?: string
  run_count: number
  max_runs: number
  until_at?: string
  created_by: ScheduleCreatedBy | string
  created_at: string
  updated_at: string
}

export interface ScheduleRun {
  id: string
  schedule_id: string
  thread_id: string
  turn_id: string
  status: ScheduleRunStatus | string
  summary: string
  unread: boolean
  created_at: string
  updated_at: string
  ended_at?: string
}

export interface ScheduleCreate {
  kind?: string
  thread_id?: string
  origin_thread_id?: string
  project_id?: string
  provider_id?: string
  model?: string
  title?: string
  prompt?: string
  delay_s?: number
  every_s?: number
  cron?: string
  max_runs?: number
  until?: string
}

export interface SchedulePatch {
  status?: string
  title?: string
  prompt?: string
  delay_s?: number
  every_s?: number
  cron?: string
}
