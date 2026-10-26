import { expect, test, type Locator, type Page } from "@playwright/test"

import { reloadOpenConversation } from "./session"

const firstEndedAt = "2026-01-01T00:00:10.000Z"
const secondEndedAt = "2026-01-01T00:00:30.000Z"
const firstAnswer = "The initial check is complete."
const secondAnswer = "The follow-up check is complete."
const toolDetail = "Stored tool detail"
const internalBriefing = "Internal rolling session briefing"

function storedEvents(threadId: string, failedReview: boolean) {
  const base = { thread_id: threadId, turn_id: "earlier-turn", agent_id: "manager" }
  const rows = [
    { kind: "user_message", text: "Continue the assigned checks.", created_at: "2026-01-01T00:00:00.000Z" },
    { kind: "goal_session", created_at: "2026-01-01T00:00:00.000Z" },
    { kind: "tool_call", text: 'exec({"command":"printf prior-check"})', tool_call_id: "earlier-call", created_at: "2026-01-01T00:00:05.000Z" },
    { kind: "tool_result", text: JSON.stringify({ stdout: toolDetail, exit_code: 0 }), tool_call_id: "earlier-call", created_at: "2026-01-01T00:00:06.000Z" },
    { kind: "agent_message", text: firstAnswer, created_at: "2026-01-01T00:00:08.000Z" },
    { kind: "done", text: firstAnswer, created_at: firstEndedAt },
    { turn_id: "later-turn", kind: "goal_continued", created_at: "2026-01-01T00:00:20.000Z" },
    { turn_id: "later-turn", kind: "agent_message", text: secondAnswer, created_at: "2026-01-01T00:00:28.000Z" },
    { turn_id: "later-turn", kind: "done", text: secondAnswer, created_at: secondEndedAt },
    // The original turn's post-turn work may finish after the next check.
    { kind: "session_memory", text: internalBriefing, created_at: "2026-01-01T00:00:40.000Z" },
  ]
  if (failedReview) rows.push({
    kind: "memory_review",
    text: JSON.stringify({ changed: false, err: "The review ended without submitting a complete memory proposal." }),
    created_at: "2026-01-01T00:00:41.000Z",
  })
  return rows.map((row, index) => ({ ...base, seq: index + 1, ...row }))
}

async function openReplay(page: Page, title: string, options: { failedReview: boolean; paginate?: boolean }) {
  const created = await page.request.post("/api/threads", { data: { title } })
  expect(created.ok()).toBe(true)
  const threadId = (await created.json()).thread.id as string
  expect(threadId).toBeTruthy()
  const events = storedEvents(threadId, options.failedReview)
  const beforeRequests: number[] = []
  await page.route(new RegExp(`/api/threads/${threadId}/log(?:\\?|$)`), async (route) => {
    const response = await route.fetch()
    const body = await response.json()
    const before = Number(new URL(route.request().url()).searchParams.get("before"))
    if (before) beforeRequests.push(before)
    // A quiet tail must not strand the reader without the original turn.
    // A visible failed review also needs older history to remain reachable.
    const tailOnly = options.paginate && !before
    body.events = tailOnly ? events.slice(9) : before ? events.filter((event) => event.seq < before) : events
    body.has_more = Boolean(tailOnly)
    body.roster = []
    await route.fulfill({ response, json: body })
  })
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: title, exact: true }).click()
  return beforeRequests
}

async function expectTwoCompletedTurns(transcript: Locator) {
  const boundaries = transcript.getByTestId("turn-boundary")
  await expect(boundaries).toHaveCount(2)
  await expect(boundaries.nth(0).locator("time")).toHaveAttribute("datetime", firstEndedAt)
  await expect(boundaries.nth(1).locator("time")).toHaveAttribute("datetime", secondEndedAt)
  await expect(transcript.getByTestId("goal-session")).toHaveCount(0)
  await expect(transcript.getByTestId("assistant-message").filter({ hasText: firstAnswer })).toBeVisible()
  await expect(transcript.getByTestId("assistant-message").filter({ hasText: secondAnswer })).toBeVisible()
  await expect(transcript).not.toContainText(internalBriefing)
}

test("late quiet session metadata does not create an empty completion row after a newer turn", async ({ page }, testInfo) => {
  await openReplay(page, `Quiet late review ${testInfo.workerIndex}-${testInfo.retry}`, { failedReview: false })
  for (let replay = 0; replay < 2; replay++) {
    await expectTwoCompletedTurns(page.getByTestId("transcript"))
    if (replay === 0) await reloadOpenConversation(page)
  }
})

test("late review failure keeps its original details reachable through paged history and reload", async ({ page }, testInfo) => {
  const beforeRequests = await openReplay(page, `Failed late review ${testInfo.workerIndex}-${testInfo.retry}`, { failedReview: true, paginate: true })
  const transcript = page.getByTestId("transcript")
  for (let replay = 0; replay < 2; replay++) {
    // Reaching the top uses the real history loader, including its merge.
    await transcript.evaluate((element) => { element.scrollTop = 0; element.dispatchEvent(new Event("scroll")) })
    await expectTwoCompletedTurns(transcript)
    expect(beforeRequests.filter((before) => before === 10)).toHaveLength(replay + 1)
    const failure = transcript.getByTestId("memory-notice").filter({ hasText: "Memory review failed" })
    await expect(failure).toHaveCount(1)
    await expect(failure).toContainText("Memory review failed")
    await expect(failure.getByRole("button", { name: "Trace earlier-turn" })).toBeVisible()
    const boundaryBox = await transcript.getByTestId("turn-boundary").last().boundingBox()
    const failureBox = await failure.boundingBox()
    expect(boundaryBox).not.toBeNull()
    expect(failureBox).not.toBeNull()
    expect(failureBox!.y).toBeGreaterThan(boundaryBox!.y)

    await expect(transcript.locator(".md").getByText(firstAnswer, { exact: true })).toBeVisible()
    const tool = transcript.getByRole("button", { name: /exec.*printf prior-check/ })
    await expect(tool).toHaveAttribute("aria-expanded", "false")
    await expect(transcript.getByText(toolDetail, { exact: true })).toBeHidden()
    await tool.click()
    await expect(transcript.getByText(toolDetail, { exact: true })).toBeVisible()
    await expect(transcript.locator(".md").getByText(secondAnswer, { exact: true })).toBeVisible()
    await expectTwoCompletedTurns(transcript)
    if (replay === 0) await reloadOpenConversation(page)
  }
  await page.screenshot({ path: testInfo.outputPath("late-review-footer.png") })
})
