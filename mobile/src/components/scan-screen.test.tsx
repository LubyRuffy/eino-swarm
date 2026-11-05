import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { PhoneAccountContext } from "@/components/gateway-account-shell"
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

describe("ScanScreen on a phone that can sign in", () => {
  beforeEach(() => setLocale("en"))

  const withAccount = (openAccount: () => void, onURI = vi.fn(), error?: string) => render(
    <PhoneAccountContext.Provider value={{ providers: [], openAccount }}>
      <ScanScreen onURI={onURI} error={error} onAddModel={vi.fn()} />
    </PhoneAccountContext.Provider>,
  )

  it("leads with the account and folds pairing and own-model entry behind Other ways", () => {
    const openAccount = vi.fn()
    withAccount(openAccount)
    expect(screen.getByRole("heading", { name: t("welcome.title") })).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: t("account.login") }))
    expect(openAccount).toHaveBeenCalledOnce()
    // The hidden controls are absent, not merely invisible, so a screen reader
    // and the keyboard order see the account first.
    for (const name of [t("scan.camera"), t("scan.paste"), t("chat.connect")]) {
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument()
    }
    const toggle = screen.getByRole("button", { name: t("welcome.other") })
    expect(toggle).toHaveAttribute("aria-expanded", "false")
    fireEvent.click(toggle)
    expect(toggle).toHaveAttribute("aria-expanded", "true")
    for (const name of [t("scan.camera"), t("scan.paste"), t("chat.connect")]) {
      expect(screen.getByRole("button", { name })).toBeInTheDocument()
    }
    fireEvent.click(toggle)
    expect(screen.queryByRole("button", { name: t("scan.camera") })).not.toBeInTheDocument()
  })

  it("still binds a pasted URI from the folded section", () => {
    const onURI = vi.fn()
    withAccount(vi.fn(), onURI)
    fireEvent.click(screen.getByRole("button", { name: t("welcome.other") }))
    const uri = sampleURI()
    fireEvent.change(screen.getByLabelText(t("scan.uri")), { target: { value: uri } })
    fireEvent.click(screen.getByRole("button", { name: t("scan.paste") }))
    expect(onURI).toHaveBeenCalledWith(uri)
  })

  it("opens the folded section when pairing reports an error so it is never hidden", () => {
    withAccount(vi.fn(), vi.fn(), "pairing failed")
    expect(screen.getByRole("alert")).toHaveTextContent("pairing failed")
    expect(screen.getByRole("button", { name: t("welcome.other") })).toHaveAttribute("aria-expanded", "true")
    expect(screen.getByLabelText(t("scan.uri"))).toBeInTheDocument()
  })

  it("keeps the embedded Add a PC form unfolded", () => {
    render(
      <PhoneAccountContext.Provider value={{ providers: [], openAccount: vi.fn() }}>
        <ScanScreen embedded onURI={vi.fn()} />
      </PhoneAccountContext.Provider>,
    )
    expect(screen.queryByRole("button", { name: t("welcome.other") })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: t("scan.camera") })).toBeInTheDocument()
  })
})
