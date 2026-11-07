import { expect, test, type Page } from "@playwright/test"

// The offline server is always "configured", so the first-run state is
// produced by rewriting only the meta answer. The settings write is answered
// locally: this suite shares one data directory and must keep its own stored
// settings untouched.
async function firstRun(page: Page, account: { configured: boolean; models: Record<string, unknown> } = { configured: false, models: { state: "signed_out", models: 0 } }) {
  const saved: unknown[] = []
  let dismissed = false
  await page.route("**/api/meta", async (route) => {
    const response = await route.fetch()
    const body = await response.json()
    await route.fulfill({ response, json: { ...body, mock: false, configured: account.configured, account_models: account.models, ui: { ...body.ui, welcome_dismissed: dismissed } } })
  })
  await page.route("**/api/settings", async (route) => {
    if (route.request().method() !== "PUT") return route.fallback()
    saved.push(route.request().postDataJSON())
    dismissed = true
    // The client reads the tool lists out of the answer, so echo the stored settings unchanged.
    await route.fulfill({ response: await page.request.get("/api/settings") })
  })
  return saved
}

test("a first run with no model asks once, remembers the answer and never traps the user", async ({ page }, info) => {
  const saved = await firstRun(page)
  await page.goto("/")
  const gate = page.getByTestId("welcome-gate")
  await expect(gate).toBeVisible()
  await expect(gate.getByRole("button", { name: "Use my own model service" })).toBeVisible()
  await page.screenshot({ path: info.outputPath("welcome-first-run.png") })
  await gate.getByRole("button", { name: "Not now" }).click()
  await expect(gate).toHaveCount(0)
  expect(saved).toEqual([{ ui: { welcome_dismissed: true } }])
  // Still unconfigured: the banner keeps the way forward visible.
  await expect(page.getByRole("button", { name: "Set up my own models" })).toBeVisible()
  await page.reload()
  await expect(page.getByRole("button", { name: "Set up my own models" })).toBeVisible()
  await expect(page.getByTestId("welcome-gate")).toHaveCount(0)
})

test("choosing the own-model path opens Settings on Models", async ({ page }) => {
  const saved = await firstRun(page)
  await page.goto("/")
  await page.getByTestId("welcome-gate").getByRole("button", { name: "Use my own model service" }).click()
  await expect(page.getByTestId("welcome-gate")).toHaveCount(0)
  const settings = page.getByRole("dialog")
  await expect(settings.getByRole("tab", { name: "Models" })).toHaveAttribute("aria-selected", "true")
  expect(saved).toEqual([{ ui: { welcome_dismissed: true } }])
})

test("a ready server catalog is a selectable default beside an own provider", async ({ page }) => {
  let settingsBody: { settings: { models: Record<string, unknown> } } | undefined
  await page.route("**/api/meta", async (route) => {
    const response = await route.fetch()
    const body = await response.json()
    await route.fulfill({
      response,
      json: { ...body, account_models: { state: "ready", models: 2, username: "account", display_name: "Account" } },
    })
  })
  await page.route("**/api/settings", async (route) => {
    const method = route.request().method()
    if (method === "GET") {
      if (!settingsBody) {
        const response = await route.fetch()
        settingsBody = await response.json()
        settingsBody!.settings.models.default_source = "provider"
      }
      await route.fulfill({ json: settingsBody })
      return
    }
    if (method !== "PUT" || !settingsBody) return route.fallback()
    const patch = route.request().postDataJSON() as { models?: Record<string, unknown> }
    settingsBody = {
      settings: {
        ...settingsBody!.settings,
        ...patch,
        models: { ...settingsBody!.settings.models, ...patch.models },
      },
    }
    await route.fulfill({ json: settingsBody })
  })
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Models" }).click()
  const row = dialog.getByTestId("server-default-row")
  await expect(row.getByText("The server's models")).toBeVisible()
  await row.getByRole("button", { name: "Make the server's models the default" }).click()
  await expect(row.getByText("default", { exact: true })).toBeVisible()
  await expect(row.getByRole("button", { name: "Make the server's models the default" })).toHaveCount(0)
})

test("a signed-in account with ready models needs no welcome even before any provider exists", async ({ page }) => {
  // The server counts a ready account as configured, which is what keeps the welcome away.
  const saved = await firstRun(page, { configured: true, models: { state: "ready", label: "Team", models: 2 } })
  await page.goto("/")
  await expect(page.getByRole("heading", { name: "What should we work on?" })).toBeVisible()
  await expect(page.getByTestId("welcome-gate")).toHaveCount(0)
  expect(saved).toEqual([])
})
