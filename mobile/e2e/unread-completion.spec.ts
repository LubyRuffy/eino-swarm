import { expect, test } from "@playwright/test"

for (const language of [
  { locale: "en", back: "Back", completed: "Completed unread", state: "Unread completion", open: "Open Sweep the unused exports", running: "In progress" },
  { locale: "zh", back: "返回", completed: "已完成未读", state: "完成未读", open: "打开 Sweep the unused exports", running: "进行中" },
]) {
  test(`completion stays above folders until explicitly read (${language.locale})`, async ({ page }, info) => {
    await page.addInitScript((locale) => localStorage.setItem("zwai.phone.locale", locale), language.locale)
    await page.goto("/?mock=1&tick=0&history=unread-completion")
    const back = page.getByRole("button", { name: language.back, exact: true })
    await back.click()
    const attention = page.getByTestId("completion-attention")
    await expect(attention).toBeVisible()
    await expect(attention).toContainText(language.completed)
    const badge = attention.getByTestId("row-state")
    await expect(badge).toHaveAttribute("data-state", "unread")
    await expect(badge).toHaveText(language.state)
    await expect(badge.locator(".motion-safe\\:animate-pulse")).toHaveCount(0)
    await expect(attention).not.toContainText(language.running)
    await page.screenshot({ path: info.outputPath(`completion-unread-${language.locale}.png`), animations: "disabled" })
    await attention.getByRole("button", { name: language.open, exact: true }).click()
    await expect(page.getByTestId("transcript")).toBeVisible()
    await page.screenshot({ path: info.outputPath(`completion-open-${language.locale}.png`), animations: "disabled" })
    await back.click()
    await expect(attention).toHaveCount(0)
    await page.screenshot({ path: info.outputPath(`completion-read-${language.locale}.png`), animations: "disabled" })
  })
}

test("a failed read preserves completion attention and a later explicit open retries", async ({ page }, info) => {
  await page.addInitScript(() => localStorage.setItem("zwai.phone.locale", "en"))
  await page.goto("/?mock=1&tick=0&history=unread-completion&readfail=1")
  const back = page.getByRole("button", { name: "Back", exact: true })
  await back.click()
  const attention = page.getByTestId("completion-attention")
  const open = () => attention.getByRole("button", { name: "Open Sweep the unused exports", exact: true }).click()
  await open()
  await expect(page.getByRole("alert")).toContainText("Read acknowledgement unavailable")
  await page.screenshot({ path: info.outputPath("completion-read-failed.png"), animations: "disabled" })
  await back.click()
  await expect(attention).toBeVisible()
  await open()
  await expect(page.getByTestId("transcript")).toBeVisible()
  // Wait for the read reply, so Back does not race its intentional post-render effect.
  await expect(page.getByRole("alert")).toHaveCount(0)
  await back.click()
  await expect(attention).toHaveCount(0)
})


test("completion More survives first-page polls and reading its cursor anchor", async ({ page }, info) => {
  await page.addInitScript(() => localStorage.setItem("zwai.phone.locale", "en"))
  await page.goto("/?mock=1&tick=0&history=unread-completion-pages")
  const back = page.getByRole("button", { name: "Back", exact: true })
  await back.click()
  const attention = page.getByTestId("completion-attention")
  const rows = attention.getByRole("button", { name: /^Open / })
  const more = attention.getByRole("button", { name: "More", exact: true })
  await expect(rows).toHaveCount(3)
  await more.click()
  await expect(rows).toHaveCount(6)
  // The normal 2s first-page poll must not hide already loaded completion pages.
  await page.waitForTimeout(2200)
  await expect(rows).toHaveCount(6)
  await attention.getByRole("button", { name: "Open Completed task 5", exact: true }).click()
  await expect(page.getByTestId("transcript")).toContainText("The result is complete.")
  await back.click()
  // Reconciliation consumes whole opaque-cursor pages through the old boundary.
  // The refreshed second page includes task6 after removing the read anchor5.
  await expect(rows).toHaveCount(6)
  await expect(attention.getByRole("button", { name: "Open Completed task 5", exact: true })).toHaveCount(0)
  await more.click()
  await expect(rows).toHaveCount(7)
  await expect(more).toHaveCount(0)
  await expect(attention.getByRole("button", { name: "Open Completed task 7", exact: true })).toBeVisible()
  await expect(attention.getByRole("button", { name: "Open Completed task 5", exact: true })).toHaveCount(0)
  await page.screenshot({ path: info.outputPath("completion-paged-after-anchor-read.png"), animations: "disabled" })
})
