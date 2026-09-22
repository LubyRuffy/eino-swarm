import { OpPut, type RemoteRequest } from "./rpc"

/** Decoded bytes of one put frame. Pairlink's plaintext cap is 64KiB;
 *  base64 of this plus the JSON envelope stays under it. The PC rejects a
 *  larger chunk. Change both sides together (`PutChunkRaw`). */
export const putChunkRaw = 36 * 1024

const frameCap = 64 * 1024

export function chunkBytes(bytes: Uint8Array, size = putChunkRaw): Uint8Array[] {
  if (bytes.length === 0) return []
  const out: Uint8Array[] = []
  for (let i = 0; i < bytes.length; i += size) out.push(bytes.subarray(i, i + size))
  return out
}

export function bytesToBase64(bytes: Uint8Array): string {
  let binary = ""
  const step = 0x8000
  for (let i = 0; i < bytes.length; i += step) {
    binary += String.fromCharCode(...bytes.subarray(i, Math.min(i + step, bytes.length)))
  }
  return btoa(binary)
}

/** A browser file name is a single path segment. A missing name still needs
 *  a suffix when the bytes are an image, so the PC treats them as vision. */
export function putName(name: string, mime: string): string {
  const base = name.trim().split(/[/\\]/).pop()?.trim() ?? ""
  if (base && base !== "." && base !== "..") return base.slice(0, 255)
  const ext = imageExt(mime)
  return ext ? `image.${ext}` : "upload"
}

function imageExt(mime: string): string {
  switch (mime.toLowerCase()) {
    case "image/png":
      return "png"
    case "image/jpeg":
    case "image/jpg":
      return "jpg"
    case "image/gif":
      return "gif"
    case "image/webp":
      return "webp"
    default:
      return ""
  }
}

export function isVisionFile(file: { name: string; type: string }): boolean {
  if (file.type.toLowerCase().startsWith("image/")) return true
  if (file.type) return false
  return /\.(png|jpe?g|gif|webp)$/i.test(file.name)
}

export function putFrames(
  id: string,
  name: string,
  mime: string,
  bytes: Uint8Array,
): Omit<RemoteRequest, "v" | "id">[] {
  const parts = chunkBytes(bytes)
  return parts.map((part, i) => ({
    op: OpPut,
    put_id: id,
    name,
    mime,
    part: i + 1,
    parts: parts.length,
    data: bytesToBase64(part),
  }))
}

export function frameBytes(req: Omit<RemoteRequest, "v" | "id">): number {
  return new TextEncoder().encode(JSON.stringify({ v: 1, id: "m", ...req })).length
}

export function fullChunkFits(): boolean {
  const req = putFrames("chunk", "blob.bin", "application/octet-stream", new Uint8Array(putChunkRaw))
  return req.length === 1 && frameBytes(req[0]) < frameCap && frameBytes(req[0]) > frameCap / 2
}
