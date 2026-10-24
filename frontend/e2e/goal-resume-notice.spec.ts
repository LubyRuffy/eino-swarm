import { expect, test, type Page } from "@playwright/test"
import { writeFile } from "node:fs/promises"

import { visibleGoalNotices } from "../src/lib/goal-notice"
import { emptyTranscript, reduceEvents } from "../src/lib/transcript"
import type { SwarmEvent } from "../src/lib/types"
import { composer, freshConversation, reloadOpenConversation, waitForIdle } from "./session"

type Event = { seq: number; kind: string; turn_id: string; text?: string; role?: string }
type Turn = { id: string; status: string }
type Log = { events: Event[]; has_more: boolean }

async function expandSessions(page: Page) {
  for (const session of await page.getByTestId("goal-session").all()) {
    if (await session.getAttribute("aria-expanded") === "false") await session.click()
  }
}

async function expectContinuationNotices(page: Page, count: number) {
  const notices = page.getByTestId("transcript").getByTestId("memory-notice")
  await expect.poll(async () => {
    await expandSessions(page)
    return {
      total: await notices.filter({ hasText: /^(Resuming|Continuing) the standing objective\.$/ }).count(),
      resumed: await notices.filter({ hasText: /^Resuming the standing objective\.$/ }).count(),
    }
  }).toEqual({ total: count, resumed: 0 })
}

test("one goal restore shows one notice on explicit resume and replay while paged Trace retains both events", async ({ page, request }, info) => {
  await freshConversation(page)
  const threadId = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
  expect(threadId).toBeTruthy()
  await composer(page).fill("/goal Complete the assigned verification.")
  await composer(page).press("Enter")
  await expect(page.getByTestId("transcript").getByText("Standing objective completed.", { exact: true })).toHaveCount(1, { timeout: 60_000 })
  await expect(page.getByTestId("goal-banner")).toHaveCount(0)
  await waitForIdle(page)

  const originalTurns = (await (await request.get(`/api/threads/${threadId}/turns`)).json()).turns as Turn[]
  const originalTurnId = originalTurns.at(-1)!.id
  expect((await request.patch(`/api/threads/${threadId}`, { data: { goal_resume: true } })).ok()).toBe(true)
  let firstContinuation: Event | undefined
  await expect.poll(async () => {
    const log = await (await request.get(`/api/threads/${threadId}/log`)).json() as Log
    firstContinuation = log.events.find((event) => event.kind === "goal_continued")
    return Boolean(firstContinuation)
  }).toBe(true)
  await expectContinuationNotices(page, 1)
  await expect.poll(async () => {
    const trace = await (await request.get(`/api/trace/${firstContinuation!.turn_id}`)).json()
    return trace.turn.status
  }).toBe("done")
  await expect(page.getByTestId("transcript").getByText("Standing objective completed.", { exact: true })).toHaveCount(2, { timeout: 60_000 })
  await expect(page.getByTestId("goal-banner")).toHaveCount(0)
  await waitForIdle(page)
  await expectContinuationNotices(page, 1)

  const resumedTrace = await (await request.get(`/api/trace/${originalTurnId}`)).json()
  const continuedTrace = await (await request.get(`/api/trace/${firstContinuation!.turn_id}`)).json()
  expect(resumedTrace.events.filter((event: Event) => event.kind === "goal_resumed")).toHaveLength(1)
  expect(continuedTrace.events.filter((event: Event) => event.kind === "goal_continued")).toHaveLength(1)
  const afterFirst = (await (await request.get(`/api/threads/${threadId}/turns`)).json()).turns as Turn[]
  expect(afterFirst).toHaveLength(originalTurns.length + 1)

  await reloadOpenConversation(page)
  await expectContinuationNotices(page, 1)
  await page.getByTestId("transcript").getByText("Continuing the standing objective.", { exact: true }).scrollIntoViewIfNeeded()
  await page.screenshot({ path: info.outputPath("first-resume-one-notice.png"), animations: "disabled" })
  await page.getByRole("tab", { name: "Trace", exact: true }).click()
  await page.getByTestId("trace-log-toggle").click()
  await expect(page.getByTestId("trace-log")).toContainText("Continuing the standing objective.")

  // Another genuine restore may use identical words, but owns a distinct turn.
  expect((await request.patch(`/api/threads/${threadId}`, { data: { goal_resume: true } })).ok()).toBe(true)
  await expect.poll(async () => {
    const { turns } = await (await request.get(`/api/threads/${threadId}/turns`)).json()
    return turns.length === originalTurns.length + 2 && turns.at(-1).status === "done"
  }).toBe(true)
  await expect(page.getByTestId("transcript").getByText("Standing objective completed.", { exact: true })).toHaveCount(3, { timeout: 60_000 })
  await expect(page.getByTestId("goal-banner")).toHaveCount(0)
  await waitForIdle(page)
  await expectContinuationNotices(page, 2)
  const completeLog = await (await request.get(`/api/threads/${threadId}/log`)).json() as Log
  const continuations = completeLog.events.filter((event) => event.kind === "goal_continued")
  expect(continuations).toHaveLength(2)
  expect(completeLog.events.filter((event) => event.kind === "goal_resumed")).toHaveLength(2)
  const pageStart = continuations[1].seq
  const beforeRequests: number[] = []
  await page.route(new RegExp(`/api/threads/${threadId}/log(?:\\?|$)`), async (route) => {
    const response = await route.fetch()
    const body = await response.json() as Log
    const before = Number(new URL(route.request().url()).searchParams.get("before"))
    if (before) beforeRequests.push(before)
    else {
      // Separate the real stored pair at the first-page boundary.
      body.events = body.events.filter((event) => event.seq >= pageStart)
      body.has_more = true
    }
    await route.fulfill({ response, json: body })
  })
  await reloadOpenConversation(page)
  const transcript = page.getByTestId("transcript")
  await transcript.evaluate((element) => { element.scrollTop = 0; element.dispatchEvent(new Event("scroll")) })
  await expect.poll(() => beforeRequests.includes(pageStart)).toBe(true)
  await expectContinuationNotices(page, 2)
  const replayedLog = await (await request.get(`/api/threads/${threadId}/log`)).json() as Log
  expect(replayedLog.events.filter((event) => event.kind === "goal_resumed" || event.kind === "goal_continued"))
    .toEqual(completeLog.events.filter((event) => event.kind === "goal_resumed" || event.kind === "goal_continued"))
  await transcript.getByText("Continuing the standing objective.", { exact: true }).last().scrollIntoViewIfNeeded()
  await page.screenshot({ path: info.outputPath("goal-resume-single-notice.png"), animations: "disabled" })
  await writeFile(info.outputPath("goal-resume-trace-evidence.json"), JSON.stringify({
    originalTurnId, firstContinuation, resumedTrace, continuedTrace, beforeRequests,
    lifecycle: completeLog.events.filter((event) => event.kind === "goal_resumed" || event.kind === "goal_continued"),
  }, null, 2), { mode: 0o600 })
})

for (const review of [
  { label: "changed", outcome: { changed: true, notes: { add: 1 }, notify: "on" }, text: "Memory updated: 1 note stored." },
  { label: "failed", outcome: { changed: false, err: "The review could not finish.", notify: "on" }, text: "Memory review failed" },
]) {
  test(`browser replay keeps a late ${review.label} review beside one goal continuation`, async ({ page }, info) => {
    const title = `Goal review replay ${review.label} ${info.workerIndex}-${info.retry}`
    const created = await page.request.post("/api/threads", { data: { title } })
    expect(created.ok()).toBe(true)
    const threadId = (await created.json()).thread.id as string
    expect(threadId).toBeTruthy()
    const base = { thread_id: threadId, turn_id: "earlier-turn", agent_id: "manager" }
    const events: SwarmEvent[] = [
      { ...base, seq: 1, kind: "goal_resumed", role: "human", created_at: "2026-01-01T00:00:01.000Z" },
      { ...base, seq: 2, kind: "memory_review", agent_id: "memory-reviewer", text: JSON.stringify(review.outcome), created_at: "2026-01-01T00:00:02.000Z" },
      { ...base, seq: 3, kind: "goal_continued", turn_id: "later-turn", created_at: "2026-01-01T00:00:03.000Z" },
      { ...base, seq: 4, kind: "done", turn_id: "later-turn", text: "The assigned check is complete.", created_at: "2026-01-01T00:00:04.000Z" },
    ]
    // This is a browser history fixture, not a claim of new server activity.
    // Trace reads these raw reducer blocks; projection must retain all three.
    const stored = reduceEvents(emptyTranscript(), events).agents.manager.blocks
    expect(visibleGoalNotices(stored).filter((block) => block.goalNoticeKind).map((block) => block.seq)).toEqual([3])
    expect(stored.filter((block) => block.goalNoticeKind || block.review).map((block) => block.seq)).toEqual([1, 2, 3])

    let paginate = false
    const beforeRequests: number[] = []
    await page.route(new RegExp(`/api/threads/${threadId}/log(?:\\?|$)`), async (route) => {
      const response = await route.fetch()
      const body = await response.json()
      const before = Number(new URL(route.request().url()).searchParams.get("before"))
      if (before) beforeRequests.push(before)
      const tailOnly = paginate && !before
      body.events = tailOnly ? events.filter((event) => event.seq >= 3) : before ? events.filter((event) => event.seq < before) : events
      body.has_more = tailOnly
      body.roster = []
      await route.fulfill({ response, json: body })
    })
    await page.goto("/")
    await page.getByTestId("dest-chats").click()
    await page.getByRole("button", { name: title, exact: true }).click()
    const transcript = page.getByTestId("transcript")
    const reviewNotice = transcript.getByTestId("memory-notice").filter({ hasText: review.text })
    for (let replay = 0; replay < 3; replay++) {
      if (replay) await reloadOpenConversation(page)
      if (paginate) {
        await transcript.evaluate((element) => { element.scrollTop = 0; element.dispatchEvent(new Event("scroll")) })
        await expect.poll(() => beforeRequests.includes(3)).toBe(true)
      }
      await expectContinuationNotices(page, 1)
      await expect(reviewNotice).toBeVisible()
      if (replay === 1) paginate = true
    }
  })
}
