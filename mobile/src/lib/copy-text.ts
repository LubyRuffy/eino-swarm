/** Copy text from a click. WKWebView (the desktop window) often rejects
 *  `navigator.clipboard.writeText`; `document.execCommand("copy")` still
 *  holds the user gesture. Try that first so a denied Clipboard API is
 *  not a dead button.
 *
 *  Do not trust execCommand's boolean. WebKit returns true for a 1px
 *  opacity-0 textarea even when the pasteboard stays empty — the button
 *  flipped to Copied and paste was blank. Success is planting the payload
 *  on the copy event. */

export async function copyText(text: string): Promise<boolean> {
  if (copyWithExecCommand(text)) return true
  const write = navigator.clipboard?.writeText
  if (!write) return false
  try {
    await write.call(navigator.clipboard, text)
    return true
  } catch {
    return false
  }
}

function copyWithExecCommand(text: string): boolean {
  if (typeof document.execCommand !== "function") return false

  let planted = false
  const onCopy = (e: Event) => {
    const clip = (e as ClipboardEvent).clipboardData
    if (!clip) return
    try {
      clip.setData("text/plain", text)
      e.preventDefault()
      planted = true
    } catch {
      // Some webviews expose clipboardData then throw on setData.
    }
  }

  document.addEventListener("copy", onCopy, true)
  const el = document.createElement("textarea")
  el.value = text
  el.setAttribute("readonly", "")
  el.setAttribute("aria-hidden", "true")
  el.tabIndex = -1
  // Off-screen with a real box. 1px + opacity:0 is what WebKit "copies"
  // as empty while still returning true.
  el.style.cssText =
    "position:fixed;top:0;left:-9999px;width:1em;height:1em;padding:0;border:0;outline:none;box-shadow:none;background:transparent;opacity:0.01;white-space:pre;user-select:text;font-size:12pt;"
  document.body.appendChild(el)
  el.focus()
  el.select()
  el.setSelectionRange(0, text.length)
  try {
    document.execCommand("copy")
  } catch {
    // The boolean is a lie on WebKit; planted is the only signal.
  } finally {
    document.removeEventListener("copy", onCopy, true)
    el.remove()
  }
  return planted
}
