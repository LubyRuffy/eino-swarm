import { expect, test, type Page } from "@playwright/test"
import { reloadOpenConversation } from "./session"

for (const historyFirst of [true, false]) {
  test(`worker history and SSE stay consistent with ${historyFirst ? "history" : "SSE"} first`, async ({ page }, testInfo) => {
    const title = `Worker history overlap ${historyFirst}-${testInfo.retry}`
    const created = await page.request.post("/api/threads", { data: { title } })
    expect(created.ok()).toBe(true)
    const { thread } = await created.json()
    const base = {
      thread_id: thread.id, turn_id: "check-1", agent_id: "worker-1",
      created_at: "2026-01-01T00:00:00Z",
    }
    const worker = [
      { ...base, seq: 41, kind: "reasoning", message_id: "output-a", text: "Inspect the source" },
      { ...base, seq: 42, kind: "tool_call", text: 'web_search({"query":"generic evidence"})', tool_call_id: "search-1" },
      { ...base, seq: 43, kind: "tool_result", text: "Complete tool evidence", tool_call_id: "search-1" },
      { ...base, turn_id: "check-2", seq: 44, kind: "reasoning", message_id: "output-a", text: "Inspect the source and evidence" },
      { ...base, turn_id: "check-3", seq: 45, kind: "reasoning", message_id: "output-a", text: "Inspect the source and evidence again" },
      // Same words from an independent model output remain a separate thought.
      { ...base, turn_id: "check-3", seq: 46, kind: "reasoning", message_id: "output-b", text: "Inspect the source and evidence again" },
      { ...base, seq: 47, kind: "reasoning", text: "Legacy stored thought remains once" },
      { ...base, seq: 48, kind: "reasoning", message_id: "output-c", text: "Current output checkpoint" },
    ]
    await page.route(new RegExp(`/api/threads/${thread.id}/log(?:\\?|$)`), (route) => route.fulfill({ json: {
      events: [{ ...base, seq: 40, kind: "user_message", agent_id: "manager", text: "Check the evidence" }],
      roster: [{ ...base, seq: 2, kind: "spawned", role: "worker", text: "Check the evidence" }], has_more: true,
    } }))
    await page.route(`**/api/threads/${thread.id}/agents/worker-1/log`, (route) => route.fulfill({ json: { events: worker } }))
    let release!: () => void
    const ready = new Promise<void>((resolve) => { release = resolve })
    if (!historyFirst) release()
    await page.route(new RegExp(`/api/threads/${thread.id}/events(?:\\?|$)`), async (route) => {
      await ready
      const live = { ...base, seq: 0, kind: "reasoning_delta", message_id: "output-c", text: "Current output checkpoint with live evidence" }
      const signal = { ...base, seq: 49, kind: "agent_message", agent_id: "manager", text: "Live overlap reached." }
      await route.fulfill({ contentType: "text/event-stream", body: [...worker, live, signal]
        .map((ev) => `id: ${ev.seq}\nevent: ${ev.kind}\ndata: ${JSON.stringify(ev)}\n\n`).join("") })
    })
    await page.goto("/")
    await page.getByTestId("dest-chats").click()
    await page.getByRole("button", { name: title, exact: true }).click()
    if (!historyFirst) await expect(page.getByTestId("transcript")).toContainText("Live overlap reached.")
    await openWorker(page)
    await expect(page.getByTestId("agent-scroller").getByTestId("thought-toggle")).toHaveCount(4)
    release()
    await expect(page.getByTestId("transcript")).toContainText("Live overlap reached.")
    await verifyWorker(page)
    await reloadOpenConversation(page)
    await openWorker(page)
    await verifyWorker(page)
    await page.screenshot({ path: testInfo.outputPath(`agent-history-overlap-${historyFirst ? "history" : "sse"}.png`) })
  })
}

async function openWorker(page: Page) {
  await page.getByRole("tab", { name: "Agents" }).click()
  await page.getByTestId("agent-roster").locator('[data-agent-id="worker-1"]').click()
}

async function verifyWorker(page: Page) {
  const log = page.getByTestId("agent-scroller")
  const thoughts = log.getByTestId("thought-toggle")
  await expect(thoughts).toHaveCount(4)
  for (const thought of await thoughts.all()) {
    if (await thought.getAttribute("aria-expanded") === "false") await thought.click()
  }
  await expect(log.getByText("Inspect the source and evidence again", { exact: true })).toHaveCount(2)
  await expect(log.getByText("Legacy stored thought remains once", { exact: true })).toHaveCount(1)
  await expect(log.getByText("Current output checkpoint with live evidence", { exact: true })).toBeVisible()
  await expect(log.getByText("Inspect the source", { exact: true })).toHaveCount(0)
  const search = log.getByRole("button", { name: /web_search.*generic evidence/ })
  await expect(search).toHaveCount(1)
  await search.click()
  await expect(log.getByText("Complete tool evidence", { exact: true })).toBeVisible()
  await expect(search).toContainText("generic evidence")
}
