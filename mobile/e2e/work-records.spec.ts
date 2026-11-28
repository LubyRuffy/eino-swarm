import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"

test("paired phones open the work graph and honor the PC setting", async ({ page }, info) => {
  await page.addInitScript(() => localStorage.setItem("zwai.phone.locale", "en"))
  await page.goto("/?mock=1&tick=0&graph=on")
  const entry = page.getByRole("button", { name: "Work records", exact: true })
  await entry.click()
  const panel = page.getByTestId("graph-panel")
  await expect(panel).toContainText("3 subjects · 2 links")
  // The real force-graph draws on a canvas; a blank one means the shared view broke on the phone.
  const canvas = panel.locator("canvas").first()
  await expect(canvas).toBeVisible()
  const todos = panel.getByRole("list", { name: "To-dos" })
  await expect(todos.getByRole("listitem")).toHaveCount(2)
  await expect(todos.getByRole("button", { name: /Identify the organisation/ })).toBeVisible()
  await todos.getByRole("button", { name: /List its domains/ }).click()
  await expect(todos.getByRole("button", { name: /List its domains/ })).toHaveAttribute("aria-pressed", "true")
  await writeFile(info.outputPath("phone-work-records.png"), await page.screenshot(), { mode: 0o600 })

  await page.getByRole("button", { name: "Back", exact: true }).click()
  await expect(page.getByTestId("transcript")).toBeVisible()
  await entry.click()
  await expect(panel).toContainText("3 subjects")

  await page.evaluate(() => {
    const url = new URL(location.href)
    url.searchParams.set("graph", "off")
    history.replaceState(null, "", url)
  })
  await expect(panel).toHaveCount(0)
  await expect(entry).toHaveCount(0)
  await expect(page.getByTestId("transcript")).toBeVisible()
})

test("a graph too large for the phone says so instead of drawing half of it", async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem("zwai.phone.locale", "en"))
  await page.goto("/?mock=1&tick=0&graph=large")
  await page.getByRole("button", { name: "Work records", exact: true }).click()
  await expect(page.getByRole("alert")).toContainText("too large for the phone")
})
