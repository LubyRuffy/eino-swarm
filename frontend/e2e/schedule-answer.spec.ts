import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { composer, freshConversation, reloadOpenConversation } from "./session"
import { startScheduleAnswerFixture, unsupportedScheduleAnswer, verifiedScheduleAnswer } from "./schedule-answer-fixture"

type Event = { kind: string; text?: string; tool_call_id?: string }

test("a live unverified scheduling answer is corrected against an actual saved wake", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_SCHEDULE_ANSWER !== "1", "requires the offline answer admission fixture")
  const fixture = await startScheduleAnswerFixture()
  let scheduleID = ""
  let threadID = ""
  let trace: { turn: { id: string; status: string }; events: Event[]; llm_calls: { agent_id: string }[] } | undefined
  const history = /\/api\/threads\/[^/]+\/log(?:\?|$)/
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "answer-fixture", providers: [{ id: "answer-fixture", label: "Offline scheduling admission", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false, schedule_min_interval_seconds: 1 },
      memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    await page.addInitScript(() => {
      const NativeEventSource = window.EventSource
      const observed = [] as unknown[]
      ;(window as Window & { __answerRetryEvents?: unknown[] }).__answerRetryEvents = observed
      window.EventSource = class extends NativeEventSource {
        constructor(url: string | URL, options?: EventSourceInit) {
          super(url, options)
          this.addEventListener("model_retry", (event) => { observed.push(JSON.parse(event.data)) })
        }
      }
    })
    await freshConversation(page)
    threadID = (await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")) ?? ""
    expect(threadID).toBeTruthy()
    // History cannot supply the correction notice during this phase. The
    // actual named SSE subscription must paint it before scheduling succeeds.
    await page.route(history, (route) => route.fulfill({ json: { events: [], has_more: false } }))
    await composer(page).fill("Inspect the assigned task state.")
    await composer(page).press("Enter")
    const transcript = page.getByTestId("transcript")
    await expect(transcript).toContainText("Checking the schedule before reporting it as arranged.")
    await expect(transcript).not.toContainText(unsupportedScheduleAnswer)
    await expect(page.getByTestId("schedule-banner")).toHaveCount(0)
    await expect.poll(() => page.evaluate(() => (window as Window & { __answerRetryEvents?: unknown[] }).__answerRetryEvents?.length ?? 0)).toBe(1)
    expect((await (await request.get("/api/schedules")).json()).schedules).toHaveLength(0)
    expect(fixture.state()).toMatchObject({ managerRequests: 2, checks: 1 })
    await page.screenshot({ path: info.outputPath("unverified-answer-withheld.png"), animations: "disabled" })
    fixture.allowArm()
    await expect(transcript).toContainText(verifiedScheduleAnswer)
    await expect(transcript).not.toContainText(unsupportedScheduleAnswer)
    await expect(page.getByTestId("schedule-banner")).toBeVisible()
    await expect(page.getByTestId("status-badge")).toContainText("Waiting")
    await expect.poll(async () => {
      const { turns } = await (await request.get(`/api/threads/${threadID}/turns`)).json()
      if (!turns[0]) return "missing"
      trace = await (await request.get(`/api/trace/${turns[0].id}`)).json()
      return trace?.turn.status
    }).toBe("done")
    const retries = trace!.events.filter((event) => event.kind === "model_retry")
    expect(retries).toHaveLength(1)
    expect(JSON.parse(retries[0].text ?? "{}")).toMatchObject({ reason: "schedule_claim_unverified", attempt: 1, cap: 1 })
    const calls = trace!.events.filter((event) => event.kind === "tool_call" && event.text?.startsWith("schedule_wake("))
    expect(calls).toHaveLength(1)
    const receipt = trace!.events.find((event) => event.kind === "tool_result" && event.tool_call_id === calls[0].tool_call_id)
    const arranged = JSON.parse(receipt?.text ?? "{}") as { ok: boolean; id: string }
    expect(arranged.ok).toBe(true)
    scheduleID = arranged.id
    const saved = await (await request.get(`/api/schedules/${scheduleID}`)).json()
    expect(saved.schedule).toMatchObject({ id: scheduleID, thread_id: threadID, status: "active", every_s: 90 })
    expect(saved.schedule.next_run_at).toBeTruthy()
    expect(trace!.llm_calls.filter((call) => call.agent_id === "schedule-answer-policy")).toHaveLength(2)
    for (const event of trace!.events.filter((event) => ["agent_message", "delta", "done"].includes(event.kind))) expect(event.text ?? "").not.toContain(unsupportedScheduleAnswer)
    await page.unroute(history)
    await reloadOpenConversation(page)
    await expect(transcript).toContainText(verifiedScheduleAnswer)
    await expect(transcript).not.toContainText(unsupportedScheduleAnswer)
    await expect(page.getByTestId("schedule-banner")).toBeVisible()
    await expect(page.getByTestId("status-badge")).toContainText("Waiting")
    expect(fixture.errors).toEqual([])
    expect(fixture.state()).toMatchObject({ managerRequests: 3, checks: 2 })
    await page.screenshot({ path: info.outputPath("verified-schedule-after-reload.png"), animations: "disabled" })
    await writeFile(info.outputPath("scheduling-admission-evidence.json"), JSON.stringify({ trace, saved, fixture: fixture.state(), errors: fixture.errors }, null, 2), { mode: 0o600 })
  } finally {
    await page.unroute(history)
    try { if (scheduleID) await request.delete(`/api/schedules/${scheduleID}`, { timeout: 5000 }) }
    finally { await fixture.close() }
  }
})
