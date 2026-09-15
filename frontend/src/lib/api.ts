import type {
  Attachment,
  FileEntry,
  Meta,
  ModelInfo,
  Settings,
  Thread,
  ThreadStatus,
  ToolDescriptor,
  Turn,
} from "./types"

/** Errors from the API carry the server's message, because it is written for
 *  the person reading it — "the conversation is already running a turn" is
 *  more use than "409". */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
  ) {
    super(message)
    this.name = "ApiError"
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
    try {
      const body = await res.json()
      if (body?.error) message = body.error
      if (body?.code) code = body.code
    } catch {
      // a non-JSON error body is still an error; the status line will do
    }
    throw new ApiError(message, res.status, code)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
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
  }
}

export const api = {
  meta: () => request<Meta>("/api/meta"),

  settings: () =>
    request<{ settings: Settings }>("/api/settings").then((r) =>
      withToolLists(r.settings),
    ),
  saveSettings: (patch: Partial<Settings>) =>
    request<{ settings: Settings }>("/api/settings", {
      method: "PUT",
      body: JSON.stringify(patch),
    }).then((r) => withToolLists(r.settings)),

  models: () =>
    request<{ models: ModelInfo[]; default: string; mock: boolean }>(
      "/api/models",
    ),
  tools: () =>
    request<{ catalog: ToolDescriptor[]; enabled: string[] }>("/api/tools"),

  threads: (archived = false) =>
    request<{ threads: Thread[] }>(
      `/api/threads${archived ? "?archived=1" : ""}`,
    ).then((r) => r.threads),
  createThread: (title?: string, providerId?: string) =>
    request<{ thread: Thread }>("/api/threads", {
      method: "POST",
      body: JSON.stringify({ title, provider_id: providerId }),
    }).then((r) => r.thread),
  thread: (id: string) =>
    request<{ thread: Thread; status: ThreadStatus }>(`/api/threads/${id}`),
  patchThread: (
    id: string,
    patch: {
      title?: string
      archived?: boolean
      provider_id?: string
      reasoning_effort?: string
    },
  ) =>
    request<{ thread: Thread }>(`/api/threads/${id}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }).then((r) => r.thread),
  deleteThread: (id: string) =>
    request<void>(`/api/threads/${id}`, { method: "DELETE" }),

  startTurn: (id: string, text: string) =>
    request<{ turn: Turn }>(`/api/threads/${id}/turns`, {
      method: "POST",
      body: JSON.stringify({ text }),
    }).then((r) => r.turn),
  /** One gesture for the composer: the server starts a turn when nothing is
   *  running and steers the manager when something is. */
  steer: (id: string, text: string) =>
    request<{ steered: boolean; turn?: Turn }>(`/api/threads/${id}/steer`, {
      method: "POST",
      body: JSON.stringify({ text }),
    }),
  interrupt: (id: string) =>
    request<{ interrupted: boolean }>(`/api/threads/${id}/interrupt`, {
      method: "POST",
    }),
  turns: (id: string) =>
    request<{ turns: Turn[] }>(`/api/threads/${id}/turns`).then((r) => r.turns),

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

  trace: (turnId: string) =>
    request<{ turn: Turn; events: unknown[]; llm_calls: unknown[] }>(
      `/api/trace/${turnId}`,
    ),
}
