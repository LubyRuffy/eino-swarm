/** Clipboard image paste. Vision input, not a workspace upload: a screenshot
 *  belongs on the message the model sees, not in the project's files. Caps
 *  match the engine (8 images, 8 MiB each). */

export const MAX_PASTE_IMAGES = 8
export const MAX_PASTE_IMAGE_BYTES = 8 << 20

export interface SendImage {
  name: string
  mime: string
  data: string
}

export interface PasteImage {
  id: string
  name: string
  mime: string
  file: File
  previewUrl: string
}

let pasteSeq = 0

const imageSuffix = /\.(png|jpe?g|gif|webp)$/i

/** Finder and WKWebView often label a local photo as a generic binary, or
 *  hand back a UTI (`public.jpeg`) instead of a MIME. Those still count when
 *  the name ends in png/jpeg/gif/webp. A real `image/*` type counts on its
 *  own. An explicit non-image MIME (text, pdf) stays a workspace file.
 *  DecodeImages still refuses bytes that are not one of those four. */
export function isPasteImage(file: File): boolean {
  const type = file.type.trim().toLowerCase()
  if (type.startsWith("image/")) return true
  if (!imageSuffix.test(file.name)) return false
  if (!type || !type.includes("/") || isGenericBinary(type)) return true
  return false
}

function isGenericBinary(type: string): boolean {
  return (
    type === "application/octet-stream" ||
    type === "application/binary" ||
    type === "binary/octet-stream" ||
    type === "application/x-octet-stream"
  )
}

/** Normalize the vision body's claim. The server still sniffs the bytes
 *  and rejects a claim that does not match their image family. */
export function visionMIME(file: File): string {
  const type = file.type.trim().toLowerCase()
  if (type === "image/jpg" || type === "image/pjpeg") return "image/jpeg"
  if (
    type === "image/png" ||
    type === "image/jpeg" ||
    type === "image/gif" ||
    type === "image/webp"
  ) {
    return type
  }
  if (type.startsWith("image/")) return file.type.trim()
  return mimeFromName(file.name) || "image/png"
}

function mimeFromName(name: string): string {
  const lower = name.toLowerCase()
  if (lower.endsWith(".png")) return "image/png"
  if (lower.endsWith(".jpg") || lower.endsWith(".jpeg")) return "image/jpeg"
  if (lower.endsWith(".gif")) return "image/gif"
  if (lower.endsWith(".webp")) return "image/webp"
  return ""
}

/** Plain text on a paste. Missing or hostile clipboard data is empty, not a throw. */
export function plainTextFromClipboard(data: DataTransfer | null | undefined): string {
  if (!data || typeof data.getData !== "function") return ""
  try {
    return data.getData("text/plain") ?? ""
  } catch {
    return ""
  }
}

/** WKWebView fires `paste` and then leaves an empty `overflow: hidden`
 *  textarea unchanged. `valueAfter` is the field once the event has
 *  returned. A change means the browser inserted; null means leave it. */
export function recoverDroppedPaste(
  valueBefore: string,
  selectionStart: number,
  selectionEnd: number,
  valueAfter: string,
  incoming: string,
): { text: string; caret: number } | null {
  if (!incoming || valueAfter !== valueBefore) return null
  const start = clampIndex(selectionStart, 0, valueBefore.length)
  const end = clampIndex(selectionEnd, start, valueBefore.length)
  return {
    text: valueBefore.slice(0, start) + incoming + valueBefore.slice(end),
    caret: start + incoming.length,
  }
}

function clampIndex(n: number, min: number, max: number): number {
  if (Number.isNaN(n)) return min
  return Math.min(Math.max(n, min), max)
}

/** Pull image files out of a paste. Text stays with the browser; we only
 *  steal the pixels. */
export function filesFromClipboard(data: DataTransfer | null | undefined): File[] {
  if (!data) return []
  const out: File[] = []
  const seen = new Set<File>()
  const items = data.items
  if (items) {
    for (let i = 0; i < items.length; i++) {
      const item = items[i]
      if (item.kind !== "file") continue
      const file = item.getAsFile()
      if (!file || !isPasteImage(file) || seen.has(file)) continue
      seen.add(file)
      out.push(file)
    }
  }
  const files = data.files
  if (files) {
    for (let i = 0; i < files.length; i++) {
      const file = files[i]
      if (!file || !isPasteImage(file) || seen.has(file)) continue
      seen.add(file)
      out.push(file)
    }
  }
  return out
}

export function pasteImageFromFile(file: File): PasteImage {
  return {
    id: `paste_${++pasteSeq}`,
    name: file.name || "image.png",
    mime: visionMIME(file),
    file,
    previewUrl: previewURL(file),
  }
}

export function addPasteImages(
  current: PasteImage[],
  files: File[],
): { next: PasteImage[]; skipped: number } {
  const room = Math.max(0, MAX_PASTE_IMAGES - current.length)
  let skipped = 0
  const accepted: PasteImage[] = []
  for (const file of files) {
    if (file.size > MAX_PASTE_IMAGE_BYTES) {
      skipped++
      continue
    }
    if (accepted.length >= room) {
      skipped++
      continue
    }
    accepted.push(pasteImageFromFile(file))
  }
  return { next: current.concat(accepted), skipped }
}

export function dropPasteImage(current: PasteImage[], id: string): PasteImage[] {
  const gone = current.find((img) => img.id === id)
  if (gone) revokePreview(gone.previewUrl)
  return current.filter((img) => img.id !== id)
}

export function revokePasteImages(images: PasteImage[]): void {
  for (const img of images) revokePreview(img.previewUrl)
}

export async function toSendImages(images: PasteImage[]): Promise<SendImage[]> {
  const out: SendImage[] = []
  for (const img of images) {
    out.push({
      name: img.name,
      mime: img.mime,
      data: await fileToBase64(img.file),
    })
  }
  return out
}

export async function fileToBase64(file: File): Promise<string> {
  const bytes = await blobBytes(file)
  let binary = ""
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return btoa(binary)
}

async function blobBytes(blob: Blob): Promise<Uint8Array> {
  if (typeof blob.arrayBuffer === "function") {
    return new Uint8Array(await blob.arrayBuffer())
  }
  // jsdom's File is a Blob without arrayBuffer; FileReader is the fallback
  // the real browser also has.
  return await new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(new Uint8Array(reader.result as ArrayBuffer))
    reader.onerror = () => reject(reader.error ?? new Error("could not read the image"))
    reader.readAsArrayBuffer(blob)
  })
}

function previewURL(file: Blob): string {
  if (typeof URL !== "undefined" && typeof URL.createObjectURL === "function") {
    return URL.createObjectURL(file)
  }
  return ""
}

function revokePreview(url: string): void {
  if (!url) return
  if (typeof URL !== "undefined" && typeof URL.revokeObjectURL === "function") {
    URL.revokeObjectURL(url)
  }
}
