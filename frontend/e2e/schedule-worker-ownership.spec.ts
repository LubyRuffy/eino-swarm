import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { composer, freshConversation, reloadOpenConversation } from "./session"
import { startScheduleWorkerOwnershipFixture } from "./schedule-worker-ownership-fixture"

type Event = { seq: number; kind: string; agent_id?: string; text?: string; err?: string; tool_call_id?: string }
type Trace = { turn: { id: string; status: string }; events: Event[] }

test("a failed scheduled check retains its owned worker and the next check collects the same result", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_SCHEDULE_WORKER_OWNERSHIP !== "1", "requires the keyless worker ownership fixture")
  const fixture = await startScheduleWorkerOwnershipFixture()
  let scheduleID = ""
  let initial: Trace | undefined
  let failed: Trace | undefined
  let recovered: Trace | undefined
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "fixture-provider", providers: [{ id: "fixture-provider", label: "Offline worker ownership regression", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false, schedule_min_interval_seconds: 1 },
      memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    await freshConversation(page)
    const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
    expect(threadID).toBeTruthy()
    const created = await request.post("/api/schedules", { data: { kind: "thread", thread_id: threadID, prompt: "Inspect the assigned task state.", every_s: 3600 } })
    expect(created.ok()).toBe(true)
    scheduleID = (await created.json()).schedule.id
    const accepted = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === `/api/threads/${threadID}/steer`)
    await composer(page).fill("Inspect the assigned inputs.")
    await composer(page).press("Enter")
    const first = await (await accepted).json()
    await expect.poll(async () => {
      initial = await (await request.get(`/api/trace/${first.turn.id}`)).json()
      return initial?.turn.status
    }).toBe("done")
    await expect.poll(() => fixture.state().workerRequests).toBe(1)
    const workerID = fixture.state().workerID
    expect(workerID).toBeTruthy()
    expect(initial!.events.filter((event) => event.kind === "spawned")).toHaveLength(1)
    await expect(page.getByTestId("status-badge")).toContainText("Waiting")
    fixture.failNextCheck()
    const failedRun = await request.post(`/api/schedules/${scheduleID}/run`)
    expect(failedRun.ok()).toBe(true)
    const { turn: failedTurn } = await failedRun.json()
    await expect.poll(async () => {
      failed = await (await request.get(`/api/trace/${failedTurn.id}`)).json()
      return failed?.turn.status
    }).toBe("error")
    expect(fixture.state().failedReplies).toBe(2)
    expect(failed!.events.filter((event) => event.kind === "model_retry")).toHaveLength(1)
    expect(failed!.events.find((event) => event.kind === "error")?.err).toContain("scheduled check ended without returning text, executing a tool or reporting its result")
    const failedSchedule = await (await request.get(`/api/schedules/${scheduleID}`)).json()
    expect(failedSchedule.runs[0]).toMatchObject({ status: "error", unread: true })
    // Save the adapter cancellation before assertions, so a regression proves
    // whether the worker really died rather than only showing the wrong icon.
    await writeFile(info.outputPath("failed-check-evidence.json"), JSON.stringify({ initial, failed, fixture: fixture.state() }, null, 2), { mode: 0o600 })
    expect(fixture.state().workerCancelled).toBe(false)
    expect(failed!.events.filter((event) => event.kind === "cleanup" || event.kind === "spawned")).toHaveLength(0)
    const retention = failed!.events.filter((event) => event.kind === "workers_retained")
    expect(retention).toHaveLength(1)
    expect(JSON.parse(retention[0].text ?? "{}")).toMatchObject({ reason: "scheduled_check_failed", owner_kind: "thread_wake", owner_id: scheduleID, worker_ids: [workerID] })
    expect(retention[0].seq).toBeLessThan(failed!.events.find((event) => event.kind === "error")!.seq)
    await expect(page.getByTestId("transcript")).toContainText("scheduled check ended without returning text, executing a tool or reporting its result")
    await expect(page.getByTestId("status-badge")).toContainText("Waiting")
    await page.getByRole("tab", { name: /^Agents(?: \d+)?$/ }).click()
    const worker = page.getByTestId("agent-roster").locator(`[data-agent-id="${workerID}"]`)
    await expect(worker).toHaveCount(1)
    await expect(worker.getByLabel("running", { exact: true })).toBeVisible()
    await expect(worker).not.toContainText("View failure reason")
    await expect(worker).not.toContainText("Stopped")
    await worker.click()
    await expect(page.getByTestId("agent-failure")).toHaveCount(0)
    const thought = page.getByTestId("agent-scroller").getByTestId("thought-toggle")
    if (await thought.getAttribute("aria-expanded") !== "true") await thought.click()
    await expect(page.getByTestId("agent-scroller")).toContainText("Inspecting the assigned inputs")
    await page.screenshot({ path: info.outputPath("failed-check-live-worker.png"), animations: "disabled" })
    await reloadOpenConversation(page)
    await page.getByRole("tab", { name: /^Agents(?: \d+)?$/ }).click()
    await expect(worker).toHaveCount(1)
    await expect(worker.getByLabel("running", { exact: true })).toBeVisible()
    await expect(worker).not.toContainText("View failure reason")
    await expect(worker).not.toContainText("Stopped")
    fixture.releaseWorker()
    await expect.poll(async () => {
      const body = await (await request.get(`/api/threads/${threadID}/log`)).json()
      return body.events.some((event: Event) => event.kind === "finished" && event.agent_id === workerID && !event.err)
    }).toBe(true)
    fixture.collectNextCheck()
    const recovery = await request.post(`/api/schedules/${scheduleID}/run`)
    expect(recovery.ok()).toBe(true)
    const { turn: recoveredTurn } = await recovery.json()
    await expect.poll(async () => {
      recovered = await (await request.get(`/api/trace/${recoveredTurn.id}`)).json()
      return recovered?.turn.status
    }).toBe("done")
    expect(recovered!.events.filter((event) => event.kind === "spawned" || event.kind === "cleanup")).toHaveLength(0)
    const wait = recovered!.events.find((event) => event.kind === "tool_call" && event.text?.startsWith("wait_agents("))
    expect(wait?.text).toContain(workerID)
    const receipt = recovered!.events.find((event) => event.kind === "tool_result" && event.tool_call_id === wait?.tool_call_id)
    expect(JSON.parse(receipt?.text ?? "{}").agents).toMatchObject([{ agent_id: workerID, status: "done" }])
    expect(fixture.state()).toMatchObject({ workerRequests: 1, workerCancelled: false, released: true, failedReplies: 2 })
    expect(fixture.errors).toEqual([])
    await expect(page.getByTestId("transcript")).toContainText("The existing worker completed and its result was collected.")
    await page.getByRole("tab", { name: "Trace", exact: true }).click()
    await expect(page.getByRole("tabpanel").getByText(recoveredTurn.id, { exact: true })).toBeVisible()
    await page.getByTestId("trace-log-toggle").click()
    await expect(page.getByTestId("trace-log")).toContainText("wait_agents")
    await page.screenshot({ path: info.outputPath("recovered-check-same-worker-trace.png"), animations: "disabled" })
  } finally {
    await writeFile(info.outputPath("worker-ownership-evidence.json"), JSON.stringify({ initial, failed, recovered, fixture: fixture.state(), rosters: fixture.rosters, errors: fixture.errors }, null, 2), { mode: 0o600 })
    try { if (scheduleID) await request.delete(`/api/schedules/${scheduleID}`, { timeout: 5000 }) }
    catch (error) { info.annotations.push({ type: "cleanup", description: String(error) }) }
    finally { await fixture.close() }
  }
})
