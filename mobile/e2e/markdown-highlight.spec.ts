import { expect, test, type Locator, type Page, type TestInfo } from "@playwright/test"

const CPP = [
  "const int total = 7; // keep the note",
  "\treturn total + 1;",
  `const char* label = "边界 <tag> ${"value ".repeat(28)}";  `,
].join("\n")
const JSON_SOURCE = '{"ready": true, "count": 3}'
const UNKNOWN = '\tint unchanged = 3;  // plain\n<div data-code="unchanged">keep</div>'
const MARKDOWN = [
  "Code remains readable:", "", "```cpp", CPP, "```", "",
  "```json", JSON_SOURCE, "```", "",
  "```opaque-language", UNKNOWN, "```",
].join("\n")

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 812 })
  await page.emulateMedia({ colorScheme: "light" })
  // Observe the real copy event after the application's capture listener has
  // planted its payload. This neither mocks the clipboard nor changes data.
  await page.addInitScript(() => {
    const copies: string[] = []
    Object.defineProperty(window, "__phoneCodeCopies", { value: copies })
    document.addEventListener("copy", (event) => {
      copies.push(event.clipboardData?.getData("text/plain") ?? "")
    })
  })
})

test("a paired conversation highlights submitted fences without changing copy or replay", async ({ page }, info) => {
  await page.goto("/?mock=1&tick=0")
  await page.getByRole("button", { name: "返回", exact: true }).click()
  await page.getByRole("button", { name: "打开 Sweep the unused exports", exact: true }).click()
  await page.getByLabel("消息", { exact: true }).fill(MARKDOWN)
  await page.locator("form").getByRole("button", { name: "发送", exact: true }).click()

  await verifyFences(page)
  await verifyThemesAndWidth(page, info, "paired")
  await copyCppIntoComposer(page)
  await page.getByLabel("消息", { exact: true }).fill("")

  // The mock host owns the conversation across watches, just like a paired
  // host. A browser reload would create another mock host and lose that data.
  await page.getByRole("button", { name: "返回", exact: true }).click()
  await page.getByRole("button", { name: "打开 Sweep the unused exports", exact: true }).click()
  await verifyFences(page)
  await page.screenshot({ path: info.outputPath("paired-rewatched.png") })
})

test("an offline direct-model answer keeps highlighted source after reload", async ({ page }, info) => {
  let completions = 0
  // Stay on the owned test origin: WebKit rejects reserved port 9 before its
  // network interception runs, even though Chromium permits the route.
  const endpoint = new URL("/offline-provider/v1", info.project.use.baseURL).href
  await page.route("**/offline-provider/v1/**", async (route) => {
    if (route.request().url().endsWith("/models")) {
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ data: [{ id: "offline-source" }] }),
      })
      return
    }
    expect(route.request().url()).toBe(`${endpoint}/chat/completions`)
    const request = route.request().postDataJSON() as { messages: { role: string; content: string }[] }
    expect(request.messages).toContainEqual({ role: "user", content: "Show the source blocks" })
    completions++
    const split = MARKDOWN.indexOf("return") + 3
    const deltas = [MARKDOWN.slice(0, split), MARKDOWN.slice(split)]
    await route.fulfill({
      contentType: "text/event-stream",
      body: deltas.map((content) => `data: ${JSON.stringify({ choices: [{ delta: { content } }] })}\n\n`).join("")
        + "data: [DONE]\n\n",
    })
  })
  await page.goto("/")
  await page.getByRole("button", { name: "连接模型", exact: true }).click()
  await page.getByLabel("接口地址", { exact: true }).fill(endpoint)
  await page.getByLabel("接口类型", { exact: true }).selectOption("chat")
  await page.getByRole("button", { name: "拉取模型", exact: true }).click()
  await expect(page.getByLabel("默认模型", { exact: true })).toHaveValue("offline-source")
  await page.getByRole("button", { name: "保存", exact: true }).click()
  await page.getByLabel("消息", { exact: true }).fill("Show the source blocks")
  await page.locator("form").getByRole("button", { name: "发送", exact: true }).click()
  await verifyFences(page)
  await expect(page.locator("form").getByRole("button", { name: "停止", exact: true })).toHaveCount(0)
  expect(completions).toBe(1)
  await verifyThemesAndWidth(page, info, "direct")
  await copyCppIntoComposer(page)
  await page.getByLabel("消息", { exact: true }).fill("")

  await page.reload()
  await page.getByRole("button", { name: "打开 Show the source blocks", exact: true }).click()
  await verifyFences(page)
  expect(completions).toBe(1)
  await page.screenshot({ path: info.outputPath("direct-reloaded.png") })
})

async function verifyFences(page: Page) {
  const cpp = page.locator("code.language-cpp")
  await expect(cpp).toHaveCount(1)
  await expect.poll(() => cpp.textContent()).toBe(CPP)
  await expect(cpp.locator(".text-syntax-keyword").first()).toBeVisible()
  await expect(cpp.locator(".text-syntax-comment")).toHaveCount(1)
  await expect(cpp.locator(".text-syntax-string")).toHaveCount(1)
  await expect.poll(() => page.locator("code.language-json").textContent()).toBe(JSON_SOURCE)
  await expect(page.locator("code.language-json .text-syntax-keyword")).toHaveText("true")
  const plain = page.locator("code.language-opaque-language")
  await expect.poll(() => plain.textContent()).toBe(UNKNOWN)
  await expect(plain.locator('[class*="text-syntax-"]')).toHaveCount(0)
  await expect(plain.locator("div")).toHaveCount(0)
}

async function verifyThemesAndWidth(page: Page, info: TestInfo, prefix: string) {
  const cpp = page.locator("code.language-cpp")
  for (const theme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: theme })
    await expect.poll(() => page.locator("html").evaluate((el) => el.classList.contains("dark")))
      .toBe(theme === "dark")
    await cpp.scrollIntoViewIfNeeded()
    const foreground = await color(cpp)
    const keyword = await color(cpp.locator(".text-syntax-keyword").first())
    const string = await color(cpp.locator(".text-syntax-string"))
    expect(keyword).not.toBe(foreground)
    expect(string).not.toBe(foreground)
    expect(keyword).not.toBe(string)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320)
    const pre = cpp.locator("..")
    expect(await pre.evaluate((el) => el.scrollWidth)).toBeGreaterThan(await pre.evaluate((el) => el.clientWidth))
    await pre.evaluate((el) => { el.scrollLeft = el.scrollWidth })
    expect(await pre.evaluate((el) => el.scrollLeft)).toBeGreaterThan(0)
    await pre.evaluate((el) => { el.scrollLeft = 0 })
    await page.screenshot({ path: info.outputPath(`${prefix}-${theme}.png`) })
    await info.attach(`${prefix}-${theme}-colors`, {
      body: JSON.stringify({ foreground, keyword, string, width: 320 }), contentType: "application/json",
    })
  }
}

async function copyCppIntoComposer(page: Page) {
  const block = page.getByTestId("markdown-code").filter({ has: page.locator("code.language-cpp") })
  const copy = block.getByRole("button", { name: "复制代码", exact: true })
  await copy.click()
  await expect(copy).toHaveAttribute("title", "已复制")
  await expect.poll(() => page.evaluate(() => (
    window as Window & { __phoneCodeCopies?: string[] }
  ).__phoneCodeCopies?.at(-1))).toBe(CPP)
  const composer = page.getByLabel("消息", { exact: true })
  await composer.focus()
  await composer.press("ControlOrMeta+V")
  await expect(composer).toHaveValue(CPP)
}

async function color(locator: Locator) {
  return locator.evaluate((el) => getComputedStyle(el).color)
}
