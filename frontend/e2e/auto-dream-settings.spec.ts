import { expect, test } from "@playwright/test"

import { freshConversation } from "./session"

test("automatic corrections start enabled and preserve an explicit opt-out", async ({ page, request }) => {
  const { settings } = await (await request.get("/api/settings")).json()
  expect(settings.memory.auto_dream).toBe(true)

  try {
    await freshConversation(page)
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Memory" }).click()
    const toggle = dialog.getByRole("switch", { name: "Automatic memory corrections" })
    await expect(toggle).toBeChecked()
    await toggle.click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    await expect.poll(async () => {
      const { settings: saved } = await (await request.get("/api/settings")).json()
      return saved.memory.auto_dream
    }).toBe(false)

    await page.reload()
    await page.getByRole("button", { name: "Settings" }).click()
    await dialog.getByRole("tab", { name: "Memory" }).click()
    await expect(toggle).not.toBeChecked()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
  } finally {
    expect((await request.put("/api/settings", { data: { memory: settings.memory } })).ok()).toBe(true)
  }
})
