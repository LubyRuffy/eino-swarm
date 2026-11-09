import { expect, test } from "@playwright/test"
import { mkdir, writeFile } from "node:fs/promises"
import path from "node:path"
import { fileURLToPath } from "node:url"

const bundle = fileURLToPath(new URL("./fixtures/portable-skill.zip", import.meta.url))

test("install, preview, and remove an external skill independently of project memory", async ({ page, request }) => {
  const projectName = `Skill packages ${Date.now()}`
  const created = await request.post("/api/projects", { data: { name: projectName, memory_enabled: false } })
  expect(created.ok()).toBeTruthy()
  const { project } = await created.json() as { project: { id: string; memory_dir: string; memory_enabled: boolean } }
  expect(project.memory_enabled).toBe(false)
  const recordedDir = path.join(project.memory_dir, "skills", "fixture-audit")
  await mkdir(recordedDir, { recursive: true })
  await writeFile(path.join(recordedDir, "SKILL.md"),
    "---\nname: fixture-audit\ndescription: Recorded separately.\n---\n\nThe project's recorded procedure.\n")

  const detailReads: string[] = []
  const previewFetches: string[] = []
  const detailPath = `/api/projects/${project.id}/installed-skills/fixture-audit`
  page.on("request", (req) => {
    const url = new URL(req.url())
    if (req.method() === "GET" && url.pathname === detailPath) detailReads.push(req.url())
    if (url.searchParams.has("skill-preview-probe")) previewFetches.push(req.url())
  })

  const openSkills = async () => {
    await page.getByRole("button", { name: `Project options for ${projectName}` }).click()
    await page.getByRole("menuitem", { name: "External skills" }).click()
  }

  await page.goto("/")
  await openSkills()
  const installed = page.getByRole("dialog", { name: "External skills" })
  const install = installed.getByRole("button", { name: "Install external skill" })
  await expect(install).toBeEnabled()

  const choose = async () => {
    await install.click()
    const dialog = page.getByRole("dialog", { name: "Install external skill" })
    await expect(dialog.getByText(/only install skills from sources you trust/i)).toBeVisible()
    const confirm = dialog.getByRole("button", { name: "Install", exact: true })
    await expect(confirm).toBeDisabled()
    await dialog.getByLabel("Skill ZIP file").setInputFiles(bundle)
    await confirm.click()
    return dialog
  }

  await choose()
  const card = installed.getByTestId("installed-skill-card")
  await expect(card.getByText("fixture-audit", { exact: true })).toBeVisible()
  await expect(installed.getByRole("status")).toContainText("Installed fixture-audit")
  expect(detailReads).toHaveLength(0)
  await card.getByRole("button", { name: /fixture-audit Verify a packaged procedure/ }).click()
  await expect(card.getByRole("heading", { name: "Package checklist" })).toBeVisible()
  expect(detailReads).toHaveLength(1)
  await expect(card.getByText("references/checklist.md")).toBeVisible()
  const preview = card.getByTestId("skill-body")
  await expect(preview.getByRole("link")).toHaveCount(0)
  await expect(preview.getByRole("img")).toHaveCount(0)
  expect(previewFetches).toHaveLength(0)

  const conflict = await choose()
  await expect(conflict.getByRole("alert")).toBeVisible()
  await conflict.getByRole("button", { name: "Cancel", exact: true }).click()
  await expect(card).toHaveCount(1)

  await page.reload()
  await openSkills()
  await expect(card.getByText("fixture-audit", { exact: true })).toBeVisible()
  await expect(card.getByRole("heading", { name: "Package checklist" })).toHaveCount(0)
  const remove = card.getByRole("button", { name: "Remove external skill fixture-audit" })
  await remove.click()
  const removal = page.getByRole("dialog", { name: /Remove external skill fixture-audit/ })
  await removal.getByRole("button", { name: "Cancel", exact: true }).click()
  await expect(card).toBeVisible()
  await remove.click()
  await removal.getByRole("button", { name: "Remove skill" }).click()
  await expect(card).toHaveCount(0)
  await expect(installed.getByRole("status")).toContainText("Removed fixture-audit")

  const recorded = await request.get(`/api/projects/${project.id}/skills/fixture-audit`)
  expect(recorded.ok()).toBeTruthy()
  expect((await recorded.json()).skill.body).toBe("The project's recorded procedure.")
})
