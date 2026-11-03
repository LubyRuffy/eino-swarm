import { expect, test, type APIRequestContext, type Page } from "@playwright/test"

import type { RemoteStatus } from "../src/lib/types"
import { freshConversation, send, waitForIdle } from "./session"

const hub = "https://account.example"
const anonymous: RemoteStatus = {
  enabled: false, hub_url: hub, auth_mode: "legacy", online: false,
  has_token: false, login_available: true,
}

async function fixture(page: Page, request: APIRequestContext, initial = anonymous) {
  const original = await (await request.get("/api/settings")).json()
  let current: RemoteStatus = { ...initial }
  let unavailable = false
  let refuseLogout = false
  const starts: unknown[] = []
  const logouts: unknown[] = []
  await page.route("**/api/settings", async (route) => {
    if (route.request().method() !== "GET") return route.continue()
    await route.fulfill({ json: {
      ...original, settings: { ...original.settings, remote: {
        ...original.settings.remote, enabled: current.enabled,
        hub_url: current.hub_url, auth_mode: current.auth_mode,
      } },
    } })
  })
  await page.route("**/api/remote/status", (route) => route.fulfill(unavailable
    ? { status: 503, json: { error: "fixture status unavailable" } }
    : { json: current }))
  await page.route("**/api/remote/bindings", (route) => route.fulfill({ json: { bindings: [] } }))
  await page.route("**/api/remote/login", async (route) => {
    if (route.request().method() === "DELETE") {
      current = { ...current, login: { state: "cancelled" } }
    } else {
      starts.push(route.request().postDataJSON())
      current = { ...current, enabled: true, auth_mode: "nosec", login: {
        state: "authorization_pending", user_code: "ABCD-1234-EF56-7890", host_fp: "1122334455667788",
      } }
    }
    await route.fulfill({ status: route.request().method() === "POST" ? 202 : 200, json: current })
  })
  await page.route("**/api/remote/logout", async (route) => {
    logouts.push(route.request().postDataJSON())
    if (refuseLogout) {
      current = { ...current, online: false, login: { ...current.login, state: "error", error_code: "logout_denied" } }
      return route.fulfill({ status: 500, json: { error: "Could not clear the account credential", code: "remote_logout_failed" } })
    }
    current = { ...current, has_token: false, online: false, login: { state: "signed_out" } }
    await route.fulfill({ json: current })
  })
  return {
    starts, logouts,
    setStatus(next: RemoteStatus) { current = next },
    getStatus() { return current },
    setUnavailable(value: boolean) { unavailable = value },
    setRefuseLogout(value: boolean) { refuseLogout = value },
  }
}

async function accountMenu(page: Page) {
  const trigger = page.getByTestId("desktop-account-trigger")
  await expect(trigger).toBeVisible()
  await trigger.click()
  return page.getByRole("menu")
}

test("account entry stays in the rail, preserves local use and restores keyboard focus", async ({ page, request }) => {
  const account = await fixture(page, request)
  await page.goto("/")
  const trigger = page.getByTestId("desktop-account-trigger")
  await expect(trigger).toBeVisible()
  await trigger.focus()
  await page.keyboard.press("Enter")
  await expect(page.getByRole("menuitem", { name: "Sign in with NOSEC", exact: true })).toBeVisible()
  await page.keyboard.press("Escape")
  await expect(trigger).toBeFocused()
  expect(account.starts).toEqual([])

  // Hiding the list preserves the destination rail and its account control.
  await page.getByRole("button", { name: "Hide conversations", exact: true }).click()
  await expect(trigger).toBeVisible()
  const rail = await page.getByTestId("dest-rail").boundingBox()
  const avatar = await trigger.boundingBox()
  expect(avatar && rail && avatar.y > rail.y + rail.height / 2).toBeTruthy()
  await page.getByRole("button", { name: "Show conversations", exact: true }).click()
  await freshConversation(page)
  await send(page, "Verify local conversation remains usable without a server account")
  await waitForIdle(page)
  expect(account.starts).toEqual([])
  expect(account.logouts).toEqual([])
})

test("sidebar NOSEC login shows review and account identity and shares Phone status", async ({ page, request }, testInfo) => {
  const account = await fixture(page, request)
  await page.goto("/")
  const menu = await accountMenu(page)
  await menu.getByRole("menuitem", { name: "Sign in with NOSEC", exact: true }).click()
  const panel = page.getByTestId("desktop-account-dialog")
  await expect(panel).toBeVisible()
  await expect(panel.getByText("ABCD-1234-EF56-7890", { exact: true })).toBeVisible()
  await expect(panel.getByText("1122334455667788", { exact: true })).toBeVisible()
  expect(account.starts).toEqual([{ enable_account: true }])
  await page.screenshot({ path: testInfo.outputPath("account-login.png"), animations: "disabled" })

  account.setStatus({ ...account.getStatus(), login: { state: "account_pending" } })
  await expect(panel.getByText("Waiting for administrator approval. No device is connected yet.")).toBeVisible()
  account.setStatus({ ...account.getStatus(), login: { state: "account_rejected" } })
  await expect(panel.getByText("This account was rejected. Ask an administrator to review it.")).toBeVisible()
  account.setStatus({ ...account.getStatus(), has_token: true, login: {
    state: "authorized", username: "nosec_fixture_account_0123456789abcdef", display_name: "Fixture Account",
  } })
  await expect(panel.getByText("Authorized. Waiting for this PC to connect to the hub.")).toBeVisible()
  await expect(panel.getByText("Account: Fixture Account", { exact: true })).toBeVisible()
  await panel.getByRole("button", { name: "Close", exact: true }).click()
  const signedIn = await accountMenu(page)
  await expect(signedIn.getByText("Fixture Account", { exact: true })).toBeVisible()
  await expect(signedIn.getByText("Signed in · PC offline", { exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath("account-menu.png"), animations: "disabled" })
  await signedIn.getByRole("menuitem", { name: "Account details", exact: true }).click()
  await panel.getByRole("button", { name: "Connection settings", exact: true }).click()
  const fromDialogSettings = page.getByRole("dialog")
  await expect(fromDialogSettings.getByRole("tab", { name: "Phone", exact: true })).toHaveAttribute("aria-selected", "true")
  await expect(fromDialogSettings.getByText("Authorized. Waiting for this PC to connect to the hub.")).toBeVisible()
  await expect(fromDialogSettings.getByText("Account: Fixture Account", { exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath("account-settings.png"), animations: "disabled" })
  await fromDialogSettings.getByRole("button", { name: "Back to app", exact: true }).click()
  const afterSettings = await accountMenu(page)
  await afterSettings.getByRole("menuitem", { name: "Account details", exact: true }).focus()
  await page.keyboard.press("Escape")
  await expect(page.getByTestId("desktop-account-trigger")).toBeFocused()

  // Both the dialog button and the original direct menu entry own a handoff.
  const settingsMenu = await accountMenu(page)
  await settingsMenu.getByRole("menuitem", { name: "Connection settings", exact: true }).click()
  const settings = page.getByRole("dialog")
  await expect(settings.getByRole("tab", { name: "Phone", exact: true })).toHaveAttribute("aria-selected", "true")
  await expect(settings.getByText("Authorized. Waiting for this PC to connect to the hub.")).toBeVisible()
  account.setStatus({ ...account.getStatus(), online: true })
  await expect(settings.getByRole("button", { name: "Show pairing QR", exact: true })).toBeEnabled()
  expect(account.starts).toHaveLength(1)
})

test("logout failure retains identity and successful logout keeps local conversations", async ({ page, request }) => {
  const account = await fixture(page, request, { ...anonymous, enabled: true, auth_mode: "nosec", has_token: true, online: true, login: { state: "authorized", username: "fixture-account" } })
  await freshConversation(page)
  await send(page, "Keep this local conversation while signing out of the server")
  await waitForIdle(page)
  const conversation = await page.getByTestId("thread-title").innerText()
  account.setRefuseLogout(true)
  const menu = await accountMenu(page)
  await menu.getByRole("menuitem", { name: "Sign out", exact: true }).click()
  const panel = page.getByTestId("desktop-account-dialog")
  await expect(panel.getByRole("alert")).toHaveText("The account action could not finish. Refresh the status or try again.")
  await panel.getByRole("button", { name: "Close", exact: true }).click()
  const retry = await accountMenu(page)
  await expect(retry.getByText("fixture-account", { exact: true })).toBeVisible()
  account.setRefuseLogout(false)
  await retry.getByRole("menuitem", { name: "Sign out", exact: true }).click()
  await expect(panel.getByText("Sign in to associate this PC with your account.", { exact: true })).toBeVisible()
  await panel.getByRole("button", { name: "Close", exact: true }).click()
  const signedOut = await accountMenu(page)
  await expect(signedOut.getByRole("menuitem", { name: "Sign in with NOSEC", exact: true })).toBeVisible()
  await expect(signedOut.getByText("fixture-account", { exact: true })).toHaveCount(0)
  await page.keyboard.press("Escape")
  expect(account.logouts).toEqual([{}, {}])
  await expect(page.getByTestId("thread-title")).toHaveText(conversation)
  await send(page, "Continue the same local conversation after server sign out")
  await waitForIdle(page)
  expect(account.starts).toEqual([])
})
