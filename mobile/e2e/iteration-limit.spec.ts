import { expect, test, type Page, type TestInfo } from "@playwright/test"
import { mkdir, writeFile } from "node:fs/promises"
import { dirname } from "node:path"

async function capture(page: Page, info: TestInfo, name: string) {
  const path = info.outputPath(name)
  await mkdir(dirname(path), { recursive: true, mode: 0o700 })
  await writeFile(path, await page.screenshot({ animations: "disabled" }), { mode: 0o600 })
}

for (const language of [
  { locale: "en", back: "Back", open: "Open", continue: "Continue for more tool rounds", stop: "Stop at the tool-round limit", prompt: /limit \(6\).*24 rounds/ },
  { locale: "zh", back: "返回", open: "打开", continue: "继续更多工具回合", stop: "在工具回合上限处停止", prompt: /上限（6）.*24/ },
]) {
  test(`a paused manager can continue from the phone after reopening: ${language.locale}`, async ({ page }, info) => {
    await page.addInitScript((locale) => localStorage.setItem("zwai.phone.locale", locale), language.locale)
    await page.goto("/?mock=1&tick=0&history=iteration-limit")
    const card = page.getByTestId("iteration-limit")
    await expect(card).toContainText(language.prompt)
    const title = await page.getByRole("heading", { level: 1 }).textContent()
    await page.getByRole("button", { name: language.back, exact: true }).click()
    await page.getByRole("button", { name: `${language.open} ${title}`, exact: true }).first().click()
    await expect(card).toContainText(language.prompt)
    const proceed = card.getByRole("button", { name: language.continue, exact: true })
    await expect(proceed).toBeEnabled()
    const bounds = await proceed.boundingBox()
    expect(bounds).not.toBeNull()
    expect(bounds!.x).toBeGreaterThanOrEqual(0)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width)
    await capture(page, info, "paused-manager.png")
    await proceed.click()
    await expect(page.getByTestId("transcript")).toContainText("The paused turn resumed.")
    await expect(card.getByRole("button")).toHaveCount(0)
    await expect(page.getByTestId("turn-boundary")).toHaveCount(1)
    await expect(page.getByTestId("transcript").locator(".text-destructive")).toHaveCount(0)
  })

  test(`declining the tool-round limit stops without reporting a model failure: ${language.locale}`, async ({ page }, info) => {
    await page.addInitScript((locale) => localStorage.setItem("zwai.phone.locale", locale), language.locale)
    await page.goto("/?mock=1&tick=0&history=iteration-limit")
    const card = page.getByTestId("iteration-limit")
    await card.getByRole("button", { name: language.stop, exact: true }).click()
    await expect(card.getByRole("button")).toHaveCount(0)
    await expect(page.getByTestId("turn-boundary")).toHaveCount(1)
    await expect(page.getByTestId("transcript")).not.toContainText("The paused turn resumed.")
    await expect(page.getByTestId("transcript").locator(".text-destructive")).toHaveCount(0)
    await capture(page, info, "stopped-manager.png")
  })
}

test("a rejected continuation remains actionable and can be retried", async ({ page }, info) => {
  await page.addInitScript(() => localStorage.setItem("zwai.phone.locale", "en"))
  await page.goto("/?mock=1&tick=0&history=iteration-limit&continuefail=1")
  const card = page.getByTestId("iteration-limit")
  const proceed = card.getByRole("button", { name: "Continue for more tool rounds", exact: true })
  await proceed.click()
  await expect(card.getByRole("alert")).toContainText("PC rejected continuation")
  await expect(proceed).toBeEnabled()
  await expect(card.getByRole("button", { name: "Stop at the tool-round limit", exact: true })).toBeEnabled()
  await capture(page, info, "continuation-rejected.png")
  await proceed.click()
  await expect(page.getByTestId("transcript")).toContainText("The paused turn resumed.")
  await expect(card.getByRole("button")).toHaveCount(0)
})

test("the live pause stays actionable when its cap event is outside the snapshot", async ({ page }, info) => {
  await page.addInitScript(() => localStorage.setItem("zwai.phone.locale", "en"))
  await page.goto("/?mock=1&tick=0&history=iteration-limit&cap_snapshot=1")
  const card = page.getByTestId("iteration-limit")
  const proceed = card.getByRole("button", { name: "Continue this turn", exact: true })
  await expect(proceed).toBeEnabled()
  await expect(card).not.toContainText(/\(6\)|24 rounds/)
  await capture(page, info, "snapshot-only-pause.png")
  await proceed.click()
  await expect(page.getByTestId("transcript")).toContainText("The paused turn resumed.")
  await expect(card.getByRole("button")).toHaveCount(0)
})
