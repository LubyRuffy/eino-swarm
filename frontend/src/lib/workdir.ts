import { nativeTitlebarInvoke } from "./titlebar"

const RESULT_EVENT = "zwai:directory-result"

export interface DirectoryOptions {
  title: string
  buttonText: string
  directory: string
}

/** A shell query is not a capability: browsers cannot reveal a folder's
 * absolute path, even when they are on the same engine as a native window. */
export function nativeDirectoryAvailable(): boolean {
  return Boolean(nativeTitlebarInvoke())
}

/** Use the native message channel because this page is served by the engine,
 * not the Wails asset server that implements /wails/runtime. */
export function chooseDirectory(options: DirectoryOptions): Promise<string> {
  const invoke = nativeTitlebarInvoke()
  if (!invoke) return Promise.reject(new Error("Directory selection requires the desktop app"))
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
        reject(new Error("Invalid directory selection response"))
      }
    }
    window.addEventListener(RESULT_EVENT, receive)
    try {
      // Wails queues ExecJS replies until it has received this handshake.
      invoke("wails:runtime:ready")
      invoke(JSON.stringify({ type: "zwai:directory", requestId, action: "choose", ...options }))
    } catch (error) {
      window.removeEventListener(RESULT_EVENT, receive)
      reject(error)
    }
  })
}
