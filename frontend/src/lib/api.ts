import type {
  Attachment,
  FileEntry,
  MemoryEntries,
  MemoryChange,
  Meta,
  ModelInfo,
  Project,
  ProjectMemory,
  RemoteBinding,
  RemoteOffer,
  RemoteStatus,
  Settings,
  SearchResult,
  Skill,
  SkillTidyReport,
  SwarmEvent,
  Thread,
  ThreadStatus,
  ToolDescriptor,
  Turn,
  Followup,
  UsageSnapshot,
  Schedule,
  ScheduleCreate,
  SchedulePatch,
  ScheduleRun,
} from "./types"
import { defaultClientsSettings, defaultRemoteSettings, defaultSearchSettings } from "./types"
import { normalizeUISettings } from "./appearance"
import type { ClientCatalog } from "./local-clients"
import type { SendImage } from "./paste-image"
import type { ThreadLog } from "./thread-log"

/** The fields a project dialog can send. Each is optional so a patch changes
 *  only what the user touched. */
export interface ProjectPatch {
  name?: string
  system_prompt?: string
  workdir?: string
  memory_enabled?: boolean
}

/** Errors from the API carry the server's message, because it is written for
 *  the person reading it — "the conversation is already running a turn" is
 *  more use than "409". */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
    readonly details?: Record<string, unknown>,
  ) {
    super(message)
    this.name = "ApiError"
  }
}

/** One beat of a streaming tidy: scan, prose so far, or a skill write. */
export interface TidyStreamEvent {
  phase?: string
  scanned?: number
  text?: string
  action?: string
  name?: string
}

export interface TidySkillsResult {
  memory: ProjectMemory
  report: SkillTidyReport
  changes: MemoryChange[]
  folded: boolean
}

async function streamTidy(
  projectId: string,
  onEvent?: (ev: TidyStreamEvent) => void,
): Promise<TidySkillsResult> {
  const res = await fetch(`/api/projects/${projectId}/memory/tidy-skills`, {
    method: "POST",
    headers: { Accept: "text/event-stream" },
  })
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`
    let code: string | undefined
    let details: Record<string, unknown> | undefined
    try {
      const body = await res.json()
      if (body && typeof body === "object") details = body as Record<string, unknown>
      if (body?.error) message = body.error
      if (body?.code) code = body.code
    } catch {
      // a non-JSON refusal is still a refusal
    }
    throw new ApiError(message, res.status, code, details)
  }
  let done: TidySkillsResult | undefined
  let failed: string | undefined
  await readSSE(res, (name, data) => {
    if (name === "tidy") {
      onEvent?.(data as TidyStreamEvent)
      return
    }
    if (name === "done") done = data as TidySkillsResult
    if (name === "error") {
      const body = data as { error?: string }
      failed = body.error || "tidy failed"
    }
  })
  if (failed) throw new ApiError(failed, 500)
  if (!done?.report || !done.memory) throw new ApiError("tidy ended without a result", 500)
  return done
}

/** Reads one fetch body framed as SSE. Events are split on a blank line. */
async function readSSE(
  res: Response,
  onEvent: (name: string, data: unknown) => void,
): Promise<void> {
  const body = res.body
  if (!body) return
  const reader = body.getReader()
  const decode = new TextDecoder()
  let buf = ""
  for (;;) {
    const chunk = await reader.read()
    if (chunk.done) break
    buf += decode.decode(chunk.value, { stream: true })
    let cut = buf.indexOf("\n\n")
    while (cut >= 0) {
      emitSSE(buf.slice(0, cut), onEvent)
      buf = buf.slice(cut + 2)
      cut = buf.indexOf("\n\n")
    }
  }
  if (buf.trim() !== "") emitSSE(buf, onEvent)
}

function emitSSE(block: string, onEvent: (name: string, data: unknown) => void) {
  let name = "message"
  const data: string[] = []
  for (const line of block.split("\n")) {
    if (line.startsWith(":")) continue
    if (line.startsWith("event:")) name = line.slice(6).trim()
    else if (line.startsWith("data:")) data.push(line.slice(5).trim())
  }
  if (data.length === 0) return
  try {
    onEvent(name, JSON.parse(data.join("\n")))
  } catch {
    // a broken frame is one lost beat, not a failed tidy
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: {
      ...(init?.body && !(init.body instanceof FormData)
        ? { "Content-Type": "application/json" }
        : {}),
      ...init?.headers,
    },
  })
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`
    let code: string | undefined
    let details: Record<string, unknown> | undefined
    try {
      const body = await res.json()
      if (body && typeof body === "object") details = body as Record<string, unknown>
      if (body?.error) message = body.error
      if (body?.code) code = body.code
    } catch {
      // a non-JSON error body is still an error; the status line will do
    }
    throw new ApiError(message, res.status, code, details)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

function turnBody(
  text: string,
  images?: SendImage[],
  files?: string[],
  fromEventSeq?: number,
) {
  const body: {
    text: string
    images?: SendImage[]
    files?: string[]
    from_event_seq?: number
  } = { text }
  if (images && images.length > 0) body.images = images
  if (files && files.length > 0) body.files = files
  if (fromEventSeq && fromEventSeq > 0) body.from_event_seq = fromEventSeq
  return body
}

// A config written by an older build can still carry null tool lists, and the
// settings UI reads them as arrays.
function withToolLists(s: Settings): Settings {
  return {
    ...s,
    tools: {
      ...s.tools,
      disabled: s.tools.disabled ?? [],
      enabled: s.tools.enabled ?? [],
    },
    ui: normalizeUISettings(s.ui),
    remote: defaultRemoteSettings(s.remote),
    search: defaultSearchSettings(s.search),
    clients: defaultClientsSettings(s.clients),
  }
}

export interface DesktopUpdateOffer {
  version: string
  page_url?: string
  asset_url?: string
  asset_name?: string
}

export interface DesktopUpdate {
  status: "current" | "available" | "unsupported" | "error"
  current?: string
  message?: string
  offer?: DesktopUpdateOffer
}

const DISMISS_KEY = "zwai.desktop.update.dismissed"

export function dismissedDesktopVersion(): string {
  try {
    return localStorage.getItem(DISMISS_KEY) ?? ""
  } catch {
    return ""
  }
}

export function dismissDesktopVersion(version: string) {
  const v = version.trim()
  if (!v) return
  try {
    localStorage.setItem(DISMISS_KEY, v)
  } catch {
    // private mode
  }
}

export const api = {
  meta: () => request<Meta>("/api/meta"),

  desktopUpdate: (fresh = false) =>
    request<DesktopUpdate>("/api/update" + (fresh ? "?fresh=1" : "")),

  installDesktopUpdate: (version: string) =>
    request<{ status: string }>("/api/update", {
      method: "POST",
      body: JSON.stringify({ version }),
    }),

  presence: (surface: "desktop" | "web" | "tui") =>
    request<{ id: string }>("/api/presence", {
      method: "POST",
      body: JSON.stringify({ surface }),
    }),

  /** Keeps the shell counted until the page closes. EventSource reconnects
   *  the same id, which the engine treats as one client. */
  holdPresence: (id: string) => {
    const stream = new EventSource(`/api/presence/${encodeURIComponent(id)}`)
    return () => stream.close()
  },

  settings: () =>
    request<{ settings: Settings }>("/api/settings").then((r) =>
      withToolLists(r.settings),
    ),
  saveSettings: (patch: Partial<Settings>) =>
    request<{ settings: Settings }>("/api/settings", {
      method: "PUT",
      body: JSON.stringify(patch),
    }).then((r) => withToolLists(r.settings)),

  clients: (before?: string) => {
    const q = before ? `?before=${encodeURIComponent(before)}` : ""
    return request<ClientCatalog>(`/api/clients${q}`)
  },
  clientTask: (id: string) =>
    request<import("@/lib/local-clients").ClientTranscript>(
      `/api/clients/task?id=${encodeURIComponent(id)}`,
    ),

  search: (q: string, limit?: number) => {
    const params = new URLSearchParams({ q })
    if (limit && limit > 0) params.set("limit", String(limit))
    return request<SearchResult>(`/api/search?${params.toString()}`)
  },

  models: () =>
    request<{ models: ModelInfo[]; default: string; mock: boolean }>(
      "/api/models",
    ),
  discoverModels: (body: {
    provider_id?: string
    base_url?: string
    api_key?: string
  }) =>
    request<{ models: string[]; context_windows?: Record<string, number> }>(
      "/api/models/discover",
      {
        method: "POST",
        body: JSON.stringify(body),
      },
    ),
  tools: () =>
    request<{ catalog: ToolDescriptor[]; enabled: string[] }>("/api/tools"),

  projects: () =>
    request<{ projects: Project[] }>("/api/projects").then((r) => r.projects),
  createProject: (patch: ProjectPatch) =>
    request<{ project: Project }>("/api/projects", {
      method: "POST",
      body: JSON.stringify(patch),
    }).then((r) => r.project),
  patchProject: (id: string, patch: ProjectPatch) =>
    request<{ project: Project }>(`/api/projects/${id}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }).then((r) => r.project),
  deleteProject: (id: string) =>
    request<void>(`/api/projects/${id}`, { method: "DELETE" }),
  reorderProjects: (ids: string[]) =>
    request<{ projects: Project[] }>("/api/projects/reorder", {
      method: "PUT",
      body: JSON.stringify({ ids }),
    }).then((r) => r.projects),

  memory: (projectId: string) =>
    request<{ memory: ProjectMemory }>(`/api/projects/${projectId}/memory`).then(
      (r) => r.memory,
    ),
  saveMemory: (projectId: string, text: string, rev?: string) =>
    request<{ memory: MemoryEntries }>(`/api/projects/${projectId}/memory`, {
      method: "PUT",
      body: JSON.stringify({ text, rev }),
    }).then((r) => r.memory),
  skill: (projectId: string, name: string) =>
    request<{ skill: Skill }>(
      `/api/projects/${projectId}/skills/${encodeURIComponent(name)}`,
    ).then((r) => r.skill),
  deleteSkill: (projectId: string, name: string) =>
    request<void>(
      `/api/projects/${projectId}/skills/${encodeURIComponent(name)}`,
      { method: "DELETE" },
    ),
  tidySkills: (
    projectId: string,
    onEvent?: (ev: TidyStreamEvent) => void,
  ) => streamTidy(projectId, onEvent),

  threads: (archived = false, projectId?: string) => {
    const params = new URLSearchParams()
    if (archived) params.set("archived", "1")
    if (projectId) params.set("project", projectId)
    const query = params.toString()
    return request<{ threads: Thread[] }>(
      `/api/threads${query ? `?${query}` : ""}`,
    ).then((r) => r.threads)
  },
  createThread: (title?: string, providerId?: string, projectId?: string) =>
    request<{ thread: Thread }>("/api/threads", {
      method: "POST",
      body: JSON.stringify({
        title,
        provider_id: providerId,
        project_id: projectId,
      }),
    }).then((r) => r.thread),
  thread: (id: string) =>
    request<{ thread: Thread; status: ThreadStatus; usage?: UsageSnapshot }>(
      `/api/threads/${id}`,
    ),
  patchThread: (
    id: string,
    patch: {
      title?: string
      archived?: boolean
      provider_id?: string
      model?: string
      reasoning_effort?: string
      project_id?: string
      goal?: string
      goal_edit?: boolean
      goal_resume?: boolean
      plan_mode?: boolean
      plan_markdown?: string
      pinned?: boolean
    },
  ) =>
    request<{ thread: Thread }>(`/api/threads/${id}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }).then((r) => r.thread),
  compactThread: (id: string) =>
    request<{ thread: Thread; status: ThreadStatus; usage?: UsageSnapshot }>(
      `/api/threads/${id}/compact`,
      { method: "POST" },
    ),
  deleteThread: (id: string) =>
    request<void>(`/api/threads/${id}`, { method: "DELETE" }),
  reorderThreads: (ids: string[], projectId?: string) => {
    const params = new URLSearchParams()
    if (projectId) params.set("project", projectId)
    const query = params.toString()
    return request<{ threads: Thread[] }>(
      `/api/threads/reorder${query ? `?${query}` : ""}`,
      { method: "PUT", body: JSON.stringify({ ids }) },
    ).then((r) => r.threads)
  },

  startTurn: (
    id: string,
    text: string,
    images?: SendImage[],
    files?: string[],
    fromEventSeq?: number,
  ) =>
    request<{ turn: Turn }>(`/api/threads/${id}/turns`, {
      method: "POST",
      body: JSON.stringify(turnBody(text, images, files, fromEventSeq)),
    }).then((r) => r.turn),
  /** Injects into a running turn. Idle conversations start a turn instead,
   *  so a Steer click that lost the race still lands. */
  steer: (id: string, text: string, images?: SendImage[], files?: string[]) =>
    request<{ steered: boolean; turn?: Turn }>(`/api/threads/${id}/steer`, {
      method: "POST",
      body: JSON.stringify(turnBody(text, images, files)),
    }),
  followups: (id: string) =>
    request<{ followups: Followup[] }>(`/api/threads/${id}/followups`).then(
      (r) => r.followups ?? [],
    ),
  enqueueFollowup: (id: string, text: string) =>
    request<{ followup: Followup }>(`/api/threads/${id}/followups`, {
      method: "POST",
      body: JSON.stringify({ text }),
    }).then((r) => r.followup),
  deleteFollowup: (id: string, fid: string) =>
    request<void>(`/api/threads/${id}/followups/${fid}`, { method: "DELETE" }),
  requeueFollowup: (id: string, fid: string, text: string) =>
    request<{ followup: Followup }>(`/api/threads/${id}/followups/${fid}`, {
      method: "PATCH",
      body: JSON.stringify({ text }),
    }).then((r) => r.followup),
  steerFollowup: (id: string, fid: string) =>
    request<{ steered: boolean }>(`/api/threads/${id}/followups/${fid}/steer`, {
      method: "POST",
    }),
  /** Aborts the current manager tool/generate so unread steering lands now.
   *  Workers stay up. Stop is still POST …/interrupt. */
  preempt: (id: string) =>
    request<{ preempted: boolean }>(`/api/threads/${id}/preempt`, {
      method: "POST",
    }),
  /** Drops one unread steer by the timeline seq. The model never sees it. */
  retractSteer: (id: string, seq: number) =>
    request<void>(`/api/threads/${id}/steers/${seq}`, { method: "DELETE" }),
  interrupt: (id: string) =>
    request<{ interrupted: boolean }>(`/api/threads/${id}/interrupt`, {
      method: "POST",
    }),
  continueTurn: (id: string, proceed: boolean) =>
    request<{ continued: boolean }>(`/api/threads/${id}/continue`, {
      method: "POST",
      body: JSON.stringify({ continue: proceed }),
    }),
  answerTurn: (
    id: string,
    body: { call_id?: string; text?: string; answers?: Record<string, { answers: string[] }> },
  ) =>
    request<{ answered: boolean }>(`/api/threads/${id}/answers`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  implementPlan: (id: string) =>
    request<{ turn: Turn }>(`/api/threads/${id}/plan/implement`, {
      method: "POST",
    }),
  /** Re-reads the last finished turn and curates the project's memory again.
   *  Accepted, not done: the result arrives on the event stream. */
  reviewThread: (id: string) =>
    request<{ turn: Turn }>(`/api/threads/${id}/review`, { method: "POST" }),
  turns: (id: string) =>
    request<{ turns: Turn[] }>(`/api/threads/${id}/turns`).then((r) => r.turns),
  /** Newest stored events, oldest first. `before` pages upward from a seq. */
  threadLog: (id: string, opts?: { before?: number; limit?: number }) => {
    const params = new URLSearchParams()
    if (opts?.before && opts.before > 0) params.set("before", String(opts.before))
    if (opts?.limit && opts.limit > 0) params.set("limit", String(opts.limit))
    const query = params.toString()
    return request<ThreadLog>(`/api/threads/${id}/log${query ? `?${query}` : ""}`)
  },
  /** One worker's stored rows. The conversation tail often dropped them. */
  agentLog: (id: string, agentId: string) =>
    request<{ events: SwarmEvent[] }>(
      `/api/threads/${id}/agents/${encodeURIComponent(agentId)}/log`,
    ),

  files: (id: string) =>
    request<{ workspace: string; files: FileEntry[] }>(
      `/api/threads/${id}/files`,
    ),
  upload: (id: string, files: File[]) => {
    const form = new FormData()
    for (const f of files) form.append("files", f)
    return request<{ files: Attachment[] }>(`/api/threads/${id}/files`, {
      method: "POST",
      body: form,
    }).then((r) => r.files)
  },
  downloadURL: (id: string, path: string) =>
    `/api/threads/${id}/download/${path.split("/").map(encodeURIComponent).join("/")}`,
  inputImageURL: (id: string, imageId: string) =>
    `/api/threads/${id}/input-images/${encodeURIComponent(imageId)}`,
  deleteFile: (id: string, path: string) =>
    request<void>(
      `/api/threads/${id}/download/${path.split("/").map(encodeURIComponent).join("/")}`,
      { method: "DELETE" },
    ),
  reveal: (id: string, path?: string) =>
    request<{ revealed: string }>(`/api/threads/${id}/reveal`, {
      method: "POST",
      body: JSON.stringify({ path: path ?? "" }),
    }),
  openURL: (url: string) =>
    request<{ opened: string }>("/api/open", {
      method: "POST",
      body: JSON.stringify({ url }),
    }),

  trace: (turnId: string) =>
    request<{ turn: Turn; events: unknown[]; llm_calls: unknown[] }>(
      `/api/trace/${turnId}`,
    ),

  schedules: (opts?: { status?: string; kind?: string }) => {
    const params = new URLSearchParams()
    if (opts?.status) params.set("status", opts.status)
    if (opts?.kind) params.set("kind", opts.kind)
    const query = params.toString()
    return request<{ schedules: Schedule[]; unread: number }>(
      `/api/schedules${query ? `?${query}` : ""}`,
    )
  },
  createSchedule: (body: ScheduleCreate) =>
    request<{ schedule: Schedule }>("/api/schedules", {
      method: "POST",
      body: JSON.stringify(body),
    }).then((r) => r.schedule),
  schedule: (id: string) =>
    request<{ schedule: Schedule; runs: ScheduleRun[] }>(`/api/schedules/${id}`),
  patchSchedule: (id: string, patch: SchedulePatch) =>
    request<{ schedule: Schedule }>(`/api/schedules/${id}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }).then((r) => r.schedule),
  deleteSchedule: (id: string) =>
    request<void>(`/api/schedules/${id}`, { method: "DELETE" }),
  runSchedule: (id: string) =>
    request<{ turn: Turn }>(`/api/schedules/${id}/run`, { method: "POST" }).then(
      (r) => r.turn,
    ),
  markScheduleRunRead: (rid: string) =>
    request<void>(`/api/schedules/runs/${rid}/read`, { method: "POST" }),

  remoteStatus: () => request<RemoteStatus>("/api/remote/status"),
  saveRemoteToken: (token: string) =>
    request<RemoteStatus>("/api/remote/token", {
      method: "PUT",
      body: JSON.stringify({ token }),
    }),
  remoteOffer: () =>
    request<RemoteOffer>("/api/remote/offer", { method: "POST", body: "{}" }),
  remoteBindings: () =>
    request<{ bindings: RemoteBinding[] }>("/api/remote/bindings").then(
      (r) => r.bindings ?? [],
    ),
  revokeRemoteBinding: (id: string) =>
    request<{ revoked: boolean }>(`/api/remote/bindings/${id}/revoke`, {
      method: "POST",
      body: "{}",
    }),
}
