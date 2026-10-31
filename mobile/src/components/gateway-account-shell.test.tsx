import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { App } from "@/app"
import type { useGatewayAccount } from "@/lib/use-gateway-account"
import { saveThreads, newThread } from "@/lib/direct-threads"
import { saveProviders } from "@/lib/direct-provider"
import { GatewayError } from "@/lib/gateway-account"
import { setLocale, t } from "@/lib/i18n"

const mocks = vi.hoisted(() => ({ account: {} as ReturnType<typeof useGatewayAccount>, discover: vi.fn() }))
vi.mock("@/lib/use-gateway-account", () => ({ useGatewayAccount: () => mocks.account }))
vi.mock("@/lib/openai-client", async (original) => ({ ...await original<typeof import("@/lib/openai-client")>(), discoverModels: mocks.discover }))

const session = { origin: "https://gateway.invalid", token: "agm_" + "a".repeat(64), user: { id: "owner", username: "User", approval_status: "approved" }, device: { id: "phone", expires_at: "2099-01-01T00:00:00Z" } }
const bootstrap = { user: session.user, device: session.device, models: { source_id: "gateway:owner", source_kind: "gateway" as const, label: "Team", execution: "phone" as const, api_base_url: "https://gateway.invalid/v1", catalog_url: "https://gateway.invalid/v1/models", api_access_enabled: true }, pairlink: { enabled: true, hub_url: session.origin, redeem_url: session.origin + "/pairlink/v1/pairings/redeem", websocket_url: "wss://gateway.invalid/pairlink/v1/ws", info_url: session.origin + "/pairlink/v1/info" }, heartbeat_interval: 30, presence_ttl: 90 }

beforeEach(() => {
  setLocale("en")
  mocks.account = { status: "signed_out", session: null, bootstrap: undefined, hosts: [], origin: session.origin, error: undefined, busy: false, setOrigin: vi.fn(), login: vi.fn(), refresh: vi.fn(async () => undefined), logout: vi.fn(async () => undefined), connectHost: vi.fn() }
  mocks.discover.mockReset().mockResolvedValue(["shared-model"])
})

describe("gateway account shell", () => {
  it.each([
    ["en", "browser_unavailable", "Could not open the sign-in browser. Try again."],
    ["en", "gateway_timeout", "The account operation timed out. Try again."],
    ["zh", "browser_unavailable", "无法打开登录浏览器，请重试。"],
    ["zh", "gateway_timeout", "账号操作超时，请重试。"],
  ] as const)("describes %s %s as an account retry instead of a server rejection", (locale, error, message) => {
    setLocale(locale)
    mocks.account = { ...mocks.account, status: "error", error }
    render(<App />)
    fireEvent.click(screen.getByRole("button", { name: t("account.login") }))
    expect(screen.getByRole("alert")).toHaveTextContent(message)
    expect(screen.queryByText(t("chat.failed"))).not.toBeInTheDocument()
  })

  it.each(["account_pending", "account_rejected", "access_denied"] as const)("%s exposes only account review, even with private models stored", async (status) => {
    saveProviders([{ id: "p-private", label: "Personal", baseURL: "https://private.invalid/v1", apiKey: "secret", api: "chat", model: "private-model", catalog: [], timeoutSeconds: 300 }])
    mocks.account = { ...mocks.account, status, session }
    render(<App />)
    expect(screen.getByRole("heading", { name: "User center" })).toBeVisible()
    expect(screen.queryByRole("button", { name: "Menu" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Scan QR" })).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Message")).not.toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Your computers" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Close" })).not.toBeInTheDocument()
    expect(mocks.discover).not.toHaveBeenCalled()
    expect(localStorage.getItem("zwai.phone.providers")).toContain("private-model")
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }))
    expect(mocks.account.logout).toHaveBeenCalledOnce()
  })

  it("keeps device management accessible when API access is disabled and no PCs are connected", async () => {
    mocks.account = { ...mocks.account, status: "authorized", session, bootstrap: { ...bootstrap, models: { ...bootstrap.models, api_access_enabled: false } } }
    render(<App />)
    expect(screen.getByRole("heading", { name: "User center" })).toBeVisible()
    expect(screen.getByRole("heading", { name: "Your computers" })).toBeVisible()
    expect(screen.getByText("API access is unavailable. Contact an administrator.")).toBeVisible()
    expect(screen.queryByRole("button", { name: "Scan QR" })).not.toBeInTheDocument()
    expect(mocks.discover).not.toHaveBeenCalled()
  })

  it("retains own server history for an API-disabled approved account and hides another account's history", async () => {
    const own = newThread({ providerId: "gateway:owner", model: "shared-model", reasoning: "" }); own.title = "Own history"
    const other = newThread({ providerId: "gateway:other", model: "shared-model", reasoning: "" }); other.title = "Other account history"
    saveThreads([own, other])
    mocks.account = { ...mocks.account, status: "authorized", session, bootstrap: { ...bootstrap, models: { ...bootstrap.models, api_access_enabled: false } } }
    render(<App />)
    fireEvent.click(screen.getByRole("button", { name: "Chat history" }))
    expect(await screen.findByText("Own history")).toBeVisible()
    expect(screen.queryByText("Other account history")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Open Own history" }))
    expect(screen.getByLabelText("Message")).toBeDisabled()
    expect(mocks.discover).not.toHaveBeenCalled()
  })

  it("automatically makes server models usable without a PC and keeps credential out of personal settings", async () => {
    mocks.account = { ...mocks.account, status: "authorized", session, bootstrap }
    render(<App />)
    await screen.findByRole("tab", { name: "Chat" })
    expect(screen.queryByRole("button", { name: "Scan QR" })).not.toBeInTheDocument()
    expect(mocks.discover).toHaveBeenCalledWith(expect.objectContaining({ id: "gateway:owner", apiKey: session.token, baseURL: "https://gateway.invalid/v1" }))
    expect(localStorage.getItem("zwai.phone.providers")).toBeNull()
    fireEvent.click(await screen.findByRole("button", { name: "Model" }))
    expect(screen.getByRole("heading", { name: "Server · Team" })).toBeVisible()
    expect(screen.getByRole("radio", { name: "shared-model" })).toBeVisible()
    expect(document.body.textContent).not.toContain(session.token)
  })

  it("opens default-server sign-in without asking for server configuration and Android Back closes its overlay", async () => {
    render(<App />)
    fireEvent.click(screen.getByRole("button", { name: "Sign in with NOSEC" }))
    expect(screen.getByTestId("gateway-account")).toBeVisible()
    expect(screen.queryByLabelText("Server address")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Sign in with NOSEC" }))
    expect(mocks.account.login).toHaveBeenCalledOnce()
    await act(async () => { expect(window.__zwaiAndroidBack?.()).toBe(true) })
    expect(screen.queryByTestId("gateway-account")).not.toBeInTheDocument()
    expect(window.__zwaiAndroidBack?.()).toBe(false)
  })

  it("keeps an account PC going offline during selection on the account refresh path", async () => {
    mocks.account = { ...mocks.account, status: "authorized", session,
      bootstrap: { ...bootstrap, models: { ...bootstrap.models, api_access_enabled: false } },
      hosts: [{ id: "pc-online", name: "Work PC", hostname: "work", version: "1", online: true, enabled: true }],
      connectHost: vi.fn(async () => { throw new GatewayError(409, "host_offline") }),
    }
    render(<App />)
    fireEvent.click(screen.getByRole("button", { name: "Connect Work PC" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("This PC is offline. Wait for the desktop to reconnect, then refresh and try again.")
    expect(screen.queryByText(/Show a new QR/)).not.toBeInTheDocument()
  })

  it("lists same-account PCs, disables offline PCs and connects only the selected online PC without scanning", async () => {
    mocks.account = { ...mocks.account, status: "authorized", session, bootstrap, hosts: [
      { id: "pc-online", name: "Work PC", hostname: "work", version: "1", online: true, enabled: true },
      { id: "pc-offline", name: "Home PC", hostname: "home", version: "1", online: false, enabled: true },
    ], connectHost: vi.fn(async () => { throw new Error("fixture keeps the account page visible") }) }
    render(<App />)
    await screen.findByRole("tab", { name: "Chat" })
    await waitFor(() => expect(screen.getByRole("tab", { name: "Chat" })).toHaveAttribute("aria-selected", "true"))
    fireEvent.click(screen.getByRole("button", { name: "Menu" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "User center" }))
    expect(screen.getByRole("button", { name: "Connect Home PC" })).toBeDisabled()
    fireEvent.click(screen.getByRole("button", { name: "Connect Work PC" }))
    await waitFor(() => expect(mocks.account.connectHost).toHaveBeenCalledWith("pc-online"))
    expect(mocks.account.connectHost).toHaveBeenCalledOnce()
    expect(screen.queryByRole("button", { name: "Scan QR" })).not.toBeInTheDocument()
  })
})
