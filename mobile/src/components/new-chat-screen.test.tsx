import { fireEvent, render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { NewChatScreen } from "./new-chat-screen"
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

const hosts = [host("a", "desk-one"), host("b", "lab")]

describe("NewChatScreen", () => {
  it("shows which PC and which project the conversation lands in", () => {
    setLocale("en")
    const onStart = vi.fn()
    render(
      <NewChatScreen
        hosts={hosts}
        activeFingerprint="a"
        projects={[
          { id: "p1", name: "work" },
          { id: "p2", name: "notes" },
        ]}
        onSelectHost={vi.fn()}
        onBack={vi.fn()}
        onStart={onStart}
      />,
    )
    const pcs = screen.getByRole("radiogroup", { name: t("home.hosts") })
    expect(within(pcs).getByRole("radio", { name: "desk-one" })).toBeChecked()
    expect(within(pcs).getByRole("radio", { name: "lab" })).not.toBeChecked()

    const projects = screen.getByRole("radiogroup", { name: t("home.project") })
    expect(within(projects).getByRole("radio", { name: t("home.defaultProject") })).toBeChecked()
    fireEvent.click(within(projects).getByRole("radio", { name: "notes" }))

    fireEvent.change(screen.getByLabelText(t("home.newMessage")), { target: { value: "go" } })
    fireEvent.click(screen.getByRole("button", { name: t("home.start") }))
    expect(onStart).toHaveBeenCalledWith("go", "p2")
  })

  // Project ids belong to one PC. Keeping the pick across a switch would
  // start the conversation in a project the new PC never heard of.
  it("opens already on the project the inbox row named", () => {
    setLocale("en")
    render(
      <NewChatScreen
        hosts={hosts}
        activeFingerprint="a"
        projects={[
          { id: "p1", name: "work" },
          { id: "p2", name: "notes" },
        ]}
        initialProject="p2"
        onSelectHost={vi.fn()}
        onBack={vi.fn()}
        onStart={vi.fn()}
      />,
    )
    expect(screen.getByRole("radio", { name: "notes" })).toBeChecked()
    expect(screen.getByRole("radio", { name: t("home.defaultProject") })).not.toBeChecked()
  })

  it("ignores a project id this PC does not have", () => {
    setLocale("en")
    render(
      <NewChatScreen
        hosts={hosts}
        activeFingerprint="a"
        projects={[{ id: "p1", name: "work" }]}
        initialProject="gone"
        onSelectHost={vi.fn()}
        onBack={vi.fn()}
        onStart={vi.fn()}
      />,
    )
    expect(screen.getByRole("radio", { name: t("home.defaultProject") })).toBeChecked()
  })

  it("re-picks the default project when the PC changes", () => {
    setLocale("en")
    const onSelectHost = vi.fn()
    const onStart = vi.fn()
    render(
      <NewChatScreen
        hosts={hosts}
        activeFingerprint="a"
        projects={[{ id: "p1", name: "work" }]}
        onSelectHost={onSelectHost}
        onBack={vi.fn()}
        onStart={onStart}
      />,
    )
    fireEvent.click(screen.getByRole("radio", { name: "work" }))
    fireEvent.click(screen.getByRole("radio", { name: "lab" }))
    expect(onSelectHost).toHaveBeenCalledWith("b")
    expect(screen.getByRole("radio", { name: t("home.defaultProject") })).toBeChecked()

    fireEvent.click(screen.getByRole("radio", { name: "desk-one" }))
    expect(onSelectHost).toHaveBeenCalledOnce()

    fireEvent.change(screen.getByLabelText(t("home.newMessage")), { target: { value: "go" } })
    fireEvent.click(screen.getByRole("button", { name: t("home.start") }))
    expect(onStart).toHaveBeenCalledWith("go", "")
  })

  it("says the PC is unreachable rather than taking a message it cannot send", () => {
    setLocale("en")
    render(
      <NewChatScreen
        hosts={hosts}
        activeFingerprint="a"
        projects={[]}
        connected={false}
        onSelectHost={vi.fn()}
        onBack={vi.fn()}
        onStart={vi.fn()}
      />,
    )
    expect(screen.getByText(t("compose.offline"))).toBeInTheDocument()
    expect(screen.getByLabelText(t("home.newMessage"))).toBeDisabled()
  })

  // A switch to a PC that is still opening its socket is not a failure.
  it("says it is connecting instead of calling the PC unreachable", () => {
    setLocale("en")
    render(
      <NewChatScreen
        hosts={hosts}
        activeFingerprint="b"
        projects={[]}
        connected={false}
        connecting
        onSelectHost={vi.fn()}
        onBack={vi.fn()}
        onStart={vi.fn()}
      />,
    )
    expect(screen.queryByText(t("compose.offline"))).not.toBeInTheDocument()
    expect(screen.getByText(t("scan.connecting"))).toBeInTheDocument()
  })

  it("goes back without starting anything", () => {
    setLocale("en")
    const onBack = vi.fn()
    render(
      <NewChatScreen
        hosts={hosts}
        activeFingerprint="a"
        projects={[]}
        onSelectHost={vi.fn()}
        onBack={onBack}
        onStart={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: t("thread.back") }))
    expect(onBack).toHaveBeenCalledOnce()
  })
})
