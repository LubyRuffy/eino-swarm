import { expect, test } from "@playwright/test"

test("a project backup joins a directory that already has another project", async ({ page, request }) => {
  const created = await request.post("/api/projects", { data: { name: "Carried" } })
  expect(created.ok()).toBeTruthy()
  const project = (await created.json()).project
  const archive = await request.post("/api/backup", {
    data: { kind: "project", project_id: project.id },
  })
  expect(archive.ok()).toBeTruthy()
  const bytes = await archive.body()
  expect(bytes.subarray(0, 2).toString()).toBe("PK")
  expect((await request.delete(`/api/projects/${project.id}`)).ok()).toBeTruthy()
  expect((await request.post("/api/projects", { data: { name: "Already" } })).ok()).toBeTruthy()

  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Backup" }).click()
  await dialog.locator("#backup-file").setInputFiles({
    name: "project.zwai-backup",
    mimeType: "application/octet-stream",
    buffer: bytes,
  })
  await dialog.getByRole("button", { name: "Add project from backup" }).click()
  await expect(dialog.getByRole("status")).toContainText("Restore finished")
  await page.keyboard.press("Escape")
  await expect(page.getByTestId("project-row").filter({ hasText: "Carried" })).toBeVisible()
  await expect(page.getByTestId("project-row").filter({ hasText: "Already" })).toBeVisible()
})
