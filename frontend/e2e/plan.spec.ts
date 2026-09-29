import { expect, test } from "@playwright/test"

import { composer, freshConversation, statusBadge, waitForIdle } from "./session"

test("plan command drafts then implements", async ({ page }) => {
  await freshConversation(page)
  await composer(page).fill("/plan inspect then change")
  await composer(page).press("Enter")
  await expect(page.getByTestId("plan-banner")).toContainText("Planning")
  await expect(statusBadge(page)).toContainText("Working")
  const ask = page.getByTestId("ask-card")
  await expect(ask).toBeVisible({ timeout: 30_000 })
  await expect(ask).toContainText("Your answer needed")
  await expect(ask.getByTestId("ask-mark")).toBeVisible()
  await expect(ask).not.toHaveClass(/animate-ask-ring/)
  await expect(ask.locator("[data-testid=ask-mark] .animate-ping")).toHaveCount(0)
  await expect(statusBadge(page)).toContainText("Your turn")
  await expect(
    statusBadge(page).locator("[data-testid=ask-mark] .animate-ping"),
  ).toHaveCount(1)
  expect(await ask.evaluate((el) => {
    const col = el.closest(".content-column")
    if (!(col instanceof HTMLElement)) return false
    const cw = col.getBoundingClientRect().width
    return cw > 512 && Math.abs(el.getBoundingClientRect().width - cw) < 2
  })).toBe(true)
  await page.getByTestId("ask-option-safer").click()
  await page.getByTestId("ask-submit").click()
  await waitForIdle(page)
  await expect(page.getByTestId("plan-text")).toContainText("# Plan")
  await expect(page.getByTestId("transcript")).toContainText("Plan updated.")
  await page.getByTestId("plan-implement").click()
  await expect(statusBadge(page)).toContainText("Working")
  await waitForIdle(page)
  await expect(page.getByTestId("plan-banner")).toHaveCount(0)
  await expect(page.getByTestId("transcript")).toContainText(
    "The human accepted the plan. Execute it.",
  )
})
