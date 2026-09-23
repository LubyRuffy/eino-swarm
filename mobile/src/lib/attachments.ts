import { bytesToBase64, isVisionFile, putName } from "./put-chunks"
import type { StoredAttachment } from "./direct-threads"
import type { DirectMessage } from "./direct-threads"
import type { WirePart, WireTurn } from "./openai-wire"

/** One photo is enough to fill localStorage if it were persisted beside the
 *  pairing ticket. The cap is the send limit, not a stored blob. */
export const maxAttachmentBytes = 8 * 1024 * 1024

export class AttachmentTooBig extends Error {
  constructor() {
    super("too-big")
    this.name = "AttachmentTooBig"
  }
}

export async function readComposerFiles(files: File[]): Promise<{
  chips: StoredAttachment[]
  parts: WirePart[]
}> {
  const chips: StoredAttachment[] = []
  const parts: WirePart[] = []
  for (const file of files) {
    const read = await readOne(file)
    chips.push(read.chip)
    parts.push(read.part)
  }
  return { chips, parts }
}

export function turnsFromMessages(messages: DirectMessage[]): WireTurn[] {
  const turns: WireTurn[] = []
  for (const message of messages) {
    if (message.error && !message.text) continue
    const parts = partsFromMessage(message)
    if (parts.length === 0) continue
    turns.push({ role: message.role, parts })
  }
  return turns
}

export function partsFromMessage(message: DirectMessage): WirePart[] {
  const parts: WirePart[] = []
  if (message.text) parts.push({ kind: "text", text: message.text })
  for (const item of message.attachments ?? []) {
    if (item.kind === "image" && item.dataUrl) {
      parts.push({
        kind: "image",
        mediaType: item.mediaType || "image/png",
        dataUrl: item.dataUrl,
      })
    } else if (item.text) {
      parts.push({ kind: "text", text: item.name + "\n" + item.text })
    } else if (item.dataUrl) {
      parts.push({
        kind: "file",
        name: item.name,
        mediaType: item.mediaType || "application/octet-stream",
        dataUrl: item.dataUrl,
      })
    }
  }
  return parts
}

async function readOne(file: File): Promise<{ chip: StoredAttachment; part: WirePart }> {
  if (file.size > maxAttachmentBytes) throw new AttachmentTooBig()
  const bytes = await fileBytes(file)
  const declared = file.type || ""
  const mime = declared || (looksLikeText(bytes, "") ? "text/plain" : "application/octet-stream")
  const name = putName(file.name, mime)
  if (isVisionFile(file)) {
    const mediaType = mime.startsWith("image/") ? mime : "image/png"
    const dataUrl = asDataUrl(mediaType, bytes)
    return {
      chip: { name, kind: "image", mediaType, dataUrl },
      part: { kind: "image", mediaType, dataUrl },
    }
  }
  if (looksLikeText(bytes, mime)) {
    const text = new TextDecoder("utf-8", { fatal: false }).decode(bytes)
    return {
      chip: { name, kind: "file", mediaType: mime, text },
      part: { kind: "text", text: name + "\n" + text },
    }
  }
  const dataUrl = asDataUrl(mime, bytes)
  return {
    chip: { name, kind: "file", mediaType: mime, dataUrl },
    part: { kind: "file", name, mediaType: mime, dataUrl },
  }
}

/** chat/completions has no portable file part. Text rides as text. A PDF
 *  stays a file part for the responses wire and for endpoints that accept one. */
export function looksLikeText(bytes: Uint8Array, mime: string): boolean {
  const kind = mime.toLowerCase()
  if (kind.startsWith("text/")) return true
  if (kind === "application/json" || kind === "application/xml" || kind === "application/javascript") {
    return true
  }
  if (kind.startsWith("image/") || kind.startsWith("audio/") || kind.startsWith("video/")) return false
  if (kind === "application/pdf" || kind.includes("zip") || kind.includes("octet-stream")) return false
  if (kind.startsWith("application/") && kind !== "application/octet-stream") return false
  return !bytes.subarray(0, 8192).includes(0)
}

/** jsdom's File has no arrayBuffer. FileReader is what the WebView and the
 *  tests both actually implement. */
function fileBytes(file: Blob): Promise<Uint8Array> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const result = reader.result
      if (!(result instanceof ArrayBuffer)) {
        reject(new Error("read"))
        return
      }
      resolve(new Uint8Array(result))
    }
    reader.onerror = () => reject(reader.error ?? new Error("read"))
    reader.readAsArrayBuffer(file)
  })
}

function asDataUrl(mime: string, bytes: Uint8Array): string {
  return "data:" + (mime || "application/octet-stream") + ";base64," + bytesToBase64(bytes)
}
