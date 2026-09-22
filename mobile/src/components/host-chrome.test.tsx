import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { HostChrome } from "./host-chrome"
import { setLocale, t } from "@/lib/i18n"
import type { SavedLink } from "@/lib/store"

function host(fp: string, label: string): SavedLink {
  return {
    hubURL: "https://hub.example.test",
    ticket: "t",
    hostPub: "p",
    sessionID: "s",
    fingerprint: fp,
    label,
  }
}

describe("HostChrome", () => {
  it("paints a tab per bound PC and keeps Add beside the tabs, not as the page", () => {
    setLocale("en")
    const onSelect = vi.fn()
    const onAdd = vi.fn()
    render(
      <HostChrome
        hosts={[
          host("a", "desk-one"),
          host("b", "lab"),
        ]}
        activeFingerprint="a"
        path="relay"
        onSelect={onSelect}
        onAdd={onAdd}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getByRole("heading", { name: t("home.app") })).toBeInTheDocument()
    expect(screen.getByRole("tablist", { name: t("home.hosts") })).toBeInTheDocument()
    expect(screen.getByRole("tab", { name: "desk-one" })).toHaveAttribute(
      "aria-selected",
      "true",
    )
    expect(screen.getByRole("tab", { name: "lab" })).toHaveAttribute(
      "aria-selected",
      "false",
    )
    const pip = screen.getByLabelText("path=relay")
    expect(pip.className).toContain("bg-[hsl(var(--online))]")
    expect(pip.className).not.toContain("primary-foreground")
    expect(pip.className).not.toContain("muted-foreground")
    expect(pip).toHaveAttribute("title", t("home.relay"))
    expect(screen.getByRole("tab", { name: "lab" }).querySelector("[aria-label^='path=']")).toBeNull()
    expect(screen.queryByText(t("scan.title"))).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("tab", { name: "lab" }))
    expect(onSelect).toHaveBeenCalledWith("b")
    fireEvent.click(screen.getByRole("button", { name: t("home.addHost") }))
    expect(onAdd).toHaveBeenCalledOnce()
  })

  it("hides unlink in the side menu so it does not compete with the inbox", () => {
    setLocale("en")
    const onUnlink = vi.fn()
    const onAdd = vi.fn()
    render(
      <HostChrome
        hosts={[host("a", "desk-one")]}
        activeFingerprint="a"
        path="direct"
        onSelect={vi.fn()}
        onAdd={onAdd}
        onUnlink={onUnlink}
      />,
    )
    const pip = screen.getByLabelText("path=direct")
    expect(pip.className).toContain("bg-[hsl(var(--online))]")
    expect(pip).toHaveAttribute("title", t("home.direct"))
    expect(screen.queryByRole("button", { name: t("home.unlink") })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: t("home.menu") }))
    fireEvent.click(screen.getByRole("menuitem", { name: t("home.addHost") }))
    expect(onAdd).toHaveBeenCalledOnce()
    fireEvent.click(screen.getByRole("button", { name: t("home.menu") }))
    fireEvent.click(screen.getByRole("menuitem", { name: t("home.unlink") }))
    expect(onUnlink).toHaveBeenCalledOnce()
  })

  it("paints a down PC gray, not the online green", () => {
    setLocale("en")
    render(
      <HostChrome
        hosts={[host("a", "desk-one")]}
        activeFingerprint="a"
        path="relay"
        connected={false}
        onSelect={vi.fn()}
        onAdd={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    const pip = screen.getByLabelText("path=offline")
    expect(pip.className).toContain("bg-muted-foreground")
    expect(pip.className).not.toContain("--online")
    expect(pip).toHaveAttribute("title", t("home.offline"))
  })
})
