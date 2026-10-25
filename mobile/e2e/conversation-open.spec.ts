import { expect, test } from "@playwright/test"

test("opening a conversation retains its live edge and earlier history through Back", async ({ page }, testInfo) => {
  await page.goto("/?mock=1&tick=0&history=hidden&pause=open")
  const transcript = page.getByTestId("transcript")
  await expect(transcript).toContainText("这一轮暂无可显示消息")
  await page.getByRole("button", { name: "更早" }).click()
  await expect(transcript).toContainText("Earlier answer")
  await page.getByRole("button", { name: "返回", exact: true }).click()
  await page.getByRole("button", { name: /^打开 Trim the layout pass$/ }).first().click()
  await expect(transcript).toContainText("这一轮暂无可显示消息")
  await page.getByRole("button", { name: "更早" }).click()
  await expect(transcript).toContainText("Earlier answer")
  await page.screenshot({ path: testInfo.outputPath("conversation-open.png") })
})
