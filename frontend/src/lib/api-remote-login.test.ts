import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "./api"

const publicStatus = { enabled: true, online: false, hub_url: "https://hub.example", has_token: false, auth_mode: "nosec", login_available: true, login: { state: "authorization_pending", user_code: "ABCD-1234-EF56-7890" } }
afterEach(() => { vi.unstubAllGlobals() })
describe("local NOSEC login API", () => {
  it("sends only explicit start/reconnect flags to the local Go service", async () => {
    const fetch = vi.fn().mockImplementation(async () => new Response(JSON.stringify(publicStatus), { status: 202 }))
    vi.stubGlobal("fetch", fetch)
    expect(await api.startRemoteLogin()).toEqual(publicStatus)
    await api.startRemoteLogin(true)
    expect(fetch.mock.calls.map(([url, init]) => [url, init.method, init.body])).toEqual([
      ["/api/remote/login", "POST", "{}"],
      ["/api/remote/login", "POST", '{"new_device":true,"confirmed":true}'],
    ])
  })
  it("cancels without a body and lets status reads be aborted", async () => {
    const fetch = vi.fn().mockImplementation(async () => new Response(JSON.stringify(publicStatus)))
    vi.stubGlobal("fetch", fetch)
    await api.cancelRemoteLogin()
    const signal = new AbortController().signal
    await api.remoteStatus(signal)
    expect(fetch.mock.calls[0][0]).toBe("/api/remote/login")
    expect(fetch.mock.calls[0][1]).toMatchObject({ method: "DELETE" })
    expect(fetch.mock.calls[0][1].body).toBeUndefined()
    expect(fetch.mock.calls[1][1].signal).toBe(signal)
  })
})
