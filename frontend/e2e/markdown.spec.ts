import { expect, test, type Page } from "@playwright/test"

/** Highlight and math live on the scripted answer. Kept out of
 *  conversation.spec.ts so that file stays under 1000 lines. */
async function freshConversation(page: Page) {
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(page.getByTestId("composer-input")).toBeVisible()
}

test("paints a tagged code fence, inline math, and a contained gfm table", async ({
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
  const tableWrap = transcript.getByTestId("markdown-table")
  await expect(tableWrap).toBeVisible()
  await expect(tableWrap.getByRole("table")).toBeVisible()
  const wrapBox = await tableWrap.boundingBox()
  const scrollerBox = await transcript.boundingBox()
  expect(wrapBox).toBeTruthy()
  expect(scrollerBox).toBeTruthy()
  expect(wrapBox!.x + wrapBox!.width).toBeLessThanOrEqual(
    scrollerBox!.x + scrollerBox!.width + 1,
  )
  const pane = page.getByTestId("side-panel")
  if (await pane.isVisible()) {
    const paneBox = await pane.boundingBox()
    expect(paneBox).toBeTruthy()
    expect(wrapBox!.x + wrapBox!.width).toBeLessThanOrEqual(paneBox!.x + 1)
  }
})

test("renders the admitted manager answer as markdown", async ({ page }) => {
  await freshConversation(page)
  await page.getByTestId("composer-input").fill(
    "Look at this from two angles and merge the findings",
  )
  await page.getByTestId("composer-input").press("Enter")
  const transcript = page.getByTestId("transcript")
  // Manager prose is published after checking scheduling claims. Its first
  // visible body must already be formatted, rather than a raw Markdown dump.
  await expect(transcript.getByRole("heading", { name: "Result" })).toBeVisible({ timeout: 60_000 })
  await expect(transcript.getByText(/^## Result/)).toHaveCount(0)
  await expect(page.getByTestId("status-badge")).toContainText("Idle", {
    timeout: 60_000,
  })
  await expect(transcript.getByRole("heading", { name: "Result" })).toBeVisible()
})
