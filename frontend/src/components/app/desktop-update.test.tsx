import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { DesktopUpdateBanner, requestDesktopUpdateCheck } from "./desktop-update"

const desktopUpdate = vi.fn()
const installDesktopUpdate = vi.fn()

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
  desktopShell: () => true,
}))

describe("DesktopUpdateBanner", () => {
  beforeEach(() => {
    desktopUpdate.mockReset()
    installDesktopUpdate.mockReset()
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
})
