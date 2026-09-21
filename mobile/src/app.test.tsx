import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { openSaved } from "@/lib/client"
import { t } from "@/lib/i18n"
import { saveLink } from "@/lib/store"

vi.mock("@/lib/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/client")>()
  return { ...actual, openSaved: vi.fn() }
})

import { App } from "./app"

function seedLink() {
  saveLink({
    hubURL: "http://127.0.0.1:9",
    ticket: "tick",
    hostPub: "pub",
    sessionID: "sid",
    fingerprint: "fp",
  })
}

describe("App boot chrome", () => {
  afterEach(() => {
    cleanup()
    vi.mocked(openSaved).mockReset()
  })

  it("shows the scan form only when this phone has never bound", () => {
    render(<App />)
    expect(screen.getByRole("button", { name: t("scan.camera") })).toBeInTheDocument()
    expect(screen.queryByRole("tablist")).not.toBeInTheDocument()
  })

  it("paints host tabs and a connecting inbox when a ticket is saved", () => {
    seedLink()
    vi.mocked(openSaved).mockReturnValue(new Promise(() => undefined))
    render(<App />)
    expect(screen.getByRole("tablist", { name: t("home.hosts") })).toBeInTheDocument()
    expect(screen.getByRole("status")).toHaveAttribute("aria-busy", "true")
    expect(screen.getByText(t("scan.connecting"))).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: t("scan.title") })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: t("scan.camera") })).not.toBeInTheDocument()
  })

  it("stays off the scan form after a failed restore so a bound phone is not mistaken for unbound", async () => {
    seedLink()
    vi.mocked(openSaved).mockRejectedValue(new Error("offline"))
    render(<App />)
    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toBe(t("err.reconnect"))
    })
    expect(screen.getByRole("tablist")).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: t("scan.title") })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: t("scan.retry") }))
    await waitFor(() => {
      expect(vi.mocked(openSaved).mock.calls.length).toBeGreaterThanOrEqual(2)
    })
  })

  it("returns to scan after unlink from a failed restore", async () => {
    seedLink()
    vi.mocked(openSaved).mockRejectedValue(new Error("offline"))
    render(<App />)
    await waitFor(() => {
      expect(screen.getByRole("button", { name: t("home.menu") })).toBeEnabled()
    })
    fireEvent.click(screen.getByRole("button", { name: t("home.menu") }))
    fireEvent.click(screen.getByRole("menuitem", { name: t("home.unlink") }))
    expect(screen.getByRole("button", { name: t("scan.camera") })).toBeInTheDocument()
    expect(screen.queryByRole("tablist")).not.toBeInTheDocument()
  })

  it("opens add as a side sheet over the inbox", async () => {
    seedLink()
    vi.mocked(openSaved).mockReturnValue(new Promise(() => undefined))
    render(<App />)
    fireEvent.click(screen.getByRole("button", { name: t("home.addHost") }))
    expect(screen.getByRole("dialog", { name: t("home.addHost") })).toBeInTheDocument()
    expect(screen.getByRole("tablist")).toBeInTheDocument()
    expect(screen.getByText(t("scan.connecting"))).toBeInTheDocument()
    expect(screen.getByRole("button", { name: t("scan.camera") })).toBeEnabled()
  })

  it("keeps a restore error on the inbox when Add a PC is open", async () => {
    seedLink()
    vi.mocked(openSaved).mockRejectedValue(new Error("offline"))
    render(<App />)
    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toBe(t("err.reconnect"))
    })
    fireEvent.click(screen.getByRole("button", { name: t("home.addHost") }))
    const dialog = screen.getByRole("dialog", { name: t("home.addHost") })
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: t("scan.camera") })).toBeEnabled()
    expect(screen.getByRole("alert").textContent).toBe(t("err.reconnect"))
  })
})
