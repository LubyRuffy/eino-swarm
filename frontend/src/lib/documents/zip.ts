import { MAX_ZIP_ENTRIES, MAX_ZIP_ENTRY_BYTES, MAX_ZIP_TOTAL_BYTES } from "./limits"

export class ZipError extends Error {
  constructor(message: string) {
    super(message)
    this.name = "ZipError"
  }
}

function u16(bytes: Uint8Array, offset: number): number {
  return bytes[offset] | (bytes[offset + 1] << 8)
}

function u32(bytes: Uint8Array, offset: number): number {
  return (bytes[offset] | (bytes[offset + 1] << 8) | (bytes[offset + 2] << 16) | (bytes[offset + 3] << 24)) >>> 0
}

function findEocd(bytes: Uint8Array): number {
  const start = Math.max(0, bytes.length - 22 - 0xffff)
  for (let offset = bytes.length - 22; offset >= start; offset--) {
    if (u32(bytes, offset) === 0x06054b50) return offset
  }
  throw new ZipError("zip directory is missing")
}

/** Drop a name containing `..`. The preview map is not a filesystem, and a
 * parent segment is never a document path. */
export function safeZipName(name: string): string | null {
  const normalized = name.replaceAll("\\", "/")
  if (!normalized || normalized.endsWith("/")) return null
  if (normalized.startsWith("/") || /^[a-zA-Z]:/.test(normalized)) return null
  const parts: string[] = []
  for (const part of normalized.split("/")) {
    if (part === "" || part === ".") continue
    if (part === "..") return null
    parts.push(part)
  }
  return parts.length ? parts.join("/") : null
}

async function inflate(data: Uint8Array, size: number): Promise<Uint8Array> {
  if (typeof DecompressionStream !== "function") throw new ZipError("deflate is unavailable")
  const source = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(data)
      controller.close()
    },
  })
  // DecompressionStream accepts any BufferSource, while this stream only
  // enqueues the entry bytes. The DOM types do not express that overlap.
  const transform = new DecompressionStream("deflate-raw") as unknown as ReadableWritablePair<Uint8Array, Uint8Array>
  const inflated = new Uint8Array(await new Response(source.pipeThrough(transform)).arrayBuffer())
  if (inflated.byteLength > size && size > 0) throw new ZipError("zip entry expanded past its declared size")
  if (inflated.byteLength > MAX_ZIP_ENTRY_BYTES) throw new ZipError("zip entry is too large")
  return inflated
}

/** Read a ZIP into entry names. Stored and deflate entries are accepted.
 * Encrypted, ZIP64 and other methods fail the preview. */
export async function readZip(bytes: Uint8Array): Promise<Map<string, Uint8Array>> {
  if (bytes.length < 22) throw new ZipError("zip directory is missing")
  const eocd = findEocd(bytes)
  const count = u16(bytes, eocd + 10)
  const directoryOffset = u32(bytes, eocd + 16)
  if (count > MAX_ZIP_ENTRIES || directoryOffset === 0xffffffff) throw new ZipError("zip directory is unsupported")
  const files = new Map<string, Uint8Array>()
  let offset = directoryOffset
  let total = 0
  for (let index = 0; index < count; index++) {
    if (offset + 46 > bytes.length || u32(bytes, offset) !== 0x02014b50) throw new ZipError("zip directory is truncated")
    const flags = u16(bytes, offset + 8)
    const method = u16(bytes, offset + 10)
    const compressedSize = u32(bytes, offset + 20)
    const uncompressedSize = u32(bytes, offset + 24)
    const nameLength = u16(bytes, offset + 28)
    const extraLength = u16(bytes, offset + 30)
    const commentLength = u16(bytes, offset + 32)
    const localOffset = u32(bytes, offset + 42)
    const nameBytes = bytes.subarray(offset + 46, offset + 46 + nameLength)
    const name = new TextDecoder(flags & 0x800 ? "utf-8" : "latin1").decode(nameBytes)
    offset += 46 + nameLength + extraLength + commentLength
    if (flags & 1) throw new ZipError("zip encryption is unsupported")
    const safe = safeZipName(name)
    if (!safe) continue
    if (uncompressedSize > MAX_ZIP_ENTRY_BYTES || total + uncompressedSize > MAX_ZIP_TOTAL_BYTES) {
      throw new ZipError("zip entry is too large")
    }
    if (localOffset + 30 > bytes.length || u32(bytes, localOffset) !== 0x04034b50) throw new ZipError("zip entry header is invalid")
    const dataStart = localOffset + 30 + u16(bytes, localOffset + 26) + u16(bytes, localOffset + 28)
    if (dataStart + compressedSize > bytes.length) throw new ZipError("zip entry is truncated")
    const compressed = bytes.subarray(dataStart, dataStart + compressedSize)
    let data: Uint8Array
    if (method === 0) data = compressed
    else if (method === 8) data = await inflate(compressed, uncompressedSize)
    else throw new ZipError("zip compression is unsupported")
    if (data.byteLength > MAX_ZIP_ENTRY_BYTES) throw new ZipError("zip entry is too large")
    total += data.byteLength
    if (total > MAX_ZIP_TOTAL_BYTES) throw new ZipError("zip entry is too large")
    files.set(safe, data)
  }
  return files
}

export function zipText(files: Map<string, Uint8Array>, name: string): string | null {
  const bytes = files.get(name)
  if (!bytes) return null
  return new TextDecoder("utf-8").decode(bytes).replace(/^\uFEFF/, "")
}
