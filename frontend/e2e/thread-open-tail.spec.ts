import { expect, test } from "@playwright/test"
import { composer } from "./session"

for (const billedUsage of [false, true]) {
test(`a long conversation paints its tail before ${billedUsage ? "billed usage" : "context characters"} finishes loading`, async ({ page }, testInfo) => {
  const title = `Deferred context ${billedUsage}-${testInfo.retry}`
  const created = await page.request.post("/api/threads", { data: { title } })
  expect(created.ok()).toBe(true)
  const { thread } = await created.json()
  const events = Array.from({ length: 80 }, (_, index) => ({
    thread_id: thread.id, turn_id: "tail-turn", agent_id: "manager", seq: 4001 + index,
    kind: index === 0 ? "user_message" : index === 79 ? "agent_message" : "tool_result",
    text: index === 0 ? "Latest retained input" : index === 79 ? "Latest retained answer" : `tool payload ${index}`,
    tool_call_id: index > 0 && index < 79 ? `tool-${index}` : undefined,
    created_at: "2026-01-01T00:00:00Z",
  }))
  await page.route(new RegExp(`/api/threads/${thread.id}/log(?:\\?|$)`), (route) => route.fulfill({
    json: { events, has_more: true, roster: [] },
  }))
  let releaseDetail!: () => void
  const pending = new Promise<void>((resolve) => { releaseDetail = resolve })
  let detailRequested = false
  let detailReleased = false
  await page.route(new RegExp(`/api/threads/${thread.id}$`), async (route) => {
    const response = await route.fetch()
    const detail = await response.json()
    detailRequested = true
    await pending
    detailReleased = true
    await route.fulfill({ json: {
      ...detail,
      thread: { ...detail.thread, context_chars: 2000, context_budget: 8000 },
      usage: billedUsage ? { context_tokens: 400, context_window: 1000,
        turn: { prompt_tokens: 400, completion_tokens: 0, total_tokens: 400, cached_tokens: 0, reasoning_tokens: 0, calls: 1 },
        thread: { prompt_tokens: 400, completion_tokens: 0, total_tokens: 400, cached_tokens: 0, reasoning_tokens: 0, calls: 1 } } : undefined,
    } })
  })
  await page.route("**/api/models", async (route) => {
    const response = await route.fetch()
    const catalog = await response.json()
    await route.fulfill({ json: { ...catalog, models: catalog.models.map((model: object) => ({ ...model, context_window: 1000 })) } })
  })
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.locator(`[data-testid="thread-row"][data-id="${thread.id}"]`).getByTestId("row-label").click()
  try {
    await expect.poll(() => detailRequested).toBe(true)
    await expect(page.getByTestId("transcript")).toContainText("Latest retained answer")
    expect(detailReleased).toBe(false)
    await expect(page.getByTestId("context-meter")).toHaveCount(0)
    await page.screenshot({ path: testInfo.outputPath("tail-before-context.png") })
  } finally {
    releaseDetail()
  }
  if (billedUsage) {
    await expect(page.getByTestId("context-meter")).toHaveAttribute("aria-label", "40% context used")
    await expect(page.getByTestId("context-meter").locator("[data-fill]")).toHaveAttribute("data-fill", "0.40")
  }
  // Without billed tokens, the compact hint uses retained replay characters.
  await composer(page).fill("/comp")
  await expect(page.getByTestId("slash-command-compact")).toContainText(billedUsage ? "40% full" : "25% full")
  await page.screenshot({ path: testInfo.outputPath("context-after-tail.png") })
})

}
