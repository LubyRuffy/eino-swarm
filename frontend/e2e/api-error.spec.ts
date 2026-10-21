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
    const backoffNotices = [5, 10].map((seconds) => `Waiting ${seconds}s before retrying the model request.`)
    if (responseCase.status === 502) {
      await expect(transcript.getByText(backoffNotices[0], { exact: true })).toBeVisible()
      await expect(transcript).not.toContainText('"wait_ms"')
    }
    await waitForIdle(page)
    await expect(transcript.getByText(responseCase.error, { exact: true })).toBeVisible()
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
    await expect(transcript.getByText(responseCase.error, { exact: true })).toBeVisible()
    await page.getByRole("tab", { name: "Trace", exact: true }).click()
    await expect(page.getByRole("tabpanel").getByText(turn.id, { exact: true })).toBeVisible()
    await expect(page.getByTestId("trace-error")).toHaveText(responseCase.error)
    await page.getByTestId("trace-log-toggle").click()
    await expect(log.getByText(responseCase.error, { exact: true })).toBeVisible()
    await expect(log.getByText(responseCase.notice, { exact: true })).toHaveCount(2)
    if (responseCase.status === 502) {
      for (const notice of backoffNotices) await expect(log.getByText(notice, { exact: true })).toBeVisible()
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
  const endpoint = createServer((incoming, response) => {
    attempts++
    incoming.resume()
    const timer = setTimeout(() => {
      response.writeHead(200, { "Content-Type": "text/event-stream" })
      response.end('data: {"id":"offline","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"The check is complete."},"finish_reason":null}]}\n\ndata: {"id":"offline","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n')
    }, 1200)
    response.once("close", () => clearTimeout(timer))
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
    await writeFile(info.outputPath("first-byte-traces.json"), JSON.stringify({ failed: trace, retried: success, attempts }, null, 2), { mode: 0o600 })
  } finally {
    await new Promise<void>((resolve) => {
      endpoint.close(() => resolve())
      endpoint.closeAllConnections()
    })
  }
})
