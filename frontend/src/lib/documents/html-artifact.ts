import { MAX_HTML_ASSET_BYTES, MAX_HTML_ASSETS } from "./limits"
import { bytesToDataUrl, classifyResource, mimeFromName, type ResourceRef } from "./paths"

export const HTML_ARTIFACT_SANDBOX = "allow-scripts allow-forms allow-modals"
export const PACKAGE_DOCUMENT_SANDBOX = ""

const ARTIFACT_CSP = [
  "default-src 'none'",
  "script-src 'unsafe-inline' 'unsafe-eval' data: blob: https:",
  "style-src 'unsafe-inline' data: blob: https:",
  "img-src data: blob: https:",
  "font-src data: blob: https:",
  "media-src data: blob: https:",
  "connect-src 'none'",
  "worker-src 'none'",
  "frame-src https:",
  "object-src 'none'",
  "base-uri 'none'",
  "form-action 'none'",
].join("; ")

const STATIC_CSP = [
  "default-src 'none'",
  "script-src 'none'",
  "style-src 'unsafe-inline' data:",
  "img-src data:",
  "font-src data:",
  "media-src data:",
  "connect-src 'none'",
  "worker-src 'none'",
  "frame-src 'none'",
  "object-src 'none'",
  "base-uri 'none'",
  "form-action 'none'",
].join("; ")

export type AssetReader = (path: string, signal: AbortSignal) => Promise<Uint8Array | null>

type Budget = { fits(size: number): boolean }

function budget(): Budget {
  let used = 0
  let count = 0
  return {
    fits(size: number) {
      if (count >= MAX_HTML_ASSETS || used + size > MAX_HTML_ASSET_BYTES) return false
      count += 1
      used += size
      return true
    },
  }
}

function urlAttributeNames(element: Element): string[] {
  const names: string[] = []
  for (const attr of element.attributes) {
    const local = attr.localName.toLowerCase()
    if (local === "href" || local === "src" || local === "action" || local === "formaction" || local === "poster" || attr.name.toLowerCase() === "xlink:href") {
      names.push(attr.name)
    }
  }
  return names
}

function decodeText(bytes: Uint8Array): string | null {
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes)
  } catch {
    return null
  }
}

async function inlineLocal(
  ref: ResourceRef,
  read: AssetReader,
  signal: AbortSignal,
  allowance: Budget,
): Promise<Uint8Array | null> {
  if (ref.kind !== "local") return null
  signal.throwIfAborted()
  const bytes = await read(ref.path, signal)
  if (!bytes || !allowance.fits(bytes.byteLength)) return null
  return bytes
}

async function rewriteCss(
  css: string,
  fromFile: string,
  appOrigin: string,
  read: AssetReader,
  signal: AbortSignal,
  allowance: Budget,
  seen: Set<string>,
  depth: number,
): Promise<string> {
  let next = css
  const imports = [...css.matchAll(/@import\s+(?:url\(\s*(['"]?)([^'")]+)\1\s*\)|(['"])([^'"]+)\3)\s*;/gi)]
  for (const match of imports) {
    const ref = classifyResource(match[2] || match[4] || "", fromFile, appOrigin)
    let replacement = ""
    if (ref.kind === "local" && depth < 4 && !seen.has(ref.path)) {
      const bytes = await inlineLocal(ref, read, signal, allowance)
      const text = bytes ? decodeText(bytes) : null
      if (text !== null) {
        seen.add(ref.path)
        replacement = await rewriteCss(text, ref.path, appOrigin, read, signal, allowance, seen, depth + 1)
      }
    } else if (ref.kind === "remote") replacement = match[0]
    next = next.replace(match[0], replacement)
  }
  const urls = [...next.matchAll(/url\(\s*(['"]?)([^'")]+)\1\s*\)/gi)]
  for (const match of urls) {
    const ref = classifyResource(match[2] || "", fromFile, appOrigin)
    let replacement = "url(\"data:,\")"
    if (ref.kind === "remote" || ref.kind === "keep") replacement = match[0]
    else if (ref.kind === "local") {
      const bytes = await inlineLocal(ref, read, signal, allowance)
      if (bytes) replacement = `url("${bytesToDataUrl(mimeFromName(ref.path), bytes)}")`
    }
    next = next.replace(match[0], replacement)
  }
  return next.replaceAll("</style", "<\\/style")
}

async function applyUrl(element: Element, name: string, fromFile: string, appOrigin: string, read: AssetReader, signal: AbortSignal, allowance: Budget, asImage: boolean): Promise<void> {
  const raw = element.getAttribute(name)
  if (raw === null) return
  const ref = classifyResource(raw, fromFile, appOrigin)
  if (ref.kind === "keep" || ref.kind === "remote") return
  if (ref.kind === "drop") {
    element.removeAttribute(name)
    return
  }
  if (!asImage) {
    element.removeAttribute(name)
    return
  }
  const bytes = await inlineLocal(ref, read, signal, allowance)
  if (!bytes) element.removeAttribute(name)
  else element.setAttribute(name, bytesToDataUrl(mimeFromName(ref.path), bytes))
}

/**
 * Build a srcdoc for an untrusted HTML document.
 * The iframe must use HTML_ARTIFACT_SANDBOX and must not use the download
 * URL as src: that URL is same-origin with the app. allow-same-origin is
 * omitted so scripts cannot read the app. Package documents (EPUB) pass
 * scripts:false and must use a sandbox that does not allow scripts.
 */
export async function buildHtmlArtifact(
  html: string,
  filePath: string,
  appOrigin: string,
  read: AssetReader,
  signal: AbortSignal,
  options?: { scripts?: boolean },
): Promise<string> {
  signal.throwIfAborted()
  const scripts = options?.scripts !== false
  const doc = new DOMParser().parseFromString(html, "text/html")
  const allowance = budget()
  const seen = new Set<string>()
  for (const element of [...doc.querySelectorAll("*")]) {
    signal.throwIfAborted()
    const local = element.localName
    if (local === "base") {
      element.remove()
      continue
    }
    if (!scripts && (local === "a" || local === "area")) {
      // A CSP restricting resources does not stop a link navigating this frame.
      for (const name of urlAttributeNames(element)) {
        if (!element.getAttribute(name)?.trim().startsWith("#")) element.removeAttribute(name)
      }
      for (const name of ["target", "download", "ping"]) element.removeAttribute(name)
    }
    if (local === "meta") {
      const equiv = element.getAttribute("http-equiv")?.trim().toLowerCase()
      if (equiv === "refresh" || equiv === "content-security-policy") element.remove()
      continue
    }
    if (local === "script") {
      if (!scripts) {
        element.remove()
        continue
      }
      const source = element.getAttribute("src")
      if (!source) continue
      const ref = classifyResource(source, filePath, appOrigin)
      if (ref.kind === "remote") {
        element.setAttribute("src", ref.value)
        continue
      }
      element.removeAttribute("src")
      const bytes = await inlineLocal(ref, read, signal, allowance)
      const text = bytes ? decodeText(bytes) : null
      if (text === null) element.remove()
      else element.textContent = text
      continue
    }
    if (local === "link") {
      const rel = (element.getAttribute("rel") ?? "").toLowerCase()
      const href = element.getAttribute("href") ?? ""
      const ref = classifyResource(href, filePath, appOrigin)
      if (/\bstylesheet\b/.test(rel)) {
        if (ref.kind === "remote") continue
        const bytes = await inlineLocal(ref, read, signal, allowance)
        const text = bytes ? decodeText(bytes) : null
        if (text === null) element.remove()
        else {
          const style = doc.createElement("style")
          style.textContent = await rewriteCss(text, ref.kind === "local" ? ref.path : filePath, appOrigin, read, signal, allowance, seen, 0)
          element.replaceWith(style)
        }
        continue
      }
      if (!scripts) {
        element.remove()
        continue
      }
      if (/\b(?:preload|modulepreload|prefetch|preconnect|dns-prefetch|prerender)\b/.test(rel) && ref.kind !== "remote") {
        element.remove()
        continue
      }
      await applyUrl(element, "href", filePath, appOrigin, read, signal, allowance, false)
      continue
    }
    if (local === "iframe" || local === "object" || local === "embed") {
      const attr = local === "object" ? "data" : "src"
      const ref = classifyResource(element.getAttribute(attr) ?? "", filePath, appOrigin)
      if (!scripts || ref.kind !== "remote") element.remove()
      continue
    }
    if (local === "img" || local === "image" || local === "source" || local === "video" || local === "audio") {
      for (const name of urlAttributeNames(element)) {
        await applyUrl(element, name, filePath, appOrigin, read, signal, allowance, true)
      }
      element.removeAttribute("srcset")
    }
    for (const name of urlAttributeNames(element)) {
      if (!element.hasAttribute(name)) continue
      const ref = classifyResource(element.getAttribute(name) ?? "", filePath, appOrigin)
      if (ref.kind === "local" || ref.kind === "drop") element.removeAttribute(name)
    }
    if (element.hasAttribute("style")) {
      element.setAttribute("style", await rewriteCss(element.getAttribute("style") ?? "", filePath, appOrigin, read, signal, allowance, seen, 0))
    }
  }
  for (const style of [...doc.querySelectorAll("style")]) {
    style.textContent = await rewriteCss(style.textContent ?? "", filePath, appOrigin, read, signal, allowance, seen, 0)
  }
  const meta = doc.createElement("meta")
  meta.setAttribute("http-equiv", "Content-Security-Policy")
  meta.setAttribute("content", scripts ? ARTIFACT_CSP : STATIC_CSP)
  doc.head.insertBefore(meta, doc.head.firstChild)
  return `<!DOCTYPE html>${doc.documentElement.outerHTML}`
}
