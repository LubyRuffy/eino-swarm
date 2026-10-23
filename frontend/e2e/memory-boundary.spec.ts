import { expect, test } from "@playwright/test"
import fs from "node:fs/promises"
import path from "node:path"

// The distinction must be visible where users edit memory, in either language.
test("the memory panel explains the different loading rules in both languages", async ({ page, request }, info) => {
  const { settings } = await (await request.get("/api/settings")).json()
  const name = `Boundary ${Date.now()}`
  const response = await request.post("/api/projects", { data: { name } })
  expect(response.ok()).toBe(true)
  const { project } = await response.json()
  try {
    const configured = await request.put("/api/settings", { data: { ui: { ...settings.ui, locale: "en" } } })
    expect(configured.ok()).toBe(true)
    await page.goto("/")
    await page.evaluate(() => localStorage.setItem("zwai.locale", "en"))
    await page.reload()
    await page.getByRole("button", { name: `Project options for ${name}` }).click()
    await page.getByRole("menuitem", { name: "View skills" }).click()
    await expect(page.getByText(/Without these preferences.*go off track/)).toBeVisible()
    await expect(page.getByText(/Without these procedures.*cannot complete/)).toBeVisible()
    await expect(page.getByText(/Names and descriptions are always listed/)).toBeVisible()
    await page.screenshot({ path: info.outputPath("memory-boundary-en.png"), animations: "disabled" })

    await page.getByRole("button", { name: "App menu" }).click()
    await page.getByRole("menuitem", { name: "Switch language" }).click()
    await expect(page.getByText(/不看会走偏/)).toBeVisible()
    await expect(page.getByText(/不看做不成/)).toBeVisible()
    await expect(page.getByText(/平时只列名称与描述，用到再打开/)).toBeVisible()
    await page.screenshot({ path: info.outputPath("memory-boundary-zh.png"), animations: "disabled" })
  } finally {
    await request.put("/api/settings", { data: { ui: settings.ui } })
    await request.delete(`/api/projects/${project.id}`)
  }
})


test("skill usage and the editable stale threshold are visible in the memory panel", async ({ page, request }, info) => {
  const { settings } = await (await request.get("/api/settings")).json()
  const name = `Usage ${Date.now()}`
  const response = await request.post("/api/projects", { data: { name } })
  expect(response.ok()).toBe(true)
  const { project } = await response.json()
  const oldViewed = new Date(Date.now() - 40 * 86_400_000).toISOString()
  try {
    const configured = await request.put("/api/settings", { data: { memory: { ...settings.memory, skill_stale_days: 30 }, ui: { ...settings.ui, locale: "en" } } })
    expect(configured.ok()).toBe(true)
    for (const skill of ["older-procedure", "current-procedure"]) {
      const dir = path.join(project.memory_dir, "skills", skill)
      await fs.mkdir(dir, { recursive: true })
      await fs.writeFile(path.join(dir, "SKILL.md"), `---\nname: ${skill}\ndescription: When the recorded checks apply\n---\n\n1. Inspect the inputs.\n2. Verify the result.\n`)
    }
    await fs.writeFile(path.join(project.memory_dir, "skills", "older-procedure", "usage.json"), JSON.stringify({ views: 9, last_viewed_at: oldViewed }))
    await page.goto("/")
    await page.evaluate(() => localStorage.setItem("zwai.locale", "en"))
    await page.reload()
    await page.getByRole("button", { name: `Project options for ${name}` }).click()
    await page.getByRole("menuitem", { name: "View skills" }).click()
    const oldCard = page.getByTestId("skill-card").filter({ hasText: "older-procedure" })
    const currentCard = page.getByTestId("skill-card").filter({ hasText: "current-procedure" })
    await expect(oldCard.getByTestId("skill-usage")).toContainText("9 views")
    await expect(oldCard.locator("time")).toHaveAttribute("datetime", oldViewed)
    await expect(oldCard.getByText("Stale candidate", { exact: true })).toBeVisible()
    await expect(currentCard.getByTestId("skill-usage")).toContainText("Never viewed")
    await expect(currentCard.getByText("Stale candidate", { exact: true })).toHaveCount(0)
    await page.screenshot({ path: info.outputPath("memory-usage-en.png"), animations: "disabled" })

    await page.getByRole("button", { name: "Settings", exact: true }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Memory", exact: true }).click()
    const threshold = dialog.getByLabel("Unviewed skill threshold (days)")
    await expect(threshold).toHaveValue("30")
    await threshold.fill("45")
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.memory.skill_stale_days).toBe(45)
    await page.getByRole("button", { name: "Reload memory" }).click()
    await expect(oldCard.getByText("Stale candidate", { exact: true })).toHaveCount(0)
    await page.getByRole("button", { name: "App menu" }).click()
    await page.getByRole("menuitem", { name: "Switch language" }).click()
    await expect(oldCard.getByTestId("skill-usage")).toContainText("查看 9 次")
    await expect(currentCard.getByTestId("skill-usage")).toContainText("从未查看")
    await page.screenshot({ path: info.outputPath("memory-usage-zh.png"), animations: "disabled" })
  } finally {
    await request.put("/api/settings", { data: { memory: settings.memory, ui: settings.ui } })
    await request.delete(`/api/projects/${project.id}`)
  }
})
