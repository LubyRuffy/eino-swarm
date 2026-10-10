import { expect, test } from "@playwright/test"

const API_ERROR = "model API request failed: HTTP 502 Bad Gateway: unexpected end of JSON input"

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
