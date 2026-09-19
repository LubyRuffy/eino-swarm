import { expect, test, type Page } from "@playwright/test"

/** Highlight and math live on the scripted answer. Kept out of
 *  conversation.spec.ts so that file stays under 1000 lines. */
async function freshConversation(page: Page) {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(page.getByTestId("composer-input")).toBeVisible()
}

test("paints a tagged code fence and inline math in the scripted answer", async ({
  page,
}) => {
  await freshConversation(page)
  await page.getByTestId("composer-input").fill(
    "Look at this from two angles and merge the findings",
  )
  await page.getByTestId("composer-input").press("Enter")
  await expect(page.getByTestId("status-badge")).toContainText("Idle", {
    timeout: 60_000,
  })
  const transcript = page.getByTestId("transcript")
  await expect(transcript.getByRole("button", { name: "Copy code" })).toBeVisible()
  await expect(transcript.locator(".katex").first()).toBeVisible()
  await expect(
    transcript.locator(".text-syntax-keyword", { hasText: "func" }),
  ).toBeVisible()
})
