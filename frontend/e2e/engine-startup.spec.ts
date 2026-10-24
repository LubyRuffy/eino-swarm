import { expect, test } from "@playwright/test"
import { composer, freshConversation, waitForIdle } from "./session"

test("restoration progress keeps history readable and a saved draft intact until an explicit send after readiness", async ({ page }, info) => {
  let startup = "ready", completed = 0
  await page.route("**/api/meta", async (route) => {
    const response = await route.fetch()
    const body = await response.json()
    await route.fulfill({ response, json: { ...body, startup, restore: { completed, total: 3 } } })
  })
  await freshConversation(page)
  await composer(page).fill("Inspect the assigned operation.")
  const first = page.waitForResponse((response) => response.request().method() === "POST" && /\/steer$/.test(new URL(response.url()).pathname))
  await composer(page).press("Enter")
  expect((await first).status()).toBe(202)
  await waitForIdle(page)
  await composer(page).fill("Retained draft during restoration")
  await expect(page.getByTestId("composer-draft-status")).toContainText("Draft saved")
  let sends = 0
  page.on("request", (request) => { if (/\/(steer|turns)$/.test(new URL(request.url()).pathname) && request.method() === "POST") sends++ })
  startup = "restoring"; completed = 1
  await expect(page.getByTestId("engine-startup")).toHaveText("Restoring unfinished tasks 1/3")
  await expect(composer(page)).toHaveValue("Retained draft during restoration")
  await expect(page.getByRole("button", { name: "Send", exact: true })).toBeDisabled()
  await expect(page.getByTestId("user-message")).toContainText("Inspect the assigned operation.")
  completed = 2
  await expect(page.getByTestId("engine-startup")).toHaveText("Restoring unfinished tasks 2/3")
  await page.screenshot({ path: info.outputPath("restoration-progress-with-saved-draft.png") })
  startup = "ready"; completed = 3
  await expect(page.getByTestId("engine-startup")).toHaveCount(0)
  await expect(composer(page)).toBeEnabled()
  await expect(composer(page)).toHaveValue("Retained draft during restoration")
  expect(sends).toBe(0)
  await composer(page).press("Enter")
  await expect.poll(() => sends).toBe(1)
  await waitForIdle(page)
})

test("a restoring refusal retains an uncertain draft and never automatically resubmits it", async ({ page }, info) => {
  await freshConversation(page)
  let refused = 0
  await page.route("**/api/threads/*/steer", async (route) => {
    refused++
    await route.fulfill({ status: 409, contentType: "application/json", body: JSON.stringify({ code: "restoring", error: "Restoring unfinished tasks" }) })
  })
  await composer(page).fill("A message requiring an explicit acknowledgement")
  await composer(page).press("Enter")
  await expect(page.getByTestId("composer-draft-status")).toContainText("send status is unknown")
  await expect(composer(page)).toHaveValue("A message requiring an explicit acknowledgement")
  await expect(page.getByTestId("engine-startup")).toHaveCount(0)
  expect(refused).toBe(1)
  await page.screenshot({ path: info.outputPath("restoring-refusal-keeps-uncertain-draft.png") })
})

test("a named live engine_restore receipt reaches Full log without a history read or chat row", async ({ page, request }, info) => {
  const created = await request.post("/api/threads", { data: { title: "Restored task evidence" } })
  const { thread } = await created.json()
  const started = await request.post(`/api/threads/${thread.id}/steer`, { data: { text: "Inspect the assigned operation." } })
  const { turn } = await started.json()
  await expect.poll(async () => (await (await request.get(`/api/threads/${thread.id}`)).json()).status.running).toBe(false)
  let release!: () => void
  const admitted = new Promise<void>((done) => { release = done })
  let delivered = false
  await page.route(`**/api/threads/${thread.id}/events*`, async (route) => {
    if (delivered) { await route.fulfill({ status: 204 }); return }
    await admitted
    delivered = true
    const event = { kind: "engine_restore", seq: 100000, thread_id: thread.id, turn_id: turn.id,
      agent_id: "manager", created_at: new Date().toISOString(),
      text: JSON.stringify({ phase: "restored", duration_ms: 8, scanned_events: 50, checkpoint_reads: 1 }) }
    await route.fulfill({ contentType: "text/event-stream", body: `event: engine_restore\ndata: ${JSON.stringify(event)}\n\n` })
  })
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.locator(`[data-testid="thread-row"][data-id="${thread.id}"]`).getByTestId("row-label").click()
  await expect(composer(page)).toBeEnabled()
  await page.getByRole("tab", { name: "Trace", exact: true }).click()
  await page.getByTestId("trace-log-toggle").click()
  let forbiddenReads = 0
  for (const path of [/\/api\/trace\//, /\/api\/threads\/[^/]+\/log(?:\?|$)/]) {
    await page.route(path, async (route) => { forbiddenReads++; await route.abort() })
  }
  release()
  const diagnostic = page.getByTestId("trace-log").locator("li").filter({ hasText: "engine_restore" })
  await expect(diagnostic).toHaveCount(1)
  await diagnostic.getByTestId("review-trace-details").click()
  const receipt = JSON.parse(await diagnostic.locator("pre").innerText())
  expect(JSON.parse(receipt.text)).toEqual({ phase: "restored", duration_ms: 8, scanned_events: 50, checkpoint_reads: 1 })
  await expect(page.getByTestId("transcript")).not.toContainText("checkpoint_reads")
  expect(forbiddenReads).toBe(0)
  await page.screenshot({ path: info.outputPath("named-live-engine-restore-full-log.png") })
})

test("restoration blocks schedule changes and Run now while preserving the editor draft", async ({ page, request }, info) => {
  let startup = "ready"
  await page.route("**/api/meta", async (route) => {
    const response = await route.fetch()
    await route.fulfill({ response, json: { ...await response.json(), startup, restore: { completed: 1, total: 3 } } })
  })
  await freshConversation(page)
  const created = await request.post("/api/schedules", { data: { kind: "standalone", title: "Independent verification", prompt: "Inspect the assigned operation.", every_s: 3600 } })
  expect(created.ok()).toBe(true)
  const { schedule } = await created.json()
  try {
    await page.getByTestId("schedule-inbox").click()
    await page.getByTestId("schedule-row").filter({ hasText: "Independent verification" }).getByTestId("schedule-row-toggle").click()
    const editor = page.getByTestId("schedule-edit-drawer")
    await editor.getByLabel("Task", { exact: true }).fill("Retained schedule editor draft")
    await expect(editor.getByRole("button", { name: "Save", exact: true })).toBeEnabled()
    startup = "restoring"
    await expect(page.getByTestId("engine-startup")).toBeVisible()
    for (const name of ["Run now", "Cancel wait", "Pause", "Save"]) {
      await expect(editor.getByRole("button", { name, exact: true })).toBeDisabled()
    }
    await expect(editor.getByLabel("Task", { exact: true })).toHaveValue("Retained schedule editor draft")
    expect((await (await request.get(`/api/schedules/${schedule.id}`)).json()).schedule.prompt).toBe("Inspect the assigned operation.")
    await page.screenshot({ path: info.outputPath("restoration-schedule-mutations-disabled.png") })
    startup = "ready"
    await expect(page.getByTestId("engine-startup")).toHaveCount(0)
    await expect(editor.getByRole("button", { name: "Run now", exact: true })).toBeEnabled()
    await expect(editor.getByRole("button", { name: "Save", exact: true })).toBeEnabled()
    await expect(editor.getByLabel("Task", { exact: true })).toHaveValue("Retained schedule editor draft")
  } finally { await request.delete(`/api/schedules/${schedule.id}`) }
})


test("a restoring schedule-create refusal keeps the instruction and requires an explicit retry", async ({ page }, info) => {
  await freshConversation(page)
  await page.getByTestId("schedule-inbox").click()
  await page.getByRole("button", { name: "New task", exact: true }).click()
  const editor = page.getByTestId("schedule-create-drawer")
  await editor.getByLabel("Task", { exact: true }).fill("Retained schedule creation instruction")
  await editor.getByLabel("Delay (seconds)", { exact: true }).fill("60")
  let requests = 0
  await page.route("**/api/schedules", async (route) => {
    if (route.request().method() !== "POST") { await route.continue(); return }
    requests++
    await route.fulfill({ status: 409, contentType: "application/json", body: JSON.stringify({ code: "restoring", error: "Engine is restoring unfinished tasks." }) })
  })
  await editor.getByRole("button", { name: "Add wait", exact: true }).click()
  await expect(page.getByText("Engine is restoring unfinished tasks.", { exact: true }).first()).toBeVisible()
  await expect(editor).toBeVisible()
  await expect(editor.getByLabel("Task", { exact: true })).toHaveValue("Retained schedule creation instruction")
  await expect(editor.getByLabel("Delay (seconds)", { exact: true })).toHaveValue("60")
  await expect(page.getByTestId("engine-startup")).toHaveCount(0)
  expect(requests).toBe(1)
  await expect(editor.getByRole("button", { name: "Add wait", exact: true })).toBeEnabled()
  await page.screenshot({ path: info.outputPath("restoring-schedule-refusal-preserves-form.png") })
})


test("Stop cancels selected recovery when stored work is running but the live engine status is idle", async ({ page }, info) => {
  let startup = "ready"
  await page.route("**/api/meta", async (route) => {
    const response = await route.fetch()
    await route.fulfill({ response, json: { ...await response.json(), startup, restore: { completed: 0, total: 1 } } })
  })
  await page.route("**/api/threads/*/turns", async (route) => {
    const threadId = new URL(route.request().url()).pathname.split("/")[3]
    await route.fulfill({ json: { turns: [{ id: "stored-before-restart", thread_id: threadId, status: "running", seq: 1,
      user_text: "Inspect the pending operation.", started_at: new Date().toISOString() }] } })
  })
  await freshConversation(page)
  await composer(page).fill("Draft kept while cancelling restoration")
  let stopped = 0
  await page.route("**/api/threads/*/interrupt", async (route) => { stopped++; await route.fulfill({ status: 202, json: { stopped: true } }) })
  startup = "restoring"
  await expect(page.getByTestId("engine-startup")).toBeVisible()
  await expect(page.getByTestId("status-badge")).toHaveText("Idle")
  const stop = page.getByRole("button", { name: "Stop", exact: true })
  await expect(stop).toBeEnabled()
  await stop.click()
  expect(stopped).toBe(1)
  await expect(composer(page)).toHaveValue("Draft kept while cancelling restoration")
  await expect(page.getByTestId("status-badge")).toHaveText("Idle")
  await page.screenshot({ path: info.outputPath("stop-idle-live-status-stored-running-recovery.png") })
})
