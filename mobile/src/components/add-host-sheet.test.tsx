import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { AddHostSheet } from "./add-host-sheet"
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
})
