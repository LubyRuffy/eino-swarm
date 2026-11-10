import { nativeTitlebarInvoke } from "./titlebar"

const RESULT_EVENT = "zwai:backup-file-result"

export interface BackupFileOptions {
  title: string
  buttonText: string
  directory?: string
  filename?: string
}

export function nativeBackupFileAvailable(): boolean {
  return Boolean(nativeTitlebarInvoke())
}

/** Save or open one backup through the desktop window. A browser has no path
 *  to return, so it uses a download and a file input instead. */
export function chooseBackupFile(action: "save" | "open", options: BackupFileOptions): Promise<string> {
  const invoke = nativeTitlebarInvoke()
  if (!invoke) return Promise.reject(new Error("Backup files require the desktop app"))
  const requestId = crypto.randomUUID()
  return new Promise((resolve, reject) => {
    const receive = (event: Event) => {
      const result = (event as CustomEvent).detail
      if (!result || result.requestId !== requestId) return
      window.removeEventListener(RESULT_EVENT, receive)
      if (typeof result.error === "string" && result.error) {
        reject(new Error(result.error))
      } else if (typeof result.path === "string") {
        resolve(result.path)
      } else {
        reject(new Error("Invalid backup file response"))
      }
    }
    window.addEventListener(RESULT_EVENT, receive)
    try {
      invoke("wails:runtime:ready")
      invoke(JSON.stringify({
        type: "zwai:backup-file",
        requestId,
        action,
        title: options.title,
        buttonText: options.buttonText,
        directory: options.directory ?? "",
        filename: options.filename ?? "",
      }))
    } catch (error) {
      window.removeEventListener(RESULT_EVENT, receive)
      reject(error)
    }
  })
}
