/** Copy text from a click. WKWebView (the desktop window) often rejects
 *  `navigator.clipboard.writeText`; `document.execCommand("copy")` still
 *  holds the user gesture. Try that first so a denied Clipboard API is
 *  not a dead button. */

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
  const el = document.createElement("textarea")
  el.value = text
  el.setAttribute("readonly", "")
  el.setAttribute("aria-hidden", "true")
  el.tabIndex = -1
  el.style.position = "fixed"
  el.style.top = "0"
  el.style.left = "0"
  el.style.width = "1px"
  el.style.height = "1px"
  el.style.opacity = "0"
  document.body.appendChild(el)
  el.focus()
  el.select()
  el.setSelectionRange(0, text.length)
  let ok = false
  try {
    ok = document.execCommand("copy")
  } catch {
    ok = false
  }
  el.remove()
  return ok
}
