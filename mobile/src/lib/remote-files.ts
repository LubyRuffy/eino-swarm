import type { RemoteLink } from "./link"
import { OpFileChunk, OpFiles, type WorkspaceFile } from "./rpc"

// A long path may leave room for only one entry per sealed frame. The host
// caps its tree at 2000 entries; the phone also bounds pages and duplicates.
const maxListPages = 2000
export const maxPhoneFileBytes = 32 * 1024 * 1024
export const maxInlinePreviewBytes = 8 * 1024 * 1024

export class RemoteFileError extends Error {
  constructor(readonly code: string, message: string) {
    super(message)
  }
}

export async function listWorkspaceFiles(link: RemoteLink, threadId: string): Promise<WorkspaceFile[]> {
  const files: WorkspaceFile[] = []
  const seen = new Set<string>()
  let cursor = ""
  for (let page = 0; page < maxListPages; page++) {
    const resp = await link.rpc({ op: OpFiles, thread_id: threadId, cursor: cursor || undefined })
    if (!resp.ok) throw new RemoteFileError(resp.code || "remote_error", resp.error || "file listing failed")
    for (const file of resp.files ?? []) {
      if (seen.has(file.path)) throw new Error("file listing repeated an entry")
      seen.add(file.path)
      files.push(file)
      if (files.length > maxListPages) throw new Error("file listing exceeds the workspace limit")
    }
    if (!resp.more) return files
    if (!resp.next || resp.next === cursor) throw new Error("file listing cursor did not advance")
    cursor = resp.next
  }
  throw new Error("file listing has too many pages")
}

export async function readWorkspaceFile(
  link: RemoteLink,
  threadId: string,
  path: string,
): Promise<{ bytes: Uint8Array; mime: string }> {
  const parts: Uint8Array[] = []
  let offset = 0
  let size = -1
  let mime = "application/octet-stream"
  for (;;) {
    const resp = await link.rpc({ op: OpFileChunk, thread_id: threadId, file_path: path, before: offset })
    const chunk = resp.file_chunk
    if (!resp.ok || !chunk) throw new RemoteFileError(resp.code || "remote_error", resp.error || "file read failed")
    if (chunk.path !== path || chunk.offset !== offset || chunk.size < 0 || chunk.size > maxPhoneFileBytes ||
      (size >= 0 && chunk.size !== size)) throw new Error("file changed while reading")
    const bytes = base64Bytes(chunk.data)
    if (chunk.next_offset !== offset + bytes.length || chunk.next_offset > chunk.size ||
      chunk.more !== (chunk.next_offset < chunk.size)) throw new Error("file chunk is incomplete")
    if (chunk.more && bytes.length === 0) throw new Error("file chunk made no progress")
    if (size < 0) {
      size = chunk.size
      mime = chunk.mime
    }
    parts.push(bytes)
    offset = chunk.next_offset
    if (!chunk.more) break
  }
  const all = new Uint8Array(size)
  let at = 0
  for (const part of parts) {
    all.set(part, at)
    at += part.length
  }
  return { bytes: all, mime }
}

function base64Bytes(data: string): Uint8Array {
  const raw = atob(data)
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

export type PreviewKind = "text" | "image" | "pdf" | "download"

export function previewKind(name: string, mime: string, size: number): PreviewKind {
  if (size > maxInlinePreviewBytes) return "download"
  const kind = mime.toLowerCase()
  if (["image/png", "image/jpeg", "image/gif", "image/webp"].includes(kind)) return "image"
  if (kind === "application/pdf") return "pdf"
  if (kind.startsWith("text/") || ["application/json", "application/xml", "application/javascript"].includes(kind) ||
    /\.(md|markdown|csv|json|ya?ml|xml|svg|go|py|tsx?|jsx?|css|html?)$/i.test(name)) return "text"
  return "download"
}
