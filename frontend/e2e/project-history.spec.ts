import { expect, test, type APIRequestContext, type Response } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { randomUUID } from "node:crypto"

import { startProjectHistoryFixture } from "./project-history-fixture"
import { composer, waitForIdle } from "./session"

type Event = { kind: string; text?: string; tool_call_id?: string; thread_id?: string; turn_id?: string; seq?: number }
type Trace = { turn: { id: string; thread_id: string; status: string; final: string; error?: string }; events: Event[]; llm_calls: { agent_id: string; err?: string }[] }

async function createProject(request: APIRequestContext, name: string): Promise<string> {
  const response = await request.post("/api/projects", { data: { name, memory_enabled: false } })
  expect(response.ok()).toBe(true)
  return (await response.json()).project.id as string
}

async function createThread(request: APIRequestContext, projectID: string, title: string): Promise<string> {
  const response = await request.post("/api/threads", { data: { title, project_id: projectID } })
  expect(response.ok()).toBe(true)
  return (await response.json()).thread.id as string
}

for (const { repairWrite, menu } of [{ repairWrite: false, menu: false }, { repairWrite: true, menu: false }, { repairWrite: false, menu: true }]) {
test(`a project ${menu ? "uses its history menu and an unfiltered query for" : repairWrite ? "repairs a refused write and queries" : "queries"} raw discussions and keeps sources after reload`, async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_PROJECT_HISTORY !== "1", "requires the keyless raw history fixture")
  const fixture = await startProjectHistoryFixture({ repairWrite, omitProjectFilter: menu })
  const queryCount = repairWrite ? 3 : 2
  const nonce = randomUUID()
  const sourceTexts = [`查询结果必须包含来源时间，证据标识 ${nonce}-first。`, `导出能力仍处于建议阶段，尚未实现，证据标识 ${nonce}-second。`]
  const foreignText = `另一个项目要求默认开启自动删除，干扰标识 ${nonce}-foreign。`
  const projectName = `History ${nonce}`
  let targetID = ""
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "history-fixture", providers: [{ id: "history-fixture", label: "Offline raw history regression", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false }, memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    const projectID = await createProject(request, projectName)
    const otherProjectID = await createProject(request, `Other ${nonce}`)
    const sourceIDs = [await createThread(request, projectID, "Input requirements"), await createThread(request, projectID, "Unfinished proposals")]
    const foreignID = await createThread(request, otherProjectID, "Unrelated discussion")
    // Seed through normal turns rather than inserting fake events or answers.
    for (const [index, id] of [...sourceIDs, foreignID].entries()) {
      const text = index < sourceTexts.length ? sourceTexts[index] : foreignText
      fixture.addSource(text)
      const response = await request.post(`/api/threads/${id}/turns`, { data: { text } })
      expect(response.status()).toBe(202)
      const { turn } = await response.json()
      await expect.poll(async () => {
        const trace = await (await request.get(`/api/trace/${turn.id}`)).json() as Trace
        return trace.turn.status
      }).toBe("done")
    }
    if (!menu) {
      targetID = await createThread(request, projectID, "Project synthesis")
      fixture.target(projectID, targetID, sourceIDs)
    }
    await page.addInitScript(() => {
      const NativeEventSource = window.EventSource
      const observed: unknown[] = []
      ;(window as Window & { __historyToolEvents?: unknown[] }).__historyToolEvents = observed
      window.EventSource = class extends NativeEventSource {
        constructor(url: string | URL, options?: EventSourceInit) {
          super(url, options)
          for (const kind of ["tool_call", "tool_result"]) this.addEventListener(kind, (event) => { observed.push(JSON.parse(event.data)) })
        }
      }
    })
    await page.goto("/")
    const history = /\/api\/threads\/[^/]+\/log(?:\?|$)/
    await page.route(history, (route) => new URL(route.request().url()).pathname === `/api/threads/${targetID}/log` ? route.fulfill({ json: { events: [], has_more: false } }) : route.continue())
    let started: Response
    if (menu) {
      // Bind the real, newly created conversation before its actual model
      // request starts; continue the application request without mocking it.
      await page.route("**/api/threads/*/turns", async (route) => {
        const body = route.request().method() === "POST" ? route.request().postDataJSON() as { history_report?: boolean } : undefined
        if (body?.history_report === true) {
          targetID = new URL(route.request().url()).pathname.split("/")[3]
          fixture.target(projectID, targetID, sourceIDs)
        }
        await route.continue()
      })
      const accepted = page.waitForResponse((reply) => reply.request().method() === "POST" && /\/api\/threads\/[^/]+\/turns$/.test(new URL(reply.url()).pathname) && reply.request().postDataJSON().history_report === true)
      const project = page.getByTestId("project-wrap").filter({ has: page.getByRole("button", { name: projectName, exact: true }) })
      await project.hover()
      await project.getByRole("button", { name: `Project options for ${projectName}`, exact: true }).click()
      await page.getByRole("menuitem", { name: "Summarize project history", exact: true }).click()
      started = await accepted
      expect(started.request().postDataJSON().history_report).toBe(true)
    } else {
      await page.getByRole("button", { name: projectName, exact: true }).click()
      await page.locator(`[data-testid="thread-row"][data-id="${targetID}"]`).getByTestId("row-label").click()
      const accepted = page.waitForResponse((reply) => reply.request().method() === "POST" && new URL(reply.url()).pathname === `/api/threads/${targetID}/steer`)
      await composer(page).fill("结合这个项目近期的讨论，提炼已经确定的要求和仍待实现的建议，并注明来源。")
      await composer(page).press("Enter")
      started = await accepted
    }
    expect(started.status()).toBe(202)
    const { turn } = await started.json()
    await expect(page.getByTestId("thread-project")).toHaveText(projectName)
    await waitForIdle(page)
    const trace = await (await request.get(`/api/trace/${turn.id}`)).json() as Trace
    await writeFile(info.outputPath("project-history-original-trace.json"), JSON.stringify(trace, null, 2), { mode: 0o600 })
    expect(trace.turn.status, trace.turn.error).toBe("done")
    if (menu) {
      expect(trace.events.some((event) => event.kind === "history_report_requested")).toBe(true)
      const query = fixture.requests.flatMap((input) => input.messages.flatMap((message) => message.tool_calls ?? [])).find((call) => call.id === "history-project-rows")
      expect(query).toBeDefined()
      expect(JSON.parse(query!.function.arguments).sql).not.toContain("t.project_id = ?")
    }
    expect(fixture.errors).toEqual([])
    expect(fixture.requests).toHaveLength(queryCount + 1)
    expect(fixture.receipts).toHaveLength(queryCount)
    for (const text of sourceTexts) expect(JSON.stringify(fixture.requests[0])).not.toContain(text)
    expect(JSON.stringify(fixture.receipts.at(-1))).not.toContain(foreignText)
    const transcript = page.getByTestId("transcript")
    for (const text of sourceTexts) await expect(transcript).toContainText(text)
    await expect(transcript).not.toContainText(foreignText)
    for (const sourceID of sourceIDs) await expect(transcript).toContainText(sourceID)
    const fold = transcript.getByTestId("work-fold")
    if (await fold.count()) await fold.first().click()
    const queryRows = transcript.getByRole("button", { name: /^query_history/ })
    await expect(queryRows).toHaveCount(queryCount)
    await expect(queryRows.first()).toContainText("Inspect history schema")
    if (repairWrite) {
      const refused = queryRows.filter({ hasText: "DELETE FROM messages" })
      await expect(refused).toHaveAttribute("aria-invalid", "true")
      await refused.click()
      await expect(transcript.getByRole("alert")).toHaveText(fixture.receipts[1].error!.message!)
      await expect(refused.locator("..").getByTestId("tool-output")).toContainText('"code":"query_failed"')
    }
    await queryRows.last().click()
    await expect(queryRows.last()).toHaveAttribute("aria-expanded", "true")
    await writeFile(info.outputPath("project-history-live.png"), await page.screenshot({ animations: "disabled" }), { mode: 0o600 })
    const live = await page.evaluate(() => (window as Window & { __historyToolEvents?: Event[] }).__historyToolEvents ?? [])
    expect(live.filter((event) => event.kind === "tool_call")).toHaveLength(queryCount)
    expect(live.filter((event) => event.kind === "tool_result")).toHaveLength(queryCount)
    expect(trace.turn).toMatchObject({ id: turn.id, thread_id: targetID, status: "done" })
    expect(trace.turn.final).not.toBe(fixture.answer())
    expect(trace.turn.final).toContain("原文证据")
    expect(trace.turn.final).not.toContain('"version":1')
    for (const text of sourceTexts) expect(trace.turn.final).toContain(text)
    const calls = trace.events.filter((event) => event.kind === "tool_call")
    expect(calls).toHaveLength(queryCount)
    expect(calls.every((event) => event.text?.startsWith("query_history("))).toBe(true)
    expect(trace.events.some((event) => event.kind === "tool_call" && event.text?.startsWith("exec("))).toBe(false)
    for (const call of calls) {
      const receipt = trace.events.find((event) => event.kind === "tool_result" && event.tool_call_id === call.tool_call_id)
      expect(receipt).toMatchObject({ thread_id: targetID, turn_id: turn.id })
      expect(JSON.parse(receipt?.text ?? "{}").ok).toBe(call.tool_call_id !== "history-write-refused")
    }
    expect(trace.llm_calls.filter((call) => call.agent_id === "manager")).toHaveLength(queryCount + 1)
    await page.unroute(history)
    await page.reload()
    await expect(page.getByRole("heading", { name: "What should we work on?" })).toBeVisible()
    const project = page.getByRole("button", { name: projectName, exact: true })
    await expect(project).toBeVisible()
    if (await project.getAttribute("aria-expanded") !== "true") await project.click()
    await page.locator(`[data-testid="thread-row"][data-id="${targetID}"]`).getByTestId("row-label").click()
    await expect(page.getByTestId("thread-title")).toHaveText(menu ? "Project history report" : "Project synthesis")
    for (const text of sourceTexts) await expect(transcript).toContainText(text)
    for (const sourceID of sourceIDs) await expect(transcript).toContainText(sourceID)
    await expect(transcript).not.toContainText(foreignText)
    await page.getByRole("tab", { name: "Trace", exact: true }).click()
    await page.getByTestId("trace-log-toggle").click()
    await expect(page.getByTestId("trace-log")).toContainText("query_history")
    expect(fixture.requests).toHaveLength(queryCount + 1)
    await writeFile(info.outputPath("project-history-reloaded.png"), await page.screenshot({ animations: "disabled" }), { mode: 0o600 })
    await writeFile(info.outputPath("project-history-evidence.json"), JSON.stringify({ sourceIDs, sourceTexts, foreignID, foreignText, trace, live, requests: fixture.requests, receipts: fixture.receipts }, null, 2), { mode: 0o600 })
  } finally {
    if (targetID) await request.post(`/api/threads/${targetID}/interrupt`).catch(() => {})
    await fixture.close()
  }
})
}
