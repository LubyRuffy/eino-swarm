const MIME: Record<string, string> = {
  css: "text/css",
  js: "text/javascript",
  mjs: "text/javascript",
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  gif: "image/gif",
  webp: "image/webp",
  svg: "image/svg+xml",
  woff: "font/woff",
  woff2: "font/woff2",
  ttf: "font/ttf",
  otf: "font/otf",
  json: "application/json",
  html: "text/html",
  htm: "text/html",
  xhtml: "application/xhtml+xml",
}

export function mimeFromName(path: string): string {
  const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase()
  return MIME[ext] ?? "application/octet-stream"
}

export function bytesToDataUrl(mime: string, bytes: Uint8Array): string {
  let binary = ""
  const chunk = 0x2000
  for (let offset = 0; offset < bytes.length; offset += chunk) {
    binary += String.fromCharCode(...bytes.subarray(offset, Math.min(offset + chunk, bytes.length)))
  }
  return `data:${mime};base64,${btoa(binary)}`
}

/** Resolve a reference against a file inside a package or workspace.
 * Root-relative paths start at the package root. `..` cannot escape it. */
export function resolvePath(fromFile: string, reference: string): string | null {
  const stripped = reference.split("#")[0]?.split("?")[0]?.trim() ?? ""
  if (!stripped) return null
  let decoded: string
  try {
    decoded = decodeURIComponent(stripped)
  } catch {
    return null
  }
  if (decoded.includes("\\") || decoded.includes("\0")) return null
  const rootRelative = decoded.startsWith("/")
  const stack = rootRelative ? [] : fromFile.split("/").filter(Boolean).slice(0, -1)
  for (const part of decoded.split("/")) {
    if (part === "" || part === ".") continue
    if (part === "..") {
      if (stack.length === 0) return null
      stack.pop()
      continue
    }
    stack.push(part)
  }
  return stack.length ? stack.join("/") : null
}

export type ResourceRef =
  | { kind: "keep"; value: string }
  | { kind: "remote"; value: string }
  | { kind: "local"; path: string }
  | { kind: "drop" }

/** Classify a URL from an untrusted document. Same-origin app URLs become
 * workspace/package paths so the preview never requests the app itself. */
export function classifyResource(raw: string, fromFile: string, appOrigin: string): ResourceRef {
  const value = raw.trim()
  if (!value || value.startsWith("#")) return { kind: "keep", value }
  const lower = value.toLowerCase()
  if (lower.startsWith("javascript:") || lower.startsWith("vbscript:")) return { kind: "drop" }
  if (lower.startsWith("data:") || lower.startsWith("blob:") || lower.startsWith("mailto:") || lower.startsWith("tel:")) {
    return { kind: "keep", value }
  }
  if (lower.startsWith("http://") || lower.startsWith("https://") || lower.startsWith("//")) {
    let url: URL
    try {
      url = new URL(lower.startsWith("//") ? `https:${value}` : value)
    } catch {
      return { kind: "drop" }
    }
    let app: URL | null = null
    try {
      app = appOrigin ? new URL(appOrigin) : null
    } catch {
      app = null
    }
    if (app && url.origin === app.origin) {
      const path = resolvePath("/", url.pathname)
      return path ? { kind: "local", path } : { kind: "drop" }
    }
    return { kind: "remote", value: url.href }
  }
  if (/^[a-z][a-z0-9+.-]*:/i.test(value)) return { kind: "drop" }
  const path = resolvePath(fromFile, value)
  return path ? { kind: "local", path } : { kind: "drop" }
}
