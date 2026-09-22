import { App } from "@capacitor/app"
import { Browser } from "@capacitor/browser"
import { Capacitor, registerPlugin } from "@capacitor/core"

// The phone asks GitHub itself. This is the sideload channel, not a hub
// the user types on the PC. A feed configured on the desktop was the long way.
export const RELEASES_LATEST_URL =
  "https://api.github.com/repos/LubyRuffy/eino-swarm/releases/latest"

export const RELEASE_REPO = "LubyRuffy/eino-swarm"

/** Quiet for six hours. A foreground resume must not hammer the unauthenticated cap. */
export const UPDATE_CHECK_TTL_MS = 6 * 60 * 60 * 1000

const DISMISS_KEY = "zwai.phone.update.dismissed"
const CACHE_KEY = "zwai.phone.update.cache"

export type UpdateOffer = {
  version: string
  pageURL: string
  apkURL: string
}

export type UpdateCache = {
  at: number
  current: string
  offer: UpdateOffer | null
}

export type UpdateStore = {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

export type InstallResult = "installed" | "permission" | "failed"

type AppUpdatePlugin = {
  install(opts: { url: string }): Promise<void>
}

const AppUpdate = registerPlugin<AppUpdatePlugin>("AppUpdate", {
  web: () => ({
    async install() {
      throw new Error("unsupported")
    },
  }),
})

export function parseVersion(raw: string): [number, number, number] | null {
  const m = raw.trim().replace(/^v/i, "").match(/^(\d+)\.(\d+)\.(\d+)/)
  if (!m) return null
  return [Number(m[1]), Number(m[2]), Number(m[3])]
}

export function isNewer(latest: string, current: string): boolean {
  const next = parseVersion(latest)
  const have = parseVersion(current)
  if (!next || !have) return false
  for (let i = 0; i < 3; i++) {
    if (next[i] !== have[i]) return next[i] > have[i]
  }
  return false
}

function httpsURL(raw: string): URL | null {
  let url: URL
  try {
    url = new URL(raw)
  } catch {
    return null
  }
  if (url.protocol !== "https:") return null
  if (url.username || url.password) return null
  return url
}

export function allowedDownloadURL(raw: string): string {
  // The offer itself stays on this repo. GitHub then redirects the bytes to
  // a release-asset host; that hop is checked in the Android downloader,
  // not accepted here, or any public release on that host would install.
  const url = httpsURL(raw)
  if (!url) return ""
  if (url.hostname.toLowerCase() !== "github.com") return ""
  if (!url.pathname.startsWith("/" + RELEASE_REPO + "/")) return ""
  return url.toString()
}

export function allowedReleasePage(raw: string): string {
  const url = httpsURL(raw)
  if (!url) return ""
  if (url.hostname.toLowerCase() !== "github.com") return ""
  if (!url.pathname.startsWith("/" + RELEASE_REPO + "/releases/")) return ""
  return url.toString()
}

function sanitizeOffer(offer: UpdateOffer | null | undefined): UpdateOffer | null {
  if (!offer || typeof offer.version !== "string" || !parseVersion(offer.version)) return null
  const pageURL = allowedReleasePage(offer.pageURL ?? "")
  const apkURL = allowedDownloadURL(offer.apkURL ?? "")
  if (!pageURL && !apkURL) return null
  return { version: parseVersion(offer.version)!.join("."), pageURL, apkURL }
}

function pickApk(assets: unknown): string {
  if (!Array.isArray(assets)) return ""
  for (const item of assets) {
    if (!item || typeof item !== "object") continue
    const row = item as { name?: unknown; browser_download_url?: unknown }
    const name = typeof row.name === "string" ? row.name : ""
    if (!/^zwai-.+-android\.apk$/.test(name)) continue
    const url = typeof row.browser_download_url === "string" ? allowedDownloadURL(row.browser_download_url) : ""
    if (url) return url
  }
  return ""
}

/** A newer public release for this platform, or nothing. Drafts and prereleases stay off. */
export function offerFromRelease(platform: string, current: string, body: unknown): UpdateOffer | null {
  if (!body || typeof body !== "object") return null
  const row = body as {
    tag_name?: unknown
    html_url?: unknown
    draft?: unknown
    prerelease?: unknown
    assets?: unknown
  }
  if (row.draft === true || row.prerelease === true) return null
  const tag = typeof row.tag_name === "string" ? row.tag_name : ""
  if (!isNewer(tag, current)) return null
  const parsed = parseVersion(tag)
  if (!parsed) return null
  const pageURL = typeof row.html_url === "string" ? allowedReleasePage(row.html_url) : ""
  const apkURL = platform === "android" ? pickApk(row.assets) : ""
  if (!pageURL && !apkURL) return null
  return { version: parsed.join("."), pageURL, apkURL }
}

function visibleOffer(offer: UpdateOffer | null, dismissed: string, current: string): UpdateOffer | null {
  if (!offer || !isNewer(offer.version, current)) return null
  if (dismissed.trim() === offer.version) return null
  return offer
}

function readCache(store: UpdateStore): UpdateCache | null {
  const raw = store.getItem(CACHE_KEY)
  if (!raw) return null
  try {
    const v = JSON.parse(raw) as Partial<UpdateCache>
    if (!v || typeof v.at !== "number" || typeof v.current !== "string") return null
    return {
      at: v.at,
      current: v.current,
      offer: sanitizeOffer(v.offer ?? null),
    }
  } catch {
    return null
  }
}

function cacheFresh(cache: UpdateCache | null, current: string, now: number, ttlMs: number): boolean {
  if (!cache) return false
  if (cache.current !== current) return false
  return now - cache.at >= 0 && now - cache.at < ttlMs
}

function safeStorage(): UpdateStore {
  return {
    getItem(key) {
      try {
        return localStorage.getItem(key)
      } catch {
        return null
      }
    },
    setItem(key, value) {
      try {
        localStorage.setItem(key, value)
      } catch {
        // private mode
      }
    },
  }
}

export function dismissUpdate(version: string, store: UpdateStore = safeStorage()) {
  const v = version.trim()
  if (!v) return
  store.setItem(DISMISS_KEY, v)
}

export async function installedVersion(): Promise<string> {
  if (Capacitor.getPlatform() === "web") return ""
  try {
    const info = await App.getInfo()
    return info.version ?? ""
  } catch {
    return ""
  }
}

export async function checkForAppUpdate(opts: {
  platform?: string
  version?: string
  now?: number
  ttlMs?: number
  fetcher?: typeof fetch
  store?: UpdateStore
} = {}): Promise<UpdateOffer | null> {
  const platform = opts.platform ?? Capacitor.getPlatform()
  if (platform === "web") return null
  const current = (opts.version ?? (await installedVersion())).trim()
  if (!parseVersion(current)) return null
  const store = opts.store ?? safeStorage()
  const now = opts.now ?? Date.now()
  const ttlMs = opts.ttlMs ?? UPDATE_CHECK_TTL_MS
  const dismissed = store.getItem(DISMISS_KEY) ?? ""
  const cache = readCache(store)
  if (cacheFresh(cache, current, now, ttlMs)) {
    return visibleOffer(cache?.offer ?? null, dismissed, current)
  }
  const fetcher = opts.fetcher ?? fetch
  try {
    const res = await fetcher(RELEASES_LATEST_URL, {
      headers: { Accept: "application/vnd.github+json" },
    })
    if (!res.ok) return visibleOffer(cache?.offer ?? null, dismissed, current)
    const offer = offerFromRelease(platform, current, await res.json())
    store.setItem(CACHE_KEY, JSON.stringify({ at: now, current, offer }))
    return visibleOffer(offer, dismissed, current)
  } catch {
    return visibleOffer(cache?.offer ?? null, dismissed, current)
  }
}

export async function installUpdate(
  offer: UpdateOffer,
  platform = Capacitor.getPlatform(),
  native?: {
    installApk(url: string): Promise<void>
    openPage(url: string): Promise<void>
  },
): Promise<InstallResult> {
  const sink = native ?? {
    installApk: (url: string) => AppUpdate.install({ url }),
    openPage: (url: string) => Browser.open({ url }).then(() => undefined),
  }
  try {
    if (platform === "android" && offer.apkURL) {
      await sink.installApk(offer.apkURL)
      return "installed"
    }
    if (!offer.pageURL) return "failed"
    await sink.openPage(offer.pageURL)
    return "installed"
  } catch (err) {
    if (errorText(err).includes("install_permission")) return "permission"
    return "failed"
  }
}

function errorText(err: unknown): string {
  if (err instanceof Error) return err.message
  if (err && typeof err === "object" && "message" in err) {
    const message = (err as { message?: unknown }).message
    if (typeof message === "string") return message
  }
  return String(err)
}
