import { expect, test, type Page } from "@playwright/test"

// Bounds must include the submission itself: a visible editor or a page with
// overflow hidden can still hide the button on its right.
async function controlsFit(page: Page, label: string) {
  const editor = page.getByLabel(label, { exact: true })
  const submit = page.locator('form button[type="submit"]')
  await editor.fill("long message ".repeat(80))
  await expect(submit).toHaveCount(1)
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
  await expect(page.locator("form").getByRole("button", { name: "停止", exact: true })).toBeEnabled()
  await expect(page.getByRole("button", { name: "插入", exact: true })).toHaveCount(1)
  await controlsFit(page, "消息")
  await page.getByRole("button", { name: "返回" }).click()
  await page.getByTestId("new-chat").click()
  await page.locator(".screen-push").evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)))
  await controlsFit(page, "新消息")
})

test("the empty live composer stops the turn while a typed draft still sends a follow-up", async ({ page }) => {
  await page.goto("/?mock=1&tick=0")
  const form = page.locator("form")
  const stop = form.getByRole("button", { name: "停止", exact: true })
  await expect(stop).toBeEnabled()
  await expect(stop.locator("svg")).toHaveClass(/lucide-square/)
  await page.getByLabel("消息").fill("follow-up")
  await expect(stop).toHaveCount(0)
  await expect(form.getByRole("button", { name: "跟进", exact: true })).toBeEnabled()
  await page.getByLabel("消息").fill("")
  await stop.click()
  await expect(page.getByRole("button", { name: "停止", exact: true })).toHaveCount(0)
  await expect(form.getByRole("button", { name: "发送", exact: true })).toBeDisabled()
  await expect(page.getByLabel("消息")).toHaveValue("")
  await page.screenshot({ path: "test-results/composer-stopped.png" })
})

test("Steer on a waiting row starts it after Stop while leaving the other row queued", async ({ page }, testInfo) => {
  await page.goto("/?mock=1&tick=400&queue=1")
  const form = page.locator("form")
  const queue = page.getByTestId("followup-queue")
  await expect(queue).toContainText("first waiting message")
  await expect(queue).toContainText("second waiting message")
  await form.getByRole("button", { name: "停止", exact: true }).click()
  await expect(page.getByRole("button", { name: "停止", exact: true })).toHaveCount(0)
  await expect(form.getByRole("button", { name: "发送", exact: true })).toBeDisabled()
  await expect(queue.locator("li")).toHaveCount(2)
  await page.screenshot({ path: testInfo.outputPath("queued-steer-after-stop-idle.png") })

  await queue.locator("li").filter({ hasText: "first waiting message" })
    .getByRole("button", { name: "插入", exact: true }).click()
  await expect(queue.locator("li")).toHaveCount(1)
  await expect(queue).not.toContainText("first waiting message")
  await expect(queue).toContainText("second waiting message")
  await expect(page.getByTestId("transcript")).toContainText("first waiting message")
  await expect(form.getByRole("button", { name: "停止", exact: true })).toBeEnabled()
  await page.screenshot({ path: testInfo.outputPath("queued-steer-after-stop-running.png") })

  await form.getByRole("button", { name: "停止", exact: true }).click()
  await expect(page.getByRole("button", { name: "停止", exact: true })).toHaveCount(0)
  await expect(form.getByRole("button", { name: "发送", exact: true })).toBeDisabled()
  await expect(queue.locator("li")).toHaveCount(1)
  await expect(queue).toContainText("second waiting message")
  await expect(page.getByRole("alert")).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath("queued-steer-after-stop-stopped-again.png") })
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

test("direct chat cancels a pending model request from the primary Stop button", async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem("zwai.phone.providers", JSON.stringify([{
    id: "offline", label: "Demo", baseURL: "https://endpoint.invalid/v1", apiKey: "", api: "chat",
    model: "scripted", catalog: ["scripted"], timeoutSeconds: 300,
  }])))
  let release!: () => void
  const gate = new Promise<void>((resolve) => { release = resolve })
  let requested = false
  await page.route("https://endpoint.invalid/v1/**", async (route) => {
    requested = true
    await gate
    await route.abort()
  })
  try {
    await page.goto("/")
    await page.getByLabel("消息").fill("start a reply")
    await page.locator("form").getByRole("button", { name: "发送", exact: true }).click()
    await expect.poll(() => requested).toBe(true)
    const form = page.locator("form")
    const stop = form.getByRole("button", { name: "停止", exact: true })
    await expect(stop).toBeEnabled()
    await stop.click()
    await expect(page.getByRole("button", { name: "停止", exact: true })).toHaveCount(0)
    await expect(page.getByLabel("消息")).toBeEnabled()
    await expect(form.getByRole("button", { name: "发送", exact: true })).toBeDisabled()
    await expect(page.getByRole("alert")).toHaveCount(0)
  } finally {
    release()
  }
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
