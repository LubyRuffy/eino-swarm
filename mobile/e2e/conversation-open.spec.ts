import { expect, test, type Page } from "@playwright/test"

for (const language of [
  { locale: "en", back: "Back", open: "Open Trim the layout pass", stop: "Stop" },
  { locale: "zh", back: "返回", open: "打开 Trim the layout pass", stop: "停止" },
]) {
  const reopen = async (page: Page) => {
    await page.getByRole("button", { name: language.back, exact: true }).click()
    // The same live thread also appears in In progress. Select its project row.
    const sameThread = page.getByTestId("project-stack").getByRole("button", { name: language.open, exact: true })
    await expect(sameThread).toHaveCount(1)
    await sameThread.click()
  }

  test(`opening backfills the latest answer without Earlier on both visits (${language.locale})`, async ({ page }, info) => {
    await page.addInitScript((locale) => localStorage.setItem("zwai.phone.locale", locale), language.locale)
    await page.goto("/?mock=1&tick=0&history=hidden&pause=open")
    const transcript = page.getByTestId("transcript")
    for (let visit = 0; visit < 2; visit++) {
      await expect(transcript.getByText("Earlier answer", { exact: true })).toHaveCount(1)
      await expect(transcript.getByText("Earlier answer", { exact: true })).toBeVisible()
      await expect(transcript.getByText("Earlier answer", { exact: true })).toBeInViewport()
      await expect(transcript).not.toContainText("No visible messages in this turn yet")
      await expect(transcript).not.toContainText("这一轮暂无可显示消息")
      await expect(page.locator("form").getByRole("button", { name: language.stop, exact: true })).toBeVisible()
      await expect(page.locator(".screen-push")).toHaveCSS("opacity", "1")
      await page.screenshot({ path: info.outputPath(`conversation-open-${language.locale}-${visit}.png`) })
      if (visit === 0) await reopen(page)
    }
  })

  test(`live manager updates survive Back and reopening (${language.locale})`, async ({ page }, info) => {
    await page.addInitScript((locale) => localStorage.setItem("zwai.phone.locale", locale), language.locale)
    await page.goto("/?mock=1&tick=150&history=streamed-agents")
    const transcript = page.getByTestId("transcript")
    await expect(transcript).toContainText("Manager update 0")
    await expect(transcript).toContainText("Manager update 2")
    for (let visit = 0; visit < 2; visit++) {
      for (let update = 0; update < 3; update++) {
        await expect(transcript.getByText(`Manager update ${update}`, { exact: true })).toHaveCount(1)
      }
      await expect(page.locator("form").getByRole("button", { name: language.stop, exact: true })).toBeVisible()
      await expect(page.locator(".screen-push")).toHaveCSS("opacity", "1")
      await page.screenshot({ path: info.outputPath(`conversation-live-${language.locale}-${visit}.png`) })
      if (visit === 0) await reopen(page)
    }
  })
}
