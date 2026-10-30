import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { createServer } from "node:http"

import { composer, freshConversation, reloadOpenConversation, waitForIdle } from "./session"

const PUBLIC_ERROR = "model API request failed: HTTP 502 Bad Gateway: unexpected end of JSON input"
const RAW_ERROR = "error, status code: 502, status: 502 Bad Gateway, message: unexpected end of JSON input, body: "

type TraceEvent = { thread_id: string; turn_id: string; agent_id?: string; seq: number; kind: string; err?: string; text?: string }
type TraceCall = { thread_id: string; turn_id: string; agent_id: string; output_chars: number; err: string }

for (const responseCase of [
  { name: "empty 502", status: 502, body: "", error: PUBLIC_ERROR, raw: RAW_ERROR, reason: "model_unavailable", notice: "The model service is unavailable. Retrying." },
  { name: "empty 400", status: 400, body: "", error: "model API request failed: HTTP 400 Bad Request: unexpected end of JSON input", raw: "error, status code: 400, status: 400 Bad Request, message: unexpected end of JSON input, body: ", reason: "model_response_invalid", notice: "The model service returned a response that could not be decoded. Retrying." },
  { name: "malformed successful stream", status: 200, body: "data: {\n\n", error: "the model service returned a response that could not be decoded", raw: "failed to receive stream chunk: unexpected end of JSON input", reason: "model_response_invalid", notice: "The model service returned a response that could not be decoded. Retrying." },
]) {
test(`${responseCase.name} stays visible in Chat and the same turn Trace after reload`, async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_MODEL_API_ERROR !== "1", "requires the opt-in keyless loopback model endpoint")
  const attempts: { method?: string; path?: string; hasAuthorization: boolean; receivedAt: number }[] = []
  const endpoint = createServer((incoming, response) => {
    attempts.push({ method: incoming.method, path: incoming.url, hasAuthorization: Boolean(incoming.headers.authorization), receivedAt: Date.now() })
    incoming.resume()
    response.writeHead(responseCase.status, { "Content-Type": responseCase.status === 200 ? "text/event-stream" : "application/json" })
    response.end(responseCase.body)
  })
  await new Promise<void>((resolve, reject) => {
    endpoint.once("error", reject)
    endpoint.listen(0, "127.0.0.1", () => { endpoint.off("error", reject); resolve() })
  })
  try {
    const address = endpoint.address()
    if (!address || typeof address === "string") throw new Error("loopback model endpoint did not listen")
    const settingsResponse = await request.get("/api/settings")
    expect(settingsResponse.ok()).toBe(true)
    const { settings } = await settingsResponse.json()
    const configured = await request.put("/api/settings", { data: {
      models: { default: "fixture-provider", providers: [{
        id: "fixture-provider", label: "Offline API regression", base_url: `http://127.0.0.1:${address.port}`,
        model: "fixture-model", api_key: "", context_window: 128000,
      }] },
      swarm: { ...settings.swarm, auto_title: false },
      memory: { ...settings.memory, auto_review: false },
    } })
    expect(configured.ok()).toBe(true)
    const current = await (await request.get("/api/settings")).json()
    expect(current.settings.models.providers).toHaveLength(1)
    expect(current.settings.models.providers[0]).toMatchObject({ id: "fixture-provider", ready: true, has_api_key: false })

    await freshConversation(page)
    const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
    expect(threadID).toBeTruthy()
    const accepted = page.waitForResponse((response) => response.request().method() === "POST"
      && new URL(response.url()).pathname === `/api/threads/${threadID}/steer`)
    await composer(page).fill("Perform the requested check")
    await composer(page).press("Enter")
    const started = await accepted
    expect(started.status()).toBe(202)
    const { turn } = await started.json()
    const transcript = page.getByTestId("transcript")
    // Generation notices now retain each attempt's cause too. Select the
    // terminal error bubble rather than one of those diagnostic details.
    const terminalError = transcript.locator("p.stream-text").and(transcript.getByText(responseCase.error, { exact: true }))
    const backoffNotices = [5, 10].map((seconds) => `Waiting ${seconds}s before retrying the model request.`)
    if (responseCase.status === 502) {
      await expect(transcript.getByText(backoffNotices[0], { exact: true })).toBeVisible()
      const row = transcript.getByTestId("backoff-notice").filter({ hasText: backoffNotices[0] })
      const details = row.getByRole("button", { name: "Retry details", exact: true })
      await details.focus()
      await details.press("Enter")
      await expect(row.getByRole("region", { name: "Retry details" })).toBeVisible()
      await expect(row).toContainText("Manager model request · manager")
      await expect(row).toContainText("does not mean each step failed")
      await row.getByRole("button", { name: "Hide details" }).click()
      await expect(transcript).not.toContainText('"wait_ms"')
    }
    await waitForIdle(page)
    await expect(terminalError).toHaveText(responseCase.error)
    await expect(terminalError).toBeVisible()
    await expect(transcript).not.toContainText("a tool call was not valid JSON")
    await expect(page.getByRole("button", { name: "Stop", exact: true })).toHaveCount(0)
    await page.screenshot({ path: info.outputPath("api-error-chat.png"), animations: "disabled" })

    const traceResponse = await request.get(`/api/trace/${turn.id}`)
    expect(traceResponse.ok()).toBe(true)
    const trace = await traceResponse.json()
    expect(trace.turn).toMatchObject({ id: turn.id, thread_id: threadID, status: "error", error: responseCase.error })
    for (const event of trace.events as TraceEvent[]) {
      expect(event).toMatchObject({ turn_id: turn.id, thread_id: threadID })
      expect(event.seq).toBeGreaterThan(0)
    }
    expect(trace.events.filter((event: TraceEvent) => event.kind === "model_retry")).toHaveLength(2)
    for (const event of trace.events.filter((event: TraceEvent) => event.kind === "model_retry") as TraceEvent[]) {
      expect(JSON.parse(event.text ?? "{}").reason).toBe(responseCase.reason)
    }
    expect(trace.events.some((event: TraceEvent) => event.kind === "tool_call")).toBe(false)
    if (responseCase.status === 502) {
      const waits = trace.events.filter((event: TraceEvent) => event.kind === "model_backoff") as TraceEvent[]
      expect(waits).toHaveLength(2)
      for (const [index, event] of waits.entries()) {
        expect(event).toMatchObject({ turn_id: turn.id, thread_id: threadID, agent_id: "manager" })
        const wait = JSON.parse(event.text ?? "{}")
        expect(wait.scope).toBe("origin")
        expect(wait.cooldown_id).toEqual(expect.any(String))
        expect(wait.cooldown_id).not.toContain("127.0.0.1")
        expect(wait.attempt).toBe(index + 1)
        expect(wait.wait_ms).toBeGreaterThan(0)
        expect(wait.wait_ms).toBeLessThanOrEqual((index + 1) * 5000)
        expect(attempts[index + 1].receivedAt - attempts[index].receivedAt).toBeGreaterThanOrEqual((index + 1) * 5000 - 100)
        await expect(transcript.getByText(backoffNotices[index], { exact: true })).toBeVisible()
      }
    }

    const errors = trace.events.filter((event: TraceEvent) => event.kind === "error")
    expect(errors).toHaveLength(1)
    expect(errors[0]).toMatchObject({ err: responseCase.error, turn_id: turn.id, thread_id: threadID })
    expect(errors[0].seq).toBeGreaterThan(0)
    expect(trace.llm_calls).toHaveLength(3)
    for (const call of trace.llm_calls as TraceCall[]) {
      expect(call).toMatchObject({ thread_id: threadID, turn_id: turn.id, agent_id: "manager", output_chars: 0, err: responseCase.raw })
    }
    expect(attempts.map(({ method, path, hasAuthorization }) => ({ method, path, hasAuthorization })))
      .toEqual(Array.from({ length: 3 }, () => ({ method: "POST", path: "/chat/completions", hasAuthorization: false })))
    const turns = await (await request.get(`/api/threads/${threadID}/turns`)).json()
    expect(turns.turns.map((row: { id: string }) => row.id)).toEqual([turn.id])

    await page.getByRole("tab", { name: "Trace", exact: true }).click()
    await expect(page.getByRole("tabpanel").getByText(turn.id, { exact: true })).toBeVisible()
    await expect(page.getByTestId("trace-status")).toHaveText("failed")
    await expect(page.getByTestId("trace-error")).toHaveText(responseCase.error)
    await page.getByTestId("trace-log-toggle").click()
    const log = page.getByTestId("trace-log")
    await expect(log.getByText(responseCase.error, { exact: true })).toBeVisible()
    await expect(log.getByText(responseCase.notice, { exact: true })).toHaveCount(2)
    if (responseCase.status === 502) {
      for (const notice of backoffNotices) await expect(log.getByText(notice, { exact: true })).toBeVisible()
    }
    await page.screenshot({ path: info.outputPath("api-error-trace.png"), animations: "disabled" })

    await reloadOpenConversation(page)
    await waitForIdle(page)
    await expect(terminalError).toHaveText(responseCase.error)
    await expect(terminalError).toBeVisible()
    await page.getByRole("tab", { name: "Trace", exact: true }).click()
    await expect(page.getByRole("tabpanel").getByText(turn.id, { exact: true })).toBeVisible()
    await expect(page.getByTestId("trace-error")).toHaveText(responseCase.error)
    await page.getByTestId("trace-log-toggle").click()
    await expect(log.getByText(responseCase.error, { exact: true })).toBeVisible()
    await expect(log.getByText(responseCase.notice, { exact: true })).toHaveCount(2)
    if (responseCase.status === 502) {
      for (const notice of backoffNotices) await expect(log.getByText(notice, { exact: true })).toBeVisible()
      const replayed = transcript.getByTestId("backoff-notice").filter({ hasText: backoffNotices[0] })
      await replayed.getByRole("button", { name: "Retry details", exact: true }).click()
      await expect(replayed.getByRole("region", { name: "Retry details" })).toContainText("Manager model request · manager")
      await expect(transcript).not.toContainText('"wait_ms"')
    }
    expect(attempts).toHaveLength(3)
    await page.screenshot({ path: info.outputPath("api-error-replayed.png"), animations: "disabled" })
    await writeFile(info.outputPath("api-error-trace.json"), JSON.stringify({ trace, attempts }, null, 2), { mode: 0o600 })
  } finally {
    await new Promise<void>((resolve, reject) => {
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    })
  }
})

}

test("a first-byte timeout guides configuration and a longer wait reaches the model response", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_MODEL_API_ERROR !== "1", "requires the opt-in keyless loopback model endpoint")
  let attempts = 0
  let policyRequests = 0
  const fixtureErrors: string[] = []
  const endpoint = createServer((incoming, response) => {
    let body = ""
    incoming.setEncoding("utf8")
    incoming.on("data", (part: string) => { body += part })
    incoming.on("end", () => {
      try {
        const input = JSON.parse(body) as { messages: { role: string; content?: string }[]; stream?: boolean; tools?: unknown[]; tool_choice?: string }
        if (input.messages[0]?.role === "system" && input.messages[0].content?.split("\n", 1)[0] === "## Schedule answer claims") {
          if (input.stream || input.tools?.length || input.tool_choice !== "none" || input.messages.length !== 2 || input.messages[1].role !== "user") {
            throw new Error("Schedule answer policy must remain a separate non-streaming, tool-free request")
          }
          policyRequests++
          response.writeHead(200, { "Content-Type": "application/json" })
          response.end(JSON.stringify({ choices: [{ index: 0, message: { role: "assistant", content: '{"claims":[]}' }, finish_reason: "stop" }] }))
          return
        }
        attempts++
        const timer = setTimeout(() => {
          response.writeHead(200, { "Content-Type": "text/event-stream" })
          response.end('data: {"id":"offline","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"The check is complete."},"finish_reason":null}]}\n\ndata: {"id":"offline","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n')
        }, 1200)
        response.once("close", () => clearTimeout(timer))
      } catch (error) {
        fixtureErrors.push(String(error))
        response.writeHead(400, { "Content-Type": "application/json" })
        response.end(JSON.stringify({ error: { message: String(error), type: "invalid_request_error" } }))
      }
    })
  })
  await new Promise<void>((resolve) => endpoint.listen(0, "127.0.0.1", resolve))
  try {
    const address = endpoint.address()
    if (!address || typeof address === "string") throw new Error("offline endpoint did not listen")
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "slow-start", providers: [{
        id: "slow-start", label: "Offline slow start", base_url: `http://127.0.0.1:${address.port}`,
        model: "offline", api_key: "", timeout_seconds: 1, first_byte_timeout_seconds: 1,
      }] },
      swarm: { ...settings.swarm, auto_title: false },
      memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)

    await freshConversation(page)
    const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
    const submit = async () => {
      const response = page.waitForResponse((r) => r.request().method() === "POST"
        && new URL(r.url()).pathname === `/api/threads/${threadID}/steer`)
      await composer(page).fill("Perform the requested check")
      await composer(page).press("Enter")
      const accepted = await response
      expect(accepted.status()).toBe(202)
      return (await accepted.json()).turn.id as string
    }
    const failed = await submit()
    const failure = page.getByTestId("first-byte-timeout-error")
    await expect(failure).toContainText("The model sent no first byte within 1s.")
    await waitForIdle(page)
    expect(attempts).toBe(2)
    const trace = await (await request.get(`/api/trace/${failed}`)).json()
    expect(trace.turn).toMatchObject({ status: "error", provider_id: "slow-start", error: "the model sent no first byte within 1s" })
    expect(trace.events.some((e: TraceEvent) => e.kind === "error" && e.err === trace.turn.error)).toBe(true)
    await reloadOpenConversation(page)
    await expect(failure).toBeVisible()
    await page.screenshot({ path: info.outputPath("first-byte-guidance.png"), animations: "disabled" })
    await failure.getByRole("button", { name: "Configure first-byte timeout" }).click()
    const dialog = page.getByRole("dialog")
    const wait = dialog.getByLabel("First-byte timeout (seconds)")
    await expect(wait).toBeFocused()
    await expect(wait).toHaveValue("1")
    await wait.fill("3")
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.models.providers[0]).toMatchObject({ first_byte_timeout_seconds: 3, timeout_seconds: 1 })
    await failure.getByRole("button", { name: "Configure first-byte timeout" }).click()
    await expect(dialog.getByLabel("First-byte timeout (seconds)")).toHaveValue("3")
    await page.screenshot({ path: info.outputPath("first-byte-setting.png"), animations: "disabled" })
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    const retried = await submit()
    await expect(page.getByTestId("transcript").getByText("The check is complete.", { exact: true })).toBeVisible()
    await waitForIdle(page)
    const success = await (await request.get(`/api/trace/${retried}`)).json()
    expect(success.turn.status).toBe("done")
    expect(attempts).toBe(3)
    expect(policyRequests).toBe(1)
    expect(fixtureErrors).toEqual([])
    await writeFile(info.outputPath("first-byte-traces.json"), JSON.stringify({ failed: trace, retried: success, attempts, policyRequests }, null, 2), { mode: 0o600 })
  } finally {
    await new Promise<void>((resolve) => {
      endpoint.close(() => resolve())
      endpoint.closeAllConnections()
    })
  }
})

test("queued human messages run FIFO after a scheduled first-byte timeout without retrying its future wake", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_MODEL_API_ERROR !== "1", "requires the opt-in keyless loopback model endpoint")
  const queued = ["Review the next input.", "Review the remaining input."]
  const completed = "The queued request is complete."
  const timeoutError = "the model sent no first byte within 2s"
  const attempts: { scheduled: boolean; user: string; hasAuthorization: boolean }[] = []
  const fixtureErrors: string[] = []
  let policyRequests = 0
  let scheduleID = ""
  const evidence: Record<string, unknown> = {}
  const endpoint = createServer((incoming, response) => {
    let body = ""
    incoming.setEncoding("utf8")
    incoming.on("data", (part: string) => { body += part })
    incoming.on("end", () => {
      try {
        if (incoming.method !== "POST" || incoming.url !== "/chat/completions" || incoming.headers.authorization) {
          throw new Error("The fixture accepts only keyless SDK chat requests")
        }
        const input = JSON.parse(body) as { messages: { role: string; content?: string }[]; stream?: boolean; tools?: unknown[]; tool_choice?: string }
        if (input.messages[0]?.role === "system" && input.messages[0].content?.split("\n", 1)[0] === "## Schedule answer claims") {
          if (input.stream || input.tools?.length || input.tool_choice !== "none" || input.messages.length !== 2 || input.messages[1].role !== "user") {
            throw new Error("Schedule answer policy must remain a separate non-streaming, tool-free request")
          }
          const candidate = JSON.parse(input.messages[1].content ?? "") as { messages: string[] }
          if (JSON.stringify(candidate.messages) !== JSON.stringify([completed])) throw new Error("Unexpected policy candidate")
          policyRequests++
          response.writeHead(200, { "Content-Type": "application/json" })
          response.end(JSON.stringify({ choices: [{ index: 0, message: { role: "assistant", content: '{"claims":[]}' }, finish_reason: "stop" }] }))
          return
        }
        const user = input.messages.findLast((message) => message.role === "user")?.content ?? ""
        const scheduled = user.startsWith("This turn is a scheduled check.")
        attempts.push({ scheduled, user, hasAuthorization: Boolean(incoming.headers.authorization) })
        // Send no headers for the scheduled request: both first-byte waits
        // must expire in the real SDK, rather than a mocked browser response.
        if (scheduled) return
        if (!queued.includes(user) || !input.stream) throw new Error("Unexpected human manager request")
        response.writeHead(200, { "Content-Type": "text/event-stream" })
        response.end(`data: ${JSON.stringify({ id: "queued", object: "chat.completion.chunk", choices: [{ index: 0, delta: { role: "assistant", content: completed }, finish_reason: null }] })}\n\ndata: ${JSON.stringify({ id: "queued", object: "chat.completion.chunk", choices: [{ index: 0, delta: {}, finish_reason: "stop" }] })}\n\ndata: [DONE]\n\n`)
      } catch (error) {
        fixtureErrors.push(String(error))
        response.writeHead(400, { "Content-Type": "application/json" })
        response.end(JSON.stringify({ error: { message: String(error), type: "invalid_request_error" } }))
      }
    })
  })
  await new Promise<void>((resolve) => endpoint.listen(0, "127.0.0.1", resolve))
  try {
    const address = endpoint.address()
    if (!address || typeof address === "string") throw new Error("offline queue endpoint did not listen")
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "queue-after-error", providers: [{
        id: "queue-after-error", label: "Offline queued-message regression", base_url: `http://127.0.0.1:${address.port}`,
        model: "offline", api_key: "", first_byte_timeout_seconds: 2, timeout_seconds: 3,
      }] },
      swarm: { ...settings.swarm, auto_title: false },
      memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    await freshConversation(page)
    const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
    expect(threadID).toBeTruthy()
    const created = await request.post("/api/schedules", { data: {
      kind: "thread", thread_id: threadID, title: "Queued-message recovery", prompt: "Inspect the assigned state.", every_s: 3600,
    } })
    expect(created.status()).toBe(201)
    scheduleID = (await created.json()).schedule.id
    const started = await request.post(`/api/schedules/${scheduleID}/run`)
    expect(started.status()).toBe(202)
    const { turn: failedTurn } = await started.json()
    const before = await (await request.get(`/api/schedules/${scheduleID}`)).json()
    evidence.before = before
    expect(before.schedule).toMatchObject({ id: scheduleID, kind: "thread", thread_id: threadID, status: "active", every_s: 3600 })
    expect(Date.parse(before.schedule.next_run_at)).toBeGreaterThan(Date.now())
    await expect(page.getByRole("button", { name: "Stop", exact: true })).toBeVisible()
    await expect.poll(() => attempts.filter((row) => row.scheduled).length).toBeGreaterThan(0)
    const queueIDs: string[] = []
    for (const text of queued) {
      const accepted = page.waitForResponse((response) => response.request().method() === "POST"
        && new URL(response.url()).pathname === `/api/threads/${threadID}/followups`)
      await composer(page).fill(text)
      await composer(page).press("Enter")
      const response = await accepted
      expect(response.status()).toBe(202)
      queueIDs.push((await response.json()).followup.id)
    }
    const tray = page.getByTestId("followup-queue")
    for (const text of queued) await expect(tray).toContainText(text)
    const savedQueue = await (await request.get(`/api/threads/${threadID}/followups`)).json()
    expect(savedQueue.followups.map((row: { id: string; text: string }) => [row.id, row.text])).toEqual(queueIDs.map((id, i) => [id, queued[i]]))
    evidence.queued = savedQueue
    await page.screenshot({ path: info.outputPath("scheduled-check-human-FIFO-queued.png"), animations: "disabled" })

    await expect.poll(async () => {
      const body = await (await request.get(`/api/threads/${threadID}/turns`)).json()
      evidence.turns = body.turns
      return body.turns.map((turn: { status: string }) => turn.status)
    }).toEqual(["error", "done", "done"])
    const turns = evidence.turns as { id: string; user_text: string; schedule_continue?: boolean; schedule_run_id?: string }[]
    expect(turns[0].id).toBe(failedTurn.id)
    expect(turns.slice(1).map((turn) => turn.user_text)).toEqual(queued)
    expect(new Set(turns.map((turn) => turn.id)).size).toBe(3)
    expect(turns.slice(1).every((turn) => !turn.schedule_continue && !turn.schedule_run_id)).toBe(true)
    const failed = await (await request.get(`/api/trace/${failedTurn.id}`)).json()
    evidence.failedTrace = failed
    expect(failed.turn).toMatchObject({ id: failedTurn.id, thread_id: threadID, status: "error", schedule_continue: true, error: timeoutError })
    expect(failed.events.filter((event: TraceEvent) => event.kind === "error")).toMatchObject([{ turn_id: failedTurn.id, err: timeoutError }])
    expect(failed.events.filter((event: TraceEvent) => event.kind === "user_message")).toHaveLength(0)
    const traces = await Promise.all(turns.slice(1).map(async (turn) => (await request.get(`/api/trace/${turn.id}`)).json()))
    evidence.humanTraces = traces
    for (const [index, trace] of traces.entries()) {
      expect(trace.turn).toMatchObject({ thread_id: threadID, status: "done", user_text: queued[index], final: completed })
      expect(trace.events.filter((event: TraceEvent) => event.kind === "user_message")).toMatchObject([{ text: queued[index], turn_id: turns[index + 1].id }])
      expect(trace.events.filter((event: TraceEvent) => event.kind === "done")).toHaveLength(1)
    }
    const after = await (await request.get(`/api/schedules/${scheduleID}`)).json()
    evidence.after = after
    expect(after.schedule).toEqual(before.schedule)
    expect(after.runs).toHaveLength(1)
    expect(after.runs[0]).toMatchObject({ turn_id: failedTurn.id, status: "error", summary: timeoutError, unread: true })
    expect((await (await request.get(`/api/threads/${threadID}/followups`)).json()).followups).toEqual([])
    await expect(tray).toHaveCount(0)
    await expect(page.getByTestId("first-byte-timeout-error")).toContainText("The model sent no first byte within 2s.")
    await expect(page.getByTestId("transcript").getByText(completed, { exact: true })).toHaveCount(2)
    await expect(page.getByTestId("status-badge")).toContainText("Waiting")
    expect(attempts.map((row) => row.scheduled)).toEqual([true, true, false, false])
    expect(policyRequests).toBe(2)
    expect(fixtureErrors).toEqual([])
    await page.screenshot({ path: info.outputPath("failed-check-and-completed-human-turns.png"), animations: "disabled" })
    await reloadOpenConversation(page)
    await expect(page.getByTestId("first-byte-timeout-error")).toBeVisible()
    await expect(page.getByTestId("transcript").getByText(completed, { exact: true })).toHaveCount(2)
    const replayed = await (await request.get(`/api/threads/${threadID}/turns`)).json()
    expect(replayed.turns).toEqual(evidence.turns)
    expect((await (await request.get(`/api/trace/${failedTurn.id}`)).json()).events).toEqual(failed.events)
    expect((await (await request.get(`/api/schedules/${scheduleID}`)).json()).schedule).toEqual(before.schedule)
    expect(attempts).toHaveLength(4)
    expect(policyRequests).toBe(2)
    await page.screenshot({ path: info.outputPath("queue-after-error-reloaded-no-duplicates.png"), animations: "disabled" })
  } finally {
    await writeFile(info.outputPath("scheduled-first-byte-FIFO-evidence.json"), JSON.stringify({ ...evidence, attempts, policyRequests, fixtureErrors }, null, 2), { mode: 0o600 })
    try { if (scheduleID) await request.delete(`/api/schedules/${scheduleID}`, { timeout: 5000 }) }
    finally {
      await new Promise<void>((resolve) => {
        endpoint.close(() => resolve())
        endpoint.closeAllConnections()
      })
    }
  }
})
