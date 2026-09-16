/** File drop on the composer. Images become vision input; everything else
 *  becomes a workspace attachment. A folder is not a file. */

import { isPasteImage } from "./paste-image"

export interface DroppedFiles {
  images: File[]
  attachments: File[]
}

export function isFileDrag(data: DataTransfer | null | undefined): boolean {
  if (!data) return false
  if (typeListHas(data.types, "Files")) return true
  const items = data.items
  if (items) {
    for (let i = 0; i < items.length; i++) {
      if (items[i].kind === "file") return true
    }
  }
  return (data.files?.length ?? 0) > 0
}

export function filesFromDataTransfer(
  data: DataTransfer | null | undefined,
): File[] {
  if (!data) return []
  const items = data.items
  if (items && items.length > 0) {
    const out: File[] = []
    const seen = new Set<File>()
    for (let i = 0; i < items.length; i++) {
      const item = items[i]
      if (item.kind !== "file") continue
      if (isDirectoryItem(item)) continue
      const file = item.getAsFile()
      if (!file || seen.has(file)) continue
      seen.add(file)
      out.push(file)
    }
    return out
  }
  const files = data.files
  if (!files) return []
  const out: File[] = []
  for (let i = 0; i < files.length; i++) {
    if (files[i]) out.push(files[i])
  }
  return out
}

export function splitDroppedFiles(files: File[]): DroppedFiles {
  const images: File[] = []
  const attachments: File[] = []
  for (const file of files) {
    if (isPasteImage(file)) images.push(file)
    else attachments.push(file)
  }
  return { images, attachments }
}

function typeListHas(
  types: DataTransfer["types"] | undefined,
  name: string,
): boolean {
  if (!types) return false
  const list = types as { contains?: (value: string) => boolean; length: number }
  if (typeof list.contains === "function") return list.contains(name)
  for (let i = 0; i < list.length; i++) {
    if (types[i] === name) return true
  }
  return false
}

function isDirectoryItem(item: DataTransferItem): boolean {
  const entry = (
    item as DataTransferItem & {
      webkitGetAsEntry?: () => { isDirectory?: boolean } | null
    }
  ).webkitGetAsEntry?.()
  return Boolean(entry?.isDirectory)
}
