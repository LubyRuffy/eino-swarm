import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { composer, freshConversation, reloadOpenConversation } from "./session"
import { startWorkerCompletionFixture, prematureWorkerAnswer, collectedWorkerAnswer, workerCompletionResult } from "./worker-completion-fixture"

test("an ordinary manager yield keeps its worker alive until the actual result is collected", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_WORKER_COMPLETION !== "1", "requires the offline worker completion fixture")
  const fixture = await startWorkerCompletionFixture()
  let threadID = ""
  const history = /\/api\/threads\/[^/]+\/log(?:\?|$)/
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "collection-fixture", providers: [{ id: "collection-fixture", label: "Offline worker completion", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false }, memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    await page.addInitScript(() => {
      const NativeEventSource = window.EventSource
      const observed = [] as unknown[]
      ;(window as Window & { __collectionEvents?: unknown[] }).__collectionEvents = observed
      window.EventSource = class extends NativeEventSource {
        constructor(url: string | URL, options?: EventSourceInit) {
          super(url, options)
          this.addEventListener("model_retry", (event) => observed.push(JSON.parse(event.data)))
        }
      }
    })
    await freshConversation(page)
    threadID = (await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")) ?? ""
    expect(threadID).toBeTruthy()
    await page.route(history, (route) => route.fulfill({ json: { events: [], has_more: false } }))
    await composer(page).fill("Inspect the assigned inputs.")
    await composer(page).press("Enter")
    const transcript = page.getByTestId("transcript")
    await expect(transcript).toContainText("Collecting delegated work before completing this turn.")
    await expect(transcript).not.toContainText(prematureWorkerAnswer)
    await expect(page.getByTestId("status-badge")).toContainText("Working")
    await expect.poll(() => fixture.state().managerRequests).toBe(3)
    expect(fixture.state()).toMatchObject({ workerRequests: 1, cancelled: false })
    const { turns } = await (await request.get(`/api/threads/${threadID}/turns`)).json()
    expect(turns).toHaveLength(1)
    expect(turns[0].status).toBe("running")
    expect((await (await request.get(`/api/threads/${threadID}`)).json()).thread.goal).toBe("")
    expect((await (await request.get("/api/schedules")).json()).schedules).toHaveLength(0)
    await expect.poll(() => page.evaluate(() => (window as Window & { __collectionEvents?: unknown[] }).__collectionEvents?.length ?? 0)).toBe(1)
    await page.screenshot({ path: info.outputPath("ordinary-yield-worker-still-running.png"), animations: "disabled" })
    fixture.allowResult()
    await expect(transcript).toContainText(collectedWorkerAnswer)
    await expect(page.getByTestId("status-badge")).toContainText("Idle")
    const trace = await (await request.get(`/api/trace/${turns[0].id}`)).json()
    expect(trace.turn).toMatchObject({ id: turns[0].id, status: "done", final: collectedWorkerAnswer })
    expect(trace.events.filter((event: { kind: string }) => event.kind === "spawned")).toHaveLength(1)
    expect(trace.events.filter((event: { kind: string }) => ["cleanup", "generation_interrupted"].includes(event.kind))).toHaveLength(0)
    const receipt = trace.events.find((event: { kind: string; tool_call_id?: string }) => event.kind === "tool_result" && event.tool_call_id === "collect-original-worker")
    expect(JSON.parse(receipt.text).agents).toEqual([expect.objectContaining({ agent_id: fixture.state().workerID, generation: 1, status: "done", result: workerCompletionResult })])
    for (const event of trace.events.filter((event: { kind: string }) => ["agent_message", "delta", "done"].includes(event.kind))) expect(event.text ?? "").not.toContain(prematureWorkerAnswer)
    await page.unroute(history)
    await reloadOpenConversation(page)
    await expect(transcript).toContainText(collectedWorkerAnswer)
    await expect(transcript).not.toContainText(prematureWorkerAnswer)
    await expect(page.getByTestId("status-badge")).toContainText("Idle")
    expect(fixture.errors).toEqual([])
    expect(fixture.state()).toMatchObject({ managerRequests: 4, workerRequests: 1, cancelled: false })
    await page.screenshot({ path: info.outputPath("ordinary-collected-after-reload.png"), animations: "disabled" })
    await writeFile(info.outputPath("ordinary-worker-completion-evidence.json"), JSON.stringify({ trace, fixture: fixture.state(), errors: fixture.errors }, null, 2), { mode: 0o600 })
  } finally {
    await page.unroute(history)
    if (threadID) await request.post(`/api/threads/${threadID}/interrupt`, { timeout: 5000 }).catch(() => undefined)
    await fixture.close()
  }
})

test("a repeated completion waits for its worker and resumes once without human Continue", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_WORKER_COMPLETION !== "1", "requires the offline worker completion fixture")
  const fixture = await startWorkerCompletionFixture({ repeatFinal: true })
  let threadID = ""
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "collection-fixture", providers: [{ id: "collection-fixture", label: "Offline worker completion", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false }, memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    await freshConversation(page)
    threadID = (await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")) ?? ""
    await composer(page).fill("Inspect the assigned inputs.")
    await composer(page).press("Enter")
    const transcript = page.getByTestId("transcript")
    const pause = transcript.getByTestId("iteration-limit")
    await expect(pause).toContainText("resume once")
    await expect(pause).not.toContainText("tool-round limit")
    await expect(transcript).not.toContainText(prematureWorkerAnswer)
    expect(fixture.state()).toMatchObject({ managerRequests: 3, workerRequests: 1, cancelled: false })
    await reloadOpenConversation(page)
    await expect(pause).toContainText("resume once")
    await expect(pause.getByRole("button", { name: "Stop waiting for delegated results" })).toBeEnabled()
    await page.screenshot({ path: info.outputPath("worker-collection-awaiting-completion.png"), animations: "disabled" })
    fixture.allowResult()
    await expect(transcript).toContainText(collectedWorkerAnswer)
    await expect(pause).toContainText("Continuing to collect delegated results")
    await expect(pause.getByRole("button")).toHaveCount(0)
    await expect(page.getByTestId("status-badge")).toContainText("Idle")
    const { turns } = await (await request.get(`/api/threads/${threadID}/turns`)).json()
    expect(turns).toHaveLength(1)
    const trace = await (await request.get(`/api/trace/${turns[0].id}`)).json()
    expect(trace.events.filter((event: { kind: string }) => event.kind === "workers_collection_resumed")).toHaveLength(1)
    expect(trace.events.filter((event: { kind: string }) => event.kind === "max_iterations_continued")).toHaveLength(0)
    expect(trace.events.filter((event: { kind: string }) => event.kind === "model_retry")).toHaveLength(1)
    expect(fixture.state()).toMatchObject({ managerRequests: 5, workerRequests: 1, cancelled: false })
    expect(fixture.errors).toEqual([])
    await reloadOpenConversation(page)
    await expect(transcript).toContainText(collectedWorkerAnswer)
    await expect(pause).not.toContainText("resume once")
    await writeFile(info.outputPath("bounded-worker-completion-evidence.json"), JSON.stringify({ trace, fixture: fixture.state() }, null, 2), { mode: 0o600 })
  } finally {
    if (threadID) await request.post(`/api/threads/${threadID}/interrupt`, { timeout: 5000 }).catch(() => undefined)
    await fixture.close()
  }
})

test("declining a collection wait stops the original worker without a completion wake", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_WORKER_COMPLETION !== "1", "requires the offline worker completion fixture")
  const fixture = await startWorkerCompletionFixture({ repeatFinal: true })
  let threadID = ""
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "collection-fixture", providers: [{ id: "collection-fixture", label: "Offline worker completion", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false }, memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    await freshConversation(page)
    threadID = (await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")) ?? ""
    await composer(page).fill("Inspect the assigned inputs.")
    await composer(page).press("Enter")
    const transcript = page.getByTestId("transcript")
    await transcript.getByRole("button", { name: "Stop waiting for delegated results", exact: true }).click()
    await expect(page.getByTestId("status-badge")).toContainText("Idle")
    await expect(transcript).toContainText("Stopped before collecting delegated results.")
    await expect(transcript).not.toContainText("stopped after")
    await expect(transcript).not.toContainText(prematureWorkerAnswer)
    await expect.poll(() => fixture.state().cancelled).toBe(true)
    const { turns } = await (await request.get(`/api/threads/${threadID}/turns`)).json()
    expect(turns).toHaveLength(1)
    const trace = await (await request.get(`/api/trace/${turns[0].id}`)).json()
    expect(trace.turn.status).toBe("cancelled")
    expect(trace.events.filter((event: { kind: string }) => event.kind === "workers_collection_resumed")).toHaveLength(0)
    expect(trace.events.filter((event: { kind: string }) => event.kind === "workers_collection_declined")).toHaveLength(1)
    expect(fixture.state()).toMatchObject({ managerRequests: 3, workerRequests: 1 })
    await reloadOpenConversation(page)
    await expect(transcript).toContainText("Stopped before collecting delegated results.")
    await page.screenshot({ path: info.outputPath("worker-collection-declined.png"), animations: "disabled" })
    await writeFile(info.outputPath("declined-worker-collection-evidence.json"), JSON.stringify({ trace, fixture: fixture.state() }, null, 2), { mode: 0o600 })
  } finally {
    if (threadID) await request.post(`/api/threads/${threadID}/interrupt`, { timeout: 5000 }).catch(() => undefined)
    await fixture.close()
  }
})
