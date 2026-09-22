import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { ScanScreen } from "./scan-screen"
import { encodeOffer } from "@/lib/offer"
import { primeScanChime } from "@/lib/scan-chime"
import { ScanCameraError, startLiveScan } from "@/lib/scan"
import { setLocale, t } from "@/lib/i18n"

vi.mock("@/lib/scan-chime", () => ({
  primeScanChime: vi.fn(),
  playScanChime: vi.fn(),
}))

vi.mock("@/lib/scan", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/scan")>()
  return { ...actual, startLiveScan: vi.fn() }
})

function sampleURI(): string {
  return encodeOffer({
    hubURL: "http://127.0.0.1:7780",
    code: "ScanCode01",
    hostPub: new Uint8Array(32).fill(1),
    lan: [],
  })
}

describe("ScanScreen", () => {
  beforeEach(() => {
    setLocale("en")
    vi.mocked(primeScanChime).mockReset()
    vi.mocked(startLiveScan).mockReset()
    vi.mocked(startLiveScan).mockResolvedValue({ stop: vi.fn() })
  })

  it("binds a pasted pairlink URI", () => {
    const onURI = vi.fn()
    render(<ScanScreen onURI={onURI} />)
    expect(screen.getByRole("button", { name: "Scan QR" })).toBeInTheDocument()
    const uri = sampleURI()
    fireEvent.change(screen.getByLabelText("Pairing URI"), {
      target: { value: uri },
    })
    fireEvent.click(screen.getByRole("button", { name: "Paste and bind" }))
    expect(onURI).toHaveBeenCalledWith(uri)
  })

  it("rejects junk paste", () => {
    const onURI = vi.fn()
    render(<ScanScreen onURI={onURI} />)
    fireEvent.change(screen.getByLabelText("Pairing URI"), {
      target: { value: "https://example.test/nope" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Paste and bind" }))
    expect(onURI).not.toHaveBeenCalled()
    expect(screen.getByRole("alert").textContent).toMatch(/pairlink/)
  })

  it("opens a live viewfinder instead of a photo shutter", async () => {
    render(<ScanScreen onURI={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: t("scan.camera") }))
    expect(primeScanChime).toHaveBeenCalled()
    expect(screen.getByRole("dialog", { name: t("scan.camera") })).toBeInTheDocument()
    expect(screen.getByText(t("scan.aim"))).toBeInTheDocument()
    await waitFor(() => expect(startLiveScan).toHaveBeenCalled())
    fireEvent.click(screen.getByRole("button", { name: t("scan.close") }))
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
  })

  it("binds the URI the viewfinder reads", async () => {
    const uri = sampleURI()
    const onURI = vi.fn()
    vi.mocked(startLiveScan).mockImplementation(async (opts) => {
      opts.onText(uri)
      return { stop: vi.fn() }
    })
    render(<ScanScreen onURI={onURI} />)
    fireEvent.click(screen.getByRole("button", { name: t("scan.camera") }))
    await waitFor(() => expect(onURI).toHaveBeenCalledWith(uri))
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    expect(screen.getByLabelText(t("scan.uri"))).toHaveValue(uri)
  })

  it("returns to paste when the camera is denied", async () => {
    vi.mocked(startLiveScan).mockRejectedValue(new ScanCameraError("denied"))
    render(<ScanScreen onURI={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: t("scan.camera") }))
    await waitFor(() => {
      expect(screen.getByRole("alert")).toHaveTextContent(t("scan.cameraDenied"))
    })
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    expect(screen.getByLabelText(t("scan.uri"))).toBeEnabled()
  })
})
