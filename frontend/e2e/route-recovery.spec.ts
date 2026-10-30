import { expect, test, type APIRequestContext, type Page } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { createServer } from "node:http"

import { composer, freshConversation, reloadOpenConversation, statusBadge, waitForIdle } from "./session"

type TraceEvent = { kind: string; agent_id?: string; message_id?: string; text?: string }
type TraceCall = { agent_id: string; err?: string; output_chars: number }
type Attempt = { method?: string; path?: string; hasAuthorization: boolean; receivedAt: number }

const completed = "The requested check completed after service recovery."

async function routeFixture(recover: boolean) {
  const attempts: Attempt[] = []
  const errors: string[] = []
  let policyRequests = 0
  const endpoint = createServer((incoming, response) => {
    let body = ""
    incoming.setEncoding("utf8")
    incoming.on("data", (part: string) => { body += part })
    incoming.on("end", () => {
      try {
        if (incoming.method !== "POST" || incoming.url !== "/chat/completions") throw new Error("unexpected model request path")
        const input = JSON.parse(body) as { messages: { role: string; content?: string }[]; stream?: boolean; tools?: unknown[]; tool_choice?: string }
        if (input.messages[0]?.content?.split("\n", 1)[0] === "## Schedule answer claims") {
          if (input.stream || input.tools?.length || input.tool_choice !== "none") throw new Error("invalid policy request protocol")
          policyRequests++
          response.writeHead(200, { "Content-Type": "application/json" })
          response.end(JSON.stringify({ choices: [{ index: 0, message: { role: "assistant", content: '{"claims":[]}' }, finish_reason: "stop" }] }))
          return
        }
        attempts.push({ method: incoming.method, path: incoming.url, hasAuthorization: Boolean(incoming.headers.authorization), receivedAt: Date.now() })
        if (!recover || attempts.length <= 3) {
          response.writeHead(503, { "Content-Type": "application/json" })
          response.end(JSON.stringify({ error: { code: "provider_unavailable", message: "no healthy provider can serve this model", type: "server_error" } }))
          return
        }
        response.writeHead(200, { "Content-Type": "text/event-stream" })
        response.end(`data: ${JSON.stringify({ id: "route-recovered", choices: [{ index: 0, delta: { role: "assistant", content: completed }, finish_reason: null }] })}\n\ndata: ${JSON.stringify({ id: "route-recovered", choices: [{ index: 0, delta: {}, finish_reason: "stop" }] })}\n\ndata: [DONE]\n\n`)
      } catch (error) {
        errors.push(String(error))
        response.writeHead(400, { "Content-Type": "application/json" })
        response.end(JSON.stringify({ error: { message: String(error), type: "invalid_request_error" } }))
      }
    })
  })
  await new Promise<void>((resolve, reject) => {
    endpoint.once("error", reject)
    endpoint.listen(0, "127.0.0.1", () => { endpoint.off("error", reject); resolve() })
  })
  const address = endpoint.address()
  if (!address || typeof address === "string") throw new Error("routing fixture did not listen")
  return {
    url: `http://127.0.0.1:${address.port}`, attempts, errors,
    policyRequests: () => policyRequests,
    close: () => new Promise<void>((resolve, reject) => {
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    }),
  }
}

async function configureRouteFixture(request: APIRequestContext, url: string) {
  const settingsResponse = await request.get("/api/settings")
  expect(settingsResponse.ok()).toBe(true)
  const { settings } = await settingsResponse.json()
  expect((await request.put("/api/settings", { data: {
    models: { default: "route-fixture", providers: [{
      id: "route-fixture", label: "Offline routing recovery", base_url: url, model: "fixture-model", api_key: "", context_window: 128000,
      first_byte_timeout_seconds: 2, timeout_seconds: 3,
    }] },
    swarm: { ...settings.swarm, auto_title: false },
    memory: { ...settings.memory, auto_review: false },
  } })).ok()).toBe(true)
}

async function startRouteTurn(page: Page) {
  await freshConversation(page)
  const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
  expect(threadID).toBeTruthy()
  const accepted = page.waitForResponse((response) => response.request().method() === "POST"
    && new URL(response.url()).pathname === `/api/threads/${threadID}/steer`)
  await composer(page).fill("Inspect the current input.")
  await composer(page).press("Enter")
  const response = await accepted
  expect(response.status()).toBe(202)
  const { turn } = await response.json()
  return { threadID: threadID!, turnID: turn.id as string }
}

test("routing refusals keep the same turn waiting through reload until the service recovers", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_MODEL_API_ERROR !== "1", "requires the opt-in keyless loopback model endpoint")
  const fixture = await routeFixture(true)
  let threadID = "", turnID = ""
  let originalFailure = false
  try {
    await configureRouteFixture(request, fixture.url)
    ;({ threadID, turnID } = await startRouteTurn(page))
    const transcript = page.getByTestId("transcript")
    const stop = page.getByRole("button", { name: "Stop", exact: true })
    const trace = async () => (await (await request.get(`/api/trace/${turnID}`)).json())
    for (const seconds of [5, 10, 20]) {
      await expect(transcript.getByText(`Waiting ${seconds}s before retrying the model request.`, { exact: true })).toBeVisible()
      await expect(stop).toBeEnabled()
      await expect(statusBadge(page)).toContainText("Working")
      const waiting = await trace()
      expect(waiting.turn).toMatchObject({ id: turnID, thread_id: threadID, status: "running" })
      expect(waiting.events.filter((event: TraceEvent) => ["model_retry", "error", "generation_interrupted", "generation_recovered"].includes(event.kind))).toEqual([])
      await expect(transcript.getByTestId("generation-notice")).toHaveCount(0)
      await expect(transcript).not.toContainText("The model service is unavailable. Retrying.")
      if (seconds === 10) {
        await reloadOpenConversation(page)
        await expect(transcript.getByText("Waiting 10s before retrying the model request.", { exact: true })).toBeVisible()
        await expect(stop).toBeEnabled()
        expect((await trace()).turn.id).toBe(turnID)
        await page.screenshot({ path: info.outputPath("routing-wait-replayed.png"), animations: "disabled" })
      }
    }
    await expect(transcript.getByText(completed, { exact: true })).toBeVisible({ timeout: 30_000 })
    await waitForIdle(page)
    const saved = await trace()
    expect(saved.turn).toMatchObject({ id: turnID, thread_id: threadID, status: "done" })
    expect(saved.turn.error ?? "").toBe("")
    expect(saved.events.filter((event: TraceEvent) => ["model_retry", "error", "generation_interrupted", "generation_recovered"].includes(event.kind))).toEqual([])
    for (const kind of ["generation_started", "generation_completed"]) {
      expect(saved.events.filter((event: TraceEvent) => event.kind === kind && event.agent_id === "manager")).toHaveLength(1)
    }
    const calls = (saved.llm_calls as TraceCall[]).filter((call) => call.agent_id === "manager")
    expect(calls).toHaveLength(4)
    expect(calls.filter((call) => (call.err ?? "").includes("no healthy provider can serve this model"))).toHaveLength(3)
    expect(calls[3].err ?? "").toBe("")
    expect(calls[3].output_chars).toBe(completed.length)
    expect(fixture.policyRequests()).toBe(1)
    expect(saved.llm_calls).toHaveLength(5) // Four real attempts plus the separate answer policy call.
    expect(fixture.attempts).toHaveLength(4)
    for (const [index, seconds] of [5, 10, 20].entries()) {
      expect(fixture.attempts[index + 1].receivedAt - fixture.attempts[index].receivedAt).toBeGreaterThanOrEqual(seconds * 1000 - 100)
    }
    expect(fixture.attempts.every((attempt) => attempt.method === "POST" && attempt.path === "/chat/completions" && !attempt.hasAuthorization)).toBe(true)
    expect(fixture.errors).toEqual([])
    const turns = await (await request.get(`/api/threads/${threadID}/turns`)).json()
    expect(turns.turns.map((turn: { id: string }) => turn.id)).toEqual([turnID])
    await expect(stop).toHaveCount(0)
    await page.screenshot({ path: info.outputPath("routing-recovered.png"), animations: "disabled" })
  } catch (error) {
    originalFailure = true
    await page.screenshot({ path: info.outputPath("first-failure.png"), animations: "disabled" }).catch((failure) => info.annotations.push({ type: "artifact", description: String(failure) }))
    throw error
  } finally {
    let artifactFailure: unknown
    try {
      const trace = turnID ? await (await request.get(`/api/trace/${turnID}`, { timeout: 5000 })).json() : undefined
      await writeFile(info.outputPath("routing-recovery-trace.json"), JSON.stringify({ trace, attempts: fixture.attempts, policyRequests: fixture.policyRequests(), errors: fixture.errors }, null, 2), { mode: 0o600 })
    } catch (error) {
      artifactFailure = error
      info.annotations.push({ type: "artifact", description: String(error) })
    } finally {
      if (threadID) await request.post(`/api/threads/${threadID}/interrupt`).catch((error) => info.annotations.push({ type: "cleanup", description: String(error) }))
      try { await fixture.close() } catch (error) { artifactFailure ??= error }
    }
    if (artifactFailure && !originalFailure) throw artifactFailure
  }
})

test("Stop cancels routing recovery before the next probe", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_MODEL_API_ERROR !== "1", "requires the opt-in keyless loopback model endpoint")
  const fixture = await routeFixture(false)
  let threadID = "", turnID = ""
  let originalFailure = false
  try {
    await configureRouteFixture(request, fixture.url)
    ;({ threadID, turnID } = await startRouteTurn(page))
    await expect(page.getByTestId("transcript").getByText("Waiting 5s before retrying the model request.", { exact: true })).toBeVisible()
    expect(fixture.attempts).toHaveLength(1)
    await page.getByRole("button", { name: "Stop", exact: true }).click()
    await expect(statusBadge(page)).toContainText("Idle", { timeout: 3000 })
    // Cross the cancelled five-second cooldown; a check immediately after Stop
    // cannot prove that its timer no longer sends a request.
    await page.waitForTimeout(5200)
    const trace = await (await request.get(`/api/trace/${turnID}`)).json()
    expect(trace.turn).toMatchObject({ id: turnID, status: "cancelled" })
    expect(trace.events.filter((event: TraceEvent) => ["model_retry", "generation_recovered", "session_memory", "memory_review"].includes(event.kind))).toEqual([])
    expect(trace.llm_calls).toHaveLength(1)
    expect(fixture.attempts).toHaveLength(1)
    expect(fixture.policyRequests()).toBe(0)
    expect(fixture.errors).toEqual([])
    await page.screenshot({ path: info.outputPath("routing-stopped.png"), animations: "disabled" })
    await writeFile(info.outputPath("routing-stopped-trace.json"), JSON.stringify({ trace, attempts: fixture.attempts }, null, 2), { mode: 0o600 })
  } catch (error) {
    originalFailure = true
    await page.screenshot({ path: info.outputPath("first-failure.png"), animations: "disabled" }).catch((failure) => info.annotations.push({ type: "artifact", description: String(failure) }))
    throw error
  } finally {
    if (threadID) await request.post(`/api/threads/${threadID}/interrupt`).catch((error) => info.annotations.push({ type: "cleanup", description: String(error) }))
    try { await fixture.close() } catch (error) {
      if (!originalFailure) throw error
      info.annotations.push({ type: "cleanup", description: String(error) })
    }
  }
})
