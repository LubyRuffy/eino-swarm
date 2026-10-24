import { expect, test, type Page } from "@playwright/test"
import { reloadOpenConversation } from "./session"

async function freshConversation(page: Page) {
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(page.getByTestId("composer-input")).toBeVisible()
}

test("a completed goal leaves only its history notice after reload and reopening", async ({ page, request }, info) => {
  await freshConversation(page)
  const composer = page.getByTestId("composer-input")
  await composer.fill("/goal keep going")
  await composer.press("Enter")
  await expect(page.getByTestId("status-badge")).toContainText("Working")
  await expect(page.getByTestId("status-badge")).toContainText("Idle", {
    timeout: 60_000,
  })
  const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
  expect(threadID).toBeTruthy()
  const completed = page.getByTestId("transcript").getByText("Standing objective completed.", { exact: true })
  await expect(completed).toHaveCount(1)
  await expect(page.getByTestId("goal-banner")).toHaveCount(0)
  await expect(page.getByTestId("goal-start")).toHaveCount(0)
  await expect(composer).toBeEnabled()
  const { thread } = await (await request.get(`/api/threads/${threadID}`)).json()
  expect(thread).toMatchObject({ goal: "keep going", goal_complete: true })

  await reloadOpenConversation(page)
  await expect(completed).toHaveCount(1)
  await expect(page.getByTestId("goal-banner")).toHaveCount(0)
  await expect(page.getByTestId("status-badge")).toContainText("Idle")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await page.locator(`[data-testid="thread-row"][data-id="${threadID}"]`).getByTestId("row-label").click()
  await expect(completed).toHaveCount(1)
  await expect(page.getByTestId("goal-banner")).toHaveCount(0)
  await expect(page.getByTestId("status-badge")).toContainText("Idle")
  await completed.scrollIntoViewIfNeeded()
  await page.screenshot({ path: info.outputPath("completed-goal-history.png"), animations: "disabled" })

  await composer.fill("/goal another objective")
  await composer.press("Enter")
  await expect(page.getByTestId("goal-banner")).toContainText("another objective")
  await expect(page.getByTestId("goal-banner")).toContainText("Pursuing")
  await expect(completed).toHaveCount(1)
})
