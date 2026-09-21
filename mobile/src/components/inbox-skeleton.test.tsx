import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { InboxSkeleton } from "./inbox-skeleton"
import { setLocale, t } from "@/lib/i18n"

describe("InboxSkeleton", () => {
  it("shows a busy inbox instead of the scan form while the saved link opens", () => {
    setLocale("en")
    render(<InboxSkeleton pending />)
    expect(screen.getByRole("status")).toHaveAttribute("aria-busy", "true")
    expect(screen.getByText(t("scan.connecting"))).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: t("scan.camera") })).not.toBeInTheDocument()
  })

  it("keeps retry on the inbox after a failed restore", () => {
    setLocale("en")
    const onRetry = vi.fn()
    render(<InboxSkeleton error={t("err.reconnect")} onRetry={onRetry} />)
    expect(screen.queryByRole("status")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: t("scan.retry") }))
    expect(onRetry).toHaveBeenCalledOnce()
  })
})
