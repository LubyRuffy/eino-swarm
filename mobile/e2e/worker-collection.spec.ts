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
      await page.getByTestId("project-stack").getByRole("button", { name: `${locale === "en" ? "Open" : "打开"} ${title}`, exact: true }).click()
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

  for (const state of ["paused", "stopped"] as const) {
    test(`worker collection ${state} survives phone replay without a failure banner: ${locale}`, async ({ page }, info) => {
      await page.addInitScript((language) => localStorage.setItem("zwai.phone.locale", language), locale)
      await page.goto("/?mock=1&tick=0&history=worker-collection")
      const title = await page.getByRole("heading", { level: 1 }).textContent()
      const reopen = async () => {
        await page.getByRole("button", { name: locale === "en" ? "Back" : "返回", exact: true }).click()
        await page.getByTestId("project-stack").getByRole("button", { name: `${locale === "en" ? "Open" : "打开"} ${title}`, exact: true }).click()
      }
      await page.goto(`/?mock=1&tick=0&history=worker-collection&collection=${state}`)
      await reopen()
      const transcript = page.getByTestId("transcript")
      const message = state === "paused"
        ? locale === "en" ? "Delegated results have not been collected. The manager is paused." : "委派结果尚未收齐，主管已暂停。"
        : locale === "en" ? "Stopped before collecting delegated results." : "已停止，委派结果未收齐。"
      const assertNotice = async () => {
        await expect(transcript.getByText(message, { exact: true })).toHaveClass(/text-muted-foreground/)
        await expect(transcript.locator(".text-destructive")).toHaveCount(0)
        await expect(transcript).not.toContainText(/automatic_resume|wait_id|worker_ids|extend_by|tool-round limit|stopped before collecting worker results/)
        await expect(transcript.getByRole("button", { name: /retry|重试/i })).toHaveCount(0)
      }
      await assertNotice()
      await page.screenshot({ path: info.outputPath(`worker-collection-${state}.png`) })
      await page.reload()
      await reopen()
      await assertNotice()
      await page.screenshot({ path: info.outputPath(`worker-collection-${state}-replay.png`) })
    })
  }
}
