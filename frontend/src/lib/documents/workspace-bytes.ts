import { api } from "@/lib/api"

const MAX_ASSET_BYTES = 1024 * 1024

/** Read one sibling of a preview. A missing or oversized asset is omitted;
 * it must not fail the document or be loaded through an iframe navigation. */
export async function readWorkspaceBytes(threadId: string, path: string, signal: AbortSignal): Promise<Uint8Array | null> {
  signal.throwIfAborted()
  const response = await fetch(api.downloadURL(threadId, path), {
    signal,
    credentials: "same-origin",
    mode: "same-origin",
    redirect: "error",
    cache: "no-store",
  })
  if (!response.ok) {
    await response.body?.cancel().catch(() => undefined)
    return null
  }
  const bytes = new Uint8Array(await response.arrayBuffer())
  signal.throwIfAborted()
  return bytes.byteLength > MAX_ASSET_BYTES ? null : bytes
}
