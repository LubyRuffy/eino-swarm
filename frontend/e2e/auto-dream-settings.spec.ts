import { expect, test } from "@playwright/test"

test("project skill verification starts on and keeps an explicit opt-out after reload", async ({ page, request }, testInfo) => {
  const readSettings = async () => {
    const response = await request.get("/api/settings")
    expect(response.ok()).toBe(true)
    return (await response.json()).settings
  }
  const original = await readSettings()
  expect(original.memory.auto_dream).toBe(true)

  const dialog = page.getByRole("dialog")
  const toggle = dialog.getByRole("switch", { name: "Verify and correct project skills" })
  const openMemory = async () => {
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    await dialog.getByRole("tab", { name: "Memory", exact: true }).click()
  }
  const closeSettings = async () => {
    await dialog.getByRole("button", { name: "Back to app", exact: true }).click()
    await expect(dialog).toBeHidden()
  }

  try {
    // Bind the server's locale before loading the fresh browser context.
    const locale = await request.put("/api/settings", { data: { ui: { locale: "en" } } })
    expect(locale.ok()).toBe(true)
    await page.goto("/")
    await openMemory()
    await expect(toggle).toBeEnabled()
    await expect(toggle).toBeChecked()
    await toggle.click()
    await closeSettings()
    await expect.poll(async () => (await readSettings()).memory.auto_dream).toBe(false)

    await page.reload()
    await openMemory()
    await expect(toggle).not.toBeChecked()
    await page.screenshot({ path: testInfo.outputPath("verified-after-reload.png") })
    await closeSettings()
  } finally {
    await page.close()
    const restored = await request.put("/api/settings", {
      data: { memory: original.memory, ui: { locale: original.ui.locale } },
    })
    expect(restored.ok()).toBe(true)
    const current = await readSettings()
    expect(current.memory).toEqual(original.memory)
    expect(current.ui.locale).toBe(original.ui.locale)
  }
})
