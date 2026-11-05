import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { App } from "@/app"
import type { useGatewayAccount } from "@/lib/use-gateway-account"
import { setLocale, t } from "@/lib/i18n"

const mocks = vi.hoisted(() => ({ account: {} as ReturnType<typeof useGatewayAccount>, discover: vi.fn() }))
vi.mock("@/lib/use-gateway-account", () => ({ useGatewayAccount: () => mocks.account }))
vi.mock("@/lib/openai-client", async (original) => ({ ...await original<typeof import("@/lib/openai-client")>(), discoverModels: mocks.discover }))

const session = { origin: "https://gateway.invalid", token: "agm_" + "a".repeat(64), user: { id: "owner", username: "User", approval_status: "approved" }, device: { id: "phone", expires_at: "2099-01-01T00:00:00Z" } }
const bootstrap = { user: session.user, device: session.device, models: { source_id: "gateway:owner", source_kind: "gateway" as const, label: "Team", execution: "phone" as const, api_base_url: "https://gateway.invalid/v1", catalog_url: "https://gateway.invalid/v1/models", api_access_enabled: true }, pairlink: { enabled: true, hub_url: session.origin, redeem_url: session.origin + "/pairlink/v1/pairings/redeem", websocket_url: "wss://gateway.invalid/pairlink/v1/ws", info_url: session.origin + "/pairlink/v1/info" }, heartbeat_interval: 30, presence_ttl: 90 }
const noModels = { ...bootstrap, models: { ...bootstrap.models, api_access_enabled: false } }
const pc = (id: string, name: string, online = true) => ({ id, name, hostname: name, version: "1", online, enabled: true })

// The page's own listener is registered on mount, after this one, so stopping
// propagation keeps the fixture from booting a real connection to a fake PC.
const opened: string[] = []
const record = (event: Event) => { opened.push((event as CustomEvent<string>).detail); event.stopImmediatePropagation() }

beforeEach(() => {
  setLocale("en")
  opened.length = 0
  window.addEventListener("zwai.gateway.connect", record)
  mocks.account = { status: "signed_out", session: null, bootstrap: undefined, hosts: [], origin: session.origin, error: undefined, busy: false, freshLogin: false, consumeFreshLogin: vi.fn(), setOrigin: vi.fn(), login: vi.fn(), refresh: vi.fn(async () => undefined), logout: vi.fn(async () => undefined), connectHost: vi.fn() }
  mocks.discover.mockReset().mockResolvedValue(["shared-model"])
})
afterEach(() => window.removeEventListener("zwai.gateway.connect", record))

const signedIn = (patch: Partial<ReturnType<typeof useGatewayAccount>>) => {
  mocks.account = { ...mocks.account, status: "authorized", session, bootstrap, ...patch }
}

describe("opening the computer right after sign-in", () => {
  it("connects the only online computer, once, and announces it to the page", async () => {
    signedIn({ hosts: [pc("pc-online", "Work PC"), pc("pc-offline", "Home PC", false)], freshLogin: true, connectHost: vi.fn(async () => ({ fingerprint: "fp-work" })) as never })
    const { rerender } = render(<App />)
    await waitFor(() => expect(opened).toEqual(["fp-work"]))
    expect(mocks.account.connectHost).toHaveBeenCalledOnce()
    expect(mocks.account.connectHost).toHaveBeenCalledWith("pc-online")
    expect(mocks.account.consumeFreshLogin).toHaveBeenCalledOnce()
    // The flag is consumed by the hook; a re-render that still shows it (as
    // this fixture does) must not connect a second time.
    rerender(<App />)
    await screen.findByRole("tab", { name: "Chat" })
    expect(mocks.account.connectHost).toHaveBeenCalledOnce()
  })

  it.each<[string, Partial<ReturnType<typeof useGatewayAccount>>, boolean]>([
    ["not a fresh sign-in (restart, refresh, returning user)", { hosts: [pc("pc-online", "Work PC")], freshLogin: false }, false],
    ["several online computers: the person chooses", { hosts: [pc("a", "Work PC"), pc("b", "Lab PC")], freshLogin: true }, true],
    ["no computer online", { hosts: [pc("pc-offline", "Home PC", false)], freshLogin: true }, true],
    ["no computers at all", { hosts: [], freshLogin: true }, true],
  ])("does not connect on %s", async (_name, patch, consumed) => {
    signedIn({ ...patch, connectHost: vi.fn(async () => ({ fingerprint: "fp" })) as never })
    render(<App />)
    await screen.findByRole("tab", { name: "Chat" })
    expect(mocks.account.connectHost).not.toHaveBeenCalled()
    expect(opened).toEqual([])
    expect(vi.mocked(mocks.account.consumeFreshLogin).mock.calls.length > 0).toBe(consumed)
  })

  it("waits until the account is authorized and its first read has loaded", () => {
    signedIn({ hosts: [pc("pc-online", "Work PC")], freshLogin: true, bootstrap: undefined, connectHost: vi.fn() as never })
    render(<App />)
    expect(mocks.account.connectHost).not.toHaveBeenCalled()
    expect(mocks.account.consumeFreshLogin).not.toHaveBeenCalled()
  })

  it("leaves the person on the account page, with the PC list, when the automatic connection fails", async () => {
    signedIn({ bootstrap: noModels, hosts: [pc("pc-online", "Work PC")], freshLogin: true, connectHost: vi.fn(async () => { throw new Error("offline") }) as never })
    render(<App />)
    await waitFor(() => expect(mocks.account.connectHost).toHaveBeenCalledOnce())
    expect(opened).toEqual([])
    expect(screen.getByRole("button", { name: "Connect Work PC" })).toBeEnabled()
  })
})

describe("the signed-in start page", () => {
  it("keeps pairing by QR and a model service of one's own under Other ways", async () => {
    signedIn({ bootstrap: noModels, hosts: [] })
    render(<App />)
    expect(screen.getByRole("heading", { name: t("account.pcs") })).toBeVisible()
    expect(screen.queryByRole("button", { name: t("home.addHost") })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: t("welcome.other") }))
    fireEvent.click(screen.getByRole("button", { name: t("chat.connect") }))
    expect(await screen.findByRole("dialog", { name: t("chat.add") })).toBeVisible()
    fireEvent.click(within(screen.getByRole("dialog", { name: t("chat.add") })).getAllByRole("button", { name: t("home.close") })[0])
    fireEvent.click(screen.getByRole("button", { name: t("home.addHost") }))
    expect(await screen.findByRole("dialog", { name: t("home.addHost") })).toBeVisible()
    expect(screen.getByLabelText(t("scan.uri"))).toBeVisible()
  })

  it("offers other ways only on the signed-in start page, not inside the account dialog", async () => {
    signedIn({ hosts: [pc("pc-online", "Work PC")], freshLogin: false })
    render(<App />)
    await screen.findByRole("tab", { name: "Chat" })
    await waitFor(() => expect(screen.getByRole("tab", { name: "Chat" })).toHaveAttribute("aria-selected", "true"))
    fireEvent.click(screen.getByRole("button", { name: "Menu" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "User center" }))
    expect(screen.getByRole("button", { name: "Connect Work PC" })).toBeVisible()
    expect(screen.queryByRole("button", { name: t("welcome.other") })).not.toBeInTheDocument()
  })
})
