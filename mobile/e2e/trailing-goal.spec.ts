import { expect, test } from "@playwright/test"

// Engine/RPC acceptance lives in internal/server/slash_goal_integration_test.go.
// This real phone surface test protects the other half: the full marker must
// leave the composer intact, so the PC can activate it rather than seeing prose.
test("phone Send preserves a trailing goal command for PC dispatch", async ({ page }, info) => {
  await page.goto("/?mock=1&tick=0")
  const message = page.getByLabel("消息", { exact: true })
  const text = "检查 assigned operation /goal"
  await message.fill(text)
  await message.press("Enter")
  await expect(message).toHaveValue("")
  await expect(page.getByTestId("transcript")).toContainText(text)
  await page.screenshot({ path: info.outputPath("phone-trailing-goal-send.png") })
})
