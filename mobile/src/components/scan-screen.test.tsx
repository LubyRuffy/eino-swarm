import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ScanScreen } from "./scan-screen"
import { encodeOffer } from "@/lib/offer"

vi.mock("@/lib/scan", () => ({
  scanPairlinkURI: vi.fn(),
}))

function sampleURI(): string {
  return encodeOffer({
    hubURL: "http://127.0.0.1:7780",
    code: "ScanCode01",
    hostPub: new Uint8Array(32).fill(1),
    lan: [],
  })
}

describe("ScanScreen", () => {
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
})
