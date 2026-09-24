import { describe, expect, it, vi } from "vitest"

import { setLocale, t } from "./i18n"
import {
  allowedDownloadURL,
  allowedReleasePage,
  checkAppVersionNow,
  checkForAppUpdate,
  classifyLatestRelease,
  dismissUpdate,
  downloadStatus,
  formatByteSize,
  installUpdate,
  isNewer,
  offerFromRelease,
  parseVersion,
  RELEASE_REPO,
  RELEASES_LATEST_URL,
  listenDownloadProgress,
  responseErrorText,
  webShellVersion,
  type UpdateOffer,
  type UpdateStore,
} from "./app-update"

function memoryStore(): UpdateStore {
  const bag = new Map<string, string>()
  return {
    getItem: (key) => bag.get(key) ?? null,
    setItem: (key, value) => {
      bag.set(key, value)
    },
  }
}

function release(patch: Record<string, unknown> = {}) {
  return {
    tag_name: "v2.4.0",
    html_url: "https://github.com/" + RELEASE_REPO + "/releases/tag/v2.4.0",
    draft: false,
    prerelease: false,
    assets: [
      {
        name: "zwai-2.4.0-android.apk",
        browser_download_url:
          "https://github.com/" + RELEASE_REPO + "/releases/download/v2.4.0/zwai-2.4.0-android.apk",
      },
    ],
    ...patch,
  }
}

const page = "https://github.com/" + RELEASE_REPO + "/releases/tag/v2.4.0"
const apk =
  "https://github.com/" + RELEASE_REPO + "/releases/download/v2.4.0/zwai-2.4.0-android.apk"

describe("app update", () => {
  it("asks GitHub for the latest release, not a pinned tag and not the PC", () => {
    expect(RELEASES_LATEST_URL).toBe(
      "https://api.github.com/repos/" + RELEASE_REPO + "/releases/latest",
    )
    expect(RELEASES_LATEST_URL).not.toMatch(/\/releases\/tag\//)
    expect(RELEASES_LATEST_URL).not.toMatch(/127\.0\.0\.1|localhost/)
  })

  it("treats versions numerically, including a leading v", () => {
    expect(isNewer("v2.10.0", "2.9.0")).toBe(true)
    expect(isNewer("2.4.0", "2.4.0")).toBe(false)
    expect(isNewer("2.4.0", "2.4.1")).toBe(false)
    expect(isNewer("dev", "1.0.0")).toBe(false)
    expect(isNewer("2.0.0", "not-a-version")).toBe(false)
  })

  it("offers the android package on a newer public release and ignores drafts", () => {
    const offer = offerFromRelease("android", "2.3.0", release())
    expect(offer).toEqual({ version: "2.4.0", pageURL: page, apkURL: apk })
    expect(offerFromRelease("android", "2.4.0", release())).toBeNull()
    expect(offerFromRelease("android", "2.3.0", release({ draft: true }))).toBeNull()
    expect(offerFromRelease("android", "2.3.0", release({ prerelease: true }))).toBeNull()
  })

  it("does not install an artefact that is not this repo's android package", () => {
    const foreign = release({
      assets: [
        {
          name: "zwai-2.4.0-android.apk",
          browser_download_url: "https://github.com/other/repo/releases/download/v2.4.0/zwai-2.4.0-android.apk",
        },
      ],
    })
    expect(offerFromRelease("android", "1.0.0", foreign)?.apkURL).toBe("")
    expect(allowedDownloadURL("http://github.com/" + RELEASE_REPO + "/a.apk")).toBe("")
    expect(allowedDownloadURL("https://user:pw@github.com/" + RELEASE_REPO + "/a.apk")).toBe("")
    expect(allowedReleasePage("https://example.com/" + RELEASE_REPO + "/releases/tag/v2.4.0")).toBe("")
    expect(allowedDownloadURL("https://release-assets.githubusercontent.com/asset")).toBe("")
    const notes = release({
      assets: [{ name: "notes.txt", browser_download_url: apk }],
    })
    expect(offerFromRelease("android", "1.0.0", notes)?.apkURL).toBe("")
  })

  it("does not offer an Android release as an install on iOS", async () => {
    expect(offerFromRelease("ios", "2.3.0", release())).toBeNull()
    const fetcher = vi.fn(async () => new Response(JSON.stringify(release()), { status: 200 }))
    const store = memoryStore()
    store.setItem(
      "zwai.phone.update.cache",
      JSON.stringify({ at: 1, current: "2.3.0", offer: { version: "2.4.0", pageURL: page, apkURL: "" } }),
    )
    expect(
      await checkForAppUpdate({ platform: "ios", version: "2.3.0", now: 1, fetcher, store }),
    ).toBeNull()
    expect(fetcher).not.toHaveBeenCalled()
  })

  it("does not touch the network from the browser, and a fresh answer is reused", async () => {
    const fetcher = vi.fn()
    expect(await checkForAppUpdate({ platform: "web", version: "1.0.0", fetcher })).toBeNull()
    expect(fetcher).not.toHaveBeenCalled()

    const store = memoryStore()
    const body = release()
    const once = vi.fn(async (input: RequestInfo | URL) => {
      expect(String(input)).toBe(RELEASES_LATEST_URL)
      return new Response(JSON.stringify(body), { status: 200 })
    })
    const first = await checkForAppUpdate({
      platform: "android",
      version: "2.3.0",
      now: 1_000,
      fetcher: once,
      store,
    })
    expect(first?.version).toBe("2.4.0")
    expect(once).toHaveBeenCalledOnce()

    const again = vi.fn()
    const second = await checkForAppUpdate({
      platform: "android",
      version: "2.3.0",
      now: 1_000 + 60_000,
      fetcher: again,
      store,
    })
    expect(second?.version).toBe("2.4.0")
    expect(again).not.toHaveBeenCalled()
  })

  it("hides a version the person already skipped, until a later one exists", async () => {
    const store = memoryStore()
    dismissUpdate("2.4.0", store)
    const fetcher = vi.fn(async () => new Response(JSON.stringify(release()), { status: 200 }))
    expect(
      await checkForAppUpdate({
        platform: "android",
        version: "2.3.0",
        now: 5_000,
        fetcher,
        store,
      }),
    ).toBeNull()

    const later = release({
      tag_name: "v2.5.0",
      html_url: "https://github.com/" + RELEASE_REPO + "/releases/tag/v2.5.0",
    })
    fetcher.mockResolvedValueOnce(new Response(JSON.stringify(later), { status: 200 }))
    const offer = await checkForAppUpdate({
      platform: "android",
      version: "2.3.0",
      now: 5_000 + 7 * 60 * 60 * 1000,
      fetcher,
      store,
    })
    expect(offer?.version).toBe("2.5.0")
  })

  it("stays quiet when the feed is down and still drops a poisoned cached url", async () => {
    const store = memoryStore()
    const fetcher = vi.fn(async () => {
      throw new Error("offline")
    })
    expect(
      await checkForAppUpdate({
        platform: "android",
        version: "2.3.0",
        now: 10,
        fetcher,
        store,
      }),
    ).toBeNull()

    const poisoned: UpdateOffer = {
      version: "9.0.0",
      pageURL: page,
      apkURL: "https://evil.example/zwai-9.0.0-android.apk",
    }
    store.setItem(
      "zwai.phone.update.cache",
      JSON.stringify({ at: 10, current: "2.3.0", offer: poisoned }),
    )
    fetcher.mockRejectedValueOnce(new Error("offline"))
    const offer = await checkForAppUpdate({
      platform: "android",
      version: "2.3.0",
      now: 10 + 7 * 60 * 60 * 1000,
      fetcher,
      store,
    })
    expect(offer?.apkURL).toBe("")
    expect(offer?.pageURL).toBe(page)

    const fresh = memoryStore()
    fresh.setItem(
      "zwai.phone.update.cache",
      JSON.stringify({
        at: 1_000,
        current: "2.4.0",
        offer: { version: "2.0.0", pageURL: page, apkURL: apk },
      }),
    )
    const quiet = vi.fn()
    expect(
      await checkForAppUpdate({
        platform: "android",
        version: "2.4.0",
        now: 2_000,
        fetcher: quiet,
        store: fresh,
      }),
    ).toBeNull()
    expect(quiet).not.toHaveBeenCalled()
  })

  it("installs the apk on Android and opens the release page otherwise", async () => {
    const offer: UpdateOffer = { version: "2.4.0", pageURL: page, apkURL: apk }
    const installApk = vi.fn(async () => undefined)
    const openPage = vi.fn(async () => undefined)
    expect(await installUpdate(offer, "android", { installApk, openPage })).toBe("installed")
    expect(installApk).toHaveBeenCalledWith(apk)
    expect(openPage).not.toHaveBeenCalled()

    installApk.mockRejectedValueOnce(new Error("install_permission"))
    expect(await installUpdate(offer, "android", { installApk, openPage })).toBe("permission")
    installApk.mockRejectedValueOnce({ message: "install_permission" })
    expect(await installUpdate(offer, "android", { installApk, openPage })).toBe("permission")

    installApk.mockRejectedValueOnce(new Error("download_failed"))
    expect(await installUpdate(offer, "android", { installApk, openPage })).toBe("failed")

    expect(
      await installUpdate({ ...offer, apkURL: "" }, "ios", { installApk, openPage }),
    ).toBe("installed")
    expect(openPage).toHaveBeenCalledWith(page)
    expect(await installUpdate({ version: "2.4.0", pageURL: "", apkURL: "" }, "ios", { installApk, openPage })).toBe(
      "failed",
    )
  })
})

describe("download progress", () => {
  it("shows a percent when the length is known and bytes when it is not", () => {
    setLocale("en")
    expect(formatByteSize(512)).toBe("512 B")
    expect(formatByteSize(1536)).toBe("1.5 KB")
    expect(formatByteSize(20 * 1024)).toBe("20 KB")
    expect(downloadStatus(null)).toEqual({ text: t("update.downloading"), percent: null })
    expect(downloadStatus({ received: 0, total: 0 })).toEqual({
      text: t("update.downloading"),
      percent: null,
    })
    expect(downloadStatus({ received: 40, total: 100 })).toEqual({
      text: t("update.downloadingProgress", { percent: 40 }),
      percent: 40,
    })
    expect(downloadStatus({ received: 150, total: 100 }).percent).toBe(100)
    expect(downloadStatus({ received: 1536, total: 0 })).toEqual({
      text: t("update.downloadingBytes", { size: "1.5 KB" }),
      percent: null,
    })
    expect(downloadStatus({ received: -1, total: Number.NaN })).toEqual({
      text: t("update.downloading"),
      percent: null,
    })
  })

  it("forwards native progress only after the listener is attached", async () => {
    const seen: { received: number; total: number }[] = []
    let listener: ((event: { received?: number; total?: number }) => void) | undefined
    let removed = false
    const stop = await listenDownloadProgress((progress) => seen.push(progress), {
      async addListener(_event, cb) {
        listener = cb
        return {
          async remove() {
            removed = true
          },
        }
      },
    })
    listener?.({ received: 10, total: 40 })
    listener?.({ received: -5, total: 0 })
    expect(seen).toEqual([
      { received: 10, total: 40 },
      { received: 0, total: 0 },
    ])
    stop()
    expect(removed).toBe(true)
    listener?.({ received: 20, total: 40 })
    expect(seen).toHaveLength(2)
  })
})

describe("manual version check", () => {
  it("keeps the response's own error and does not invent one from the status", () => {
    setLocale("en")
    expect(
      responseErrorText(403, "Forbidden", JSON.stringify({ message: "feed refused the check" })),
    ).toBe("feed refused the check")
    expect(responseErrorText(403, "Forbidden", JSON.stringify({ message: "", error: "nope" }))).toBe(
      "nope",
    )
    expect(responseErrorText(403, "Forbidden", JSON.stringify({ message: 3 }))).toBe("403 Forbidden")
    expect(responseErrorText(502, "Bad Gateway", "{")).toBe("502 Bad Gateway")
    expect(responseErrorText(502, "Bad Gateway", JSON.stringify(["nope"]))).toBe("502 Bad Gateway")
    expect(responseErrorText(400, "Bad Request", "missing token")).toBe("missing token")
    expect(responseErrorText(502, "Bad Gateway", "<html>nope</html>")).toBe("502 Bad Gateway")
    expect(responseErrorText(400, "Bad Request", "x".repeat(400))).toBe("400 Bad Request")
    expect(responseErrorText(0, "", "")).toBe(t("update.badResponse"))
  })

  it("says current, available, or unreadable without treating a side message as the verdict", () => {
    setLocale("en")
    expect(classifyLatestRelease("android", "2.4.0", { ...release(), message: "feed refused the check" })).toEqual({
      status: "current",
    })
    expect(classifyLatestRelease("android", "2.4.1", release())).toEqual({ status: "current" })
    expect(classifyLatestRelease("android", "2.3.0", release({ prerelease: true }))).toEqual({
      status: "current",
    })
    expect(classifyLatestRelease("ios", "2.3.0", release())).toEqual({
      status: "error",
      message: t("update.iosSideload"),
    })
    expect(classifyLatestRelease("android", "1.0.0", { tag_name: "v2.0.0" })).toEqual({
      status: "error",
      message: t("update.noArtifact"),
    })
    expect(classifyLatestRelease("android", "1.0.0", { message: "not a release" })).toEqual({
      status: "error",
      message: "not a release",
    })
    expect(classifyLatestRelease("android", "", release())).toEqual({
      status: "error",
      message: t("update.noLocalVersion"),
    })
    expect(classifyLatestRelease("android", "1.0.0", null)).toEqual({
      status: "error",
      message: t("update.badResponse"),
    })
  })

  it("always asks the feed, even when a quiet check would reuse cache or stay hidden", async () => {
    const store = memoryStore()
    store.setItem(
      "zwai.phone.update.cache",
      JSON.stringify({ at: 1_000, current: "2.3.0", offer: null }),
    )
    dismissUpdate("2.4.0", store)
    const fetcher = vi.fn(async () => new Response(JSON.stringify(release()), { status: 200 }))
    const asked = await checkAppVersionNow({
      platform: "android",
      version: "2.3.0",
      now: 2_000,
      fetcher,
      store,
    })
    expect(fetcher).toHaveBeenCalledOnce()
    expect(asked).toEqual({
      status: "available",
      offer: { version: "2.4.0", pageURL: page, apkURL: apk },
    })

    const quiet = vi.fn()
    expect(
      await checkForAppUpdate({
        platform: "android",
        version: "2.3.0",
        now: 2_000,
        fetcher: quiet,
        store,
      }),
    ).toBeNull()
    expect(quiet).not.toHaveBeenCalled()
  })

  it("returns the feed's error and does not cache that failure as up to date", async () => {
    const store = memoryStore()
    const fetcher = vi.fn(async () => {
      return new Response(JSON.stringify({ message: "feed refused the check" }), {
        status: 403,
        statusText: "Forbidden",
      })
    })
    expect(
      await checkAppVersionNow({
        platform: "android",
        version: "1.0.0",
        now: 50,
        fetcher,
        store,
      }),
    ).toEqual({ status: "error", message: "feed refused the check" })
    expect(store.getItem("zwai.phone.update.cache")).toBeNull()

    fetcher.mockResolvedValueOnce(new Response("<html>nope</html>", { status: 502, statusText: "Bad Gateway" }))
    expect(
      await checkAppVersionNow({ platform: "android", version: "1.0.0", now: 60, fetcher, store }),
    ).toEqual({ status: "error", message: "502 Bad Gateway" })
    fetcher.mockResolvedValueOnce(new Response("not-json", { status: 200 }))
    expect(
      await checkAppVersionNow({ platform: "android", version: "1.0.0", now: 65, fetcher, store }),
    ).toEqual({ status: "error", message: t("update.badResponse") })
    expect(store.getItem("zwai.phone.update.cache")).toBeNull()

    fetcher.mockRejectedValueOnce(new Error("socket reset"))
    expect(
      await checkAppVersionNow({ platform: "android", version: "1.0.0", now: 70, fetcher, store }),
    ).toEqual({ status: "error", message: "socket reset" })

    const quiet = vi.fn(async () => new Response(JSON.stringify({ message: "feed refused the check" }), { status: 403 }))
    expect(
      await checkForAppUpdate({
        platform: "android",
        version: "1.0.0",
        now: 80,
        fetcher: quiet,
        store: memoryStore(),
      }),
    ).toBeNull()
  })

  it("remembers a successful check so the quiet bar does not ask again immediately", async () => {
    const store = memoryStore()
    const fetcher = vi.fn(
      async () =>
        new Response(JSON.stringify(release({ tag_name: "v1.2.0" })), { status: 200 }),
    )
    expect(
      await checkAppVersionNow({
        platform: "android",
        version: "1.2.0",
        now: 90,
        fetcher,
        store,
      }),
    ).toEqual({ status: "current" })
    const quiet = vi.fn()
    expect(
      await checkForAppUpdate({
        platform: "android",
        version: "1.2.0",
        now: 100,
        fetcher: quiet,
        store,
      }),
    ).toBeNull()
    expect(quiet).not.toHaveBeenCalled()
  })

  it("compares a web check against the shell version instead of claiming there is no app", async () => {
    const current = webShellVersion()
    expect(parseVersion(current)).not.toBeNull()
    const parsed = parseVersion(current)!
    const newer = `${parsed[0]}.${parsed[1]}.${parsed[2] + 1}`
    const fetcher = vi.fn(async () => new Response(JSON.stringify(release({ tag_name: "v" + current })), { status: 200 }))
    expect(
      await checkAppVersionNow({ platform: "web", now: 3, fetcher, store: memoryStore() }),
    ).toEqual({ status: "current" })
    fetcher.mockResolvedValueOnce(
      new Response(JSON.stringify(release({ tag_name: "v" + newer })), { status: 200 }),
    )
    const update = await checkAppVersionNow({
      platform: "web",
      now: 4,
      fetcher,
      store: memoryStore(),
    })
    expect(update.status).toBe("available")
    if (update.status === "available") expect(update.offer.version).toBe(newer)
  })
})
