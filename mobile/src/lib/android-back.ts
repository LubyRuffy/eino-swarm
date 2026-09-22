/** Name of the function Android calls from the activity back dispatcher.
 *  Screens live in React state, so WebView history is empty and a plain
 *  Activity back would finish the task from a conversation. */
export const ANDROID_BACK_HOOK = "__zwaiAndroidBack"

export type AndroidBackLayer = "sheet" | "compose" | "thread" | "root"

/** The add-PC sheet sits above the new-conversation screen, which sits above
 *  a conversation, which sits above the inbox (or the unbound scan screen).
 *  Only the root finishes the activity. */
export function androidBackLayer(input: {
  sheet: boolean
  compose: boolean
  thread: boolean
}): AndroidBackLayer {
  if (input.sheet) return "sheet"
  if (input.compose) return "compose"
  if (input.thread) return "thread"
  return "root"
}

type BackHook = () => boolean

declare global {
  interface Window {
    __zwaiAndroidBack?: BackHook
  }
}

export function installAndroidBack(hook: BackHook): () => void {
  window.__zwaiAndroidBack = hook
  return () => {
    if (window.__zwaiAndroidBack === hook) delete window.__zwaiAndroidBack
  }
}
