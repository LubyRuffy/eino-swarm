import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/app-update", async () => {
  const actual = await vi.importActual<typeof import("@/lib/app-update")>("@/lib/app-update")
  return {
    ...actual,
    checkForAppUpdate: vi.fn(),
    installUpdate: vi.fn(),
    dismissUpdate: vi.fn(),
    listenDownloadProgress: vi.fn(async () => () => undefined),
  }
})

import {
  checkForAppUpdate,
  dismissUpdate,
  installUpdate,
  listenDownloadProgress,
  type DownloadProgress,
  type UpdateOffer,
} from "@/lib/app-update"
import { setLocale, t } from "@/lib/i18n"

import { UpdateNotice } from "./update-notice"

const offer: UpdateOffer = {
  version: "2.4.0",
  pageURL: "https://github.com/LubyRuffy/eino-swarm/releases/tag/v2.4.0",
  apkURL: "https://github.com/LubyRuffy/eino-swarm/releases/download/v2.4.0/zwai-2.4.0-android.apk",
}

describe("UpdateNotice", () => {
  beforeEach(() => {
    setLocale("en")
    vi.mocked(checkForAppUpdate).mockReset()
    vi.mocked(installUpdate).mockReset()
    vi.mocked(dismissUpdate).mockReset()
    vi.mocked(listenDownloadProgress).mockReset()
    vi.mocked(listenDownloadProgress).mockResolvedValue(() => undefined)
  })

  it("stays out of the way when nothing is newer", async () => {
    vi.mocked(checkForAppUpdate).mockResolvedValue(null)
    render(<UpdateNotice />)
    await waitFor(() => expect(checkForAppUpdate).toHaveBeenCalled())
    expect(screen.queryByRole("button", { name: t("update.upgrade") })).not.toBeInTheDocument()
  })

  it("offers the newer version and hands the install to the platform", async () => {
    vi.mocked(checkForAppUpdate).mockResolvedValue(offer)
    vi.mocked(installUpdate).mockResolvedValue("installed")
    render(<UpdateNotice />)
    expect(await screen.findByRole("status")).toHaveTextContent(
      t("update.available", { version: "2.4.0" }),
    )
    fireEvent.click(screen.getByRole("button", { name: t("update.upgrade") }))
    await waitFor(() => expect(installUpdate).toHaveBeenCalledWith(offer))
  })

  it("says when Android still needs install permission", async () => {
    vi.mocked(checkForAppUpdate).mockResolvedValue(offer)
    vi.mocked(installUpdate).mockResolvedValue("permission")
    render(<UpdateNotice />)
    fireEvent.click(await screen.findByRole("button", { name: t("update.upgrade") }))
    expect(await screen.findByRole("status")).toHaveTextContent(t("update.permission"))
  })

  it("does not start a second download while the first is still running", async () => {
    vi.mocked(checkForAppUpdate).mockResolvedValue(offer)
    let finish: (result: "installed") => void = () => undefined
    vi.mocked(installUpdate).mockImplementation(
      () => new Promise((resolve) => {
        finish = resolve
      }),
    )
    render(<UpdateNotice />)
    const button = await screen.findByRole("button", { name: t("update.upgrade") })
    fireEvent.click(button)
    fireEvent.click(button)
    // The listener attaches before the download, so the call is not sync.
    await waitFor(() => expect(installUpdate).toHaveBeenCalledOnce())
    finish("installed")
    await waitFor(() => expect(button).not.toBeDisabled())
  })

  it("shows how much of the package has arrived", async () => {
    vi.mocked(checkForAppUpdate).mockResolvedValue(offer)
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
    render(<UpdateNotice />)
    fireEvent.click(await screen.findByRole("button", { name: t("update.upgrade") }))
    const bar = await screen.findByRole("progressbar")
    expect(bar).toHaveAttribute("aria-valuenow", "40")
    expect(screen.getByRole("status")).toHaveTextContent(t("update.downloadingProgress", { percent: 40 }))
    finish("installed")
    await waitFor(() => expect(screen.queryByRole("progressbar")).not.toBeInTheDocument())
  })

  it("hides that version after Not now", async () => {
    vi.mocked(checkForAppUpdate).mockResolvedValue(offer)
    render(<UpdateNotice />)
    fireEvent.click(await screen.findByRole("button", { name: t("update.later") }))
    expect(dismissUpdate).toHaveBeenCalledWith("2.4.0")
    expect(screen.queryByRole("button", { name: t("update.upgrade") })).not.toBeInTheDocument()
  })
})
