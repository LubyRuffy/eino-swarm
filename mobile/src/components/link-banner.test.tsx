import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { LinkBanner } from "./link-banner"
import { setLocale, t } from "@/lib/i18n"

describe("LinkBanner", () => {
  it("tells the human the socket is gone and retry does not unlink", () => {
    setLocale("en")
    const onRetry = vi.fn()
    render(<LinkBanner error={t("err.net.closed", { reason: "offline" })} onRetry={onRetry} />)
    expect(screen.getByRole("alert").textContent).toBe(t("err.net.closed", { reason: "offline" }))
    fireEvent.click(screen.getByRole("button", { name: t("scan.retry") }))
    expect(onRetry).toHaveBeenCalledOnce()
  })

  it("hides retry while a reconnect is already in flight", () => {
    setLocale("en")
    render(<LinkBanner reconnecting error={t("err.net.closed", { reason: "offline" })} onRetry={vi.fn()} />)
    expect(screen.getByRole("alert").textContent).toBe(t("home.reconnecting"))
    expect(screen.queryByRole("button", { name: t("scan.retry") })).not.toBeInTheDocument()
  })
})
