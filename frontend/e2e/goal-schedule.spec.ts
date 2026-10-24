import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { composer, freshConversation, reloadOpenConversation } from "./session"
import { startGoalScheduleFixture } from "./goal-schedule-fixture"

type Event = { kind: string; text: string; tool_call_id?: string }
type Trace = { turn: { id: string; status: string }; events: Event[] }

function toolReceipt(trace: Trace, name: string) {
  const calls = trace.events.filter((event) => event.kind === "tool_call" && event.text.startsWith(`${name}(`))
  expect(calls).toHaveLength(1)
  expect(calls[0].tool_call_id).toBeTruthy()
  const results = trace.events.filter((event) => event.kind === "tool_result" && event.tool_call_id === calls[0].tool_call_id)
  expect(results).toHaveLength(1)
  return JSON.parse(results[0].text) as { ok: boolean; code?: string }
}

for (const automatic of [false, true]) {
  test(`${automatic ? "automatic" : "Run now"} check keeps the objective open until the final check`, async ({ page, request }, info) => {
    test.skip(process.env.ZWAI_E2E_GOAL_SCHEDULE !== "1", "requires the keyless lifecycle fixture")
    const fixture = await startGoalScheduleFixture(automatic)
    let scheduleID = ""
    try {
      const { settings } = await (await request.get("/api/settings")).json()
      expect((await request.put("/api/settings", { data: {
        models: { default: "fixture-provider", providers: [{ id: "fixture-provider", label: "Offline lifecycle regression", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
        swarm: { ...settings.swarm, auto_title: false, schedule_min_interval_seconds: 1 },
        memory: { ...settings.memory, auto_review: false },
      } })).ok()).toBe(true)
      await freshConversation(page)
      const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
      expect(threadID).toBeTruthy()
      expect((await request.patch(`/api/threads/${threadID}`, { data: { goal: "Verify the complete objective." } })).ok()).toBe(true)
      await composer(page).fill("Inspect the required state.")
      await composer(page).press("Enter")
      await expect.poll(async () => {
        const body = await (await request.get(`/api/threads/${threadID}/turns`)).json()
        return body.turns.some((turn: { status: string }) => turn.status === "done")
      }).toBe(true)
      await expect.poll(async () => {
        const body = await (await request.get("/api/schedules")).json()
        scheduleID = body.schedules.find((row: { thread_id: string }) => row.thread_id === threadID)?.id ?? ""
        return scheduleID
      }).not.toBe("")
      if (!automatic) expect((await request.post(`/api/schedules/${scheduleID}/run`)).ok()).toBe(true)
      let checked: Trace | undefined
      await expect.poll(async () => {
        const body = await (await request.get(`/api/schedules/${scheduleID}`)).json()
        const run = body.runs.find((row: { status: string }) => row.status === "findings")
        if (!run) return false
        checked = await (await request.get(`/api/trace/${run.turn_id}`)).json()
        return checked?.turn.status === "done"
      }).toBe(true)
      expect(checked!.events.some((event) => event.kind === "goal_complete")).toBe(false)
      expect(toolReceipt(checked!, "report_schedule").ok).toBe(true)
      expect(toolReceipt(checked!, "complete_goal")).toMatchObject({ ok: false, code: "goal_schedule_conflict" })
      const { thread: openGoal } = await (await request.get(`/api/threads/${threadID}`)).json()
      expect(openGoal).toMatchObject({ goal: "Verify the complete objective.", goal_complete: false, goal_blocked: false, goal_capped: false, goal_idle: false })
      await reloadOpenConversation(page)
      await expect(page.getByTestId("goal-banner")).toContainText("Pursuing")
      await expect(page.getByTestId("status-badge")).toContainText("Waiting")
      await expect(page.getByTestId("schedule-banner")).toBeVisible()
      await page.getByRole("tab", { name: "Trace", exact: true }).click()
      await expect(page.getByRole("tabpanel").getByText(checked!.turn.id, { exact: true })).toBeVisible()
      await page.getByTestId("trace-log-toggle").click()
      await expect(page.getByTestId("trace-log")).toContainText("complete_goal")
      // Settings persist between browser contexts; the second case may
      // already be in developer view. Close its menu through the overlay.
      await page.getByRole("button", { name: "App menu" }).click()
      const developerView = page.getByRole("menuitem", { name: "Switch to developer view", exact: true })
      if (await developerView.isVisible()) await developerView.click()
      else await page.getByRole("button", { name: "Close menu", exact: true }).click()
      const refusal = page.getByTestId("transcript").getByRole("button", { name: /^complete_goal/ }).last()
      await refusal.click()
      await expect(page.getByTestId("transcript")).toContainText("goal_schedule_conflict")
      await page.screenshot({ path: info.outputPath("open-goal-waiting.png"), animations: "disabled" })
      fixture.finish()
      const final = await request.post(`/api/schedules/${scheduleID}/run`)
      expect(final.ok()).toBe(true)
      const { turn } = await final.json()
      let finished: Trace | undefined
      await expect.poll(async () => {
        finished = await (await request.get(`/api/trace/${turn.id}`)).json()
        return finished?.turn.status
      }).toBe("done")
      expect(finished!.events.filter((event) => event.kind === "goal_complete")).toHaveLength(1)
      expect(toolReceipt(finished!, "report_schedule").ok).toBe(true)
      expect(toolReceipt(finished!, "complete_goal").ok).toBe(true)
      const { thread: doneGoal } = await (await request.get(`/api/threads/${threadID}`)).json()
      expect(doneGoal).toMatchObject({ goal: "Verify the complete objective.", goal_complete: true, goal_blocked: false, goal_capped: false })
      await reloadOpenConversation(page)
      await expect(page.getByTestId("goal-banner")).toHaveCount(0)
      await expect(page.getByTestId("transcript")).toContainText("Standing objective completed.")
      await expect(page.getByTestId("status-badge")).toContainText("Idle")
      await expect(page.getByTestId("schedule-banner")).toHaveCount(0)
      expect(fixture.errors).toEqual([])
      await page.screenshot({ path: info.outputPath("completed-goal-idle.png"), animations: "disabled" })
      await writeFile(info.outputPath("goal-schedule-evidence.json"), JSON.stringify({ checked, finished, errors: fixture.errors }, null, 2), { mode: 0o600 })
    } finally {
      try { if (scheduleID) await request.delete(`/api/schedules/${scheduleID}`, { timeout: 5000 }) }
      catch (error) { info.annotations.push({ type: "cleanup", description: String(error) }) }
      finally { await fixture.close() }
    }
  })
}
