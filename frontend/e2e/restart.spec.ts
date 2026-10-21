import { expect, test } from "@playwright/test"
import { freshConversation, reloadOpenConversation, send, waitForIdle } from "./session"

// Process replacement is exercised by the Go two-process acceptance. This
// replay walks the actual conversation UI after that persisted handoff.
test("shows saved restart progress without exposing recovery internals", async ({ page }, testInfo) => {
  const title = `Restart recovery acceptance ${testInfo.workerIndex}-${testInfo.retry}`
  const created = await page.request.post("/api/threads", { data: { title } })
  expect(created.ok()).toBe(true)
  await page.route(/\/api\/threads\/[^/]+\/log(?:\?|$)/, async (route) => {
    const response = await route.fetch()
    const body = await response.json()
    const base = {
      thread_id: new URL(route.request().url()).pathname.split("/")[3],
      turn_id: "restart-turn", agent_id: "manager", created_at: new Date().toISOString(),
    }
    body.events = [
      { ...base, seq: 1, kind: "user_message", text: "Continue the assigned work." },
      { ...base, seq: 2, kind: "restart_preparing", text: "Preparing task handoff before restarting." },
      { ...base, seq: 3, kind: "engine_restart_checkpoint", text: '{"manager_history":["internal recovery input"]}' },
      { ...base, seq: 4, kind: "restart_ready", text: "Task handoff saved; waiting for the replacement engine." },
      { ...base, seq: 5, kind: "restart_resumed", text: "Saved task progress restored; continuing." },
    ]
    await route.fulfill({ response, json: body })
  })
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: title, exact: true }).click()
  const transcript = page.getByTestId("transcript")
  await expect(transcript.getByText("Preparing task handoff before restarting.", { exact: true })).toBeVisible()
  await expect(transcript.getByText("Task handoff saved; waiting for the replacement engine.", { exact: true })).toBeVisible()
  await expect(transcript.getByText("Saved task progress restored; continuing.", { exact: true })).toBeVisible()
  await expect(transcript).not.toContainText("internal recovery input")
  await expect(transcript).not.toContainText("engine_restart_checkpoint")
  await page.reload()
  await page.getByRole("button", { name: title, exact: true }).click()
  await expect(page.getByTestId("transcript").getByText("Saved task progress restored; continuing.", { exact: true })).toBeVisible()
  await page.screenshot({ path: "test-results/restart-recovered.png" })
})

test("cleanup and interruption replay shows stopped workers separately from completed work", async ({ page }, testInfo) => {
  const title = `Stopped worker replay ${testInfo.workerIndex}-${testInfo.retry}`
  const created = await page.request.post("/api/threads", { data: { title } })
  expect(created.ok()).toBe(true)
  await page.route(/\/api\/threads\/[^/]+\/log(?:\?|$)/, async (route) => {
    const response = await route.fetch()
    const body = await response.json()
    const base = { thread_id: new URL(route.request().url()).pathname.split("/")[3],
      turn_id: "stopped-turn", agent_id: "manager", created_at: new Date().toISOString() }
    body.events = [
      { kind: "user_message", text: "Continue the assigned work." },
      { kind: "spawned", agent_id: "worker-1", role: "worker" },
      { kind: "spawned", agent_id: "worker-2", role: "worker" },
      { kind: "spawned", agent_id: "worker-3", role: "worker" },
      { kind: "finished", agent_id: "worker-3", text: "Verified result." },
      { kind: "tool_call", agent_id: "worker-1", text: "exec({})", tool_call_id: "call" },
      { kind: "user_stop_requested", text: "Stop requested by the user." },
      { kind: "cleanup", text: "Stopped unfinished workers." },
      { kind: "error", err: "interrupted" },
      { kind: "finished", agent_id: "worker-1", err: "swarm: agent worker-1 cancelled: context canceled" },
      { kind: "finished", agent_id: "worker-2", text: "Late result." },
    ].map((event, index) => ({ ...base, seq: index + 1, ...event }))
    await route.fulfill({ response, json: body })
  })
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: title, exact: true }).click()
  for (let replay = 0; replay < 2; replay++) {
    await page.getByRole("tab", { name: "Agents" }).click()
    const roster = page.getByTestId("agent-roster")
    await expect(roster.getByText("Stopped (2)", { exact: true })).toBeVisible()
    await expect(roster.getByText("Done (1)", { exact: true })).toBeVisible()
    await expect(roster.getByText(/Active \(/)).toHaveCount(0)
    await expect(roster.locator('[aria-label="stopped"]')).toHaveCount(2)
    await expect(roster.locator('[data-agent-id="worker-3"] [aria-label="done"]')).toBeVisible()
    await expect(page.locator(".animate-spin, .animate-breathe")).toHaveCount(0)
    if (replay === 0) await reloadOpenConversation(page)
  }
  await page.screenshot({ path: "test-results/restart-stopped-replay.png" })
})

test("Stop keeps unfinished workers stopped after a reload", async ({ page }) => {
  await freshConversation(page)
  const accepted = page.waitForResponse((response) =>
    response.request().method() === "POST" && /\/api\/threads\/[^/]+\/steer$/.test(new URL(response.url()).pathname),
  )
  await send(page, "Look at this from two angles and merge the findings")
  const started = await accepted
  expect(started.ok()).toBe(true)
  const { turn } = await started.json()
  await expect(page.getByText(/Waiting for/)).toBeVisible({ timeout: 30_000 })
  await page.getByRole("button", { name: "Stop", exact: true }).click()
  await waitForIdle(page)
  // Idle can precede the cancelled model calls' finished receipts. Wait for
  // that real ordering before checking both the live roster and replay.
  type Event = { seq: number; kind: string }
  const readTrace = async () => (await (await page.request.get(`/api/trace/${turn.id}`)).json()).events as Event[]
  await expect.poll(async () => (await readTrace()).filter((event) => event.kind === "finished").length).toBe(2)
  const events = await readTrace()
  const cleanup = events.find((event) => event.kind === "cleanup")
  expect(cleanup).toBeDefined()
  expect(events.filter((event) => event.kind === "finished").every((event) => event.seq > cleanup!.seq)).toBe(true)
  for (let replay = 0; replay < 2; replay++) {
    await page.getByRole("tab", { name: "Agents" }).click()
    const roster = page.getByTestId("agent-roster")
    await expect(roster.getByText("Stopped (2)", { exact: true })).toBeVisible()
    await expect(roster.getByText(/Done \(|Active \(/)).toHaveCount(0)
    await expect(roster.locator('[aria-label="stopped"]')).toHaveCount(2)
    await expect(roster.locator(".bg-done, .bg-failed, .animate-breathe")).toHaveCount(0)
    await expect(page.locator(".animate-spin")).toHaveCount(0)
    if (replay === 0) await reloadOpenConversation(page)
  }
  await page.screenshot({ path: "test-results/restart-user-stopped.png" })
})
