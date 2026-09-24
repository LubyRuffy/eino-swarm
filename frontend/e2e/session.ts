import { expect, type Page } from "@playwright/test"

/** Every spec starts on its own conversation, so one failing run cannot leave
 *  state that breaks the next. */
export async function freshConversation(page: Page) {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(composer(page)).toBeVisible()
}

export const composer = (page: Page) => page.getByTestId("composer-input")
export const statusBadge = (page: Page) => page.getByTestId("status-badge")

export async function send(page: Page, text: string) {
  await composer(page).fill(text)
  await composer(page).press("Enter")
  await expect(statusBadge(page)).toContainText("Working")
}

export async function waitForIdle(page: Page) {
  await expect(statusBadge(page)).toContainText("Idle", { timeout: 60_000 })
}

export async function openFiles(page: Page) {
  await page.getByRole("tab", { name: "Files" }).click()
  return page.getByRole("tabpanel").filter({ has: page.getByTestId("file-tree") })
}

export async function filterFiles(page: Page, query: string) {
  const panel = await openFiles(page)
  await panel.getByLabel("Filter files").fill(query)
  return panel
}
