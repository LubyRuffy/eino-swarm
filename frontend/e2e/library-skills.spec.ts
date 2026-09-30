import { expect, test, type APIRequestContext } from "@playwright/test"

import { freshConversation, send, waitForIdle } from "./session"

/** A conversation in no project still records a procedure, in the shared
 *  library, and that procedure can be copied into a project afterwards. */

async function createProject(request: APIRequestContext, name: string) {
  const res = await request.post("/api/projects", { data: { name } })
  expect(res.ok()).toBeTruthy()
  const body = (await res.json()) as { project: { id: string; name: string } }
  return body.project
}

test("a conversation outside a project records a skill that can be copied", async ({
  page,
  request,
}) => {
  const project = await createProject(request, `Agent ${Date.now()}`)
  await freshConversation(page)
  await send(page, `Keep a procedure from ${Date.now()}`)
  await waitForIdle(page)

  await page.getByRole("tab", { name: "Memory" }).click()
  await expect(page.getByText("Shared skills")).toBeVisible()
  const card = page.getByTestId("skill-card")
  await expect(card).toBeVisible({ timeout: 60_000 })

  await card.getByRole("button", { name: /Copy skill / }).click()
  await page.getByLabel("Destination project").click()
  await page.getByRole("option", { name: project.name, exact: true }).click()
  await page.getByRole("button", { name: "Copy skill", exact: true }).click()
  await expect(page.getByTestId("skill-copy-notice")).toContainText(project.name)

  await page.getByTestId("dest-projects").click()
  await page.getByRole("button", { name: `Project options for ${project.name}` }).click()
  await page.getByRole("menuitem", { name: "View skills" }).click()
  await expect(page.getByRole("tab", { name: "Memory", selected: true })).toBeVisible()
  await expect(page.getByTestId("skill-card")).toBeVisible()
  await expect(page.getByTestId("skill-origin")).toContainText("shared skill library")
})
