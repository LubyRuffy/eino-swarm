import { expect, test } from "@playwright/test"

test("replayed whitespace answers reserve no vertical space", async ({ page }, info) => {
  const title = `Whitespace layout ${info.workerIndex}-${info.retry}`
  const created = await page.request.post("/api/threads", { data: { title } })
  expect(created.ok()).toBe(true)
  const { thread } = await created.json()
  const base = { thread_id: thread.id, turn_id: "turn", agent_id: "manager", created_at: "2026-01-01T00:00:00Z" }
  const events = [
    { ...base, seq: 1, kind: "user_message", text: "Review the findings." },
    { ...base, seq: 2, kind: "agent_message", message_id: "first", text: "Initial findings are ready." },
    ...Array.from({ length: 27 }, (_, i) => ({
      ...base, seq: i + 3, kind: "agent_message", message_id: `blank-${i}`, text: i % 2 ? "\n\n" : " \t\r\n",
    })),
    { ...base, seq: 30, kind: "agent_message", message_id: "last", text: "Final findings are ready." },
    { ...base, seq: 31, kind: "done", text: "Final findings are ready." },
  ]
  await page.route(new RegExp(`/api/threads/${thread.id}/log(?:\\\\?|$)`), (route) =>
    route.fulfill({ json: { events, roster: [], has_more: false } }),
  )
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: title, exact: true }).click()
  const messages = page.getByTestId("transcript").getByTestId("assistant-message")
  await expect(messages).toHaveCount(2)
  await expect(messages.first()).toContainText("Initial findings")
  await expect(messages.last()).toContainText("Final findings")
  const first = await messages.first().boundingBox()
  const last = await messages.last().boundingBox()
  expect(first).not.toBeNull()
  expect(last).not.toBeNull()
  expect(last!.y - first!.y - first!.height).toBeLessThanOrEqual(8)
})
