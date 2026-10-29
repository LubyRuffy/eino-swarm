import { Capacitor, registerPlugin } from "@capacitor/core"

import { bytesToBase64 } from "./put-chunks"
import { maxPhoneFileBytes } from "./remote-files"

type FileSavePlugin = {
  saveFile(input: { name: string; mime: string; data: string; size: number }): Promise<{ saved: boolean }>
}

const FileSave = registerPlugin<FileSavePlugin>("FileSave")

export function nativeFileSave(): boolean {
  return Capacitor.isNativePlatform()
}

// Export the received bytes, including binary data and text encoding markers.
// A preview's decoded text is not a lossless source for a downloaded file.
export async function savePhoneFile(name: string, mime: string, bytes: Uint8Array): Promise<boolean> {
  if (!nativeFileSave()) throw new Error("native_file_save_unavailable")
  if (!name || name === "." || name === ".." || /[/\\\u0000-\u001f\u007f]/.test(name)) {
    throw new Error("invalid_file_name")
  }
  if (bytes.length > maxPhoneFileBytes) throw new Error("file_too_large")
  const type = /^[a-z0-9!#$&^_.+-]+\/[a-z0-9!#$&^_.+-]+$/i.test(mime) ? mime : "application/octet-stream"
  const result = await FileSave.saveFile({ name, mime: type, data: bytesToBase64(bytes), size: bytes.length })
  if (typeof result?.saved !== "boolean") throw new Error("invalid_file_save_result")
  return result.saved
}
