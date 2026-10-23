import { expect, test } from "@playwright/test"

// The distinction must be visible where users edit memory, in either language.
test("the memory panel explains the different loading rules in both languages", async ({ page, request }, info) => {
  const name = `Boundary ${Date.now()}`
  const response = await request.post("/api/projects", { data: { name } })
  expect(response.ok()).toBe(true)
  const { project } = await response.json()
  try {
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
    await request.delete(`/api/projects/${project.id}`)
  }
})
