/** Name of the function Android calls from the activity back dispatcher.
 *  Screens live in React state, so WebView history is empty and a plain
 *  Activity back would finish the task from a conversation. */
export const ANDROID_BACK_HOOK = "__zwaiAndroidBack"

export type AndroidBackLayer = "sheet" | "compose" | "thread" | "chat" | "root"

/** The add-PC sheet (and the model sheet) sit above the new-conversation
 *  screen, which sits above a conversation. A direct-model thread sits above
 *  the chat list. Only the root finishes the activity. */
export function androidBackLayer(input: {
  sheet: boolean
  compose: boolean
  thread: boolean
  chat?: boolean
}): AndroidBackLayer {
  if (input.sheet) return "sheet"
  if (input.compose) return "compose"
  if (input.thread) return "thread"
  if (input.chat) return "chat"
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
