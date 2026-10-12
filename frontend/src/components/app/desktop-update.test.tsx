import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { DesktopUpdateBanner, requestDesktopUpdateCheck } from "./desktop-update"

const desktopUpdate = vi.fn()
const installDesktopUpdate = vi.fn()
let isDesktop = true

vi.mock("@/lib/api", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api")>("@/lib/api")
  return {
    ...actual,
    api: {
      ...actual.api,
      desktopUpdate: (...args: unknown[]) => desktopUpdate(...args),
      installDesktopUpdate: (...args: unknown[]) => installDesktopUpdate(...args),
    },
  }
})

vi.mock("@/lib/shell", () => ({
  desktopShell: () => isDesktop,
}))

describe("DesktopUpdateBanner", () => {
  beforeEach(() => {
    desktopUpdate.mockReset()
    installDesktopUpdate.mockReset()
    isDesktop = true
    localStorage.clear()
  })

  it("asks before replacing the app and installs on confirm", async () => {
    desktopUpdate.mockResolvedValue({
      status: "available",
      offer: { version: "1.2.4" },
    })
    installDesktopUpdate.mockResolvedValue({ status: "restarting" })
    render(<DesktopUpdateBanner />)
    expect(await screen.findByRole("status")).toHaveTextContent("Version 1.2.4 is available. Update?")
    fireEvent.click(screen.getByRole("button", { name: "Update" }))
    await waitFor(() => expect(installDesktopUpdate).toHaveBeenCalledWith("1.2.4"))
    expect(await screen.findByRole("status")).toHaveTextContent("Installing the update")
  })

  it("hides a dismissed version until a manual check", async () => {
    localStorage.setItem("zwai.desktop.update.dismissed", "1.2.4")
    desktopUpdate.mockResolvedValue({
      status: "available",
      offer: { version: "1.2.4" },
    })
    render(<DesktopUpdateBanner />)
    await waitFor(() => expect(desktopUpdate).toHaveBeenCalled())
    expect(screen.queryByTestId("desktop-update")).toBeNull()
    await act(async () => {
      requestDesktopUpdateCheck()
    })
    expect(await screen.findByRole("status")).toHaveTextContent("Version 1.2.4 is available. Update?")
  })

  it("keeps a failed background network request quiet", async () => {
    desktopUpdate.mockRejectedValue(new Error("network unavailable"))
    render(<DesktopUpdateBanner />)
    await act(async () => { await Promise.resolve() })
    expect(screen.queryByTestId("desktop-update")).toBeNull()
  })

  it("explains why this app cannot update after a manual check", async () => {
    desktopUpdate.mockResolvedValue({ status: "unsupported", message: "Use the packaged Mac app" })
    render(<DesktopUpdateBanner />)
    await act(async () => { await Promise.resolve() })
    await act(async () => { requestDesktopUpdateCheck() })
    expect(await screen.findByRole("status")).toHaveTextContent("Use the packaged Mac app")
    fireEvent.click(screen.getByRole("button", { name: "Close update notice" }))
    expect(screen.queryByTestId("desktop-update")).toBeNull()
  })

  it("retries a manual error and closes the recovered current-version notice", async () => {
    desktopUpdate.mockResolvedValueOnce({ status: "current" })
      .mockRejectedValueOnce(new Error("GitHub request timed out"))
      .mockResolvedValueOnce({ status: "current" })
    render(<DesktopUpdateBanner />)
    await act(async () => { await Promise.resolve() })
    await act(async () => { requestDesktopUpdateCheck() })
    expect(await screen.findByRole("status")).toHaveTextContent("GitHub request timed out")
    fireEvent.click(screen.getByRole("button", { name: "Retry update check" }))
    expect(await screen.findByRole("status")).toHaveTextContent("This is the latest version")
    fireEvent.click(screen.getByRole("button", { name: "Close update notice" }))
    expect(screen.queryByTestId("desktop-update")).toBeNull()
  })

  it("does not check for updates in a browser window", async () => {
    isDesktop = false
    render(<DesktopUpdateBanner />)
    await act(async () => { await Promise.resolve() })
    window.dispatchEvent(new Event("online"))
    window.dispatchEvent(new Event("focus"))
    expect(desktopUpdate).not.toHaveBeenCalled()
    expect(screen.queryByTestId("desktop-update")).toBeNull()
  })
})
