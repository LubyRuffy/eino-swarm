import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { createServer } from "node:http"

import { composer, freshConversation, reloadOpenConversation, waitForIdle } from "./session"

const PUBLIC_ERROR = "model API request failed: HTTP 502 Bad Gateway: unexpected end of JSON input"
const RAW_ERROR = "error, status code: 502, status: 502 Bad Gateway, message: unexpected end of JSON input, body: "

type TraceEvent = { thread_id: string; turn_id: string; seq: number; kind: string; err?: string }
type TraceCall = { thread_id: string; turn_id: string; agent_id: string; output_chars: number; err: string }

test("an actual model API failure stays visible in Chat and the same turn Trace after reload", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_MODEL_API_ERROR !== "1", "requires the opt-in keyless loopback model endpoint")
  const attempts: { method?: string; path?: string; hasAuthorization: boolean }[] = []
  const endpoint = createServer((incoming, response) => {
    attempts.push({ method: incoming.method, path: incoming.url, hasAuthorization: Boolean(incoming.headers.authorization) })
    incoming.resume()
    response.writeHead(502, { "Content-Type": "application/json" })
    response.end()
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
    await waitForIdle(page)
    const transcript = page.getByTestId("transcript")
    await expect(transcript.getByText(PUBLIC_ERROR, { exact: true })).toBeVisible()
    await expect(transcript).not.toContainText("a tool call was not valid JSON")
    await expect(page.getByRole("button", { name: "Stop", exact: true })).toHaveCount(0)
    await page.screenshot({ path: info.outputPath("api-error-chat.png"), animations: "disabled" })

    const traceResponse = await request.get(`/api/trace/${turn.id}`)
    expect(traceResponse.ok()).toBe(true)
    const trace = await traceResponse.json()
    expect(trace.turn).toMatchObject({ id: turn.id, thread_id: threadID, status: "error", error: PUBLIC_ERROR })
    for (const event of trace.events as TraceEvent[]) {
      expect(event).toMatchObject({ turn_id: turn.id, thread_id: threadID })
      expect(event.seq).toBeGreaterThan(0)
    }
    expect(trace.events.filter((event: TraceEvent) => event.kind === "model_retry")).toHaveLength(2)
    const errors = trace.events.filter((event: TraceEvent) => event.kind === "error")
    expect(errors).toHaveLength(1)
    expect(errors[0]).toMatchObject({ err: PUBLIC_ERROR, turn_id: turn.id, thread_id: threadID })
    expect(errors[0].seq).toBeGreaterThan(0)
    expect(trace.llm_calls).toHaveLength(3)
    for (const call of trace.llm_calls as TraceCall[]) {
      expect(call).toMatchObject({ thread_id: threadID, turn_id: turn.id, agent_id: "manager", output_chars: 0, err: RAW_ERROR })
    }
    expect(attempts).toEqual(Array.from({ length: 3 }, () => ({ method: "POST", path: "/chat/completions", hasAuthorization: false })))
    const turns = await (await request.get(`/api/threads/${threadID}/turns`)).json()
    expect(turns.turns.map((row: { id: string }) => row.id)).toEqual([turn.id])

    await page.getByRole("tab", { name: "Trace", exact: true }).click()
    await expect(page.getByRole("tabpanel").getByText(turn.id, { exact: true })).toBeVisible()
    await expect(page.getByTestId("trace-status")).toHaveText("failed")
    await expect(page.getByTestId("trace-error")).toHaveText(PUBLIC_ERROR)
    await page.getByTestId("trace-log-toggle").click()
    const log = page.getByTestId("trace-log")
    await expect(log.getByText(PUBLIC_ERROR, { exact: true })).toBeVisible()
    await expect(log.getByText("Retrying after a model error.", { exact: true })).toHaveCount(2)
    await page.screenshot({ path: info.outputPath("api-error-trace.png"), animations: "disabled" })

    await reloadOpenConversation(page)
    await waitForIdle(page)
    await expect(transcript.getByText(PUBLIC_ERROR, { exact: true })).toBeVisible()
    await page.getByRole("tab", { name: "Trace", exact: true }).click()
    await expect(page.getByRole("tabpanel").getByText(turn.id, { exact: true })).toBeVisible()
    await expect(page.getByTestId("trace-error")).toHaveText(PUBLIC_ERROR)
    await page.getByTestId("trace-log-toggle").click()
    await expect(log.getByText(PUBLIC_ERROR, { exact: true })).toBeVisible()
    await expect(log.getByText("Retrying after a model error.", { exact: true })).toHaveCount(2)
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
