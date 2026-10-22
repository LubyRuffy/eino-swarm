import { expect, test, type Page } from "@playwright/test"

test.use({ trace: "retain-on-failure" })
const failures = new WeakMap<Page, string[]>()
test.beforeEach(async ({ page }) => {
  const rows: string[] = []
  failures.set(page, rows)
  page.on("pageerror", (error) => rows.push(String(error)))
  page.on("requestfailed", (request) => rows.push(`${request.url()}: ${request.failure()?.errorText}`))
})
test.afterEach(async ({ page }, info) => {
  await info.attach("browser-errors", { body: JSON.stringify(failures.get(page) ?? []), contentType: "application/json" })
  if (info.status !== info.expectedStatus) await page.screenshot({ path: info.outputPath("first-failure.png") })
})

for (const language of [
  { locale: "en", agents: "Agents (1)", back: "Back", running: "running", failed: "failed", cause: /engine restart/i, recovery: /resum/i },
  { locale: "zh", agents: "子 Agent (1)", back: "返回", running: "运行中", failed: "失败", cause: /引擎重启/, recovery: /已恢复/ },
]) {
  test(`phone retains the interrupted output while the same worker continues (${language.locale})`, async ({ page }, info) => {
    await page.addInitScript((locale) => localStorage.setItem("zwai.phone.locale", locale), language.locale)
    await page.goto("/?mock=1&tick=0&history=generation-interruption")
    const agents = page.getByRole("button", { name: language.agents, exact: true })
    await expect(agents).toBeVisible({ timeout: 20_000 })
    for (let replay = 0; replay < 2; replay++) {
      await agents.click()
      const roster = page.getByTestId("agent-roster")
      const worker = roster.getByRole("button", { name: /worker-1/ })
      await expect(worker).toHaveCount(1)
      await expect(worker).toContainText(language.running)
      await expect(worker).not.toContainText(language.failed)
      await worker.click()
      const transcript = page.getByTestId("transcript")
      const notice = transcript.getByTestId("generation-notice")
      await expect(notice).toHaveCount(1)
      await expect(notice).toContainText(language.cause)
      await expect(notice).toContainText(language.recovery)
      const thoughts = transcript.getByTestId("work-fold")
      await expect(thoughts).toHaveCount(2)
      await expect(thoughts.first().locator(".animate-spin")).toHaveCount(0)
      await expect(transcript.locator(".text-destructive")).toHaveCount(0)
      await thoughts.first().click()
      await thoughts.last().click()
      await expect(transcript).toContainText("Interrupted thought before restart.")
      await expect(transcript).toContainText("New thought after recovery")
      await page.screenshot({ path: info.outputPath(`phone-generation-${language.locale}-${replay}.png`), animations: "disabled" })
      await page.getByRole("button", { name: language.back, exact: true }).click()
      await page.getByRole("button", { name: language.back, exact: true }).click()
      if (replay === 0) await page.reload()
    }
  })
}
