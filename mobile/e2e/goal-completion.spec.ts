import { expect, test } from "@playwright/test"

for (const language of [
  { locale: "en", message: "Message", send: "Send", back: "Back", open: "Open", complete: "Standing objective completed." },
  { locale: "zh", message: "消息", send: "发送", back: "返回", open: "打开", complete: "目标已完成。" },
]) {
  test(`completed goal leaves only a history notice: ${language.locale}`, async ({ page }, info) => {
    await page.addInitScript((locale) => localStorage.setItem("zwai.phone.locale", locale), language.locale)
    await page.goto("/?mock=1&tick=0&history=goal-complete")
    await page.getByRole("button", { name: language.back, exact: true }).click()
    await page.getByRole("button", { name: `${language.open} Trim the layout pass`, exact: true }).click()
    const title = await page.getByRole("heading", { level: 1 }).textContent()
    await expect(page.getByTestId("goal-banner")).toContainText("Verify current work")
    await page.getByLabel(language.message, { exact: true }).fill("Verify the current result.")
    await page.locator("form").getByRole("button", { name: language.send, exact: true }).click()
    for (let replay = 0; replay < 2; replay++) {
      await expect(page.getByTestId("goal-banner")).toHaveCount(0)
      await expect(page.getByTestId("transcript").getByText(language.complete, { exact: true })).toHaveCount(1)
      await page.locator(".screen-push").evaluate((el) => Promise.all(el.getAnimations().map((animation) => animation.finished)))
      await page.screenshot({ path: info.outputPath(`goal-completed-${replay}.png`), animations: "disabled" })
      if (replay === 0) {
        await page.getByRole("button", { name: language.back, exact: true }).click()
        await page.getByRole("button", { name: `${language.open} ${title}`, exact: true }).first().click()
      }
    }
  })
}
