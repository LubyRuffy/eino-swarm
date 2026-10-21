import { expect, test } from "@playwright/test"

for (const language of [
  { locale: "en", stop: "Stop", agents: "Agents (0)", stopped: "Stopped (2)", finished: "Finished (2)", stoppedRow: "stopped", done: "done", failed: "failed", back: "Back", open: "Open" },
  { locale: "zh", stop: "停止", agents: "子 Agent (0)", stopped: "已停止 (2)", finished: "已结束 (2)", stoppedRow: "已停止", done: "已完成", failed: "失败", back: "返回", open: "打开" },
]) {
  for (const order of ["before_cleanup", "after_cleanup", "after_terminal"]) {
    test(`Stop retains worker outcomes after replay: ${language.locale} ${order}`, async ({ page }, info) => {
      await page.addInitScript((locale) => localStorage.setItem("zwai.phone.locale", locale), language.locale)
      await page.goto(`/?mock=1&tick=0&history=stopped-workers&stop_order=${order}`)
      const stop = page.locator("form").getByRole("button", { name: language.stop, exact: true })
      await expect(stop).toBeEnabled()
      const title = await page.getByRole("heading", { level: 1 }).textContent()
      await stop.click()
      for (let replay = 0; replay < 2; replay++) {
        await page.getByRole("button", { name: language.agents, exact: true }).click()
        const roster = page.getByTestId("agent-roster")
        await expect(roster.getByRole("heading", { name: language.stopped, exact: true })).toBeVisible()
        await expect(roster.getByRole("heading", { name: language.finished, exact: true })).toBeVisible()
        await expect(roster.getByRole("button")).toHaveCount(4)
        for (const id of ["reader-1", "writer-2"]) {
          await expect(roster.getByRole("button", { name: new RegExp(`${id}.*${language.stoppedRow}$`) })).toBeVisible()
        }
        await expect(roster.getByRole("button", { name: new RegExp(`done-3.*${language.done}$`) })).toBeVisible()
        await expect(roster.getByRole("button", { name: new RegExp(`failed-4.*${language.failed}$`) })).toBeVisible()
        await roster.getByRole("button", { name: /reader-1/ }).click()
        const transcript = page.getByTestId("transcript")
        await expect(transcript).toContainText("swarm: agent reader-1 cancelled: context canceled")
        await expect(transcript.locator(".text-destructive, .animate-spin")).toHaveCount(0)
        await page.screenshot({ path: info.outputPath(`stopped-worker-${replay}.png`) })
        await page.getByRole("button", { name: language.back, exact: true }).click()
        if (replay === 0) {
          await page.getByRole("button", { name: language.back, exact: true }).click()
          await page.getByRole("button", { name: language.back, exact: true }).click()
          await page.getByRole("button", { name: `${language.open} ${title}`, exact: true }).click()
        }
      }
    })
  }
}
