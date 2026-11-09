import { expect, test } from "@playwright/test"

// The history is a deterministic UI fixture; real model restoration and durable
// overlays are exercised separately by the Registry/HTTP Go integration test.
test("explicitly repairs a missing worker provider using the displayed model", async ({ page }, testInfo) => {
  const title = `Worker recovery ${testInfo.retry}`
  const models = (await (await page.request.get("/api/models")).json()).models
  const selected = models.find((row: { ready: boolean; model: string }) => row.ready && row.model)
  expect(selected).toBeTruthy()
  const created = await page.request.post("/api/threads", { data: { title, provider_id: selected.provider_id } })
  const { thread } = await created.json()
  expect((await page.request.patch(`/api/threads/${thread.id}`, { data: { model: selected.model } })).ok()).toBe(true)
  const base = { thread_id: thread.id, turn_id: "failed-worker-restore", agent_id: "manager", created_at: "2026-01-01T00:00:00Z" }
  await page.route(new RegExp(`/api/threads/${thread.id}/log(?:\\?|$)`), (route) => route.fulfill({ json: {
    events: [{ ...base, seq: 1, kind: "user_message", text: "Continue" },
      { ...base, seq: 2, kind: "error", err: 'restore worker configuration: provider: unknown provider "removed"' }],
    has_more: false,
  } }))
  const submissions: unknown[] = []
  await page.route(`**/api/threads/${thread.id}/worker-model-recovery`, async (route) => {
    submissions.push(route.request().postDataJSON())
    await route.fulfill({ json: { configurations: 1 } })
  })
  let turnsStarted = 0
  page.on("request", (request) => {
    if (request.method() === "POST" && request.url().endsWith(`/threads/${thread.id}/turns`)) turnsStarted++
  })
  try {
    await page.goto("/")
    await page.getByTestId("dest-chats").click()
    await page.locator(`[data-testid="thread-row"][data-id="${thread.id}"]`).getByTestId("row-label").click()
    const repair = page.getByTestId("worker-model-recovery")
    await expect(repair).toBeVisible()
    expect(submissions).toHaveLength(0)
    await repair.getByRole("button", { name: `Repair recovery using ${selected.model}` }).click()
    await expect(repair.getByRole("status")).toContainText("You can now Continue or Retry")
    await expect(repair.getByRole("button")).toBeDisabled()
    expect(submissions).toEqual([{ provider_id: selected.provider_id, model: selected.model }])
    expect(turnsStarted).toBe(0)
    await page.screenshot({ path: testInfo.outputPath("worker-recovery.png"), fullPage: true })
  } finally {
    expect((await page.request.delete(`/api/threads/${thread.id}`)).ok()).toBe(true)
  }
})
