import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { DestRail } from "./dest-rail"
import type { Meta, RemoteStatus } from "@/lib/types"
import { useApp } from "@/store/app"
import { useSettingsSheet } from "@/store/settings-sheet"

const fake = vi.hoisted(() => ({
  status: undefined as RemoteStatus | undefined,
  unavailable: false,
  busy: false,
  refresh: vi.fn(async () => {}),
  perform: vi.fn(async (action: () => Promise<RemoteStatus>) => { await action() }),
  start: vi.fn(),
  cancel: vi.fn(),
  logout: vi.fn(),
  settings: vi.fn(),
  saveSettings: vi.fn(),
  remoteStatus: vi.fn(),
  meta: vi.fn(async () => ({ product_mode: "local" })),
  models: vi.fn(async () => ({ models: [] })),
}))

vi.mock("@/lib/use-remote-status", () => ({
  useDesktopAccount: () => ({
    status: fake.status, unavailable: fake.unavailable, busy: fake.busy,
    refresh: fake.refresh, perform: fake.perform,
  }),
}))
vi.mock("@/lib/api", () => ({
  api: {
    clients: async () => ({ enabled: false, pending: false, tools: [] }),
    startRemoteLogin: fake.start,
    cancelRemoteLogin: fake.cancel,
    logoutRemote: fake.logout,
    settings: fake.settings,
    saveSettings: fake.saveSettings,
    remoteStatus: fake.remoteStatus,
    meta: fake.meta,
    models: fake.models,
  },
}))

const anonymous: RemoteStatus = {
  enabled: false, hub_url: "https://configured.example", auth_mode: "independent",
  has_token: false, online: false, login_available: true,
}
const props = { onSettings: vi.fn(), onToggleTheme: vi.fn(), onToggleLocale: vi.fn() }

function openMenu() {
  const trigger = screen.getByRole("button", { name: "Account" })
  trigger.focus()
  fireEvent.keyDown(trigger, { key: "ArrowDown" })
  return screen.getByRole("menu")
}

function openAccount() {
  const menu = openMenu()
  fireEvent.click(within(menu).getByRole("menuitem", { name: /Sign in|Account details/ }))
  return screen.getByRole("dialog", { name: "Account" })
}

function animateMenuExit() {
  const computedStyle = window.getComputedStyle.bind(window)
  return vi.spyOn(window, "getComputedStyle").mockImplementation((element, pseudo) => {
    const original = computedStyle(element, pseudo)
    if (element.getAttribute("role") !== "menu") return original
    // Radix Presence sees live computed CSS in a browser. jsdom needs a
    // live animation name to retain the closed menu until animationend.
    return new Proxy(original, {
      get(target, key) {
        if (key === "animationName") return element.getAttribute("data-state") === "closed" ? "menu-exit" : "menu-enter"
        const value = Reflect.get(target, key, target)
        return typeof value === "function" ? value.bind(target) : value
      },
    })
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  fake.status = anonymous
  fake.unavailable = false
  fake.busy = false
  fake.start.mockResolvedValue({ ...anonymous, auth_mode: "account", enabled: true, login: { state: "authorization_pending" } })
  fake.cancel.mockResolvedValue({ ...anonymous, login: { state: "cancelled" } })
  fake.logout.mockResolvedValue({ ...anonymous, auth_mode: "account", login: { state: "signed_out" } })
  fake.saveSettings.mockResolvedValue({})
  useApp.setState({ meta: undefined })
  useSettingsSheet.setState({ open: false, section: "general" })
})

describe("desktop account entry", () => {
  it("restores avatar focus on a later Escape after opening Settings from the account dialog", async () => {
    render(<DestRail {...props} />)
    const dialog = openAccount()
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)) })
    fireEvent.click(within(dialog).getByRole("button", { name: "Connection settings" }))
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)) })
    useSettingsSheet.setState({ open: false, section: "general" })
    const menu = openMenu()
    within(menu).getByRole("menuitem", { name: "Sign in" }).focus()
    await act(async () => {
      fireEvent.keyDown(menu, { key: "Escape" })
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(screen.getByRole("button", { name: "Account" })).toHaveFocus()
  })

  it("hands connection settings focus away from a menu that would retain its exit animation", async () => {
    const style = animateMenuExit()
    try {
      render(<DestRail {...props} />)
      const avatar = screen.getByRole("button", { name: "Account" })
      const menu = openMenu()
      fireEvent.click(within(menu).getByRole("menuitem", { name: "Connection settings" }))
      await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)) })
      expect(useSettingsSheet.getState()).toMatchObject({ open: true, section: "remote" })
      expect(menu).not.toBeInTheDocument()
      // Settings owns the next focus target. Restoring the old menu trigger
      // would be an outside-focus event on that newly opened nonmodal sheet.
      expect(avatar).not.toHaveFocus()
    } finally {
      style.mockRestore()
    }
  })

  it("keeps the account dialog open when the selected menu would retain an exit animation", async () => {
    const style = animateMenuExit()
    try {
      render(<DestRail {...props} />)
      const menu = openMenu()
      const item = within(menu).getByRole("menuitem", { name: "Sign in" })
      fireEvent.click(item)
      const dialog = screen.getByRole("dialog", { name: "Account" })
      await act(async () => { await Promise.resolve() })
      const leave = new Event("pointerout", { bubbles: true })
      Object.defineProperties(leave, {
        pointerType: { value: "mouse" },
        relatedTarget: { value: within(dialog).getByRole("button", { name: "Connection settings" }) },
      })
      // A disappearing menu item fires this transition when the dialog covers
      // it. A retained Radix menu then focuses itself and dismisses the dialog.
      fireEvent(item, leave)
      await act(async () => { await Promise.resolve() })
      expect(screen.getByRole("dialog", { name: "Account" })).toBeVisible()
      expect(menu).not.toBeInTheDocument()
    } finally {
      style.mockRestore()
    }
  })

  it("disables account mutations while another client is signing out", () => {
    fake.status = { ...anonymous, enabled: true, auth_mode: "account", has_token: true,
      login: { state: "signing_out" as NonNullable<RemoteStatus["login"]>["state"] } }
    render(<DestRail {...props} />)
    const menu = openMenu()
    expect(within(menu).getByText("Signing out of this PC…")).toBeVisible()
    expect(within(menu).getByRole("menuitem", { name: "Sign out" })).toHaveAttribute("data-disabled")
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Account details" }))
    const panel = screen.getByRole("dialog", { name: "Account" })
    expect(within(panel).getByRole("button", { name: "Sign out" })).toBeDisabled()
    for (const button of within(panel).getAllByRole("button", { name: /Sign in|Reconnect as a new device/ })) {
      expect(button).toBeDisabled()
    }
    expect(fake.start).not.toHaveBeenCalled()
    expect(fake.logout).not.toHaveBeenCalled()
  })

  it("explains an unsupported Gateway logout without reporting that local credentials were cleared", () => {
    fake.status = { ...anonymous, enabled: true, auth_mode: "account", has_token: true,
      login: { state: "error", error_code: "logout_unsupported" } }
    render(<DestRail {...props} />)
    const panel = openAccount()
    expect(within(panel).getByText("This Gateway needs an update before this PC can sign out safely. Your sign-in credential has been retained.")).toBeVisible()
    expect(fake.logout).not.toHaveBeenCalled()
  })

  it("adds an anonymous local avatar after Settings without starting login or fetching an image", () => {
    render(<DestRail {...props} />)
    const rail = screen.getByTestId("dest-rail")
    const avatar = within(rail).getByRole("button", { name: "Account" })
    expect(rail.lastElementChild?.lastElementChild).toContainElement(avatar)
    expect(avatar.querySelector("svg")).toBeInTheDocument()
    expect(avatar.querySelector("img")).toBeNull()
    expect(within(rail).getByRole("button", { name: "Settings" })).toBeInTheDocument()
    expect(within(rail).getByRole("button", { name: "App menu" })).toBeInTheDocument()
    expect(fake.start).not.toHaveBeenCalled()
    expect(fake.logout).not.toHaveBeenCalled()
  })

  it("opens an independent login dialog and only the explicit sign-in enables account mode", async () => {
    render(<DestRail {...props} />)
    const menu = openMenu()
    expect(fake.start).not.toHaveBeenCalled()
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Sign in" }))
    const dialog = screen.getByRole("dialog", { name: "Account" })
    expect(within(dialog).getByText("You can keep using local conversations without signing in.")).toBeVisible()
    expect(useSettingsSheet.getState().open).toBe(false)
    await waitFor(() => expect(fake.start).toHaveBeenCalledWith(false, true))
    expect(fake.perform).toHaveBeenCalledOnce()
  })

  it.each([
    ["nosec_alvaro_0123456789abcdef", " Álvaro García ", "Álvaro García", "Á"],
    ["nosec_名字_2_0123456789abcdef", " ", "名字_2", "名"],
    ["nosec_名字_2_0123456789abcdeg", undefined, "nosec_名字_2_0123456789abcdeg", "N"],
    ["álvaro", undefined, "álvaro", "Á"],
  ] as const)("shows the account label for %s without claiming PC connectivity", (username, display_name, name, initial) => {
    const login = { state: "authorized" as const, username, display_name }
    fake.status = { ...anonymous, enabled: true, auth_mode: "account", has_token: true, login }
    render(<DestRail {...props} />)
    const trigger = screen.getByRole("button", { name: "Account" })
    expect(trigger).toHaveTextContent(initial)
    expect(trigger.querySelector("img")).toBeNull()
    const menu = openMenu()
    expect(within(menu).getByText(name)).toBeVisible()
    expect(within(menu).getByText("Signed in · PC offline")).toBeVisible()
    const dialog = (() => {
      fireEvent.click(within(menu).getByRole("menuitem", { name: "Account details" }))
      return screen.getByRole("dialog", { name: "Account" })
    })()
    expect(within(dialog).getByText("Authorized. Waiting for this PC to connect to the hub.")).toBeVisible()
    expect(within(dialog).getByRole("switch", { name: "This PC's connection" })).toBeChecked()
    expect(within(dialog).getByText(`Account: ${name}`)).toBeVisible()
  })

  it("shows a public avatar and returns to the initial when the image cannot load", () => {
    const picture = "https://images.fofa.info/a.png?sig=a+b&v=1"
    fake.status = {
      ...anonymous, enabled: true, auth_mode: "account", has_token: true,
      login: { state: "authorized", display_name: "Ada", avatar_url: picture },
    }
    render(<DestRail {...props} />)
    const trigger = screen.getByRole("button", { name: "Account" })
    const image = trigger.querySelector("img")
    expect(image).toHaveAttribute("src", picture)
    expect(image).toHaveAttribute("referrerpolicy", "no-referrer")
    expect(trigger).not.toHaveTextContent("A")
    fireEvent.error(image!)
    expect(trigger.querySelector("img")).toBeNull()
    expect(trigger).toHaveTextContent("A")
  })

  it("does not request an avatar address that is not a public image URL", () => {
    fake.status = {
      ...anonymous, enabled: true, auth_mode: "account", has_token: true,
      login: { state: "authorized", display_name: "Ada", avatar_url: "javascript:alert(1)" },
    }
    render(<DestRail {...props} />)
    const trigger = screen.getByRole("button", { name: "Account" })
    expect(trigger.querySelector("img")).toBeNull()
    expect(trigger).toHaveTextContent("A")
  })

  it("says an authorized connection that is off is not waiting, and turning it on keeps the saved remote", async () => {
    const saved = {
      enabled: false, hub_url: "https://configured.example", auth_mode: "account" as const,
      thread_limit: 5, summary_chars: 280, open_turns: 6, event_chars: 4000, watch_events: 80,
      keep_awake: true, display_name: "this-pc",
    }
    fake.status = { ...anonymous, auth_mode: "account", has_token: true, login: { state: "authorized", display_name: "Ada" } }
    fake.settings.mockResolvedValue({ remote: saved })
    fake.saveSettings.mockResolvedValue({})
    fake.remoteStatus.mockResolvedValue({ ...fake.status, enabled: true })
    render(<DestRail {...props} />)
    const menu = openMenu()
    expect(within(menu).getByText("Signed in · PC connection is off")).toBeVisible()
    expect(within(menu).queryByText("Signed in · PC offline")).not.toBeInTheDocument()
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Account details" }))
    const dialog = screen.getByRole("dialog", { name: "Account" })
    expect(within(dialog).getByText("Signed in, but this PC's connection is off, so it is not connecting to the hub.")).toBeVisible()
    expect(within(dialog).queryByText("Authorized. Waiting for this PC to connect to the hub.")).not.toBeInTheDocument()
    const toggle = within(dialog).getByRole("switch", { name: "This PC's connection" })
    expect(toggle).not.toBeChecked()
    fireEvent.click(toggle)
    await waitFor(() => expect(fake.saveSettings).toHaveBeenCalledWith({ remote: { ...saved, enabled: true } }))
    expect(fake.start).not.toHaveBeenCalled()
    expect(fake.remoteStatus).toHaveBeenCalledOnce()
  })

  it("says a recorded connection failure is not still waiting", () => {
    fake.status = {
      ...anonymous, enabled: true, auth_mode: "account", has_token: true, online: false,
      error: "remote: authorized host connection failed",
      login: { state: "authorized", display_name: "Ada" },
    }
    render(<DestRail {...props} />)
    expect(within(openMenu()).getByText("Signed in · PC could not connect")).toBeVisible()
    expect(screen.queryByText("remote: authorized host connection failed")).not.toBeInTheDocument()
  })

  it.each([
    ["authorization_pending", "Awaiting browser authorization", "Complete sign-in and authorize this PC in your browser."],
    ["account_pending", "Waiting for approval", "Waiting for administrator approval. No device is connected yet."],
    ["account_rejected", "Account rejected", "This account was rejected. Ask an administrator to review it."],
    ["access_denied", "Access denied", "This account cannot connect devices. Contact an administrator."],
  ] as const)("keeps %s truthful and preserves public approval details", async (state, summary, detail) => {
    fake.status = { ...anonymous, enabled: true, auth_mode: "account", login: { state, username: "pending-user", user_code: "ABCD-1234", host_fp: "public-fingerprint" } }
    render(<DestRail {...props} />)
    const menu = openMenu()
    expect(within(menu).getByText(summary)).toBeVisible()
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Account details" }))
    const dialog = screen.getByRole("dialog", { name: "Account" })
    expect(within(dialog).getByText(detail)).toBeVisible()
    expect(within(dialog).getByText("ABCD-1234")).toBeVisible()
    expect(within(dialog).getByText("public-fingerprint")).toBeVisible()
    expect(screen.getByRole("button", { name: "Account" })).not.toHaveTextContent("P")
  })

  it("reports unreadable status as unknown and offers a read retry instead of a false signed-out state", async () => {
    fake.unavailable = true
    fake.status = { ...anonymous, auth_mode: "account", has_token: true, login: { state: "authorized", username: "alice" } }
    render(<DestRail {...props} />)
    const menu = openMenu()
    expect(within(menu).getByText("Account status unavailable")).toBeVisible()
    expect(screen.getByRole("button", { name: "Account" })).not.toHaveTextContent("A")
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Refresh account status" }))
    expect(fake.refresh).toHaveBeenCalledOnce()
    expect(fake.start).not.toHaveBeenCalled()
  })

  it("uses real logout, keeps errors visible, and does not clear the confirmed account locally", async () => {
    fake.status = { ...anonymous, enabled: true, auth_mode: "account", has_token: true, login: { state: "authorized", username: "alice" } }
    fake.logout.mockRejectedValue(new Error("private-credential-must-not-render"))
    render(<DestRail {...props} />)
    const dialog = openAccount()
    fireEvent.click(within(dialog).getByRole("button", { name: "Sign out" }))
    await waitFor(() => expect(fake.logout).toHaveBeenCalledOnce())
    expect(fake.cancel).not.toHaveBeenCalled()
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("The account action could not finish. Refresh the status or try again.")
    expect(screen.queryByText("private-credential-must-not-render")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Account" })).toHaveTextContent("A")
  })

  it("preserves explicit new-device confirmation and enables account mode only after confirmation", async () => {
    fake.status = { ...anonymous, enabled: true, auth_mode: "account", login: { state: "error", error_code: "device_conflict" } }
    render(<DestRail {...props} />)
    const dialog = openAccount()
    fireEvent.click(within(dialog).getByRole("button", { name: "Reconnect as a new device" }))
    expect(fake.start).not.toHaveBeenCalled()
    const confirm = screen.getByRole("dialog", { name: "Reconnect as a new device?" })
    fireEvent.click(within(confirm).getByRole("button", { name: "Confirm reconnect" }))
    await waitFor(() => expect(fake.start).toHaveBeenCalledWith(true, true))
  })

  it("closes the keyboard menu with Escape and restores avatar focus", async () => {
    render(<DestRail {...props} />)
    const menu = openMenu()
    await act(async () => {
      fireEvent.keyDown(menu, { key: "Escape" })
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(screen.queryByRole("menu")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Account" })).toHaveFocus()
    expect(fake.start).not.toHaveBeenCalled()
  })

  it("keeps the connection settings link explicit and closes the account dialog", async () => {
    render(<DestRail {...props} />)
    const dialog = openAccount()
    fireEvent.click(within(dialog).getByRole("button", { name: "Connection settings" }))
    expect(useSettingsSheet.getState()).toMatchObject({ open: true, section: "remote" })
    await act(async () => {})
    expect(screen.queryByRole("dialog", { name: "Account" })).not.toBeInTheDocument()
  })

  it("shows a portrait in account details and writes local after three clicks", async () => {
    render(<DestRail {...props} />)
    const dialog = openAccount()
    const portrait = within(dialog).getByTestId("account-portrait")
    expect(portrait.querySelector("svg")).toBeTruthy()
    fireEvent.click(portrait)
    fireEvent.click(portrait)
    expect(fake.saveSettings).not.toHaveBeenCalled()
    fireEvent.click(portrait)
    await waitFor(() => expect(fake.saveSettings).toHaveBeenCalledWith({ product: { mode: "local" } }))
    expect(fake.saveSettings).toHaveBeenCalledTimes(1)
  })

  it("leaves an install that is already local unchanged", () => {
    useApp.setState({ meta: { product_mode: "local" } as Meta })
    render(<DestRail {...props} />)
    const portrait = within(openAccount()).getByTestId("account-portrait")
    fireEvent.click(portrait)
    fireEvent.click(portrait)
    fireEvent.click(portrait)
    expect(fake.saveSettings).not.toHaveBeenCalled()
  })
})
