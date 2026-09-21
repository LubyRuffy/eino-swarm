import { expect, test, type Page } from "@playwright/test"

/** User-view folding lives here so conversation.spec.ts stays under 1000. */
async function freshConversation(page: Page) {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(page.getByTestId("composer-input")).toBeVisible()
}

test("collapses live thinking and tools behind one row", async ({ page }) => {
  await freshConversation(page)
  await page.getByTestId("composer-input").fill(
    "Look at this from two angles and merge the findings",
  )
  await page.getByTestId("composer-input").press("Enter")
  const fold = page.getByTestId("work-fold")
  await expect(fold).toBeVisible({ timeout: 15_000 })
  await expect(page.getByTestId("thought-scroll")).toBeHidden()
  await fold.click()
  await expect(page.getByTestId("thought-scroll")).toBeVisible()
  await fold.click()
  await expect(page.getByTestId("thought-scroll")).toBeHidden()
  await expect(fold).toHaveAttribute("aria-expanded", "false")
})
