import { expect, test as base, type APIRequestContext, type Page } from "@playwright/test"
import { createServer } from "node:http"
import { writeFile } from "node:fs/promises"

const failure = "The fixture rejected this request."
const originalText = "Inspect the available workspace and report the findings."
const result = "The check is complete."

type ModelRequest = {
  messages: { role: string; content?: string }[]
  stream?: boolean
  tools?: unknown[]
  tool_choice?: string
}
type Loopback = { recover: () => void; hold: () => void; calls: ModelRequest[]; faults: string[] }
type StoredTurn = { id: string; status: string; user_text: string }

const test = base.extend<{ loopback: Loopback }>({
  loopback: async ({ request }, use) => {
    let rejecting = true, held = false
    const calls: ModelRequest[] = [], faults: string[] = []
    const endpoint = createServer((incoming, response) => {
      let body = ""
      incoming.setEncoding("utf8")
      incoming.on("data", (part: string) => { body += part })
      incoming.on("end", () => {
        try {
          const input = JSON.parse(body) as ModelRequest
          if (input.messages[0]?.role === "system" && input.messages[0].content?.startsWith("## Schedule answer claims")) {
            if (input.stream || input.tools?.length || input.tool_choice !== "none" ||
              input.messages.length !== 2 || input.messages[1].role !== "user" ||
              JSON.stringify(JSON.parse(input.messages[1].content ?? "{}").messages) !== JSON.stringify([result])) {
              throw new Error("Unexpected schedule policy request or candidate")
            }
            response.writeHead(200, { "Content-Type": "application/json" })
            response.end(JSON.stringify({ choices: [{ index: 0, message: { role: "assistant", content: '{"claims":[]}' }, finish_reason: "stop" }] }))
            return
          }
          calls.push(input)
          if (held) return
          if (rejecting) {
            response.writeHead(401, { "Content-Type": "application/json" })
            response.end(JSON.stringify({ error: { message: failure, type: "invalid_request_error" } }))
            return
          }
          response.writeHead(200, { "Content-Type": "text/event-stream" })
          response.end(`data: ${JSON.stringify({ id: "retry-fixture", object: "chat.completion.chunk", choices: [{ index: 0, delta: { role: "assistant", content: result }, finish_reason: null }] })}\n\ndata: ${JSON.stringify({ id: "retry-fixture", object: "chat.completion.chunk", choices: [{ index: 0, delta: {}, finish_reason: "stop" }] })}\n\ndata: [DONE]\n\n`)
        } catch (error) {
          faults.push(String(error))
          response.writeHead(400, { "Content-Type": "application/json" })
          response.end(JSON.stringify({ error: { message: String(error), type: "invalid_request_error" } }))
        }
      })
    })
    await new Promise<void>((resolve) => endpoint.listen(0, "127.0.0.1", resolve))
    try {
      const address = endpoint.address()
      if (!address || typeof address === "string") throw new Error("Retry fixture did not listen")
      const { settings } = await (await request.get("/api/settings")).json()
      expect((await request.put("/api/settings", { data: {
        models: { default: "retry-fixture", providers: [{ id: "retry-fixture", label: "Offline retry regression",
          base_url: `http://127.0.0.1:${address.port}`, model: "offline", api_key: "", context_window: 128000 }] },
        swarm: { ...settings.swarm, auto_title: false, composer_predictions: false },
        memory: { ...settings.memory, auto_review: false },
      } })).ok()).toBe(true)
      await use({ recover: () => { rejecting = false }, hold: () => { held = true }, calls, faults })
      expect(faults).toEqual([])
    } finally {
      await new Promise<void>((resolve, reject) => {
        endpoint.close((error) => error ? reject(error) : resolve())
        endpoint.closeAllConnections()
      })
    }
  },
})

test.skip(process.env.ZWAI_E2E_MODEL_API_ERROR !== "1", "requires the opt-in keyless loopback model endpoint")

async function turns(request: APIRequestContext, id: string): Promise<StoredTurn[]> {
  const response = await request.get(`/api/threads/${id}/turns`)
  expect(response.ok()).toBe(true)
  return (await response.json()).turns
}

async function reopenFailure(page: Page, id: string) {
  await page.reload()
  await page.getByTestId("dest-chats").click()
  await page.locator(`[data-testid="thread-row"][data-id="${id}"]`).getByTestId("row-label").click()
}

/** The source failure and its Retry cross the real HTTP/model/store boundary.
 *  Retry continues that same turn from the context it had when the call failed. */
async function openFailure(page: Page, request: APIRequestContext, loopback: Loopback, locale = "en", cancelled = false) {
  await request.put("/api/settings", { data: { ui: { locale } } })
  const created = await request.post("/api/threads", { data: { title: `Retry terminal failure ${locale}` } })
  expect(created.ok()).toBe(true)
  const id = (await created.json()).thread.id as string
  if (cancelled) loopback.hold()
  const started = await request.post(`/api/threads/${id}/steer`, { data: { text: originalText } })
  expect(started.ok()).toBe(true)
  const original = (await started.json()).turn as StoredTurn
  if (cancelled) {
    await expect.poll(() => loopback.calls.length).toBeGreaterThan(0)
    expect((await request.post(`/api/threads/${id}/interrupt`)).ok()).toBe(true)
  }
  await expect.poll(async () => (await turns(request, id)).find((turn) => turn.id === original.id)?.status,
    { timeout: 60_000 }).toBe(cancelled ? "cancelled" : "error")
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.locator(`[data-testid="thread-row"][data-id="${id}"]`).getByTestId("row-label").click()
  return { id, original }
}

test.afterEach(async ({ request }) => {
  await request.put("/api/settings", { data: { ui: { locale: "system" } } })
})

function observeWrites(page: Page, id: string) {
  const writes: string[] = []
  page.on("request", (incoming) => {
    const path = new URL(incoming.url()).pathname
    if (["POST", "PATCH", "DELETE"].includes(incoming.method()) &&
      (path === `/api/threads/${id}` || path === `/api/threads/${id}/turns` ||
        path === `/api/threads/${id}/retry` ||
        path === `/api/threads/${id}/steer` || path === `/api/threads/${id}/interrupt` ||
        path.startsWith(`/api/threads/${id}/followups`) || path.startsWith("/api/schedules"))) {
      writes.push(`${incoming.method()} ${path}`)
    }
  })
  return writes
}

for (const locale of ["en", "zh"] as const) {
  test(`Retry continues the failed turn once after reload (${locale})`, async ({ page, request, loopback }, info) => {
    const { id, original } = await openFailure(page, request, loopback, locale)
    const transcript = page.getByTestId("transcript")
    const terminal = transcript.locator("p.stream-text").filter({ hasText: failure })
    const retry = transcript.getByTestId("transcript-error-retry")
    const editor = transcript.getByTestId("user-message-editor")
    await expect(terminal).toBeVisible()
    await expect(retry).toHaveText(locale === "zh" ? "重试" : "Retry")
    await reopenFailure(page, id)
    await expect(retry).toBeEnabled()
    const sourceTrace = await (await request.get(`/api/trace/${original.id}`)).json()
    const sourceSeq = sourceTrace.events.find((event: { kind: string }) => event.kind === "user_message")?.seq
    expect(sourceSeq).toBeGreaterThan(0)
    const before = await turns(request, id)
    const beforeCalls = loopback.calls.length
    const submissions: Record<string, unknown>[] = []
    const writes = observeWrites(page, id)
    let release!: () => void
    const held = new Promise<void>((resolve) => { release = resolve })
    await page.route(`**/api/threads/${id}/retry`, async (route) => {
      submissions.push(route.request().postDataJSON())
      await held
      await route.continue()
    })
    loopback.recover()
    try {
      await retry.dblclick()
      await expect.poll(() => submissions.length).toBe(1)
      expect(submissions).toEqual([{ turn_id: original.id }])
      await expect(editor).toHaveCount(0)
      expect(writes).toEqual([`POST /api/threads/${id}/retry`])
      expect(await turns(request, id)).toEqual(before)
      expect(loopback.calls).toHaveLength(beforeCalls)
      await writeFile(info.outputPath(`terminal-retry-pending-${locale}.png`), await page.screenshot(), { mode: 0o600 })
      const accepted = page.waitForResponse((response) => response.request().method() === "POST" &&
        new URL(response.url()).pathname === `/api/threads/${id}/retry`)
      release()
      expect((await accepted).ok()).toBe(true)
      await expect.poll(async () => (await turns(request, id)).find((turn) => turn.id === original.id)?.status,
        { timeout: 60_000 }).toBe("done")
      const stored = await turns(request, id)
      expect(stored).toHaveLength(1)
      expect(stored[0]).toMatchObject({ id: original.id, user_text: originalText, status: "done" })
      expect(loopback.calls.at(-1)?.messages.filter((message) => message.role === "user")).toEqual([
        expect.objectContaining({ role: "user", content: originalText }),
      ])
      const after = await (await request.get(`/api/trace/${original.id}`)).json()
      expect(after.events.some((event: { kind: string }) => event.kind === "rewound")).toBe(false)
      expect(after.events.some((event: { kind: string }) => event.kind === "error")).toBe(true)
      expect(after.events.find((event: { kind: string }) => event.kind === "user_message")?.seq).toBe(sourceSeq)
      expect(after.events.some((event: { kind: string }) => event.kind === "failure_continued")).toBe(true)
      await expect(transcript.getByTestId("user-message")).toHaveCount(1)
      await expect(transcript.getByTestId("user-message")).toHaveText(originalText)
      await expect(transcript.getByText(locale === "zh" ? "从失败处的上下文继续。" : "Continuing from the failed step.")).toBeVisible()
      await expect(editor).toHaveCount(0)
      await expect(terminal).toHaveCount(0)
      await expect(retry).toHaveCount(0)
      expect(submissions).toHaveLength(1)
      await reopenFailure(page, id)
      await expect(transcript.getByTestId("user-message")).toHaveText(originalText)
      await expect(retry).toHaveCount(0)
    } finally {
      release()
    }
  })
}

test("Retry continues the failed turn while keeping a separate unsent composer draft", async ({ page, request, loopback }) => {
  const { id, original } = await openFailure(page, request, loopback)
  const draft = "This separate follow-up has not been submitted."
  const composer = page.getByTestId("composer-input")
  await composer.fill(draft)
  const submissions: Record<string, unknown>[] = []
  await page.route(`**/api/threads/${id}/retry`, async (route) => {
    submissions.push(route.request().postDataJSON())
    await route.continue()
  })
  loopback.recover()
  const transcript = page.getByTestId("transcript")
  await transcript.getByTestId("transcript-error-retry").click()
  await expect.poll(async () => (await turns(request, id)).find((turn) => turn.id === original.id)?.status).toBe("done")
  expect(submissions).toEqual([{ turn_id: original.id }])
  await expect(composer).toHaveValue(draft)
  await expect(transcript.getByTestId("user-message-editor")).toHaveCount(0)
  expect((await turns(request, id)).map((turn) => turn.id)).toEqual([original.id])
})

test("a refused Retry preserves the original failure and can be clicked again", async ({ page, request, loopback }) => {
  const { id, original } = await openFailure(page, request, loopback)
  const transcript = page.getByTestId("transcript")
  const retry = transcript.getByTestId("transcript-error-retry")
  const before = await turns(request, id)
  const sourceTrace = await (await request.get(`/api/trace/${original.id}`)).json()
  const beforeCalls = loopback.calls.length
  loopback.recover()
  let attempts = 0
  await page.route(`**/api/threads/${id}/retry`, async (route) => {
    expect(route.request().postDataJSON()).toEqual({ turn_id: original.id })
    attempts++
    if (attempts === 1) {
      await route.fulfill({ status: 503, json: { error: "Retry submission is temporarily unavailable." } })
    } else await route.continue()
  })
  await retry.click()
  await expect(page.getByText("Retry submission is temporarily unavailable.", { exact: true })).toBeVisible()
  await expect(retry).toBeEnabled()
  await expect(transcript.locator("p.stream-text").filter({ hasText: failure })).toBeVisible()
  await expect(transcript.getByTestId("user-message-editor")).toHaveCount(0)
  expect(await turns(request, id)).toEqual(before)
  expect((await (await request.get(`/api/trace/${original.id}`)).json()).events).toEqual(sourceTrace.events)
  expect(loopback.calls).toHaveLength(beforeCalls)
  await retry.click()
  await expect.poll(() => attempts).toBe(2)
  await expect(retry).toHaveCount(0)
  await expect.poll(async () => (await turns(request, id)).find((turn) => turn.id === original.id)?.status).toBe("done")
  expect((await turns(request, id)).map((turn) => turn.user_text)).toEqual([originalText])
})

test("Retry continues the latest failed turn and leaves the earlier turn in place", async ({ page, request, loopback }, info) => {
  const { id, original } = await openFailure(page, request, loopback)
  // The earlier failure stays above the viewport. Retry continues the latest
  // failed turn and does not follow historical retry_turn_id.
  const legacyText = Array.from({ length: 120 }, (_, line) => `Legacy fixture retry cue, line ${line + 1}.`).join("\n")
  const created = await request.post(`/api/threads/${id}/turns`, { data: { text: legacyText } })
  expect(created.ok()).toBe(true)
  const legacy = (await created.json()).turn as StoredTurn
  await expect.poll(async () => (await turns(request, id)).find((turn) => turn.id === legacy.id)?.status,
    { timeout: 60_000 }).toBe("error")
  const before = await turns(request, id)
  const sourceTrace = await (await request.get(`/api/trace/${original.id}`)).json()
  const sourceSeq = sourceTrace.events.find((event: { kind: string }) => event.kind === "user_message")?.seq
  expect(before).toEqual(expect.arrayContaining([
    expect.objectContaining({ id: original.id, user_text: originalText, status: "error" }),
    expect.objectContaining({ id: legacy.id, user_text: legacyText, status: "error" }),
  ]))
  const beforeCalls = loopback.calls.length
  const writes = observeWrites(page, id)
  const submissions: Record<string, unknown>[] = []
  let release!: () => void
  const held = new Promise<void>((resolve) => { release = resolve })
  let pendingMetadata = 0
  await page.route(`**/api/threads/${id}/turns`, async (route) => {
    if (route.request().method() === "GET") {
      pendingMetadata++
      await held
      // Legacy builds persisted this ancestry. Current writes no longer create
      // continuation turns, so simulate that stored relationship on the read.
      const response = await route.fetch()
      expect(response.ok()).toBe(true)
      const body = await response.json() as { turns: StoredTurn[] }
      await route.fulfill({ response, json: { ...body, turns: body.turns.map((turn) =>
        turn.id === legacy.id ? { ...turn, retry_turn_id: original.id } : turn,
      ) } })
      return
    }
    if (route.request().method() === "POST") {
      submissions.push({ rewind: route.request().postDataJSON() })
      await route.fulfill({ status: 500, json: { error: "Retry must not rewind." } })
      return
    }
    await route.continue()
  })
  await page.route(`**/api/threads/${id}/retry`, async (route) => {
    submissions.push(route.request().postDataJSON())
    await route.continue()
  })
  try {
    await reopenFailure(page, id)
    const transcript = page.getByTestId("transcript")
    const retry = transcript.getByTestId("transcript-error-retry")
    await expect.poll(() => pendingMetadata).toBeGreaterThan(0)
    await expect(transcript.locator("p.stream-text").filter({ hasText: failure })).toHaveCount(2)
    const originalBubble = transcript.getByTestId("user-message").filter({ hasText: originalText })
    await expect(originalBubble).toBeAttached()
    await expect(originalBubble).not.toBeInViewport()
    await expect(retry).toHaveCount(0)
    await expect(transcript.getByTestId("user-message-editor")).toHaveCount(0)
    expect(writes).toEqual([])
    expect(await turns(request, id)).toEqual(before)
    expect(loopback.calls).toHaveLength(beforeCalls)
    await writeFile(info.outputPath("terminal-retry-ancestry-pending.png"), await page.screenshot(), { mode: 0o600 })
    loopback.recover()
    release()
    await expect(retry).toBeEnabled()
    await retry.click()
    await expect.poll(() => submissions.length).toBe(1)
    expect(submissions).toEqual([{ turn_id: legacy.id }])
    expect(writes).toEqual([`POST /api/threads/${id}/retry`])
    await expect(transcript.getByTestId("user-message-editor")).toHaveCount(0)
    await expect.poll(async () => (await turns(request, id)).find((turn) => turn.id === legacy.id)?.status,
      { timeout: 60_000 }).toBe("done")
    const stored = await turns(request, id)
    expect(stored).toHaveLength(2)
    expect(stored.find((turn) => turn.id === original.id)).toMatchObject({ user_text: originalText, status: "error" })
    expect(stored.find((turn) => turn.id === legacy.id)).toMatchObject({ user_text: legacyText, status: "done" })
    expect(loopback.calls.at(-1)?.messages.filter((message) => message.role === "user").map((message) => message.content)).toEqual([
      originalText, legacyText,
    ])
    const originalTrace = await (await request.get(`/api/trace/${original.id}`)).json()
    expect(originalTrace.events.find((event: { kind: string }) => event.kind === "user_message")?.seq).toBe(sourceSeq)
    expect(originalTrace.events.some((event: { kind: string }) => event.kind === "rewound")).toBe(false)
    const continued = await (await request.get(`/api/trace/${legacy.id}`)).json()
    expect(continued.events.some((event: { kind: string }) => event.kind === "failure_continued")).toBe(true)
    expect(continued.events.some((event: { kind: string }) => event.kind === "rewound")).toBe(false)
    await expect(transcript.getByTestId("user-message")).toHaveCount(2)
    await expect(transcript.locator("p.stream-text").filter({ hasText: failure })).toHaveCount(1)
    await expect(transcript.getByText("Continuing from the failed step.")).toBeVisible()
    await writeFile(info.outputPath("terminal-retry-ancestry-continued.png"), await page.screenshot(), { mode: 0o600 })
  } finally {
    release()
  }
})

test("a human interruption does not offer failure Retry", async ({ page, request, loopback }) => {
  await openFailure(page, request, loopback, "en", true)
  await expect(page.getByTestId("transcript").getByTestId("turn-boundary")).toBeVisible()
  await expect(page.getByTestId("transcript-error-retry")).toHaveCount(0)
})
