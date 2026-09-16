/** The Wails host message that zooms the window the way a native title bar
 *  double-click does, including the user's System Settings preference.
 *
 *  Dragging already works: macOS InvisibleTitleBarHeight eats the first
 *  mousedown. The second click is left for this message, because starting
 *  another native drag would race AppKit's zoom restore frame. */
export const TITLEBAR_DOUBLE_CLICK = "wails:drag:doubleclick"

export type TitlebarInvoke = (message: string) => void

type NativeWindow = Window & {
  _wails?: { invoke?: TitlebarInvoke }
  webkit?: {
    messageHandlers?: { external?: { postMessage: (message: string) => void } }
  }
  chrome?: { webview?: { postMessage: (message: string) => void } }
}

/** Controls a double-click must not zoom: they are clicks, not chrome. */
const NO_ZOOM = "button, a, input, textarea, select, [role='button'], [data-no-drag]"

function eventElement(event: Event): Element {
  const target = event.target
  if (target instanceof Element) return target
  if (target instanceof Node && target.parentElement) return target.parentElement
  return document.documentElement
}

/** Whether this event landed on the window chrome that moves (and zooms) the
 *  desktop window, not on a control sitting in that chrome. */
export function isTitlebarDragEvent(event: Event): boolean {
  const start = eventElement(event)
  if (start.closest(NO_ZOOM)) return false
  if (!start.closest("[data-drag-region]")) return false
  return (
    window.getComputedStyle(start).getPropertyValue("--wails-draggable").trim() !==
    "no-drag"
  )
}

/** The webview message bridge. Missing in a browser tab, so a double-click
 *  there stays a double-click. */
export function nativeTitlebarInvoke(): TitlebarInvoke | undefined {
  const w = window as NativeWindow
  if (typeof w._wails?.invoke === "function") {
    return (message) => {
      w._wails!.invoke!(message)
    }
  }
  const webkit = w.webkit?.messageHandlers?.external
  if (webkit && typeof webkit.postMessage === "function") {
    return (message) => webkit.postMessage(message)
  }
  const webview = w.chrome?.webview
  if (webview && typeof webview.postMessage === "function") {
    return (message) => webview.postMessage(message)
  }
  return undefined
}

/** Listen for a title-bar double-click and ask the native window to zoom.
 *  Returns a disposer so tests can take the listener off. */
export function installTitlebarZoom(invoke?: TitlebarInvoke): () => void {
  if (typeof document === "undefined") return () => {}
  const onDblClick = (event: Event) => {
    if (!isTitlebarDragEvent(event)) return
    const send = invoke ?? nativeTitlebarInvoke()
    if (!send) return
    event.preventDefault()
    event.stopImmediatePropagation()
    send(TITLEBAR_DOUBLE_CLICK)
  }
  document.addEventListener("dblclick", onDblClick, true)
  return () => document.removeEventListener("dblclick", onDblClick, true)
}
