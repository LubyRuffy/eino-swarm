import { expect, test } from "@playwright/test"

test("jump to latest supersedes an earlier turn jump when later live blocks arrive", async ({ page }, info) => {
  await page.setViewportSize({ width: 1280, height: 520 })
  const title = `Latest view stays selected ${info.workerIndex}-${info.retry}`
  const created = await page.request.post("/api/threads", { data: { title } })
  expect(created.ok()).toBe(true)
  const { thread } = await created.json()
  const base = { thread_id: thread.id, agent_id: "manager", created_at: "2026-01-01T00:00:00Z" }
  const answer = Array.from({ length: 16 }, (_, index) => `Recorded finding ${index + 1}: the current evidence is ready for review.`).join("\n\n")
  const events = [
    { ...base, seq: 1, turn_id: "earlier-turn", kind: "user_message", text: "Review the earlier findings." },
    { ...base, seq: 2, turn_id: "earlier-turn", kind: "agent_message", message_id: "earlier-answer", text: answer },
    { ...base, seq: 3, turn_id: "earlier-turn", kind: "done", text: "Earlier review complete." },
    { ...base, seq: 4, turn_id: "latest-turn", kind: "user_message", text: "Review the current findings." },
    { ...base, seq: 5, turn_id: "latest-turn", kind: "agent_message", message_id: "latest-answer", text: answer },
  ]
  await page.route(new RegExp(`/api/threads/${thread.id}/log(?:\\?|$)`), (route) => route.fulfill({ json: { events, roster: [], has_more: false } }))

  let releaseFirst!: () => void
  let releaseSecond!: () => void
  const first = new Promise<void>((resolve) => { releaseFirst = resolve })
  const second = new Promise<void>((resolve) => { releaseSecond = resolve })
  let delivery = 0
  await page.route(new RegExp(`/api/threads/${thread.id}/events(?:\\?|$)`), async (route) => {
    const batch = ++delivery
    if (batch > 2) { await route.fulfill({ status: 204 }); return }
    await (batch === 1 ? first : second)
    const event = { ...base, turn_id: "latest-turn", seq: 5 + batch, kind: "agent_message", message_id: `live-answer-${batch}`, text: `Live finding batch ${batch} is available.` }
    await route.fulfill({ contentType: "text/event-stream", body: `id: ${event.seq}\nevent: ${event.kind}\ndata: ${JSON.stringify(event)}\n\n` })
  })

  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: title, exact: true }).click()
  const transcript = page.getByTestId("transcript")
  const ticks = page.getByTestId("turn-nav").locator("[data-turn-nav-tick]")
  await expect(ticks).toHaveCount(2)
  await expect(ticks.last()).toHaveAttribute("aria-current", "true")
  await ticks.first().click()
  await expect(ticks.first()).toHaveAttribute("aria-current", "true")

  // New content offers the explicit live-edge action. No wheel event is used:
  // that used to be the only action that cancelled a remembered turn jump.
  releaseFirst()
  await expect(transcript).toContainText("Live finding batch 1 is available.")
  const jump = page.getByTestId("jump-to-latest")
  await expect(jump).toBeVisible()
  await jump.click()
  await expect(jump).toBeHidden()
  expect(await transcript.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight)).toBeLessThanOrEqual(24)

  // The next SSE connection releases a separate block after the click. It
  // must follow the latest view rather than re-apply the old rail selection.
  releaseSecond()
  await expect(transcript).toContainText("Live finding batch 2 is available.")
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))))
  await page.screenshot({ path: info.outputPath("jump-latest-after-late-block.png") })
  expect(await transcript.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight)).toBeLessThanOrEqual(24)
  await expect(jump).toBeHidden()
  await expect(ticks.last()).toHaveAttribute("aria-current", "true")
})
