import { expect, test, type APIRequestContext, type Page } from "@playwright/test"

/** Every spec starts on its own conversation, so one failing run cannot leave
 *  state that breaks the next. */
async function freshConversation(page: Page) {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(page.getByTestId("composer-input")).toBeVisible()
}

const statusBadge = (page: Page) => page.getByTestId("status-badge")

async function waitForIdle(page: Page) {
  await expect(statusBadge(page)).toContainText("Idle", { timeout: 60_000 })
}

async function waitUntilUnread(request: APIRequestContext) {
  // Standalone Run now mints another conversation. The origin status badge
  // stays Idle, so unread is how we know the mock report finished.
  await expect
    .poll(
      async () => {
        const body = (await (await request.get("/api/schedules")).json()) as {
          unread?: number
        }
        return body.unread ?? 0
      },
      { timeout: 60_000 },
    )
    .toBeGreaterThan(0)
}

async function openInbox(page: Page) {
  await page.getByTestId("schedule-inbox").click()
  await expect(page.getByRole("dialog")).toBeVisible()
}

async function closeInbox(page: Page) {
  await page.keyboard.press("Escape")
  await expect(page.getByRole("dialog")).toHaveCount(0)
}

async function revealThread(page: Page, id: string) {
  const row = page.locator(`[data-testid="thread-row"][data-id="${id}"]`)
  if ((await row.count()) === 0) {
    const more = page.getByRole("button", { name: "Show more" })
    if ((await more.count()) > 0) await more.click()
  }
  await expect(row).toBeVisible()
  return row
}

test("standalone wait from the inbox runs now and opens findings", async ({
  page,
  request,
}) => {
  test.setTimeout(120_000)
  await freshConversation(page)

  await openInbox(page)
  const inbox = page.getByRole("dialog")
  await inbox.getByRole("button", { name: "Create" }).click()
  await inbox.getByLabel("Title").fill("periodic check")
  await inbox.getByLabel("Prompt").fill("Continue the wait.")
  await inbox.getByLabel("Cadence").click()
  await page.getByRole("option", { name: "Every (seconds)" }).click()
  await inbox.getByLabel("Every (seconds)").fill("60")
  await inbox.getByRole("button", { name: "Add wait" }).click()
  await expect(inbox.getByTestId("schedule-row")).toBeVisible()
  await expect(inbox.getByTestId("schedule-row")).toContainText("periodic check")

  await inbox.getByTestId("schedule-row-toggle").click()
  await inbox.getByRole("button", { name: "Run now" }).click()
  // The dialog inert-hides the title-bar status badge; close it so Idle is
  // readable, then wait for the minted turn via unread.
  await closeInbox(page)
  await waitForIdle(page)
  await waitUntilUnread(request)

  await openInbox(page)
  const unread = page.getByTestId("schedule-unread")
  const findings = page.getByRole("button", { name: "Open findings" })
  if (await unread.isVisible().catch(() => false)) {
    await expect(unread).not.toHaveText("0")
  }
  await expect(findings).toBeVisible()
  await findings.click()

  await expect(page.getByRole("dialog")).toHaveCount(0)
  await expect(page.getByTestId("thread-title")).toHaveText("periodic check")
  await expect(
    page.locator('[data-testid="thread-row"][aria-current="true"]'),
  ).toContainText("periodic check")
  await expect(page.getByTestId("schedule-notice")).toContainText("Scheduled check.")
  await expect(page.getByTestId("transcript")).not.toContainText(
    "This turn is a scheduled check.",
  )
  await expect(page.getByTestId("user-message")).toHaveCount(0)
})

test("a thread wake from REST shows a banner that cancel removes", async ({
  page,
  request,
}) => {
  await freshConversation(page)

  const listed = (await (await request.get("/api/threads")).json()) as {
    threads: { id: string; last_active_at?: string }[]
  }
  const activeId = await page
    .locator('[data-testid="thread-row"][aria-current="true"]')
    .getAttribute("data-id")
  const newest = [...(listed.threads ?? [])].sort(
    (a, b) =>
      Date.parse(b.last_active_at ?? "") - Date.parse(a.last_active_at ?? ""),
  )[0]
  const threadId = listed.threads.find((th) => th.id === activeId)?.id ?? newest?.id
  expect(threadId).toBeTruthy()

  const created = await request.post("/api/schedules", {
    data: {
      kind: "thread",
      thread_id: threadId,
      prompt: "Continue the wait.",
      every_s: 60,
    },
  })
  expect(created.status()).toBe(201)

  await page.reload()
  await expect(page.getByTestId("composer-input")).toBeVisible()
  const row = await revealThread(page, threadId!)
  if ((await row.getAttribute("aria-current")) !== "true") {
    await row.getByTestId("row-label").click()
  }

  await expect(page.getByTestId("schedule-banner")).toBeVisible()
  await expect(
    page.getByTestId("schedule-banner").getByRole("button", { name: "Run now" }),
  ).toBeVisible()
  await expect(statusBadge(page)).toContainText("Waiting")
  await expect(row.getByTestId("wait-mark")).toBeVisible()
  await page
    .getByTestId("schedule-banner")
    .getByRole("button", { name: "Cancel wait" })
    .click()
  await expect(page.getByTestId("schedule-banner")).toHaveCount(0)
  await expect(statusBadge(page)).toContainText("Idle")
  await expect(row.getByTestId("wait-mark")).toHaveCount(0)
})

test("a thread wake banner can run now instead of waiting", async ({
  page,
  request,
}) => {
  test.setTimeout(120_000)
  await freshConversation(page)

  const listed = (await (await request.get("/api/threads")).json()) as {
    threads: { id: string; last_active_at?: string }[]
  }
  const activeId = await page
    .locator('[data-testid="thread-row"][aria-current="true"]')
    .getAttribute("data-id")
  const newest = [...(listed.threads ?? [])].sort(
    (a, b) =>
      Date.parse(b.last_active_at ?? "") - Date.parse(a.last_active_at ?? ""),
  )[0]
  const threadId = listed.threads.find((th) => th.id === activeId)?.id ?? newest?.id
  expect(threadId).toBeTruthy()

  const created = await request.post("/api/schedules", {
    data: {
      kind: "thread",
      thread_id: threadId,
      prompt: "Continue the wait.",
      every_s: 60,
    },
  })
  expect(created.status()).toBe(201)

  await page.reload()
  await expect(page.getByTestId("composer-input")).toBeVisible()
  const row = await revealThread(page, threadId!)
  if ((await row.getAttribute("aria-current")) !== "true") {
    await row.getByTestId("row-label").click()
  }

  await expect(page.getByTestId("schedule-banner")).toBeVisible()
  await expect(statusBadge(page)).toContainText("Waiting")
  await expect(row.getByTestId("wait-mark")).toBeVisible()
  await page
    .getByTestId("schedule-banner")
    .getByRole("button", { name: "Run now" })
    .click()
  await expect(statusBadge(page)).toContainText("Working")
  await expect(page.getByTestId("schedule-banner")).toHaveCount(0)
  await expect(
    page.getByTestId("schedule-notice").filter({ hasText: "Scheduled check." }),
  ).toBeVisible()
  await expect(statusBadge(page)).toContainText("Waiting", { timeout: 60_000 })
  await expect(page.getByTestId("schedule-banner")).toBeVisible()
  await expect(row.getByTestId("wait-mark")).toBeVisible()
})
