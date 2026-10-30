import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"

for (const history of ["goal-paused", "goal-blocked", "goal-idle"]) {
test(`${history}: a completed manual reply keeps automatic pursuit paused until Start`, async ({ page }, info) => {
  await page.addInitScript(() => localStorage.setItem("zwai.phone.locale", "en"))
  await page.goto(`/?mock=1&tick=0&history=${history}`)
  await page.getByRole("button", { name: "Back", exact: true }).click()
  await page.getByRole("button", { name: "Open Trim the layout pass", exact: true }).first().click()
  await expect(page.getByTestId("goal-banner")).toContainText("Auto-run paused")
  await expect(page.getByTestId("goal-age")).toContainText("Goal age:")
  await expect(page.getByTestId("goal-reason")).toContainText("Auto-run pause reason:")
  await expect(page.getByRole("button", { name: "Start goal", exact: true })).toBeVisible()
  const completedBefore = await page.getByTestId("turn-boundary").count()
  await page.getByLabel("Message", { exact: true }).fill("Describe the current state.")
  await page.locator("form").getByRole("button", { name: "Send", exact: true }).click()
  await expect(page.getByTestId("transcript").getByText("Describe the current state.", { exact: true })).toBeVisible()
  await expect(page.getByTestId("turn-boundary")).toHaveCount(completedBefore + 1)
  await expect(page.getByTestId("goal-banner")).toContainText("Auto-run paused")
  await expect(page.getByTestId("goal-age")).toContainText("Goal age:")
  await expect(page.getByRole("button", { name: "Start goal", exact: true })).toBeVisible()
  await writeFile(info.outputPath("paused-after-send.png"), await page.screenshot({ animations: "disabled" }), { mode: 0o600 })
  await page.getByRole("button", { name: "Start goal", exact: true }).click()
  await expect(page.getByTestId("goal-banner")).toContainText("Pursuing")
  await expect(page.getByRole("button", { name: "Start goal", exact: true })).toHaveCount(0)
  await writeFile(info.outputPath("pursuing-after-start.png"), await page.screenshot({ animations: "disabled" }), { mode: 0o600 })
})
}
