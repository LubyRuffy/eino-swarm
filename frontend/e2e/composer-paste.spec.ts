import { expect, test } from "@playwright/test"

import { composer, freshConversation } from "./session"

test("a paste into an empty composer lands once", async ({ page, context }) => {
  await freshConversation(page)
  await context.grantPermissions(["clipboard-read", "clipboard-write"], {
    origin: new URL(page.url()).origin,
  })
  const box = composer(page)
  await box.click()
  await page.evaluate(() => navigator.clipboard.writeText("alpha-paste"))
  await box.press("ControlOrMeta+v")
  await expect(box).toHaveValue("alpha-paste")
  await page.evaluate(() => navigator.clipboard.writeText("-more"))
  await box.press("ControlOrMeta+v")
  await expect(box).toHaveValue("alpha-paste-more")
})
