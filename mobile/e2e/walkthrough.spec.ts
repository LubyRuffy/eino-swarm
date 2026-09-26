import { expect, test, type Page } from "@playwright/test"

/** The inbox and a conversation against the scripted host. `tick=0` plays a
 *  turn as fast as the browser will paint it; the product path is the same
 *  screens over a real link. */
const WALKTHROUGH = "/?mock=1&tick=0"

test("switching PCs stays on the selected inbox instead of opening its latest conversation", async ({ page }) => {
  await page.addInitScript(() => {
    const link = (fingerprint: string, label: string) => ({
      hubURL: "http://127.0.0.1:0", ticket: "mock", hostPub: "A".repeat(43),
      sessionID: "ab".repeat(16), fingerprint, label,
    })
    localStorage.setItem("zwai.remote.links", JSON.stringify([
      link("walkthrough", "Walkthrough PC"), link("another", "Another PC"),
    ]))
    localStorage.setItem("zwai.remote.active", "walkthrough")
  })
  await page.goto(WALKTHROUGH)
  await page.getByRole("button", { name: "返回" }).click()
  await page.getByRole("tab", { name: "Another PC" }).click()
  await expect(page.getByRole("tablist")).toBeVisible()
  await expect(page.getByTestId("transcript")).toHaveCount(0)
})

test("Android Back closes a Clients task before the inbox can exit", async ({ page }) => {
  await page.goto(WALKTHROUGH)
  await page.getByRole("button", { name: "返回" }).click()
  await page.getByRole("button", { name: "open session" }).click()
  await expect(page.getByTestId("client-transcript")).toBeVisible()
  expect(await page.evaluate(() => (window as Window & { __zwaiAndroidBack?: () => boolean }).__zwaiAndroidBack?.())).toBe(true)
  await expect(page.getByTestId("client-transcript")).toHaveCount(0)
  await expect(page.getByRole("button", { name: "open session" })).toBeVisible()
  expect(await page.evaluate(() => (window as Window & { __zwaiAndroidBack?: () => boolean }).__zwaiAndroidBack?.())).toBe(false)
})

test("Clients More shows progress during a slow page and then reveals the older task", async ({ page }) => {
  await page.goto("/?mock=1&tick=0&pause=clients")
  await page.getByRole("button", { name: "返回" }).click()
  const more = page.getByTestId("client-more-claude")
  await more.click()
  await expect(more).toHaveAttribute("aria-busy", "true")
  await expect(more).toContainText("正在加载更多")
  await expect(more).toBeDisabled()
  await expect(page.getByRole("button", { name: "older session" })).toBeVisible()
  await expect(more).toHaveCount(0)
})

test("phone model chooser uses the app type scale and keeps long names on screen", async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem("zwai.phone.providers", JSON.stringify([{
      id: "p-test", label: "Demo", baseURL: "https://endpoint.invalid/v1",
      apiKey: "", api: "chat", model: "small",
      catalog: ["small", "a-very-long-model-name-that-must-wrap-inside-the-phone"],
      timeoutSeconds: 300,
    }]))
  })
  await page.goto("/")
  await page.getByRole("button", { name: "模型" }).click()
  const picker = page.getByRole("dialog", { name: "模型" })
  await expect(picker).toBeVisible()
  const longName = picker.getByRole("radio", { name: "a-very-long-model-name-that-must-wrap-inside-the-phone" })
  await expect(longName).toHaveCSS("font-size", "14px")
  await expect(longName).toBeInViewport()
  await longName.click()
  await expect(picker).toHaveCount(0)
  await expect(page.getByRole("button", { name: "模型" })).toContainText("a-very-long-model-name")
})

test("worker activity opens in its own phone view without becoming a manager answer", async ({ page }) => {
  await page.goto("/?mock=1&tick=0&agents=1")
  await page.getByRole("button", { name: "返回" }).click()
  await page.getByRole("button", { name: /^打开 Sweep the unused exports$/ }).click()
  const transcript = page.getByTestId("transcript")
  await expect(transcript).toContainText("Here is what changed:")
  await expect(transcript).not.toContainText("worker answer")
  await page.getByRole("button", { name: "子 Agent (1)" }).click()
  await expect(page.getByTestId("agent-roster")).toContainText("reader")
  await page.getByTestId("agent-roster").getByRole("button").click()
  await expect(transcript).toContainText("worker answer")
  await expect(transcript).not.toContainText("Here is what changed:")
  await page.getByRole("button", { name: "返回" }).click()
  await expect(page.getByTestId("agent-roster")).toBeVisible()
})

test("interrupting a waiting phone message first inserts it into the live turn", async ({ page }) => {
  await page.goto("/?mock=1&tick=0&queue=1")
  const queue = page.getByTestId("followup-queue")
  await expect(queue).toContainText("first waiting message")
  await expect(queue).toContainText("second waiting message")

  await queue.getByRole("button", { name: "中断插入" }).click()

  await expect(queue).not.toContainText("first waiting message")
  await expect(queue).toContainText("second waiting message")
  await expect(page.getByTestId("transcript")).toContainText("first waiting message")
  await expect(page.getByText("服务端拒绝了这次请求", { exact: false })).toHaveCount(0)
})

test("selected answer text becomes a removable quote in the next phone message", async ({ page }) => {
  await page.goto(WALKTHROUGH)
  await page.getByLabel("消息").fill("show result")
  await page.getByRole("button", { name: "跟进" }).click()
  const source = page.getByTestId("transcript").getByText("Here is what changed:")
  await expect(source).toBeVisible()
  await source.evaluate((element) => {
    const range = document.createRange()
    range.selectNodeContents(element)
    window.getSelection()?.removeAllRanges()
    window.getSelection()?.addRange(range)
    document.dispatchEvent(new Event("selectionchange"))
  })
  await page.getByRole("button", { name: "加入对话" }).click()
  await expect(page.getByTestId("quote-draft")).toHaveValue("Here is what changed:")
  await page.getByTestId("quote-draft").fill("")
  await page.getByTestId("quote-draft").pressSequentially("Here is the corrected source:")
  await page.getByLabel("消息").fill("解释这一句")
  await page.getByRole("button", { name: /跟进|发送/ }).click()
  await expect(page.getByTestId("quoted-message").last()).toContainText("Here is the corrected source:")
  await expect(page.getByTestId("quoted-message").last()).toContainText("解释这一句")
  await expect(page.getByTestId("quote-drafts")).toHaveCount(0)
})

async function backToInbox(page: Page) {
  await page.getByRole("button", { name: "返回" }).click()
  await expect(page.getByRole("tablist", { name: "电脑" })).toBeVisible()
}

test("the inbox separates live work from recents and says how old a row is", async ({
  page,
}) => {
  await page.goto(WALKTHROUGH)
  await backToInbox(page)

  await expect(page.getByRole("tab", { name: /Walkthrough PC/ })).toBeVisible()
  await expect(page.getByText("进行中", { exact: true }).first()).toBeVisible()

  // Running is happening now, so it carries no age; a wait does. The same
  // row also sits under its project. Recents still does not repeat it.
  const live = page.getByRole("button", { name: /^打开 Trim the layout pass$/ })
  await expect(live).toHaveCount(2)
  await expect(live.nth(0).getByTestId("row-state")).toHaveAttribute("data-state", "running")
  await expect(live.nth(1).getByTestId("row-state")).toHaveAttribute("data-state", "running")
  await expect(live.nth(0)).not.toContainText("前")
  await expect(live.nth(1)).not.toContainText("前")
  const parked = page.getByRole("button", { name: /^打开 Watch the nightly export$/ })
  await expect(parked).toHaveCount(2)
  await expect(parked.nth(0).getByTestId("row-state")).toHaveAttribute("data-state", "waiting")
  await expect(parked.nth(1).getByTestId("row-state")).toHaveAttribute("data-state", "waiting")
  // A parked wait has no live action; its own summary is what the row says.
  await expect(parked.nth(0)).toContainText("Checking again after the next run")
  await expect(parked.nth(0)).toContainText("前")
  await expect(parked.nth(1)).toContainText("Checking again after the next run")

  const idle = page.getByRole("button", { name: /^打开 Sweep the unused exports$/ })
  await expect(idle.getByTestId("row-state")).toHaveCount(0)
  await expect(idle).toContainText("分钟前")
})

test("a turn plays into the transcript and folds its work behind the answer", async ({
  page,
}) => {
  await page.goto(WALKTHROUGH)
  const box = page.getByLabel("消息")
  await expect(box).toBeVisible()

  await box.fill("walk the transcript")
  await page.getByRole("button", { name: "跟进" }).click()

  const transcript = page.getByTestId("transcript")
  await expect(transcript.getByText("walk the transcript")).toBeVisible()
  await expect(transcript.getByText("Here is what changed:")).toBeVisible()
  await expect(transcript.getByTestId("phone-chart")).toBeVisible()
  await transcript.getByRole("tab", { name: "表格" }).click()
  await expect(transcript.getByRole("table", { name: "Counts" })).toBeVisible()

  // Tools do not each get a chat row; they collapse behind one fold.
  await expect(transcript.getByTestId("work-fold").last()).toContainText("2 个工具")
  await expect(transcript.getByText("npm test -- layout")).toHaveCount(0)
  await transcript.getByTestId("work-fold").last().click()
  await expect(transcript.getByText("exec").first()).toBeVisible()

  // The turn is over, so the header stops offering to stop it.
  await expect(page.getByRole("button", { name: "停止" })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "发送" })).toBeVisible()
})

test("a short conversation sits on the composer instead of under a blank screen", async ({
  page,
}) => {
  await page.goto(WALKTHROUGH)
  const transcript = page.getByTestId("transcript")
  await expect(transcript).toBeVisible()

  const gap = await transcript.evaluate((el) => {
    const column = el.firstElementChild as HTMLElement
    return column.getBoundingClientRect().bottom - el.getBoundingClientRect().bottom
  })
  expect(Math.abs(gap)).toBeLessThan(24)
})

// The inbox is for reading what runs; starting is a screen that asks which
// PC and which project first.
test("new chat keeps the project row and the message box inside a narrow screen", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 568 })
  await page.goto(WALKTHROUGH)
  await backToInbox(page)
  await page.getByTestId("new-chat").click()
  // The screen slides in. Measuring during that slide reports a column
  // that has not yet landed.
  await page.locator(".screen-push").evaluate((el) =>
    Promise.all(el.getAnimations().map((a) => a.finished)),
  )

  const width = page.viewportSize()?.width ?? 0
  // The column used to size itself to the chips and the model row, so both
  // ran past a narrow phone.
  for (const locator of [
    page.locator("main"),
    page.getByRole("radiogroup", { name: "项目" }),
    page.getByLabel("新消息"),
    page.getByLabel("模型"),
  ]) {
    const box = await locator.boundingBox()
    expect(box).toBeTruthy()
    expect(box!.x).toBeGreaterThanOrEqual(-1)
    expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1)
  }
})

test("New chat picks a PC and a project, then opens what it started", async ({ page }) => {
  await page.goto(WALKTHROUGH)
  await backToInbox(page)
  await expect(page.getByLabel("新消息")).toHaveCount(0)

  await page.getByTestId("new-chat").click()
  await expect(page.getByRole("radiogroup", { name: "电脑" }).getByRole("radio")).toHaveCount(1)
  await page.getByRole("radio", { name: "Field notes" }).click()
  await page.getByLabel("新消息").fill("a fresh one")
  await page.getByRole("button", { name: "开始" }).click()

  await expect(page.getByRole("heading", { name: "a fresh one" })).toBeVisible()
  await expect(page.getByTestId("transcript").getByText("Here is what changed:")).toBeVisible()
})

test("a project row opens new chat already in that project", async ({ page }) => {
  await page.goto(WALKTHROUGH)
  await backToInbox(page)
  const startHere = page.getByRole("button", { name: "在Field notes新建对话" })
  await expect(startHere).toHaveText("")
  await startHere.click()
  await expect(page.getByRole("radio", { name: "Field notes" })).toBeChecked()
  await expect(page.getByRole("radio", { name: "默认" })).not.toBeChecked()
})

test("a project folds and the live row under it stays in progress", async ({ page }) => {
  await page.goto(WALKTHROUGH)
  await backToInbox(page)

  const folder = page.getByRole("button", { name: "Platform", exact: true })
  await expect(folder).toHaveAttribute("aria-expanded", "true")
  await folder.click()
  await expect(folder).toHaveAttribute("aria-expanded", "false")
  await expect(page.getByRole("button", { name: /^打开 Sweep the unused exports$/ })).toHaveCount(0)
  await expect(page.getByRole("button", { name: /^打开 Trim the layout pass$/ })).toHaveCount(1)
  await expect(page.getByRole("button", { name: "在Platform新建对话" })).toBeVisible()
  await expect(page.getByRole("button", { name: /^打开 Summarise this week$/ })).toBeVisible()

  await folder.click()
  await expect(page.getByRole("button", { name: /^打开 Sweep the unused exports$/ })).toBeVisible()
  await expect(page.getByRole("button", { name: /^打开 Trim the layout pass$/ })).toHaveCount(2)
})

test("search narrows the inbox to the row that was typed", async ({ page }) => {
  await page.goto(WALKTHROUGH)
  await backToInbox(page)

  await page.getByLabel("搜索对话").fill("nightly")
  await expect(page.getByRole("button", { name: /^打开 Watch the nightly export$/ })).toHaveCount(2)
  await expect(page.getByRole("button", { name: /^打开 Trim the layout pass$/ })).toHaveCount(0)

  await page.getByRole("button", { name: "清除搜索" }).click()
  await expect(page.getByRole("button", { name: /^打开 Trim the layout pass$/ })).toHaveCount(2)
})

// An In progress row is not always a turn. A wait opens onto its own
// controls; Stop there would abort nothing. (The one-frame stub that a tap
// paints before `open` answers is `resume.test.ts` — Playwright waits past
// it.)
test("tapping a parked wait opens a wait, not a turn you can stop", async ({ page }) => {
  await page.goto(WALKTHROUGH)
  await backToInbox(page)

  await page.getByRole("button", { name: /^打开 Watch the nightly export$/ }).first().click()
  await expect(page.getByRole("heading", { name: "Watch the nightly export" })).toBeVisible()
  await expect(page.getByRole("button", { name: "停止" })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "立即运行" })).toBeVisible()
})

test("the composer grows with the text and send stays off while it is empty", async ({
  page,
}) => {
  await page.goto(WALKTHROUGH)
  const box = page.getByLabel("消息")
  const send = page.getByRole("button", { name: /跟进|发送/ })
  await expect(send).toBeDisabled()

  const oneLine = await box.evaluate((el) => el.clientHeight)
  await box.fill("one\ntwo\nthree\nfour")
  await expect(send).toBeEnabled()
  expect(await box.evaluate((el) => el.clientHeight)).toBeGreaterThan(oneLine)

  await box.fill("   ")
  await expect(send).toBeDisabled()
})

test("a page loaded with more is still there after the inbox refreshes", async ({ page }) => {
  await page.goto(WALKTHROUGH)
  await backToInbox(page)
  const recent = page.getByTestId("inbox-list").locator("section").filter({
    has: page.getByRole("heading", { name: "最近", exact: true }),
  })
  await recent.getByRole("button", { name: "更多" }).click()
  const older = page.getByRole("button", { name: /^打开 Page past the first$/ })
  await expect(older).toBeVisible()
  await page.waitForTimeout(2500)
  await expect(older).toBeVisible()
  await expect(recent.getByRole("button", { name: "更多" })).toHaveCount(0)
})

test("opening a conversation shows loading before the transcript", async ({ page }) => {
  await page.goto("/?mock=1&tick=0&pause=open")
  await expect(page.getByRole("status")).toContainText("加载中")
  await expect(page.getByTestId("transcript")).toBeVisible()
  await expect(page.getByRole("status")).toHaveCount(0)
})

test("the composer can pick a model, a thinking level, and a file", async ({ page }) => {
  await page.goto(WALKTHROUGH)
  await expect(page.getByLabel("模型")).toBeVisible()
  await expect(page.getByLabel("思考强度")).toBeVisible()
  await expect(page.getByLabel("添加文件")).toBeVisible()
  await page.getByRole("button", { name: "模型" }).click()
  await expect(page.getByRole("dialog", { name: "模型" }).getByRole("radio", { name: "scripted" })).toHaveCount(1)
  await page.keyboard.press("Escape")
  await expect(page.getByRole("option", { name: "低思考" })).toHaveCount(1)
})
