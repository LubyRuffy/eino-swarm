import { expect, test } from "@playwright/test"

test("background completion survives reload and a folded project until its result is opened", async ({ page, request }, info) => {
  const name = `Unread project ${Date.now()}`
  const project = (await (await request.post("/api/projects", { data: { name } })).json()).project
  const thread = (await (await request.post("/api/threads", { data: { title: "Background result", project_id: project.id } })).json()).thread
  const started = await request.post(`/api/threads/${thread.id}/turns`, { data: { text: "Inspect the workspace and report a concise result" } })
  expect(started.status()).toBe(202)
  await expect.poll(async () => (await (await request.get(`/api/threads/${thread.id}`)).json()).thread.unread,
    { timeout: 60_000 }).toBe(true)
  await page.goto("/")
  const projectRow = page.locator(`[data-testid="project-row"][data-id="${project.id}"]`)
  const projectMark = projectRow.getByTestId("project-unread-completion")
  await expect(projectMark).toHaveAccessibleName("1 unread completed task")
  await expect(projectMark).not.toHaveClass(/animate-breathe/)
  await page.screenshot({ path: info.outputPath("folded-unread.png"), animations: "disabled" })
  await page.reload()
  await expect(projectMark).toBeVisible()
  await projectRow.getByRole("button", { name, exact: true }).click()
  const row = page.locator(`[data-testid="thread-row"][data-id="${thread.id}"]`)
  await expect(row.getByLabel("Completed, unread")).toBeVisible()
  await row.getByTestId("row-label").click()
  await expect(page.getByTestId("transcript")).toBeVisible()
  await expect(row.getByLabel("Completed, unread")).toHaveCount(0)
  await expect.poll(async () => (await (await request.get(`/api/threads/${thread.id}`)).json()).thread.unread).toBe(false)
  await page.screenshot({ path: info.outputPath("opened-read.png"), animations: "disabled" })
  await page.reload()
  await expect(projectMark).toHaveCount(0)
})
