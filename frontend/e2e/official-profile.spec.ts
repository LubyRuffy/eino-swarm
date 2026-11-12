import { expect, test, type Page } from "@playwright/test"

// The offline server has no Gateway profile. This rewrites only the catalog
// answers the composer and Settings read, so the locked name and the switch
// can be exercised without a real account.
async function officialCatalog(page: Page) {
  let presentation = "official"
  await page.route("**/api/meta", async (route) => {
    const response = await route.fetch()
    const body = await response.json()
    await route.fulfill({
      response,
      json: {
        ...body,
        configured: true,
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
    const own = Array.isArray(body.models) ? body.models : []
    await route.fulfill({
      response,
      json: { ...body, models: presentation === "full" ? [...own, official] : [official] },
    })
  })
  await page.route("**/api/settings", async (route) => {
    if (route.request().method() !== "PUT") return route.fallback()
    const patch = route.request().postDataJSON() as { models?: { account_presentation?: string } }
    const next = patch.models?.account_presentation
    if (next === "full" || next === "official") presentation = next
    const current = await page.request.get("/api/settings")
    const stored = await current.json()
    if (patch.models) stored.settings.models = { ...stored.settings.models, ...patch.models }
    await route.fulfill({ json: stored })
  })
}

test("an official profile shows one name until every model is requested", async ({ page }) => {
  await officialCatalog(page)
  await page.goto("/")
  const picker = page.getByRole("button", { name: "Model" })
  await expect(picker).toContainText("Partner model")
  await expect(picker).not.toContainText("vendor-secret-model")
  await picker.click()
  await expect(page.getByRole("combobox", { name: "Search models" })).toHaveCount(0)

  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Models" }).click()
  await expect(dialog.getByRole("combobox", { name: "Model list" })).toHaveText("Official model")
  await expect(dialog.getByText("Your own model services")).toHaveCount(0)
  await expect(dialog.getByText("vendor-secret-model")).toHaveCount(0)
  await dialog.getByRole("combobox", { name: "Model list" }).click()
  await page.getByRole("option", { name: "All models" }).click()
  const warning = page.getByRole("dialog", { name: "Switch to all models?" })
  await expect(warning).toContainText("online capabilities")
  await warning.getByRole("button", { name: "Stay on the official model" }).click()
  await expect(dialog.getByRole("combobox", { name: "Model list" })).toHaveText("Official model")
  await expect(dialog.getByText("Your own model services")).toHaveCount(0)
  await dialog.getByRole("combobox", { name: "Model list" }).click()
  await page.getByRole("option", { name: "All models" }).click()
  await page.getByRole("dialog", { name: "Switch to all models?" }).getByRole("button", { name: "Switch" }).click()
  await expect(dialog.getByText("Your own model services")).toBeVisible()
})
