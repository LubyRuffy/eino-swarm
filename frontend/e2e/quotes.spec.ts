import { expect, test, type Page } from "@playwright/test"

async function freshConversation(page: Page) {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(composer(page)).toBeVisible()
}

const composer = (page: Page) => page.getByTestId("composer-input")
const statusBadge = (page: Page) => page.getByTestId("status-badge")

async function send(page: Page, text: string) {
  await composer(page).fill(text)
  await composer(page).press("Enter")
  await expect(statusBadge(page)).toContainText("Working")
}

async function waitForIdle(page: Page) {
  await expect(statusBadge(page)).toContainText("Idle", { timeout: 60_000 })
}

test("quotes selected transcript text into the next message", async ({ page }) => {
  await freshConversation(page)
  const first = "First task: outline the work"
  await send(page, first)
  await waitForIdle(page)

  const bubble = page.getByTestId("transcript").getByText(first, { exact: true })
  await bubble.selectText()
  await page.getByRole("menuitem", { name: "Add to chat" }).click()
  await expect(page.getByLabel("1 annotation")).toBeVisible()
  await expect(page.getByTestId("quote-snippet")).toContainText("Selected text:")
  await expect(page.getByTestId("quote-snippet")).toContainText(first)
  await expect(page.getByRole("button", { name: "Edit selected text 1" })).toBeVisible()
  await expect(page.getByRole("button", { name: "Remove selected text 1" })).toBeVisible()

  const follow = "Second task: tighten that outline"
  await composer(page).fill(follow)
  await composer(page).press("Enter")
  await expect(statusBadge(page)).toContainText("Working")
  await waitForIdle(page)

  const last = page.getByTestId("user-message").last()
  await expect(last).toContainText("Selected text:")
  await expect(last).toContainText(first)
  await expect(last).toContainText(follow)
  await expect(last).not.toContainText("<selected_text>")
  await expect(last).not.toContainText("<user_request>")
  await expect(page.getByLabel("1 annotation")).toHaveCount(0)
})

test("quotes selected text while a turn is still streaming", async ({ page }) => {
  await freshConversation(page)
  const first = "First task: outline the work"
  await send(page, first)
  await waitForIdle(page)

  await send(page, "Second task: keep writing")
  const bubble = page.getByTestId("transcript").getByText(first, { exact: true })
  await bubble.selectText()
  const add = page.getByRole("menuitem", { name: "Add to chat" })
  await expect(add).toBeVisible()

  // A live thought's inner scroll and auto-follow used to flash the pill
  // then hide it on the next token.
  await expect(page.getByTestId("thought-scroll")).toBeVisible({ timeout: 15_000 })
  await expect(add).toBeVisible()
  await add.click()
  await expect(page.getByLabel("1 annotation")).toBeVisible()
})
