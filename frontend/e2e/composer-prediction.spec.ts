import { expect, test } from "@playwright/test"

import { composer, freshConversation, send, statusBadge, waitForIdle } from "./session"

const suggestion = "Continue from the latest reply"

test("a finished reply offers a next step that Tab inserts", async ({ page }) => {
  await freshConversation(page)
  await send(page, "hello")
  await waitForIdle(page)
  const input = composer(page)
  await expect(input).toHaveAttribute("placeholder", suggestion)
  await expect(input).toHaveAttribute("data-prediction", "true")
  await input.press("Tab")
  await expect(input).toHaveValue(suggestion)
  await expect(statusBadge(page)).toContainText("Idle")
  await input.fill("")
  await expect(input).toHaveAttribute("data-prediction", "true")
  await input.press("Escape")
  await expect(input).not.toHaveAttribute("data-prediction", "true")
})
