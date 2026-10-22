import { fireEvent, render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { HostChrome } from "./host-chrome"
import { setLocale } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import type { SavedLink } from "@/lib/store"

const host: SavedLink = { hubURL: "https://hub.example.test", ticket: "t", hostPub: "p", sessionID: "s", fingerprint: "desk", label: "Office PC" }
const link: RemoteLink = { path: "relay", alive: () => true, close: vi.fn(), announceDevice: vi.fn(), rpc: vi.fn(async () => ({ v: 1, id: "r", ok: true, timeout_settings: { agent_timeout_seconds: 600, providers: [{ id: "primary", label: "Primary", timeout_seconds: 300, first_byte_timeout_seconds: 30, effective_first_byte_timeout_seconds: 30 }] } })) }
function props() { return { hosts: [host], activeFingerprint: "desk", path: "relay", onSelect: vi.fn(), onAdd: vi.fn(), onNewChat: vi.fn(), onUnlink: vi.fn(), pcLink: link } }

describe("bound PC timeout entry", () => {
  it("opens actual PC settings separately from phone-direct Models", async () => {
    setLocale("en")
    render(<HostChrome {...props()} />)
    fireEvent.click(screen.getByRole("button", { name: "Menu" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "PC timeouts" }))
    expect(await screen.findByLabelText("Sub-agent lifetime (seconds)")).toHaveValue(600)
    expect(screen.getByLabelText("First byte (seconds) · Primary")).toHaveValue(30)
    expect(screen.getByLabelText("Stream idle (seconds) · Primary")).toHaveValue(300)
    expect(screen.getByRole("heading", { name: "PC timeouts" })).toBeInTheDocument()
    expect(within(screen.getByRole("dialog")).getByText("Office PC")).toBeInTheDocument()
  })

  it("does not expose PC settings in the direct-chat tab", () => {
    setLocale("en")
    render(<HostChrome {...props()} chatSelected />)
    fireEvent.click(screen.getByRole("button", { name: "Menu" }))
    expect(screen.queryByRole("menuitem", { name: "PC timeouts" })).not.toBeInTheDocument()
  })
})
