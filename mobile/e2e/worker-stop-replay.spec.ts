import { expect, test } from "@playwright/test"
import { mkdir, writeFile } from "node:fs/promises"
import { dirname } from "node:path"

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


// Receipt-bearing MockLink data exercises the actual phone renderer/session.
// The host forwards this internal kind; it must remain absent from chat.
for (const locale of ["en", "zh"] as const) {
  for (const order of ["after_terminal", "before_cleanup"]) {
    test(`collection decline fixture stays internal live and after cold/paged replay: ${locale} ${order}`, async ({ page }, info) => {
      await page.addInitScript((language) => localStorage.setItem("zwai.phone.locale", language), locale)
      const query = `mock=1&tick=0&history=worker-collection&stop_order=${order}`
      await page.goto(`/?${query}&collection=stopping`)
      const stop = page.locator("form").getByRole("button", { name: locale === "en" ? "Stop" : "停止", exact: true })
      await expect(stop).toBeEnabled()
      const title = await page.getByRole("heading", { level: 1 }).textContent()
      await stop.click()
      const transcript = page.getByTestId("transcript")
      const assertStopped = async () => {
        await expect(transcript.getByText(locale === "en" ? "Stopped before collecting delegated results." : "已停止，委派结果未收齐。", { exact: true })).toHaveClass(/text-muted-foreground/)
        await expect(transcript).not.toContainText(/worker_ids|wait_id|generation|running.*true|stopped before collecting worker results/)
        await expect(transcript.locator(".text-destructive, .animate-spin")).toHaveCount(0)
        await expect(transcript.getByRole("button", { name: /retry|重试/i })).toHaveCount(0)
      }
      const capture = async (name: string) => {
        const path = info.outputPath(name)
        await mkdir(dirname(path), { recursive: true, mode: 0o700 })
        await writeFile(path, await page.screenshot(), { mode: 0o600 })
      }
      await assertStopped()
      await capture("collection-decline-live.png")
      await page.goto(`/?${query}&collection=stopped&collection_page=1`)
      const reopen = async () => {
        await page.getByRole("button", { name: locale === "en" ? "Back" : "返回", exact: true }).click()
        await page.getByTestId("project-stack").getByRole("button", { name: `${locale === "en" ? "Open" : "打开"} ${title}`, exact: true }).click()
      }
      await reopen()
      for (let replay = 0; replay < 2; replay++) {
        await assertStopped()
        await page.getByRole("button", { name: locale === "en" ? "Earlier" : "更早", exact: true }).click()
        await assertStopped()
        await capture(`collection-decline-paged-${replay}.png`)
        if (replay === 0) { await page.reload(); await reopen() }
      }
    })
  }
}
