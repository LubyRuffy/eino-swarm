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
  it("paints a tab per bound PC and keeps the tabs on the page, not a scan form", () => {
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
        onNewChat={vi.fn()}
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
    // Add a PC lives on the menu at the other end of the row. A second
    // control for it in the corner spent the one reachable spot on a thing
    // you do once per PC.
    expect(screen.queryByRole("button", { name: t("home.addHost") })).not.toBeInTheDocument()
    expect(onAdd).not.toHaveBeenCalled()
  })

  it("puts New chat in the corner the plus used to hold", () => {
    setLocale("en")
    const onNewChat = vi.fn()
    render(
      <HostChrome
        hosts={[host("a", "desk-one")]}
        activeFingerprint="a"
        path="relay"
        onSelect={vi.fn()}
        onAdd={vi.fn()}
        onNewChat={onNewChat}
        onUnlink={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByTestId("new-chat-top"))
    expect(onNewChat).toHaveBeenCalledOnce()
    expect(screen.getByTestId("new-chat-top")).toHaveAttribute("aria-label", t("home.newChat"))
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
        onNewChat={vi.fn()}
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

  // The tabs share one row with the menu and Add. Past three or four PCs the
  // row scrolls, and the chip naming the PC you are on can be the one off it.
  it("scrolls the PC you switched to back into the row", () => {
    setLocale("en")
    const seen: Element[] = []
    const spy = vi.fn(function (this: Element) {
      seen.push(this)
    })
    Object.defineProperty(Element.prototype, "scrollIntoView", {
      value: spy,
      configurable: true,
      writable: true,
    })
    const hosts = [host("a", "desk-one"), host("b", "lab"), host("c", "shed")]
    const view = render(
      <HostChrome
        hosts={hosts}
        activeFingerprint="a"
        path="relay"
        onSelect={vi.fn()}
        onAdd={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(seen.at(-1)).toBe(screen.getByRole("tab", { name: "desk-one" }))

    view.rerender(
      <HostChrome
        hosts={hosts}
        activeFingerprint="c"
        path="relay"
        onSelect={vi.fn()}
        onAdd={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(seen.at(-1)).toBe(screen.getByRole("tab", { name: "shed" }))
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
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    const pip = screen.getByLabelText("path=offline")
    expect(pip.className).toContain("bg-muted-foreground")
    expect(pip.className).not.toContain("--online")
    expect(pip).toHaveAttribute("title", t("home.offline"))
  })

  it("adds a chat tab only after a model exists, and does not mark a PC selected under it", () => {
    setLocale("en")
    const onSelectChat = vi.fn()
    const onModels = vi.fn()
    const { rerender } = render(
      <HostChrome
        hosts={[host("a", "desk-one")]}
        activeFingerprint="a"
        path="relay"
        onSelect={vi.fn()}
        onAdd={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.queryByRole("tab", { name: t("chat.tab") })).not.toBeInTheDocument()
    rerender(
      <HostChrome
        hosts={[host("a", "desk-one")]}
        activeFingerprint="a"
        path="relay"
        showChat
        chatSelected
        onSelect={vi.fn()}
        onSelectChat={onSelectChat}
        onAdd={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
        onModels={onModels}
      />,
    )
    expect(screen.getByRole("tablist", { name: t("home.nav") })).toBeInTheDocument()
    expect(screen.getByRole("tab", { name: t("chat.tab") })).toHaveAttribute("aria-selected", "true")
    expect(screen.getByRole("tab", { name: "desk-one" })).toHaveAttribute("aria-selected", "false")
    fireEvent.click(screen.getByRole("tab", { name: t("chat.tab") }))
    expect(onSelectChat).toHaveBeenCalledOnce()
    fireEvent.click(screen.getByRole("button", { name: t("home.menu") }))
    fireEvent.click(screen.getByRole("menuitem", { name: t("chat.models") }))
    expect(onModels).toHaveBeenCalledOnce()
  })
})
