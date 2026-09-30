import { expect, test } from "@playwright/test"

test("a rejected phone question keeps Other text and can be submitted again", async ({ page }) => {
  await page.goto("/?mock=1&ask=1&askfail=1&tick=0")
  const card = page.getByTestId("ask-card")
  await expect(card).toBeVisible()
  await card.getByRole("button", { name: "其他" }).click()
  await card.getByRole("textbox", { name: "其他" }).fill("After review")
  await card.getByTestId("ask-submit").click()
  await expect(card.getByRole("alert")).toContainText("PC rejected the answer")
  await expect(card.getByRole("textbox", { name: "其他" })).toHaveValue("After review")
  await card.getByTestId("ask-submit").click()
  await expect(card).toHaveCount(0)
  await expect(page.getByTestId("transcript")).toContainText("The answer was received.")
})
