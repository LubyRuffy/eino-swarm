import { expect, test, type Page } from "@playwright/test"

async function status(page: Page, startup: string, completed: number) {
  await page.evaluate(({ startup, completed }) => {
    const url = new URL(location.href)
    url.searchParams.set("startup", startup)
    url.searchParams.set("restore", `${completed},3`)
    history.replaceState(null, "", url)
  }, { startup, completed })
}

test("phone restoration leaves transcript readable and preserves a draft without replay until explicit send", async ({ page }, info) => {
  await page.goto("/?mock=1&tick=0")
  const input = page.getByLabel("消息", { exact: true })
  await expect(input).toBeVisible()
  await input.fill("恢复期间保留的消息")
  const transcript = (await page.getByTestId("transcript").textContent()) ?? ""
  await status(page, "restoring", 1)
  await expect(page.getByTestId("engine-startup")).toHaveText("正在恢复未完成任务 1/3")
  await expect(input).toBeDisabled()
  await expect(input).toHaveValue("恢复期间保留的消息")
  await expect(page.getByTestId("transcript")).toHaveText(transcript)
  await status(page, "restoring", 2)
  await expect(page.getByTestId("engine-startup")).toHaveText("正在恢复未完成任务 2/3")
  await page.screenshot({ path: info.outputPath("phone-restoration-draft.png") })
  await status(page, "ready", 3)
  await expect(page.getByTestId("engine-startup")).toHaveCount(0)
  await expect(input).toBeEnabled()
  await expect(input).toHaveValue("恢复期间保留的消息")
  await expect(page.getByTestId("transcript")).toHaveText(transcript)
  await input.press("Enter")
  await expect(input).toHaveValue("")
})

test("phone restoration disables Run now and Cancel wait and clears automatically when ready", async ({ page }, info) => {
  await page.goto("/?mock=1&tick=0&startup=restoring&restore=1,3")
  await expect(page.getByTestId("engine-startup")).toHaveText("正在恢复未完成任务 1/3")
  await page.getByRole("button", { name: "返回", exact: true }).click()
  await page.getByRole("button", { name: /^打开 Watch the nightly export$/ }).first().click()
  const run = page.getByRole("button", { name: "立即运行", exact: true })
  const cancel = page.getByRole("button", { name: "取消等待", exact: true })
  await expect(run).toBeDisabled()
  await expect(cancel).toBeDisabled()
  await page.screenshot({ path: info.outputPath("phone-restoration-wait-actions.png") })
  await status(page, "ready", 3)
  await expect(page.getByTestId("engine-startup")).toHaveCount(0)
  await expect(run).toBeEnabled()
  await expect(cancel).toBeEnabled()
})


test("phone Stop reaches a pending stored turn before its live runtime is restored", async ({ page }, info) => {
  await page.goto("/?mock=1&tick=0&history=restore-pending&startup=restoring&restore=0,1")
  await page.getByRole("button", { name: "返回", exact: true }).click()
  await page.getByRole("button", { name: /^打开 Sweep the unused exports$/ }).first().click()
  const stop = page.getByRole("button", { name: "停止", exact: true })
  await expect(stop).toBeEnabled()
  await expect(page.getByLabel("消息", { exact: true })).toBeDisabled()
  await stop.click()
  await page.screenshot({ path: info.outputPath("phone-stop-idle-live-status-stored-running-recovery.png") })
  await status(page, "ready", 1)
  await expect(page.getByTestId("engine-startup")).toHaveCount(0)
  await expect(stop).toHaveCount(0)
  await expect(page.getByLabel("消息", { exact: true })).toBeEnabled()
})
