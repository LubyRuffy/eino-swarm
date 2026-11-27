import { expect, test, type Page } from "@playwright/test"

// The offline server has no Gateway profile. This rewrites only the catalog
// answers the composer and Settings read, so server-owned managed policy
// can be exercised without a real account.
async function officialCatalog(page: Page) {
  const presentation = "full"
  const policy = { mode: "gateway", configurable: false, ready: true, scope: "gateway:managed" }
  await page.route("**/api/meta", async (route) => {
    const response = await route.fetch()
    const body = await response.json()
    await route.fulfill({
      response,
      json: {
        ...body,
        configured: true,
        model_policy: policy,
        account_models: {
          state: "ready",
          models: 1,
          label: "Partner model",
          username: "account",
          source_id: "gateway:0123456789abcdef0123456789abcdef",
          profile: {
            presentation,
            instruction: true,
            restrict_tools: true,
            tools: ["read", "exec"],
          },
        },
      },
    })
  })
  await page.route("**/api/models", async (route) => {
    const response = await route.fetch()
    const body = await response.json()
    const official = {
      id: "gateway:0123456789abcdef0123456789abcdef\taccount",
      provider_id: "gateway:0123456789abcdef0123456789abcdef",
      provider_label: "Partner model",
      label: "Partner model",
      model: "account",
      ready: true,
      default: true,
      default_follows_setting: true,
      source: "account",
    }
    await route.fulfill({
      response,
      json: { ...body, models: [official] },
    })
  })
  await page.route("**/api/settings", async route => {
    const response = await route.fetch()
    const body = await response.json()
    await route.fulfill({ json: { ...body, settings: { ...body.settings, model_policy: policy, models: { ...body.settings.models, account_presentation: "full", providers: [] } } } })
  })
}

test("a managed official profile exposes feature switches without exposing model configuration", async ({ page }) => {
  await officialCatalog(page)
  await page.goto("/")
  await expect(page.getByLabel("Model", { exact: true })).toHaveCount(0)
  await page.getByRole("button", { name: "Settings", exact: true }).click()
  const dialog = page.getByRole("dialog", { name: "Settings", exact: true })
  await dialog.getByRole("tab", { name: "Models", exact: true }).click()
  await expect(dialog.getByRole("heading", { name: "Optional AI features", exact: true })).toBeVisible()
  await expect(dialog.getByRole("heading", { name: "Functional models", exact: true })).toHaveCount(0)
  await expect(dialog.getByRole("combobox", { name: "Model list" })).toHaveCount(0)
  await expect(dialog.getByText("Your own model services")).toHaveCount(0)
  await expect(dialog.getByText("vendor-secret-model")).toHaveCount(0)
  await dialog.getByRole("button", { name: "Configure Name conversations automatically", exact: true }).click()
  const naming = page.getByRole("dialog", { name: "Name conversations automatically", exact: true })
  await expect(naming.getByRole("switch", { name: "Name conversations automatically", exact: true })).toBeVisible()
  await expect(naming.getByRole("combobox")).toHaveCount(0)
})
