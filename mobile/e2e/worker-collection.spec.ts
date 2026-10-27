import { expect, test } from "@playwright/test"

for (const locale of ["en", "zh"]) {
  test(`worker collection waiting and resumption survive phone replay: ${locale}`, async ({ page }, info) => {
    await page.addInitScript((language) => localStorage.setItem("zwai.phone.locale", language), locale)
    await page.goto("/?mock=1&tick=0&history=worker-collection")
    const transcript = page.getByTestId("transcript")
    const waiting = locale === "en" ? "The manager will resume once" : "主管会自动继续一次"
    await expect(transcript).toContainText(waiting)
    const title = await page.getByRole("heading", { level: 1 }).textContent()
    const openCompleted = async () => {
      await page.getByRole("button", { name: locale === "en" ? "Back" : "返回", exact: true }).click()
      await page.getByRole("button", { name: `${locale === "en" ? "Open" : "打开"} ${title}`, exact: true }).click()
    }
    await expect(transcript).not.toContainText("extend_by")
    await expect(transcript).not.toContainText("tool-round limit")
    await page.screenshot({ path: info.outputPath("worker-collection-waiting.png") })
    await page.reload()
    await expect(transcript).toContainText(waiting)
    await page.goto("/?mock=1&tick=0&history=worker-collection&collection=done")
    await openCompleted()
    await expect(transcript).toContainText(locale === "en" ? "Continuing to collect delegated results" : "正在继续收取委派结果")
    await expect(transcript).toContainText("The original delegated result has been collected.")
    await page.reload()
    await openCompleted()
    await expect(transcript).toContainText("The original delegated result has been collected.")
    await expect(transcript).not.toContainText("worker_ids")
    await page.screenshot({ path: info.outputPath("worker-collection-resumed-replay.png") })
  })
}
