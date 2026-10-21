import { expect, test, type APIRequestContext, type Page } from "@playwright/test"

async function openStaleQueue(page: Page, request: APIRequestContext, locale = "en") {
  await request.put("/api/settings", { data: { ui: { locale } } })
  const created = await request.post("/api/threads", { data: { title: "Queue error acceptance" } })
  const { thread } = await created.json() as { thread: { id: string } }
  const text = "waiting message from an earlier list"
  const fid = "fu_0000000000000000"
  let injecting = false
  let reads = 0
  // The client has an older list than the server. The POST itself reaches
  // the real HTTP/engine/store boundary and must fail without resending text.
  await page.route(`**/api/threads/${thread.id}/followups`, async (route) => {
    reads++
    if (injecting) await route.continue()
    else await route.fulfill({ json: { followups: [{
      id: fid, thread_id: thread.id, seq: 1, text, created_at: new Date().toISOString(),
    }] } })
  })
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.locator(`[data-testid="thread-row"][data-id="${thread.id}"]`).getByTestId("row-label").click()
  await expect(page.getByTestId("followup-queue")).toContainText(text)
  return { id: thread.id, fid, text, begin: () => { injecting = true }, reads: () => reads }
}

test.afterEach(async ({ request }) => {
  await request.put("/api/settings", { data: { ui: { locale: "system" } } })
})

for (const locale of ["en", "zh"] as const) {
  test(`a repeated stale-row click sends one request and explains the refreshed queue (${locale})`, async ({ page, request }) => {
    const row = await openStaleQueue(page, request, locale)
    const path = `/api/threads/${row.id}/followups/${row.fid}/steer`
    let release!: () => void
    const held = new Promise<void>((resolve) => { release = resolve })
    let posts = 0
    let resends = 0
    page.on("request", (req) => {
      if (req.method() === "POST" && new URL(req.url()).pathname === `/api/threads/${row.id}/steer`) resends++
    })
    await page.route(`**${path}`, async (route) => {
      posts++
      row.begin()
      await held
      await route.continue()
    })
    const response = page.waitForResponse((res) => new URL(res.url()).pathname === path)
    const steer = page.getByTestId("followup-queue").getByRole("button", {
      name: locale === "zh" ? `注入：${row.text}` : `Steer: ${row.text}`,
    })
    // Use the displayed button twice while the first real request is pending.
    await steer.dblclick()
    await expect.poll(() => posts).toBe(1)
    release()
    expect((await response).status()).toBe(404)
    const explanation = locale === "zh" ? "这条消息已不在待发送队列中" : "This message is no longer in the waiting queue"
    await expect(page.getByText(explanation, { exact: false })).toBeVisible()
    await expect(page.getByTestId("followup-queue")).toHaveCount(0)
    expect(posts).toBe(1)
    expect(row.reads()).toBe(2)
    expect(resends).toBe(0)
    await expect(page.getByText("store: not found", { exact: true })).toHaveCount(0)
    if (locale === "zh") await page.screenshot({ path: "test-results/followup-error-zh.png" })
  })
}

test("a deleted conversation remains a conversation error rather than claiming the queued message was sent", async ({ page, request }) => {
  const row = await openStaleQueue(page, request)
  await request.delete(`/api/threads/${row.id}`)
  await page.getByTestId("followup-queue").getByRole("button", { name: `Steer: ${row.text}` }).click()
  await expect(page.getByText("The conversation for this action no longer exists.", { exact: false })).toBeVisible()
  await expect(page.getByTestId("followup-queue")).toContainText(row.text)
  expect(row.reads()).toBe(1)
  await expect(page.getByText("The queue has been refreshed.", { exact: false })).toHaveCount(0)
})

test("a late queued-message error cannot appear in another conversation", async ({ page, request }) => {
  const row = await openStaleQueue(page, request)
  let release!: () => void
  const held = new Promise<void>((resolve) => { release = resolve })
  let started = false
  await page.route(`**/api/threads/${row.id}/followups/${row.fid}/steer`, async (route) => {
    started = true
    await held
    await route.continue()
  })
  await page.getByTestId("followup-queue").getByRole("button", { name: `Steer: ${row.text}` }).click()
  await expect.poll(() => started).toBe(true)
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(page.getByTestId("followup-queue")).toHaveCount(0)
  const response = page.waitForResponse((res) => res.url().endsWith(`/${row.fid}/steer`))
  release()
  expect((await response).status()).toBe(404)
  await expect(page.getByText("This message is no longer in the waiting queue", { exact: false })).toHaveCount(0)
  await expect(page.getByText("store: not found", { exact: true })).toHaveCount(0)
  expect(row.reads()).toBe(1)
})
