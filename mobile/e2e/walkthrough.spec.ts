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

  // Running is happening now, so it carries no age; a wait does, and a
  // roster row is never also a Recents row it could borrow one from.
  const live = page.getByRole("button", { name: /^打开 Trim the layout pass$/ })
  await expect(live.getByTestId("row-state")).toHaveAttribute("data-state", "running")
  await expect(live).not.toContainText("前")
  const parked = page.getByRole("button", { name: /^打开 Watch the nightly export$/ })
  await expect(parked.getByTestId("row-state")).toHaveAttribute("data-state", "waiting")
  // A parked wait has no live action; its own summary is what the row says.
  await expect(parked).toContainText("Checking again after the next run")
  await expect(parked).toContainText("前")

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

test("starting from the inbox opens the new conversation", async ({ page }) => {
  await page.goto(WALKTHROUGH)
  await backToInbox(page)

  await page.getByRole("radio", { name: "Field notes" }).click()
  await page.getByLabel("新消息").fill("a fresh one")
  await page.getByRole("button", { name: "开始" }).click()

  await expect(page.getByRole("heading", { name: "a fresh one" })).toBeVisible()
  await expect(page.getByTestId("transcript").getByText("Here is what changed:")).toBeVisible()
})

// An In progress row is not always a turn. A wait opens onto its own
// controls; Stop there would abort nothing. (The one-frame stub that a tap
// paints before `open` answers is `resume.test.ts` — Playwright waits past
// it.)
test("tapping a parked wait opens a wait, not a turn you can stop", async ({ page }) => {
  await page.goto(WALKTHROUGH)
  await backToInbox(page)

  await page.getByRole("button", { name: /^打开 Watch the nightly export$/ }).click()
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
