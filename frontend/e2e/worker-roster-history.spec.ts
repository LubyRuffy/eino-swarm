import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { composer, freshConversation, reloadOpenConversation, waitForIdle } from "./session"
import { startRosterFixture } from "./worker-roster-fixture"

type Event = { kind: string; agent_id: string; text: string }
type Trace = { turn: { id: string }; events: Event[] }

test("a follow-up queries a finished worker without inventing a new delegation", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_WORKER_ROSTER !== "1", "requires the keyless loopback roster fixture")
  const fixture = await startRosterFixture()
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "fixture-provider", providers: [{ id: "fixture-provider", label: "Offline roster regression", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false, schedule_min_interval_seconds: 1 },
      memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    await freshConversation(page)
    const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
    const scheduled = await request.post("/api/schedules", { data: { kind: "thread", thread_id: threadID, prompt: "Perform the next assigned check.", delay_s: 86400 } })
    expect(scheduled.ok()).toBe(true)
    const { schedule } = await scheduled.json()
    const traces: Trace[] = []
    for (const text of ["Verify the assigned inputs.", "Inspect the existing worker state."]) {
      const accepted = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === `/api/threads/${threadID}/steer`)
      await composer(page).fill(text)
      await composer(page).press("Enter")
      const response = await accepted
      expect(response.status()).toBe(202)
      const { turn } = await response.json()
      await expect.poll(async () => (await (await request.get(`/api/trace/${turn.id}`)).json()).turn.status).toBe("done")
      traces.push(await (await request.get(`/api/trace/${turn.id}`)).json())
    }
    const runNow = await request.post(`/api/schedules/${schedule.id}/run`)
    expect(runNow.ok()).toBe(true)
    const { turn: scheduledTurn } = await runNow.json()
    await expect.poll(async () => (await (await request.get(`/api/trace/${scheduledTurn.id}`)).json()).turn.status).toBe("done")
    traces.push(await (await request.get(`/api/trace/${scheduledTurn.id}`)).json())
    const automatic = await request.post("/api/schedules", { data: { kind: "thread", thread_id: threadID, prompt: "Inspect existing worker state.", delay_s: 1 } })
    expect(automatic.ok()).toBe(true)
    await expect.poll(async () => (await (await request.get(`/api/threads/${threadID}/turns`)).json()).turns.length).toBe(4)
    const turns = (await (await request.get(`/api/threads/${threadID}/turns`)).json()).turns
    const fired = turns.find((turn: { id: string }) => !traces.some((trace) => trace.turn.id === turn.id))
    await expect.poll(async () => (await (await request.get(`/api/trace/${fired.id}`)).json()).turn.status).toBe("done")
    traces.push(await (await request.get(`/api/trace/${fired.id}`)).json())
    expect((traces[0].events as Event[]).filter((event) => event.kind === "spawned")).toHaveLength(1)
    expect((traces[0].events as Event[]).filter((event) => event.kind === "finished")).toHaveLength(1)
    for (const trace of traces.slice(1)) {
      expect((trace.events as Event[]).filter((event) => event.kind === "spawned")).toHaveLength(0)
      expect((trace.events as Event[]).filter((event) => event.kind === "tool_call" && event.text.startsWith("spawn_agent("))).toHaveLength(0)
      expect((trace.events as Event[]).some((event) => event.kind === "tool_call" && event.text.startsWith("wait_agents("))).toBe(true)
      const waitReceipt = (trace.events as Event[]).find((event) => event.kind === "tool_result" && event.text.includes('"agents"'))
      expect(JSON.parse(waitReceipt?.text ?? "{}").agents).toMatchObject([{ status: "done" }])
    }
    for (const trace of traces.slice(2)) {
      expect((trace.events as Event[]).filter((event) => event.kind === "schedule_report")).toHaveLength(1)
    }
    expect(fixture.errors).toEqual([])
    expect(fixture.snapshots.length).toBeGreaterThan(0)
    expect(fixture.authorization.every((value) => !value)).toBe(true)
    await expect(page.getByTestId("transcript")).toContainText("Existing worker verified; no new worker was created.")
    expect((await request.delete(`/api/schedules/${schedule.id}`)).ok()).toBe(true)
    await waitForIdle(page)
    await reloadOpenConversation(page)
    await expect(page.getByTestId("transcript")).toContainText("Existing worker verified; no new worker was created.")
    await page.getByRole("tab", { name: "Trace", exact: true }).click()
    await expect(page.getByRole("tabpanel").getByText(traces[traces.length - 1].turn.id, { exact: true })).toBeVisible()
    await page.screenshot({ path: info.outputPath("historical-worker-trace.png"), animations: "disabled" })
    await writeFile(info.outputPath("historical-worker-evidence.json"), JSON.stringify({ traces, snapshots: fixture.snapshots, errors: fixture.errors }, null, 2), { mode: 0o600 })
  } finally {
    await fixture.close()
  }
})
