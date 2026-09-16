/** How a clicked href should be handled. "leave" is anything that would
 *  replace the app window if the webview followed it. */
export type HrefAction = "stay" | "leave" | "block"

export function classifyHref(
  href: string | null | undefined,
  origin: string,
): HrefAction {
  if (href == null) return "stay"
  const raw = href.trim()
  if (raw === "" || raw.startsWith("#")) return "stay"
  let url: URL
  try {
    url = new URL(raw, origin)
  } catch {
    return "block"
  }
  const proto = url.protocol.toLowerCase()
  if (
    proto === "javascript:" ||
    proto === "data:" ||
    proto === "vbscript:" ||
    proto === "file:"
  ) {
    return "block"
  }
  if (url.origin === origin) return "stay"
  if (proto === "http:" || proto === "https:" || proto === "mailto:") return "leave"
  return "block"
}

export function attachExternalLinkHandler(opts: {
  origin?: string
  openNative?: (url: string) => void
  openWindow?: (url: string) => void
}): () => void {
  const onClick = (event: MouseEvent) => {
    handleLinkClick(event, opts)
  }
  document.addEventListener("click", onClick)
  document.addEventListener("auxclick", onClick)
  return () => {
    document.removeEventListener("click", onClick)
    document.removeEventListener("auxclick", onClick)
  }
}

/** WKWebView loads target=_blank in the same window. preventDefault is the
 *  actual fix; the desktop shell then opens the system browser. */
export function handleLinkClick(
  event: MouseEvent,
  opts: {
    origin?: string
    openNative?: (url: string) => void
    openWindow?: (url: string) => void
  },
): boolean {
  if (event.defaultPrevented) return false
  if (event.button !== 0 && event.button !== 1) return false
  const target = event.target
  if (!(target instanceof Element)) return false
  const a = target.closest("a")
  if (!a) return false
  if (a.hasAttribute("download")) return false
  const origin =
    opts.origin ?? (typeof window === "undefined" ? "" : window.location.origin)
  const href = a.getAttribute("href")
  const action = classifyHref(href, origin)
  if (action === "stay") return false
  event.preventDefault()
  if (action === "block" || href == null) return true
  let abs = href
  try {
    abs = new URL(href, origin).href
  } catch {
    return true
  }
  if (opts.openNative) {
    opts.openNative(abs)
    return true
  }
  const open =
    opts.openWindow ??
    ((url: string) => {
      window.open(url, "_blank", "noopener,noreferrer")
    })
  open(abs)
  return true
}
