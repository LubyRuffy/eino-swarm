import { api, ApiError } from "@/lib/api"

export const MAX_FILE_PREVIEW_BYTES = 8 * 1024 * 1024
export const MAX_RENDERED_PREVIEW_CHARS = 256 * 1024

export type FilePreview =
  | { kind: "text"; text: string }
  | { kind: "image"; blob: Blob }
  | { kind: "download"; reason: "too-large" | "unsupported" }

const imageMimes = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"])
const textMimes = new Set([
  "application/json", "application/xml", "application/javascript",
  "application/x-javascript", "application/yaml", "application/x-yaml",
  "application/toml", "application/sql", "application/x-sh",
])
const textExtension = /\.(md|markdown|mdx|txt|log|csv|tsv|json|jsonl|ndjson|ya?ml|toml|ini|conf|cfg|xml|svg|go|mod|sum|py|rb|rs|[ch]|cc|cpp|hpp|java|kt|swift|mm?|sh|bash|zsh|fish|ps1|bat|sql|tsx?|jsx?|css|scss|sass|less|html?|vue|svelte)$/i

/** Consume attachment bytes without navigating to agent-authored content.
 * The response, rather than a potentially stale tree entry, owns the size cap. */
export async function readFilePreview(
  threadId: string,
  path: string,
  signal: AbortSignal,
): Promise<FilePreview> {
  signal.throwIfAborted()
  const response = await fetch(api.downloadURL(threadId, path), {
    signal,
    credentials: "same-origin",
    mode: "same-origin",
    redirect: "error",
    cache: "no-store",
  })
  signal.throwIfAborted()
  if (!response.ok) {
    await response.body?.cancel().catch(() => undefined)
    throw new ApiError(`${response.status} ${response.statusText || "File read failed"}`, response.status)
  }
  const length = response.headers.get("content-length")
  if (length !== null && Number(length) > MAX_FILE_PREVIEW_BYTES) {
    await response.body?.cancel().catch(() => undefined)
    signal.throwIfAborted()
    return { kind: "download", reason: "too-large" }
  }
  const bytes = await readBoundedBytes(response, signal)
  if (bytes === null) return { kind: "download", reason: "too-large" }
  const mime = (response.headers.get("content-type") ?? "").split(";")[0].trim().toLowerCase()
  if (imageMimes.has(mime)) {
    return { kind: "image", blob: new Blob([bytes], { type: mime }) }
  }
  const name = path.slice(path.lastIndexOf("/") + 1)
  const textFile = mime.startsWith("text/") || textMimes.has(mime) ||
    /^application\/[\w.+-]+\+(json|xml)$/.test(mime) || textExtension.test(name) ||
    /^(dockerfile|makefile|\.env(?:\..+)?|\.gitignore|\.editorconfig)$/i.test(name)
  if (textFile) {
    try {
      const text = new TextDecoder("utf-8", { fatal: true }).decode(bytes)
      if (!/[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/.test(text)) return { kind: "text", text }
    } catch {
      // Invalid UTF-8 stays downloadable instead of silently replacing bytes.
    }
  }
  return { kind: "download", reason: "unsupported" }
}

async function readBoundedBytes(response: Response, signal: AbortSignal): Promise<Uint8Array<ArrayBuffer> | null> {
  if (!response.body) {
    if (response.headers.get("content-length") === "0" || response.status === 204) return new Uint8Array(0)
    throw new Error("File response did not include a readable body")
  }
  const reader = response.body.getReader()
  const parts: Uint8Array[] = []
  let size = 0
  const cancel = () => { void reader.cancel(signal.reason).catch(() => undefined) }
  signal.addEventListener("abort", cancel, { once: true })
  try {
    for (;;) {
      signal.throwIfAborted()
      const { done, value } = await reader.read()
      signal.throwIfAborted()
      if (done) break
      size += value.byteLength
      if (size > MAX_FILE_PREVIEW_BYTES) {
        await reader.cancel().catch(() => undefined)
        signal.throwIfAborted()
        return null
      }
      parts.push(value)
    }
    const bytes = new Uint8Array(size)
    let offset = 0
    for (const part of parts) {
      bytes.set(part, offset)
      offset += part.byteLength
    }
    return bytes
  } finally {
    signal.removeEventListener("abort", cancel)
    reader.releaseLock()
  }
}
