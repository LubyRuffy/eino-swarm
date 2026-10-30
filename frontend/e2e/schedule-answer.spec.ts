import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { setTimeout as delay } from "node:timers/promises"
import { composer, freshConversation, reloadOpenConversation } from "./session"
import { startScheduleAnswerFixture, unsupportedScheduleAnswer, verifiedScheduleAnswer } from "./schedule-answer-fixture"

type Event = { kind: string; text?: string; tool_call_id?: string; agent_id?: string }
type Trace = { turn: { id: string; status: string; final?: string }; events: Event[]; llm_calls: { agent_id: string; duration_ms: number; err?: string }[] }

test("a live scheduling answer survives classifier failures and a response beyond thirty seconds without repeating the manager", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_SCHEDULE_ANSWER !== "1", "requires the offline answer admission fixture")
  const fixture = await startScheduleAnswerFixture({ secondPolicyDelayMS: 31_000, secondPolicyFailures: [503, 429] })
  let scheduleID = ""
  let threadID = ""
  let trace: Trace | undefined
  const history = /\/api\/threads\/[^/]+\/log(?:\?|$)/
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "answer-fixture", providers: [{ id: "answer-fixture", label: "Offline scheduling admission", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000, first_byte_timeout_seconds: 40, timeout_seconds: 10 }] },
      swarm: { ...settings.swarm, auto_title: false, schedule_min_interval_seconds: 1 },
      memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    await page.addInitScript(() => {
      const NativeEventSource = window.EventSource
      const observed = [] as unknown[]
      const backoffs = [] as unknown[]
      ;(window as Window & { __answerRetryEvents?: unknown[] }).__answerRetryEvents = observed
      ;(window as Window & { __answerBackoffEvents?: unknown[] }).__answerBackoffEvents = backoffs
      window.EventSource = class extends NativeEventSource {
        constructor(url: string | URL, options?: EventSourceInit) {
          super(url, options)
          this.addEventListener("model_retry", (event) => { observed.push(JSON.parse(event.data)) })
          this.addEventListener("model_backoff", (event) => { backoffs.push(JSON.parse(event.data)) })
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
    await expect(transcript).toContainText("The previous answer contained an unverified schedule claim. One correction was requested.")
    await expect(transcript).not.toContainText(unsupportedScheduleAnswer)
    await expect(page.getByTestId("schedule-banner")).toHaveCount(0)
    await expect.poll(() => page.evaluate(() => (window as Window & { __answerRetryEvents?: unknown[] }).__answerRetryEvents?.length ?? 0)).toBe(1)
    expect((await (await request.get("/api/schedules")).json()).schedules).toHaveLength(0)
    expect(fixture.state()).toMatchObject({ managerRequests: 2, checks: 1 })
    await page.screenshot({ path: info.outputPath("unverified-answer-withheld.png"), animations: "disabled" })
    fixture.allowArm()
    await expect(transcript).toContainText("Waiting 5s before checking the unpublished answer again.")
    await expect(transcript).not.toContainText("Waiting 5s before retrying the model request.")
    await expect.poll(() => page.evaluate(() => (window as Window & { __answerBackoffEvents?: unknown[] }).__answerBackoffEvents?.length ?? 0)).toBeGreaterThan(0)
    await expect(transcript).not.toContainText(verifiedScheduleAnswer)
    await expect(transcript).not.toContainText(unsupportedScheduleAnswer)
    expect(fixture.state()).toMatchObject({ managerRequests: 3, checks: 2 })
    await page.screenshot({ path: info.outputPath("classification-recovery-wait.png"), animations: "disabled" })
    await expect(transcript).toContainText(verifiedScheduleAnswer, { timeout: 65_000 })
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
    const policyCalls = trace!.llm_calls.filter((call) => call.agent_id === "schedule-answer-policy")
    expect(policyCalls).toHaveLength(4)
    expect(policyCalls[1].err).toContain("503")
    expect(policyCalls[2].err).toContain("429")
    expect(policyCalls[3].duration_ms).toBeGreaterThanOrEqual(30_000)
    expect(policyCalls[3].err ?? "").toBe("")
    const candidateWaits = trace!.events.filter((event) => event.kind === "model_backoff")
    expect(candidateWaits).toHaveLength(2)
    for (const event of candidateWaits) {
      expect(event.agent_id).toBe("schedule-answer-policy")
      expect(JSON.parse(event.text ?? "{}")).toMatchObject({ scope: "schedule_answer", code: "classification_unavailable" })
      expect(JSON.parse(event.text ?? "{}").cooldown_id).toBeUndefined()
    }
    for (const event of trace!.events.filter((event) => ["agent_message", "delta", "done"].includes(event.kind))) expect(event.text ?? "").not.toContain(unsupportedScheduleAnswer)
    await page.unroute(history)
    await reloadOpenConversation(page)
    await expect(transcript).toContainText(verifiedScheduleAnswer)
    await expect(transcript).toContainText("Waiting 5s before checking the unpublished answer again.")
    await expect(transcript).not.toContainText("Waiting 5s before retrying the model request.")
    await expect(transcript).not.toContainText(unsupportedScheduleAnswer)
    await expect(page.getByTestId("schedule-banner")).toBeVisible()
    await expect(page.getByTestId("status-badge")).toContainText("Waiting")
    expect(fixture.errors).toEqual([])
    expect(fixture.state()).toMatchObject({ managerRequests: 3, checks: 4 })
    await page.screenshot({ path: info.outputPath("verified-schedule-after-reload.png"), animations: "disabled" })
    await writeFile(info.outputPath("scheduling-admission-evidence.json"), JSON.stringify({ trace, saved, fixture: fixture.state(), errors: fixture.errors }, null, 2), { mode: 0o600 })
  } finally {
    await page.unroute(history)
    try { if (scheduleID) await request.delete(`/api/schedules/${scheduleID}`, { timeout: 5000 }) }
    finally { await fixture.close() }
  }
})

test("Stop cancels a classification recovery wait without publishing or rerunning the manager", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_SCHEDULE_ANSWER !== "1", "requires the offline answer admission fixture")
  const fixture = await startScheduleAnswerFixture({ unavailable: true })
  let threadID = ""
  const history = /\/api\/threads\/[^/]+\/log(?:\?|$)/
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "answer-stop-fixture", providers: [{ id: "answer-stop-fixture", label: "Offline classification cancellation", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000, first_byte_timeout_seconds: 40, timeout_seconds: 10 }] },
      swarm: { ...settings.swarm, auto_title: false },
      memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    await page.addInitScript(() => {
      const NativeEventSource = window.EventSource
      const observed = [] as unknown[]
      ;(window as Window & { __answerBackoffEvents?: unknown[] }).__answerBackoffEvents = observed
      window.EventSource = class extends NativeEventSource {
        constructor(url: string | URL, options?: EventSourceInit) {
          super(url, options)
          this.addEventListener("model_backoff", (event) => { observed.push(JSON.parse(event.data)) })
        }
      }
    })
    await freshConversation(page)
    threadID = (await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")) ?? ""
    expect(threadID).toBeTruthy()
    await page.route(history, (route) => route.fulfill({ json: { events: [], has_more: false } }))
    await composer(page).fill("Inspect the current task state.")
    await composer(page).press("Enter")
    const transcript = page.getByTestId("transcript")
    await expect(transcript).toContainText("Waiting 5s before checking the unpublished answer again.")
    await expect.poll(() => page.evaluate(() => (window as Window & { __answerBackoffEvents?: unknown[] }).__answerBackoffEvents?.length ?? 0)).toBeGreaterThan(0)
    await expect(transcript).not.toContainText(unsupportedScheduleAnswer)
    await expect(transcript).not.toContainText(verifiedScheduleAnswer)
    expect(fixture.state()).toMatchObject({ managerRequests: 1, checks: 1 })
    const { turns } = await (await request.get(`/api/threads/${threadID}/turns`)).json()
    const turnID = turns[0].id as string
    await page.getByRole("button", { name: "Stop", exact: true }).click()
    let trace: Trace | undefined
    await expect.poll(async () => {
      trace = await (await request.get(`/api/trace/${turnID}`)).json()
      return trace?.turn.status
    }).toBe("cancelled")
    expect(trace!.turn.id).toBe(turnID)
    expect(trace!.turn.final ?? "").toBe("")
    expect(trace!.events.filter((event) => event.kind === "model_retry")).toHaveLength(0)
    expect(fixture.state()).toMatchObject({ managerRequests: 1, checks: 1 })
    const stopped = fixture.state()
    // Keep the real offline HTTP endpoint alive beyond the pending 5s retry.
    // This negative observation catches a leaked retry timer after Stop.
    await delay(6_000)
    expect(fixture.state()).toEqual(stopped)
    for (const event of trace!.events.filter((event) => ["agent_message", "delta", "done"].includes(event.kind))) {
      expect(event.text ?? "").not.toContain(unsupportedScheduleAnswer)
      expect(event.text ?? "").not.toContain(verifiedScheduleAnswer)
    }
    await page.unroute(history)
    await reloadOpenConversation(page)
    await expect(transcript).not.toContainText(unsupportedScheduleAnswer)
    await expect(transcript).not.toContainText(verifiedScheduleAnswer)
    await expect(page.getByRole("button", { name: "Stop", exact: true })).toHaveCount(0)
    expect(fixture.errors).toEqual([])
    await page.screenshot({ path: info.outputPath("classification-recovery-stopped.png"), animations: "disabled" })
    await writeFile(info.outputPath("classification-stop-evidence.json"), JSON.stringify({ trace, stopped, afterObservation: fixture.state(), observationMS: 6_000, errors: fixture.errors }, null, 2), { mode: 0o600 })
  } finally {
    await page.unroute(history)
    if (threadID) await request.post(`/api/threads/${threadID}/interrupt`, { timeout: 5000 }).catch(() => undefined)
    await fixture.close()
  }
})

test("consecutive unsupported corrections stop goal continuation with separate retry and terminal rows", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_SCHEDULE_ANSWER !== "1", "requires the offline answer admission fixture")
  const fixture = await startScheduleAnswerFixture({ uncorrected: true })
  let threadID = ""
  const history = /\/api\/threads\/[^/]+\/log(?:\?|$)/
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "answer-refusal-fixture", providers: [{ id: "answer-refusal-fixture", label: "Offline correction limit", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false },
      memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    await page.addInitScript(() => {
      const NativeEventSource = window.EventSource
      const observed = [] as { kind: string; text?: string }[]
      ;(window as Window & { __answerLifecycle?: unknown[] }).__answerLifecycle = observed
      window.EventSource = class extends NativeEventSource {
        constructor(url: string | URL, options?: EventSourceInit) {
          super(url, options)
          for (const kind of ["model_retry", "error", "goal_capped"]) this.addEventListener(kind, (event) => observed.push(JSON.parse((event as MessageEvent).data)))
        }
      }
    })
    await freshConversation(page)
    threadID = (await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")) ?? ""
    expect(threadID).toBeTruthy()
    expect((await request.patch(`/api/threads/${threadID}`, { data: { goal: "Verify the complete objective." } })).ok()).toBe(true)
    await page.route(history, (route) => route.fulfill({ json: { events: [], has_more: false } }))
    await composer(page).fill("Inspect the assigned state.")
    await composer(page).press("Enter")
    const transcript = page.getByTestId("transcript")
    const receiptText = "The previous answer contained an unverified schedule claim. One correction was requested."
    const failureText = "This turn's schedule claim could not be verified. No final answer was published (schedule_claim_unverified / no_matching_schedule)."
    const limitText = "Stopped auto-continuing: consecutive schedule claims could not be verified. Press Start on the goal to keep going."
    await expect(transcript).toContainText(receiptText)
    await expect(transcript).not.toContainText(unsupportedScheduleAnswer)
    await page.screenshot({ path: info.outputPath("correction-receipt-live.png"), animations: "disabled" })
    fixture.allowArm()
    await expect(transcript).toContainText(limitText)
    await expect(transcript.getByText(failureText, { exact: true })).toHaveCount(3)
    await expect(page.getByTestId("goal-banner")).toContainText("Auto-run paused")
    await expect(page.getByTestId("goal-banner")).not.toContainText("Blocked")
    await expect(transcript).not.toContainText("Checking the schedule before reporting it as arranged.")
    await expect(transcript).not.toContainText("Standing objective blocked: progress needs you or an external change.")
    const notice = transcript.getByTestId("memory-notice").filter({ hasText: receiptText }).first()
    const error = transcript.getByText(failureText, { exact: true }).first()
    const noticeBounds = await notice.boundingBox()
    const errorBounds = await error.boundingBox()
    expect(noticeBounds).toBeTruthy()
    expect(errorBounds).toBeTruthy()
    expect(errorBounds!.y).toBeGreaterThanOrEqual(noticeBounds!.y + noticeBounds!.height)
    const { thread } = await (await request.get(`/api/threads/${threadID}`)).json()
    expect(thread).toMatchObject({ goal_blocked: false, goal_capped: true, goal_auto_turns: 2 })
    expect(thread.goal_block_reason ?? "").toBe("")
    const { turns } = await (await request.get(`/api/threads/${threadID}/turns`)).json()
    expect(turns).toHaveLength(3)
    const traces: Trace[] = await Promise.all(turns.map(async (turn: { id: string }) => (await request.get(`/api/trace/${turn.id}`)).json() as Promise<Trace>))
    for (const trace of traces) {
      expect(trace.turn.status).toBe("error")
      expect(trace.turn.final ?? "").toBe("")
      expect(trace.events.filter((event) => event.kind === "model_retry")).toHaveLength(1)
      expect(trace.events.filter((event) => event.kind === "goal_blocked")).toHaveLength(0)
    }
    const lifecycle = await page.evaluate(() => (window as Window & { __answerLifecycle?: { kind: string; text?: string }[] }).__answerLifecycle ?? [])
    expect(lifecycle.filter((event) => event.kind === "error")).toHaveLength(3)
    expect(lifecycle.filter((event) => event.kind === "model_retry")).toHaveLength(3)
    expect(JSON.parse(lifecycle.find((event) => event.kind === "goal_capped")!.text ?? "{}")).toMatchObject({ reason: "schedule_claim_unverified", code: "no_matching_schedule", attempt: 3, cap: 3 })
    expect(fixture.state()).toEqual({ managerRequests: 6, checks: 6 })
    await page.screenshot({ path: info.outputPath("goal-schedule-refusal-limit.png"), animations: "disabled" })
    await page.unroute(history)
    await reloadOpenConversation(page)
    await expect(transcript).toContainText(limitText)
    await expect(transcript.getByText(failureText, { exact: true })).toHaveCount(3)
    await expect(transcript).not.toContainText(unsupportedScheduleAnswer)
    await expect(page.getByTestId("goal-banner")).toContainText("Auto-run paused")
    expect(fixture.state()).toEqual({ managerRequests: 6, checks: 6 })
    expect(fixture.errors).toEqual([])
    await page.screenshot({ path: info.outputPath("goal-schedule-refusal-replay.png"), animations: "disabled" })
    await writeFile(info.outputPath("goal-schedule-refusal-evidence.json"), JSON.stringify({ thread, traces, lifecycle, fixture: fixture.state(), errors: fixture.errors }, null, 2), { mode: 0o600 })
  } finally {
    await page.unroute(history)
    if (threadID) await request.post(`/api/threads/${threadID}/interrupt`, { timeout: 5000 }).catch(() => undefined)
    await fixture.close()
  }
})
