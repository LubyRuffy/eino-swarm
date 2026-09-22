import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { ScanViewfinder } from "./scan-viewfinder"
import { encodeOffer } from "@/lib/offer"
import { playScanChime } from "@/lib/scan-chime"
import { ScanCameraError, startLiveScan } from "@/lib/scan"
import { setLocale, t } from "@/lib/i18n"

vi.mock("@/lib/scan-chime", () => ({
  playScanChime: vi.fn(),
  primeScanChime: vi.fn(),
}))

vi.mock("@/lib/scan", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/scan")>()
  return { ...actual, startLiveScan: vi.fn() }
})

function offerURI(): string {
  return encodeOffer({
    hubURL: "http://127.0.0.1:7780",
    code: "ScanCode01",
    hostPub: new Uint8Array(32).fill(1),
    lan: [],
  })
}

describe("ScanViewfinder", () => {
  beforeEach(() => {
    setLocale("en")
    vi.mocked(startLiveScan).mockReset()
    vi.mocked(playScanChime).mockReset()
    vi.mocked(startLiveScan).mockResolvedValue({ stop: vi.fn() })
  })

  it("shows a frame and a beam over the live preview", () => {
    const { container } = render(
      <ScanViewfinder onClose={vi.fn()} onURI={vi.fn()} onError={vi.fn()} />,
    )
    expect(screen.getByRole("dialog", { name: t("scan.camera") })).toBeInTheDocument()
    expect(screen.getByText(t("scan.aim"))).toBeInTheDocument()
    expect(container.querySelector("video")).toBeTruthy()
    expect(container.querySelector(".scan-frame")).toBeTruthy()
    expect(container.querySelectorAll(".scan-corner")).toHaveLength(4)
    expect(container.querySelector(".scan-beam")).toBeTruthy()
  })

  it("closes from the button and from Escape", () => {
    const onClose = vi.fn()
    render(<ScanViewfinder onClose={onClose} onURI={vi.fn()} onError={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: t("scan.close") }))
    expect(onClose).toHaveBeenCalledTimes(1)
    fireEvent.keyDown(window, { key: "Escape" })
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it("ignores a QR that is not a pairing offer", async () => {
    const onURI = vi.fn()
    const stop = vi.fn()
    vi.mocked(startLiveScan).mockImplementation(async (opts) => {
      opts.onText("not-an-offer")
      return { stop }
    })
    render(<ScanViewfinder onClose={vi.fn()} onURI={onURI} onError={vi.fn()} />)
    await waitFor(() => expect(startLiveScan).toHaveBeenCalled())
    expect(onURI).not.toHaveBeenCalled()
    expect(playScanChime).not.toHaveBeenCalled()
    expect(screen.getByRole("dialog")).toBeInTheDocument()
  })

  it("chimes once and binds when the frame reads a pairing QR", async () => {
    const uri = offerURI()
    const onURI = vi.fn()
    const stop = vi.fn()
    vi.mocked(startLiveScan).mockImplementation(async (opts) => {
      opts.onText(uri)
      opts.onText(uri)
      return { stop }
    })
    render(<ScanViewfinder onClose={vi.fn()} onURI={onURI} onError={vi.fn()} />)
    await waitFor(() => expect(onURI).toHaveBeenCalledWith(uri))
    expect(onURI).toHaveBeenCalledTimes(1)
    expect(playScanChime).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(stop).toHaveBeenCalled())
  })

  it("surfaces a denied camera on the form", async () => {
    const onError = vi.fn()
    vi.mocked(startLiveScan).mockRejectedValue(new ScanCameraError("denied"))
    render(<ScanViewfinder onClose={vi.fn()} onURI={vi.fn()} onError={onError} />)
    await waitFor(() => expect(onError).toHaveBeenCalledWith(t("scan.cameraDenied")))
    expect(playScanChime).not.toHaveBeenCalled()
  })
})
