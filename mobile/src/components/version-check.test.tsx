import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/app-update", async () => {
  const actual = await vi.importActual<typeof import("@/lib/app-update")>("@/lib/app-update")
  return {
    ...actual,
    checkAppVersionNow: vi.fn(),
    installUpdate: vi.fn(),
    dismissUpdate: vi.fn(),
    listenDownloadProgress: vi.fn(async () => () => undefined),
  }
})

import {
  checkAppVersionNow,
  dismissUpdate,
  installUpdate,
  listenDownloadProgress,
  type DownloadProgress,
  type UpdateOffer,
  type VersionCheckResult,
} from "@/lib/app-update"
import { setLocale, t } from "@/lib/i18n"
import type { SavedLink } from "@/lib/store"

import { HostChrome } from "./host-chrome"
import { VersionCheck } from "./version-check"

const offer: UpdateOffer = {
  version: "2.4.0",
  pageURL: "https://github.com/LubyRuffy/eino-swarm/releases/tag/v2.4.0",
  apkURL: "https://github.com/LubyRuffy/eino-swarm/releases/download/v2.4.0/zwai-2.4.0-android.apk",
}

function host(): SavedLink {
  return {
    hubURL: "https://hub.example.test",
    ticket: "t",
    hostPub: "p",
    sessionID: "s",
    fingerprint: "a",
    label: "desk",
  }
}

function deferred<T>() {
  let resolve: (value: T) => void = () => undefined
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

describe("VersionCheck", () => {
  beforeEach(() => {
    setLocale("en")
    vi.mocked(checkAppVersionNow).mockReset()
    vi.mocked(installUpdate).mockReset()
    vi.mocked(dismissUpdate).mockReset()
    vi.mocked(listenDownloadProgress).mockReset()
    vi.mocked(listenDownloadProgress).mockResolvedValue(() => undefined)
    delete window.__zwaiAndroidBack
  })

  it("shows that a check is in progress, then that this build is current", async () => {
    const pending = deferred<VersionCheckResult>()
    vi.mocked(checkAppVersionNow).mockReturnValue(pending.promise)
    render(<VersionCheck onClose={vi.fn()} />)
    expect(screen.getByRole("status")).toHaveTextContent(t("update.checking"))
    const bar = screen.getByRole("progressbar")
    expect(bar).not.toHaveAttribute("aria-valuenow")
    pending.resolve({ status: "current" })
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent(t("update.current")))
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: t("update.upgrade") })).not.toBeInTheDocument()
  })

  it("asks before upgrading and does not hide the version the way Not now does", async () => {
    vi.mocked(checkAppVersionNow).mockResolvedValue({ status: "available", offer })
    const onClose = vi.fn()
    render(<VersionCheck onClose={onClose} />)
    expect(await screen.findByRole("status")).toHaveTextContent(t("update.ask", { version: "2.4.0" }))
    fireEvent.click(screen.getByRole("button", { name: t("update.later") }))
    expect(onClose).toHaveBeenCalledOnce()
    expect(dismissUpdate).not.toHaveBeenCalled()
  })

  it("shows the feed's error and offers no upgrade", async () => {
    vi.mocked(checkAppVersionNow).mockResolvedValue({
      status: "error",
      message: "feed refused the check",
    })
    render(<VersionCheck onClose={vi.fn()} />)
    expect(await screen.findByRole("alert")).toHaveTextContent("feed refused the check")
    expect(screen.queryByRole("button", { name: t("update.upgrade") })).not.toBeInTheDocument()
  })

  it("hands an accepted upgrade to the installer and reports how far the download is", async () => {
    vi.mocked(checkAppVersionNow).mockResolvedValue({ status: "available", offer })
    let finish: (result: "installed") => void = () => undefined
    vi.mocked(installUpdate).mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    vi.mocked(listenDownloadProgress).mockImplementation(async (onProgress: (progress: DownloadProgress) => void) => {
      onProgress({ received: 40, total: 100 })
      return () => undefined
    })
    const onClose = vi.fn()
    render(<VersionCheck onClose={onClose} />)
    const button = await screen.findByRole("button", { name: t("update.upgrade") })
    fireEvent.click(button)
    fireEvent.click(button)
    await waitFor(() => expect(installUpdate).toHaveBeenCalledOnce())
    expect(installUpdate).toHaveBeenCalledWith(offer)
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "40")
    expect(screen.getByRole("status")).toHaveTextContent(t("update.downloadingProgress", { percent: 40 }))
    finish("installed")
    await waitFor(() => expect(onClose).toHaveBeenCalled())
  })

  it("keeps the dialog open when the install is refused", async () => {
    vi.mocked(checkAppVersionNow).mockResolvedValue({ status: "available", offer })
    vi.mocked(installUpdate).mockResolvedValue("permission")
    const onClose = vi.fn()
    render(<VersionCheck onClose={onClose} />)
    fireEvent.click(await screen.findByRole("button", { name: t("update.upgrade") }))
    expect(await screen.findByRole("alert")).toHaveTextContent(t("update.permission"))
    expect(onClose).not.toHaveBeenCalled()

    vi.mocked(installUpdate).mockResolvedValue("failed")
    await waitFor(() => expect(screen.getByRole("button", { name: t("update.upgrade") })).toBeEnabled())
    fireEvent.click(screen.getByRole("button", { name: t("update.upgrade") }))
    expect(await screen.findByRole("alert")).toHaveTextContent(t("update.failed"))
    expect(onClose).not.toHaveBeenCalled()
  })

  it("closes on the Android back hook and puts the previous hook back", async () => {
    vi.mocked(checkAppVersionNow).mockResolvedValue({ status: "current" })
    const prev = vi.fn(() => false)
    window.__zwaiAndroidBack = prev
    const onClose = vi.fn()
    const view = render(<VersionCheck onClose={onClose} />)
    await waitFor(() => expect(window.__zwaiAndroidBack).not.toBe(prev))
    expect(window.__zwaiAndroidBack?.()).toBe(true)
    expect(onClose).toHaveBeenCalledOnce()
    expect(prev).not.toHaveBeenCalled()
    view.unmount()
    expect(window.__zwaiAndroidBack).toBe(prev)
  })

  it("opens the check from the menu and closes the menu while it runs", async () => {
    const pending = deferred<VersionCheckResult>()
    vi.mocked(checkAppVersionNow).mockReturnValue(pending.promise)
    render(
      <HostChrome
        hosts={[host()]}
        activeFingerprint="a"
        path="relay"
        onSelect={vi.fn()}
        onAdd={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: t("home.menu") }))
    fireEvent.click(screen.getByRole("menuitem", { name: t("update.check") }))
    expect(screen.queryByRole("menu")).not.toBeInTheDocument()
    expect(screen.getByRole("dialog", { name: t("update.check") })).toBeInTheDocument()
    expect(screen.getByRole("status")).toHaveTextContent(t("update.checking"))
  })
})
