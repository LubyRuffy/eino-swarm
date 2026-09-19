import { expect, test, type Page } from "@playwright/test"

async function freshConversation(page: Page) {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(page.getByTestId("composer-input")).toBeVisible()
}

test("Start on a completed goal reopens pursuit", async ({ page }) => {
  await freshConversation(page)
  const composer = page.getByTestId("composer-input")
  await composer.fill("/goal keep going")
  await composer.press("Enter")
  await expect(page.getByTestId("status-badge")).toContainText("Idle", {
    timeout: 60_000,
  })
  await expect(page.getByTestId("goal-banner")).toContainText("Done")
  await page.getByTestId("goal-start").click()
  await expect(page.getByTestId("status-badge")).toContainText("Working")
})
