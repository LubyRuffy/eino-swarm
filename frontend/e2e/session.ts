import { expect, type Page } from "@playwright/test"

/** Every spec starts on its own conversation, so one failing run cannot leave
 *  state that breaks the next. */
export async function freshConversation(page: Page) {
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(composer(page)).toBeVisible()
  await expect(composer(page)).toBeEnabled()
}

/** Reload lands on the home page with nothing selected. Tests that still
 *  need the conversation click back into the row that was open. */
export async function reloadOpenConversation(page: Page) {
  const title = (await page.getByTestId("thread-title").innerText()).trim()
  const project = page.getByTestId("thread-project")
  const projectName = (await project.count()) > 0 ? (await project.innerText()).trim() : ""
  const id = await page
    .locator('[data-testid="thread-row"][aria-current="true"]')
    .getAttribute("data-id")
  if (!id) throw new Error("reloadOpenConversation: no conversation selected")
  await page.reload()
  await expect(page.getByRole("heading", { name: "What should we work on?" })).toBeVisible()
  await expect(page.locator('[data-testid="thread-row"][aria-current="true"]')).toHaveCount(0)
  if (projectName) {
    await page.getByRole("button", { name: projectName, exact: true }).click()
  }
  const row = page.locator(`[data-testid="thread-row"][data-id="${id}"]`)
  if ((await row.count()) === 0) {
    const more = page.getByRole("button", { name: "Show more" })
    if ((await more.count()) > 0) await more.first().click()
  }
  await expect(row).toBeVisible()
  await row.getByTestId("row-label").click()
  await expect(page.getByTestId("thread-title")).toHaveText(title)
}

export const composer = (page: Page) => page.getByTestId("composer-input")
export const statusBadge = (page: Page) => page.getByTestId("status-badge")

/** Saved input stays visually quiet. Wait for the current scope's actual
 *  persistence acknowledgement, rather than treating absent UI as a save. */
export async function waitForDraftSaved(page: Page) {
  const text = await composer(page).inputValue()
  const key = await page.evaluate(() => (window as Window & {
    __zwaiRecoveryAttribution?: { draft_key: string }
  }).__zwaiRecoveryAttribution?.draft_key)
  expect(key).toBeTruthy()
  await expect.poll(async () => {
    const acknowledged = await page.evaluate(() => (window as Window & {
      __zwaiRecoveryAttribution?: { draft_key: string; draft_saved: boolean; draft_revision: number }
    }).__zwaiRecoveryAttribution)
    if (!acknowledged || !acknowledged.draft_saved || acknowledged.draft_key !== key) return false
    const response = await page.request.get(`/api/composer/drafts/${encodeURIComponent(key!)}`)
    if (!response.ok()) return false
    const { draft } = await response.json()
    return draft.text === text && draft.revision === acknowledged.draft_revision
  }).toBe(true)
}

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
