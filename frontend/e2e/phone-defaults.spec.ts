import { expect, test, type APIRequestContext, type Page } from "@playwright/test"
import type { RemoteSettings, RemoteStatus } from "../src/lib/types"

async function fixture(page: Page, request: APIRequestContext, patch: Partial<RemoteSettings> = {}) {
  const original = await (await request.get("/api/settings")).json()
  let remote: RemoteSettings = { ...original.settings.remote, ...patch }
  let status: RemoteStatus = { ...remote, online: false, has_token: false, login_available: true }
  const writes: RemoteSettings[] = []
  const patches: Record<string, unknown>[] = []
  const starts: unknown[] = []
  const order: string[] = []
  let failStartAndRead = false
  let failSettingsOnce = false
  await page.route("**/api/settings", async (route) => {
    if (route.request().method() === "GET" && failSettingsOnce) {
      failSettingsOnce = false
      return route.fulfill({ status: 503, json: { error: "fixture settings read unavailable" } })
    }
    if (route.request().method() === "PUT") {
      const body = route.request().postDataJSON()
      patches.push(body)
      order.push("settings")
      if (body.remote) {
        remote = { ...remote, ...body.remote }
        writes.push({ ...remote })
        status = { ...status, enabled: remote.enabled, hub_url: remote.hub_url, auth_mode: remote.auth_mode }
      }
    }
    await route.fulfill({ json: { ...original, settings: { ...original.settings, remote } } })
  })
  await page.route("**/api/remote/status", (route) => route.fulfill({ json: status }))
  await page.route("**/api/remote/bindings", (route) => route.fulfill({ json: { bindings: [] } }))
  await page.route("**/api/remote/login", async (route) => {
    const body = route.request().postDataJSON()
    starts.push(body)
    order.push("login")
    expect(body.enable_account).toBe(true)
    remote = { ...remote, enabled: true, auth_mode: "account" }
    status = { ...status, ...remote, login: { state: "authorization_pending", user_code: "CONFIRM-THIS-PC", host_fp: "1122334455667788" } }
    if (failStartAndRead) {
      failStartAndRead = false
      failSettingsOnce = true
      status = { ...status, login: { state: "error", error_code: "network_unavailable" } }
      return route.fulfill({ status: 500, json: { error: "fixture login acknowledgement lost" } })
    }
    await route.fulfill({ status: 202, json: status })
  })
  return { starts, writes, patches, order, remote: () => remote, failNextStartAndRead() { failStartAndRead = true } }
}

async function openPhone(page: Page) {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings", exact: true }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Phone", exact: true }).click()
  return dialog
}

test("Phone defaults offer account sign-in without requiring a server form", async ({ page, request }, testInfo) => {
  const account = await fixture(page, request)
  const server = account.remote().hub_url
  expect(server).toBeTruthy() // Effective default comes from Go, not the renderer.
  const phone = await openPhone(page)
  const signIn = phone.getByRole("button", { name: "Sign in", exact: true })
  await expect(signIn).toBeEnabled()
  await expect(phone.getByText(server, { exact: true })).toBeVisible()
  await expect(phone.getByLabel("Hub URL")).toHaveCount(0)
  await expect(phone.getByLabel("Connection sign-in")).toHaveCount(0)
  await expect(phone.getByLabel("Host Token")).toHaveCount(0)
  await expect(phone.getByText("Bound phones", { exact: true })).toBeVisible()
  expect(account.starts).toEqual([])
  expect(account.writes).toEqual([])
  await page.screenshot({ path: testInfo.outputPath("phone-default.png"), animations: "disabled" })
  await signIn.click()
  await expect(phone.getByText("CONFIRM-THIS-PC", { exact: true })).toBeVisible()
  expect(account.starts).toEqual([{ enable_account: true }])
  expect(account.writes).toEqual([])
  await page.screenshot({ path: testInfo.outputPath("phone-confirmation.png"), animations: "disabled" })
})

test("advanced settings preserve and restore a configured anonymous server", async ({ page, request }, testInfo) => {
  const account = await fixture(page, request, { hub_url: "https://self-hosted.example", auth_mode: "independent", display_name: "saved-device" })
  let phone = await openPhone(page)
  await expect(phone.getByText("https://self-hosted.example", { exact: true })).toBeVisible()
  await expect(phone.getByLabel("Hub URL")).toHaveCount(0)
  await phone.getByRole("button", { name: "Advanced / self-hosted server", exact: true }).click()
  await expect(phone.getByLabel("Hub URL")).toHaveValue("https://self-hosted.example")
  await expect(phone.getByLabel("Connection sign-in")).toHaveText("Independent hub")
  await phone.getByLabel("Hub URL").scrollIntoViewIfNeeded()
  await page.screenshot({ path: testInfo.outputPath("phone-advanced.png"), animations: "disabled" })
  await phone.getByLabel("Hub URL").fill("https://replacement.example")
  await phone.getByRole("button", { name: "Back to app", exact: true }).click()
  expect(account.remote()).toMatchObject({ hub_url: "https://replacement.example", auth_mode: "independent", display_name: "saved-device" })
  phone = await openPhone(page)
  await expect(phone.getByText("https://replacement.example", { exact: true })).toBeVisible()
  await expect(phone.getByLabel("Hub URL")).toHaveCount(0)
  expect(account.starts).toEqual([])
})

test("explicit Phone sign-in flushes an older draft and later edits retain the enabled account", async ({ page, request }) => {
  const account = await fixture(page, request, { hub_url: "https://self-hosted.example", auth_mode: "independent" })
  const phone = await openPhone(page)
  await expect(phone.getByRole("button", { name: "Sign in", exact: true })).toBeEnabled()
  await phone.getByRole("button", { name: "Advanced / self-hosted server", exact: true }).click()
  await phone.getByLabel("This computer's name").fill("updated-device")
  await phone.getByRole("button", { name: "Sign in", exact: true }).click()
  await expect(phone.getByText("CONFIRM-THIS-PC", { exact: true })).toBeVisible()
  expect(account.order).toEqual(["settings", "login"])
  await expect(phone.getByLabel("Connection sign-in")).toHaveText("Gateway account")
  await phone.getByLabel("Threads on the phone").fill("7")
  await phone.getByRole("button", { name: "Back to app", exact: true }).click()
  expect(account.writes).toHaveLength(2)
  expect(account.writes[1]).toMatchObject({ enabled: true, auth_mode: "account", hub_url: "https://self-hosted.example", display_name: "updated-device", thread_limit: 7 })
  expect(account.starts).toEqual([{ enable_account: true }])
})


test("uncertain account configuration has an explicit read-only recovery", async ({ page, request }) => {
  const account = await fixture(page, request, { enabled: false, auth_mode: "independent" })
  account.failNextStartAndRead()
  const phone = await openPhone(page)
  await phone.getByRole("button", { name: "Sign in", exact: true }).click()
  const reload = phone.getByRole("button", { name: "Reload account settings", exact: true })
  await expect(reload).toBeVisible()
  await expect(phone.getByRole("button", { name: "Sign in", exact: true })).toHaveCount(0)
  await phone.getByRole("tab", { name: "Personality", exact: true }).click()
  await phone.getByLabel("Personal preferences").fill("Independent recovery draft")
  await phone.getByRole("tab", { name: "Phone", exact: true }).click()
  await reload.click()
  await expect(phone.getByRole("button", { name: "Sign in", exact: true })).toBeEnabled()
  await expect(reload).toHaveCount(0)
  await expect(phone.getByLabel("Hub URL")).toHaveCount(0)
  await expect.poll(() => account.patches.length).toBe(1)
  expect(account.patches[0]).toHaveProperty("personality")
  expect(account.patches[0]).not.toHaveProperty("remote")
  await phone.getByRole("tab", { name: "Personality", exact: true }).click()
  await expect(phone.getByLabel("Personal preferences")).toHaveValue("Independent recovery draft")
  await phone.getByLabel("Personal preferences").fill("Next edit retains account")
  await phone.getByRole("button", { name: "Back to app", exact: true }).click()
  expect(account.patches).toHaveLength(2)
  expect(account.patches[1]).not.toHaveProperty("remote")
  expect(account.starts).toEqual([{ enable_account: true }])
  expect(account.writes).toEqual([])
})
