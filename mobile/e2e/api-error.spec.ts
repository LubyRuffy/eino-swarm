import { expect, test } from "@playwright/test"

const API_ERROR = "model API request failed: HTTP 502 Bad Gateway: unexpected end of JSON input"

for (const language of [
  { locale: "en", back: "Back", open: "Open Sweep the unused exports", stop: "Stop", send: "Send", retry: "The model service returned a response that could not be decoded. Retrying.", error: "the model service returned a response that could not be decoded", blame: "tool call" },
  { locale: "zh", back: "返回", open: "打开 Sweep the unused exports", stop: "停止", send: "发送", retry: "模型服务返回的响应无法解析，正在重试。", error: "模型服务返回的响应无法解析。", blame: "工具调用" },
]) {
  test(`phone replays the model response decoding error without tool blame (${language.locale})`, async ({ page }, info) => {
    await page.addInitScript((locale) => localStorage.setItem("zwai.phone.locale", locale), language.locale)
    await page.goto("/?mock=1&tick=0&history=model-response-error")
    const back = page.getByRole("button", { name: language.back, exact: true })
    const open = page.getByRole("button", { name: language.open, exact: true })
    await back.click()
    const transcript = page.getByTestId("transcript")
    const error = transcript.locator("p.text-destructive")
    for (const phase of ["first-open", "reopened", "replayed"]) {
      await open.click()
      await expect(error).toHaveText(language.error)
      await expect(error).toBeVisible()
      await expect(transcript.getByText(language.retry, { exact: true })).toHaveCount(2)
      await expect(transcript).not.toContainText(language.blame)
      await expect(transcript.getByTestId("work-fold")).toHaveCount(0)
      await expect(page.getByRole("button", { name: language.stop, exact: true })).toHaveCount(0)
      await expect(page.getByRole("button", { name: language.send, exact: true })).toBeDisabled()
      await page.screenshot({ path: info.outputPath(`model-response-error-${language.locale}-${phase}.png`), animations: "disabled" })
      await back.click()
      if (phase === "reopened") {
        await page.reload()
        await back.click()
      }
    }
  })
}

test("phone preserves a failed model API status and cause when replaying an idle conversation", async ({ page }, info) => {
  await page.goto("/?mock=1&tick=0&history=api-error")
  const back = page.getByRole("button", { name: "返回", exact: true })
  const open = page.getByRole("button", { name: /^打开 Sweep the unused exports$/ })
  await back.click()
  await open.click()
  const transcript = page.getByTestId("transcript")
  // Model failures are error paragraphs, not tool arguments hidden in Work.
  const error = transcript.locator("p.text-destructive")
  await expect(error).toHaveText(API_ERROR)
  await expect(error).toBeVisible()
  await expect(transcript.getByRole("button", { name: /\b\d+ tools?\b/ })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "停止", exact: true })).toHaveCount(0)
  await page.screenshot({ path: info.outputPath("api-error-chat.png"), animations: "disabled" })
  await back.click()
  await open.click()
  await expect(error).toHaveText(API_ERROR)
  await page.screenshot({ path: info.outputPath("api-error-reopened.png"), animations: "disabled" })
  await page.reload()
  await back.click()
  await open.click()
  await expect(error).toHaveText(API_ERROR)
  await expect(error).toBeVisible()
  await expect(page.getByRole("button", { name: "停止", exact: true })).toHaveCount(0)
  await page.screenshot({ path: info.outputPath("api-error-replayed.png"), animations: "disabled" })
})
