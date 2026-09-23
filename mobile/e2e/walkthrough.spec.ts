import { expect, test, type Page } from "@playwright/test"

/** The inbox and a conversation against the scripted host. `tick=0` plays a
 *  turn as fast as the browser will paint it; the product path is the same
 *  screens over a real link. */
const WALKTHROUGH = "/?mock=1&tick=0"

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
  await page.getByRole("button", { name: "更多" }).click()
  const older = page.getByRole("button", { name: /^打开 Page past the first$/ })
  await expect(older).toBeVisible()
  await page.waitForTimeout(2500)
  await expect(older).toBeVisible()
  await expect(page.getByRole("button", { name: "更多" })).toHaveCount(0)
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
  await expect(page.getByRole("option", { name: "scripted" })).toHaveCount(1)
  await expect(page.getByRole("option", { name: "低思考" })).toHaveCount(1)
})
