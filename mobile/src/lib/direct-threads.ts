import { mintID } from "./direct-provider"

const KEY = "zwai.phone.directThreads"

/** Tickets share this store. A long transcript must not be the thing that
 *  pushes the binding out, and image bytes never belong next to a ticket. */
const maxThreads = 40
const maxMessages = 200

export type StoredAttachment = {
  name: string
  kind: "image" | "file"
  mediaType?: string
  /** Session only. Stripped before localStorage. */
  dataUrl?: string
  /** Decoded text of a file, session only, so the next turn can resend it. */
  text?: string
}

export type DirectMessage = {
  id: string
  role: "user" | "assistant"
  text: string
  reasoning?: string
  attachments?: StoredAttachment[]
  error?: string
}

export type DirectThread = {
  id: string
  title: string
  providerId: string
  model: string
  reasoning: string
  updatedAt: number
  messages: DirectMessage[]
}

export function loadThreads(): DirectThread[] {
  const raw = read()
  if (!raw) return []
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.map(asThread).filter((row): row is DirectThread => Boolean(row))
  } catch {
    return []
  }
}

export function saveThreads(rows: DirectThread[]) {
  const slim = rows
    .map(slimThread)
    .sort((a, b) => b.updatedAt - a.updatedAt)
    .slice(0, maxThreads)
  if (slim.length === 0) {
    try {
      localStorage.removeItem(KEY)
    } catch {
      // nothing stored is the empty state
    }
    return
  }
  try {
    localStorage.setItem(KEY, JSON.stringify(slim))
  } catch {
    // Losing a transcript is better than throwing over a quota.
  }
}

export function threadTitle(text: string, attachmentName: string): string {
  const line = text.trim().split("\n")[0]?.trim() ?? ""
  const base = line || attachmentName.trim()
  if (!base) return ""
  return base.length > 48 ? base.slice(0, 48) : base
}

export function newThread(pick: {
  providerId: string
  model: string
  reasoning: string
}): DirectThread {
  return {
    id: mintID("c"),
    title: "",
    providerId: pick.providerId,
    model: pick.model,
    reasoning: pick.reasoning,
    updatedAt: Date.now(),
    messages: [],
  }
}

function slimThread(row: DirectThread): DirectThread {
  return {
    ...row,
    messages: row.messages.slice(-maxMessages).map((message) => ({
      ...message,
      attachments: message.attachments?.map((item) => ({
        name: item.name,
        kind: item.kind,
      })),
    })),
  }
}

function asThread(value: unknown): DirectThread | null {
  if (!value || typeof value !== "object") return null
  const row = value as Partial<DirectThread>
  if (typeof row.id !== "string" || !row.id) return null
  const messages = Array.isArray(row.messages)
    ? row.messages.map(asMessage).filter((item): item is DirectMessage => Boolean(item))
    : []
  return {
    id: row.id,
    title: typeof row.title === "string" ? row.title : "",
    providerId: typeof row.providerId === "string" ? row.providerId : "",
    model: typeof row.model === "string" ? row.model : "",
    reasoning: typeof row.reasoning === "string" ? row.reasoning : "",
    updatedAt: typeof row.updatedAt === "number" ? row.updatedAt : 0,
    messages,
  }
}

function asMessage(value: unknown): DirectMessage | null {
  if (!value || typeof value !== "object") return null
  const row = value as Partial<DirectMessage>
  if (row.role !== "user" && row.role !== "assistant") return null
  if (typeof row.id !== "string" || !row.id) return null
  return {
    id: row.id,
    role: row.role,
    text: typeof row.text === "string" ? row.text : "",
    reasoning: typeof row.reasoning === "string" ? row.reasoning : undefined,
    error: typeof row.error === "string" ? row.error : undefined,
    attachments: Array.isArray(row.attachments)
      ? row.attachments.flatMap((item) => {
          const chip = asAttachment(item)
          return chip ? [chip] : []
        })
      : undefined,
  }
}

function asAttachment(value: unknown): StoredAttachment | null {
  if (!value || typeof value !== "object") return null
  const row = value as Partial<StoredAttachment>
  if (typeof row.name !== "string" || !row.name) return null
  if (row.kind !== "image" && row.kind !== "file") return null
  return { name: row.name, kind: row.kind }
}

function read(): string | null {
  try {
    return localStorage.getItem(KEY)
  } catch {
    return null
  }
}
