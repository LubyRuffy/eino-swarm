import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import type { AccountModels, Meta, ModelInfo, RemoteStatus, Settings } from "@/lib/types"
import { useApp } from "@/store/app"

import { ModelsTab } from "./model-settings"

HTMLElement.prototype.scrollIntoView = function () {}
HTMLElement.prototype.hasPointerCapture = function () { return false }
HTMLElement.prototype.releasePointerCapture = function () {}

const fake = vi.hoisted(() => ({
  status: undefined as RemoteStatus | undefined,
  unavailable: false,
  perform: vi.fn(async (action: () => Promise<unknown>) => { await action() }),
  start: vi.fn(),
}))

vi.mock("@/lib/use-remote-status", () => ({
  useDesktopAccount: () => ({
    status: fake.status, unavailable: fake.unavailable, busy: false,
    refresh: vi.fn(async () => {}), perform: fake.perform,
  }),
}))
vi.mock("@/lib/api", () => ({
  api: { startRemoteLogin: fake.start, cancelRemoteLogin: vi.fn(), discoverModels: vi.fn() },
}))

const own = {
  id: "default", label: "Endpoint", base_url: "http://endpoint.invalid/v1", model: "alpha", catalog: ["alpha"],
  timeout_seconds: 300, has_api_key: true, ready: true, enabled: true,
}
const settings = (patch: Partial<Settings["models"]> = {}): Settings => ({
  server: { addr: "127.0.0.1:1", open_browser: false },
  models: { default: "default", default_source: "account", providers: [own], ...patch },
  swarm: {
    max_concurrent: 1, agent_timeout_seconds: 1, max_turns: 1, manager_max_iterations: 1,
    progress_interval_seconds: 1, delta_coalesce_ms: 1, auto_title: true,
  },
  tools: { disabled: [], enabled: [], proxy: { http: "", https: "", no_proxy: "" }, web_search_max_results: 8 },
  memory: {
    enabled: true, auto_review: true, char_limit: 1, entry_max: 1, review_max_iterations: 1,
    skills_index_max: 1, notifications: "on",
  },
  log: { level: "info" },
} as Settings)
const meta = (account: AccountModels): Meta => ({
  version: "test", mode: "desktop", mock: false, configured: true, default_provider: "",
  reasoning_levels: [], data_dir: "", capabilities: {}, swarm: {} as Meta["swarm"], account_models: account,
})
const row = (provider: string, name: string, source: ModelInfo["source"]): ModelInfo => ({
  id: `${provider}\t${name}`, provider_id: provider, provider_label: provider, label: name, model: name, ready: true, source,
})
const signedIn: RemoteStatus = {
  enabled: true, hub_url: "https://server.example", auth_mode: "account", has_token: true, online: true,
  login_available: true, login: { state: "authorized", username: "account" },
}
const signedOut: RemoteStatus = { ...signedIn, has_token: false, login: { state: "signed_out" } }

beforeEach(() => {
  vi.clearAllMocks()
  fake.status = signedOut
  fake.unavailable = false
  fake.start.mockResolvedValue(signedOut)
  useApp.setState({ meta: meta({ state: "signed_out", models: 0 }), models: [] })
})

describe("server models on the Models page", () => {
  it("invites sign-in first and keeps the own-service list below it", () => {
    render(<ModelsTab settings={settings({ providers: [{ ...own, ready: false, model: "" }] })} onChange={vi.fn()} />)
    const card = screen.getByTestId("account-models-card")
    expect(within(card).getByText(/Sign in to use the server's models/)).toBeTruthy()
    fireEvent.click(within(card).getByRole("button", { name: "Sign in to the official server" }))
    expect(fake.start).toHaveBeenCalledWith(false, true)
    // Only the untouched placeholder row exists, so the page says so.
    expect(screen.getByText(/None set up yet/)).toBeTruthy()
  })

  it("lists what the server provides and lets the person pick the starting source", async () => {
    fake.status = signedIn
    useApp.setState({
      meta: meta({ state: "ready", models: 2, username: "account", display_name: "Account Name" }),
      models: [row("gateway:x", "server-a", "account"), row("gateway:x", "server-b", "account"), row("default", "alpha", "provider")],
    })
    const onChange = vi.fn()
    render(<ModelsTab settings={settings()} onChange={onChange} />)
    const card = screen.getByTestId("account-models-card")
    expect(within(card).getByText("2 models available · signed in as Account Name")).toBeTruthy()
    const names = within(screen.getByRole("list", { name: "Models from the server" })).getAllByRole("listitem")
    // The server's models only: the own provider's model must not show here.
    expect(names.map((n) => n.textContent)).toEqual(["server-a", "server-b"])
    expect(within(card).queryByRole("button", { name: "Sign in to the official server" })).toBeNull()

    fireEvent.click(screen.getByRole("combobox", { name: "New conversations start with" }))
    fireEvent.click(await screen.findByRole("option", { name: "My own model service" }))
    expect(onChange.mock.calls[0][0].models.default_source).toBe("provider")
    // Everything else in the models block survives the choice.
    expect(onChange.mock.calls[0][0].models.providers).toHaveLength(1)
  })

  it("offers no starting-source choice until there is a second source to start from", () => {
    fake.status = signedIn
    useApp.setState({ meta: meta({ state: "ready", models: 1 }), models: [row("gateway:x", "server-a", "account")] })
    render(<ModelsTab settings={settings({ providers: [{ ...own, ready: false, model: "" }] })} onChange={vi.fn()} />)
    expect(screen.queryByRole("combobox", { name: "New conversations start with" })).toBeNull()
    expect(screen.getByText(/Set up your own model service below/)).toBeTruthy()
  })

  it.each([
    [{ state: "no_access", models: 0 }, /cannot use server models/],
    [{ state: "unavailable", reason: "unsupported", models: 0 }, /does not offer models to the desktop app yet/],
    [{ state: "unavailable", reason: "rejected", models: 0 }, /no longer accepts this sign-in/],
    [{ state: "pending", models: 0 }, /Reading your models/],
  ] as Array<[AccountModels, RegExp]>)("explains %j in words, not codes", async (account, words) => {
    fake.status = signedIn
    useApp.setState({ meta: meta(account), models: [] })
    render(<ModelsTab settings={settings()} onChange={vi.fn()} />)
    await waitFor(() => expect(within(screen.getByTestId("account-models-card")).getByText(words)).toBeTruthy())
    expect(screen.queryByText(account.reason ?? "no-reason")).toBeNull()
  })

  it("keeps the chosen source when an own provider is removed", () => {
    const second = { ...own, id: "second", label: "Second" }
    const onChange = vi.fn()
    render(<ModelsTab settings={settings({ default_source: "provider", providers: [own, second] })} onChange={onChange} />)
    fireEvent.click(screen.getAllByRole("button", { name: /remove|delete/i })[0])
    fireEvent.click(screen.getByRole("button", { name: /^(Delete|Remove)/ }))
    expect(onChange).toHaveBeenCalled()
    expect(onChange.mock.calls[0][0].models.default_source).toBe("provider")
  })
})
