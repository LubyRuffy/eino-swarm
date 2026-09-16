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

export function isPasteImage(file: File): boolean {
  if (file.type.startsWith("image/")) return true
  if (file.type) return false
  return /\.(png|jpe?g|gif|webp)$/i.test(file.name)
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
    mime: file.type || "image/png",
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
