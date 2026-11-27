import { expect, test } from "@playwright/test"

test("legacy LOCAL voice settings save while policy fields are absent", async ({ page, request }) => {
  const original = await (await request.get("/api/settings")).json()
  await page.route("**/api/meta", async route => {
    const response = await route.fetch()
    const body = await response.json()
    delete body.model_policy
    await route.fulfill({ response, json: body })
  })
  await page.route("**/api/settings", async route => {
    const response = await route.fetch()
    const body = await response.json()
    delete body.settings.model_policy
    await route.fulfill({ response, json: body })
  })
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    const dialog = page.getByRole("dialog", { name: "Settings" })
    await dialog.getByRole("tab", { name: "Models", exact: true }).click()
    await dialog.getByRole("button", { name: "Configure Voice input", exact: true }).click()
    const input = page.getByRole("dialog", { name: "Voice input", exact: true })
    const enabled = input.getByRole("switch", { name: "Voice input", exact: true })
    if (original.settings.speech.input.enabled) {
      await expect(input.getByLabel("API base URL", { exact: true })).toBeVisible()
    }
    await enabled.setChecked(!original.settings.speech.input.enabled)
    if (!original.settings.speech.input.enabled) {
      await expect(input.getByLabel("API base URL", { exact: true })).toBeVisible()
    }
    await expect.poll(async () => (await (await request.get("/api/settings")).json()).settings.speech.input.enabled)
      .toBe(!original.settings.speech.input.enabled)
    await page.reload()
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    await page.getByRole("dialog").getByRole("tab", { name: "Models", exact: true }).click()
    await page.getByRole("button", { name: "Configure Voice input", exact: true }).click()
    await expect(enabled)
      .toHaveAttribute("aria-checked", String(!original.settings.speech.input.enabled))
  } finally {
    await request.put("/api/settings", { data: { speech: original.settings.speech } })
  }
})
