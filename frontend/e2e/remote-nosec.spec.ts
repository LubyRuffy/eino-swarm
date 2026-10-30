import { expect, test, type APIRequestContext, type Page } from "@playwright/test"
import type { RemoteSettings, RemoteStatus } from "../src/lib/types"

let original: RemoteSettings
const hub = "https://hub.example"
const initial: RemoteStatus = {
  enabled: true, hub_url: hub, auth_mode: "nosec", online: false,
  has_token: false, login_available: true, login: { state: "signed_out" },
}
async function openPhone(page: Page) {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings", exact: true }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Phone", exact: true }).click()
  return dialog
}
async function configure(request: APIRequestContext, remote: RemoteSettings) {
  expect((await request.put("/api/settings", { data: { remote } })).ok()).toBeTruthy()
}

test.beforeEach(async ({ request }) => {
  const { settings } = await (await request.get("/api/settings")).json()
  original = settings.remote
  await configure(request, { ...original, enabled: true, hub_url: hub, auth_mode: "nosec" })
})
test.afterEach(async ({ request }) => { await configure(request, original) })

test("NOSEC device sign-in distinguishes review, authorization and a live PC", async ({ page }) => {
  let current = { ...initial }
  const starts: unknown[] = []
  let cancelled = 0
  let offers = 0
  await page.route("**/api/remote/status", (route) => route.fulfill({ json: current }))
  await page.route("**/api/remote/bindings", (route) => route.fulfill({ json: { bindings: [] } }))
  await page.route("**/api/remote/login", async (route) => {
    if (route.request().method() === "DELETE") {
      cancelled++
      current = { ...initial, login: { state: "cancelled" } }
    } else {
      starts.push(route.request().postDataJSON())
      current = { ...initial, login: { state: "authorization_pending", user_code: "ABCD-1234-EF56-7890", host_fp: "1122334455667788" } }
    }
    await route.fulfill({ status: route.request().method() === "POST" ? 202 : 200, json: current })
  })
  await page.route("**/api/remote/offer", async (route) => {
    offers++
    await route.fulfill({ json: { uri: "pairlink:v1:http://127.0.0.1:9:code:pub", png: "data:image/png;base64,aaaa", expires_at: new Date(Date.now() + 60_000).toISOString() } })
  })
  const dialog = await openPhone(page)
  await expect(dialog.getByLabel("Connection sign-in")).toHaveText("NOSEC account")
  const login = dialog.getByRole("button", { name: "Sign in with NOSEC" })
  const qr = dialog.getByRole("button", { name: "Show pairing QR" })
  await expect(login).toBeEnabled()
  expect(starts).toHaveLength(0)
  await expect(qr).toBeDisabled()
  await login.click()
  await expect(dialog.getByText("ABCD-1234-EF56-7890", { exact: true })).toBeVisible()
  await expect(dialog.getByText("1122334455667788", { exact: true })).toBeVisible()
  expect(starts).toEqual([{}])
  current = { ...current, login: { ...current.login!, state: "account_pending" } }
  await expect(dialog.getByText("Waiting for administrator approval. No device is connected yet.")).toBeVisible()
  await expect(qr).toBeDisabled()
  current = { ...current, login: { ...current.login!, state: "account_rejected" } }
  await expect(dialog.getByText("This account was rejected. Ask an administrator to review it.")).toBeVisible()
  current = { ...current, login: { ...current.login!, state: "expired" } }
  await expect(dialog.getByText("Sign-in expired. Start again in your browser.")).toBeVisible()
  await login.click()
  await dialog.getByRole("button", { name: "Cancel sign-in" }).click()
  await expect(dialog.getByText("Sign-in cancelled.")).toBeVisible()
  expect(cancelled).toBe(1)
  await login.click()
  current = { ...current, login: { state: "authorized", username: "fixture-user" } }
  await expect(dialog.getByText("Authorized. Waiting for this PC to connect to the hub.")).toBeVisible()
  await expect(qr).toBeDisabled()
  expect(offers).toBe(0)
  current = { ...current, has_token: true, online: true }
  await expect(qr).toBeEnabled()
  await qr.click()
  await expect(dialog.getByTestId("remote-qr")).toBeVisible()
  current = { ...current, online: false, login: { state: "access_denied" } }
  await expect(qr).toBeDisabled()
  await expect(dialog.getByTestId("remote-qr")).toHaveCount(0)
  await expect(dialog.getByLabel("Host Token")).toHaveCount(0)
})

test("new-device recovery needs confirmation and a changed Hub discards late login state", async ({ page }) => {
  let current: RemoteStatus = { ...initial, login: { state: "error", error_code: "device_conflict" } }
  const starts: unknown[] = []
  let release!: () => void
  const barrier = new Promise<void>((resolve) => { release = resolve })
  await page.route("**/api/remote/status", (route) => route.fulfill({ json: current }))
  await page.route("**/api/remote/bindings", (route) => route.fulfill({ json: { bindings: [] } }))
  await page.route("**/api/remote/login", async (route) => {
    starts.push(route.request().postDataJSON())
    await barrier
    await route.fulfill({ status: 202, json: { ...initial, login: { state: "authorization_pending", user_code: "OLD1-0000-0000-0000" } } })
  })
  const dialog = await openPhone(page)
  await dialog.getByRole("button", { name: "Reconnect as a new device", exact: true }).click()
  expect(starts).toHaveLength(0)
  const confirm = page.getByRole("dialog", { name: "Reconnect as a new device?" })
  await expect(confirm).toBeVisible()
  await confirm.getByRole("button", { name: "Confirm reconnect" }).click()
  await expect.poll(() => starts.length).toBe(1)
  expect(starts).toEqual([{ new_device: true, confirmed: true }])
  current = { ...initial, hub_url: "https://next-hub.example" }
  const saved = page.waitForResponse((response) => response.url().endsWith("/api/settings") && response.request().method() === "PUT")
  await dialog.getByLabel("Hub URL").fill(current.hub_url)
  await saved
  release()
  await expect(dialog.getByRole("button", { name: "Sign in with NOSEC" })).toBeEnabled()
  await expect(dialog.getByText("OLD1-0000-0000-0000", { exact: true })).toHaveCount(0)
  expect(starts).toHaveLength(1)
})

test("NOSEC saved configuration uses real local status and switching mode removes login", async ({ page, request }) => {
  const status = await (await request.get("/api/remote/status")).json()
  expect(status).toMatchObject({ enabled: true, hub_url: hub, auth_mode: "nosec", online: false, login: { state: "signed_out" } })
  expect(status).not.toHaveProperty("device_code")
  expect(status).not.toHaveProperty("token")
  const dialog = await openPhone(page)
  await expect(dialog.getByText("Sign in to associate this PC with your account.")).toBeVisible()
  await expect(dialog.getByRole("button", { name: "Show pairing QR" })).toBeDisabled()
  await dialog.getByLabel("Accept phone pairing").click()
  await expect.poll(async () => (await (await request.get("/api/remote/status")).json()).enabled).toBe(false)
  await dialog.getByLabel("Connection sign-in").click()
  await page.getByRole("option", { name: "Independent hub (legacy)" }).click()
  await expect(dialog.getByRole("button", { name: "Sign in with NOSEC" })).toHaveCount(0)
  await expect.poll(async () => (await (await request.get("/api/remote/status")).json()).auth_mode).toBe("legacy")
})
