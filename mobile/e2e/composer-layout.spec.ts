import { expect, test, type Page } from "@playwright/test"

// Bounds must include the submission itself: a visible editor or a page with
// overflow hidden can still hide the button on its right.
async function controlsFit(page: Page, label: string) {
  const editor = page.getByLabel(label, { exact: true })
  const submit = page.locator('form button[type="submit"]')
  await expect(submit).toHaveCount(1)
  await editor.fill("long message ".repeat(80))
  for (const width of [320, 375, 402]) {
    await page.setViewportSize({ width, height: 812 })
    await editor.focus()
    for (const item of [editor, submit, ...await page.getByRole("button", { name: "插入", exact: true }).all()]) {
      const box = await item.boundingBox()
      expect(box).not.toBeNull()
      expect(box!.x).toBeGreaterThanOrEqual(0)
      expect(box!.x + box!.width).toBeLessThanOrEqual(width)
      await expect(item).toBeInViewport()
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
  }
  await editor.blur()
  await expect(submit).toBeEnabled()
  await submit.click()
  await expect(page.locator("form textarea")).toHaveValue("")
}

test("PC follow-up and new chat keep submission inside the phone", async ({ page }) => {
  await page.goto("/?mock=1&tick=0")
  await expect(page.getByRole("button", { name: "跟进", exact: true })).toHaveCount(1)
  await expect(page.getByRole("button", { name: "插入", exact: true })).toHaveCount(1)
  await controlsFit(page, "消息")
  await page.getByRole("button", { name: "返回" }).click()
  await page.getByTestId("new-chat").click()
  await page.locator(".screen-push").evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)))
  await controlsFit(page, "新消息")
})

test("direct model chat keeps submission inside the phone", async ({ page }) => {
  await page.addInitScript(() => {
    const model = "long-model-name-".repeat(12)
    localStorage.setItem("zwai.phone.providers", JSON.stringify([{
      id: "layout", label: "Demo", baseURL: "https://endpoint.invalid/v1", apiKey: "", api: "chat", model,
      catalog: [model], timeoutSeconds: 300,
    }]))
  })
  await page.route("https://endpoint.invalid/v1/**", (route) => route.fulfill({
    contentType: "text/event-stream",
    body: 'data: {"choices":[{"delta":{"content":"offline reply"}}]}\n\ndata: [DONE]\n\n',
  }))
  await page.goto("/")
  await controlsFit(page, "消息")
  await expect(page.getByText("offline reply", { exact: true })).toBeVisible()
  await controlsFit(page, "消息")
})

test("selected answer keeps Add to chat near the highlight in WebKit", async ({ page }) => {
  await page.goto("/?mock=1&tick=0")
  await page.getByLabel("消息").fill("show result")
  await page.getByRole("button", { name: "跟进" }).click()
  const source = page.getByTestId("transcript").getByText("Here is what changed:")
  await expect(source).toBeVisible()
  await source.evaluate((element) => {
    element.scrollIntoView({ block: "center" })
    const range = document.createRange()
    range.selectNodeContents(element)
    window.getSelection()?.removeAllRanges()
    window.getSelection()?.addRange(range)
    document.dispatchEvent(new Event("selectionchange"))
  })
  const bottom = await page.evaluate(() => window.getSelection()?.getRangeAt(0).getBoundingClientRect().bottom)
  const action = page.getByRole("button", { name: "加入对话" })
  await expect(action).toBeVisible()
  const box = await action.boundingBox()
  expect(box).not.toBeNull()
  expect(bottom).toBeDefined()
  expect(box!.y - bottom!).toBeGreaterThanOrEqual(56)
  expect(box!.y - bottom!).toBeLessThanOrEqual(80)
  await action.click()
  await expect(page.getByTestId("quote-draft")).toHaveValue("Here is what changed:")
})

test("local client detail keeps its Back header in the iPhone viewport during inbox pull", async ({ page }) => {
  await page.goto("/?mock=1&tick=0")
  await page.getByRole("button", { name: "返回" }).click()
  await page.getByRole("button", { name: "open session" }).click()
  const detail = page.getByTestId("client-transcript")
  await expect(detail).toBeVisible()

  // The inbox pull applies a transform to its scrolling sheet. A full-screen
  // detail must stay pinned to the viewport even while that sheet moves.
  await page.getByTestId("inbox-scroller").evaluate((sheet) => {
    sheet.scrollTop = 120
    sheet.style.transform = "translateY(64px)"
  })
  const box = await detail.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.y).toBe(0)
  expect(box!.height).toBe(await page.evaluate(() => window.innerHeight))
  await expect(detail.getByRole("button", { name: "返回" })).toBeInViewport()
  await detail.getByRole("button", { name: "返回" }).click()
  await expect(detail).toHaveCount(0)
})
