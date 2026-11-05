import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { AddHostSheet } from "./add-host-sheet"
import { PhoneAccountContext } from "@/components/gateway-account-shell"
import { setLocale, t } from "@/lib/i18n"
import { encodeOffer } from "@/lib/offer"

describe("AddHostSheet", () => {
  it("keeps the bind form in a dialog so it cannot replace the inbox", () => {
    setLocale("en")
    const onURI = vi.fn()
    const onClose = vi.fn()
    render(<AddHostSheet onURI={onURI} onClose={onClose} />)
    expect(screen.getByRole("dialog", { name: t("home.addHost") })).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: t("scan.title") })).not.toBeInTheDocument()
    const uri = encodeOffer({
      hubURL: "http://127.0.0.1:7780",
      code: "ScanCode01",
      hostPub: new Uint8Array(32).fill(1),
      lan: [],
    })
    fireEvent.change(screen.getByLabelText(t("scan.uri")), { target: { value: uri } })
    fireEvent.click(screen.getByRole("button", { name: t("scan.paste") }))
    expect(onURI).toHaveBeenCalledWith(uri)
    fireEvent.click(screen.getAllByRole("button", { name: t("home.close") })[0])
    expect(onClose).toHaveBeenCalled()
  })

  const hosts = [
    { id: "pc-online", name: "Work PC", hostname: "work", version: "1", online: true, enabled: true },
    { id: "pc-offline", name: "Home PC", hostname: "home", version: "1", online: false, enabled: true },
  ]

  it("lists the signed-in account's computers before pairing a new one", () => {
    setLocale("en")
    const connect = vi.fn(async () => undefined)
    render(
      <PhoneAccountContext.Provider value={{ providers: [], pcs: { hosts, connect } }}>
        <AddHostSheet onURI={vi.fn()} onClose={vi.fn()} />
      </PhoneAccountContext.Provider>,
    )
    const section = screen.getByRole("region", { name: t("account.addPcs") })
    const pairing = screen.getByLabelText(t("scan.uri"))
    // Existing computers come first; pairing stays available below them.
    expect(section.compareDocumentPosition(pairing) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByRole("button", { name: "Connect Home PC" })).toBeDisabled()
    fireEvent.click(screen.getByRole("button", { name: "Connect Work PC" }))
    expect(connect).toHaveBeenCalledWith("pc-online")
  })

  it("shows only the pairing form when signed out or the account has no computers", () => {
    setLocale("en")
    for (const pcs of [undefined, { hosts: [], connect: vi.fn(async () => undefined) }]) {
      const { unmount } = render(
        <PhoneAccountContext.Provider value={{ providers: [], pcs }}>
          <AddHostSheet onURI={vi.fn()} onClose={vi.fn()} />
        </PhoneAccountContext.Provider>,
      )
      expect(screen.queryByRole("region", { name: t("account.addPcs") })).not.toBeInTheDocument()
      expect(screen.getByLabelText(t("scan.uri"))).toBeInTheDocument()
      unmount()
    }
  })
})
