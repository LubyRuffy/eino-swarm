import { expect, test, type APIRequestContext, type Page } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { createServer, type ServerResponse } from "node:http"
import { composer, freshConversation, reloadOpenConversation } from "./session"

const deniedText = "Keep working in this conversation. Do not suspend it or arm another timed wake."
const allowedText = "You may now suspend this conversation and arm its next timed wake."
const waitingClaim = "I have armed the next check and will wait for it."
const refusedWait = "The next check was not armed; the current waiting prohibition remains in effect."
type Event = { seq: number; kind: string; text: string; tool_call_id?: string }
type Trace = { turn: { id: string; status: string }; events: Event[] }
type Message = { role: string; content?: string; tool_calls?: { function: { name: string } }[] }
type HumanSource = { seq: number; kind: string; text: string }

// This fixture supplies decisions from known human packets. The manager always
// attempts to wait, then reports the actual tool result rather than inventing success.
async function startWakePolicyFixture() {
  const errors: string[] = []
  const sources: HumanSource[] = []
  let nextCall = 0
  const reply = (response: ServerResponse, stream: boolean, text: string, call?: { name: string; args: object }) => {
    const message = call
      ? { role: "assistant", tool_calls: [{ id: `wake-policy-fixture-${++nextCall}`, type: "function", function: { name: call.name, arguments: JSON.stringify(call.args) } }] }
      : { role: "assistant", content: text }
    if (!stream) {
      response.writeHead(200, { "Content-Type": "application/json" })
      response.end(JSON.stringify({ id: "fixture", object: "chat.completion", choices: [{ index: 0, message, finish_reason: call ? "tool_calls" : "stop" }] }))
      return
    }
    const delta = call ? { ...message, tool_calls: message.tool_calls?.map((value) => ({ index: 0, ...value })) } : message
    response.writeHead(200, { "Content-Type": "text/event-stream" })
    response.end(`data: ${JSON.stringify({ id: "fixture", object: "chat.completion.chunk", choices: [{ index: 0, delta, finish_reason: null }] })}\n\ndata: ${JSON.stringify({ id: "fixture", object: "chat.completion.chunk", choices: [{ index: 0, delta: {}, finish_reason: call ? "tool_calls" : "stop" }] })}\n\ndata: [DONE]\n\n`)
  }
  const endpoint = createServer((incoming, response) => {
    let body = ""
    incoming.setEncoding("utf8")
    incoming.on("data", (part: string) => { body += part })
    incoming.on("end", () => {
      try {
        if (incoming.headers.authorization) throw new Error("The fixture must be keyless")
        const input = JSON.parse(body) as { messages: Message[]; tools?: { function: { name: string } }[]; stream?: boolean }
        if (input.messages[0]?.role === "system" && input.messages[0].content?.startsWith("## Schedule answer claims")) {
          if (input.stream || input.tools?.length || input.messages.length !== 2) throw new Error("Answer classification must remain a separate tool-free request")
          const candidate = JSON.parse(input.messages[1].content ?? "") as { messages: string[] }
          reply(response, false, JSON.stringify({ claims: candidate.messages.includes(waitingClaim) ? [{ stance: "affirmed", temporal: "current", attribution: "assistant", quote: waitingClaim, scope: "thread", new: true }] : [] }))
          return
        }
        if (input.messages[0]?.content?.includes("## Thread wake authorization classification")) {
          const source = JSON.parse(input.messages[1]?.content ?? "") as HumanSource
          if (input.messages.length !== 2 || source.seq <= 0 || !["user_message", "steer"].includes(source.kind)) throw new Error("Classification must contain one original human source")
          if (![deniedText, allowedText].includes(source.text)) throw new Error("Classification received generated or unexpected text")
          sources.push(source)
          reply(response, Boolean(input.stream), JSON.stringify({ decision: source.text === allowedText ? "allow" : "deny", quote: source.text }))
          return
        }
        if (!input.tools?.some((value) => value.function.name === "schedule_wake")) {
          reply(response, Boolean(input.stream), "Conversation check")
          return
        }
        const user = input.messages.findLastIndex((message) => message.role === "user")
        const scheduled = input.messages[user]?.content?.startsWith("This turn is a scheduled check.")
        const name = scheduled ? "report_schedule" : "schedule_wake"
        const called = input.messages.slice(user + 1).some((message) => message.tool_calls?.some((call) => call.function.name === name))
        const result = input.messages.slice(user + 1).findLast((message) => message.role === "tool")
        const accepted = result ? (JSON.parse(result.content ?? "{}") as { ok?: boolean }).ok === true : false
        reply(response, Boolean(input.stream), accepted ? waitingClaim : refusedWait, called ? undefined : {
          name,
          args: scheduled ? { findings: "The check ran.", keep: true, next_in_s: 3600 } : { prompt: "Inspect the current work state.", delay_s: 3600 },
        })
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
  if (!address || typeof address === "string") throw new Error("Fixture did not listen")
  return {
    url: `http://127.0.0.1:${address.port}`, errors, sources,
    close: () => new Promise<void>((resolve, reject) => {
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    }),
  }
}

async function configureFixture(request: APIRequestContext, url: string) {
  const { settings } = await (await request.get("/api/settings")).json()
  expect((await request.put("/api/settings", { data: {
    models: { default: "fixture-provider", providers: [{ id: "fixture-provider", label: "Offline wake policy regression", base_url: url, model: "fixture-model", api_key: "", context_window: 128000 }] },
    swarm: { ...settings.swarm, auto_title: false, schedule_min_interval_seconds: 1 },
    memory: { ...settings.memory, auto_review: false },
  } })).ok()).toBe(true)
}

async function sendHuman(page: Page, request: APIRequestContext, threadID: string, text: string) {
  const { turns: before } = await (await request.get(`/api/threads/${threadID}/turns`)).json()
  const previous = new Set((before as { id: string }[]).map((turn) => turn.id))
  await composer(page).fill(text)
  await composer(page).press("Enter")
  let trace: Trace | undefined
  await expect.poll(async () => {
    const { turns } = await (await request.get(`/api/threads/${threadID}/turns`)).json()
    const turn = (turns as { id: string }[]).find((value) => !previous.has(value.id))
    if (!turn) return "pending"
    trace = await (await request.get(`/api/trace/${turn.id}`)).json()
    return trace?.turn.status
  }).toBe("done")
  return trace!
}

function receipt(trace: Trace, name: string) {
  const calls = trace.events.filter((event) => event.kind === "tool_call" && event.text.startsWith(`${name}(`))
  expect(calls).toHaveLength(1)
  const results = trace.events.filter((event) => event.kind === "tool_result" && event.tool_call_id === calls[0].tool_call_id)
  expect(results).toHaveLength(1)
  return JSON.parse(results[0].text) as { ok: boolean; code?: string; source_seq?: number; source_quote?: string }
}

async function expectNoWait(page: Page) {
  await expect(page.getByTestId("status-badge")).toContainText("Idle")
  await expect(page.getByTestId("schedule-banner")).toHaveCount(0)
  await expect(page.locator('[data-testid="thread-row"][aria-current="true"]').getByTestId("wait-mark")).toHaveCount(0)
}

test("a human prohibition prevents a wait card and the manager reports the refused wait", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_HUMAN_WAKE_POLICY !== "1", "requires the keyless wake policy fixture")
  const fixture = await startWakePolicyFixture()
  let scheduleID = ""
  try {
    await configureFixture(request, fixture.url)
    await freshConversation(page)
    const threadID = (await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id"))!
    const denied = await sendHuman(page, request, threadID, deniedText)
    const source = denied.events.find((event) => event.kind === "user_message")!
    expect(receipt(denied, "schedule_wake")).toMatchObject({ ok: false, code: "thread_wake_prohibited", source_seq: source.seq, source_quote: deniedText })
    expect(denied.events.some((event) => event.kind === "schedule")).toBe(false)
    await expect(page.getByTestId("transcript")).toContainText(refusedWait)
    await expect(page.getByTestId("transcript")).not.toContainText(waitingClaim)
    await expect(page.getByTestId("schedule-notice")).toHaveCount(0)
    await expectNoWait(page)
    const { schedules } = await (await request.get(`/api/threads/${threadID}/schedules`)).json()
    expect(schedules).toEqual([])
    await reloadOpenConversation(page)
    await expect(page.getByTestId("transcript")).not.toContainText(waitingClaim)
    await expectNoWait(page)
    await page.getByRole("tab", { name: "Trace", exact: true }).click()
    await page.getByTestId("trace-log-toggle").click()
    await expect(page.getByTestId("trace-log")).toContainText("schedule_wake")
    await page.getByRole("button", { name: "App menu" }).click()
    const developerView = page.getByRole("menuitem", { name: "Switch to developer view", exact: true })
    if (await developerView.isVisible()) await developerView.click()
    else await page.getByRole("button", { name: "Close menu", exact: true }).click()
    await page.getByTestId("transcript").getByRole("button", { name: /^schedule_wake/ }).last().click()
    await expect(page.getByTestId("transcript")).toContainText("thread_wake_prohibited")
    await expect(page.getByTestId("transcript")).toContainText(`"source_seq":${source.seq}`)
    await page.screenshot({ path: info.outputPath("prohibited-wake-no-card.png"), animations: "disabled" })
    const allowed = await sendHuman(page, request, threadID, allowedText)
    expect(receipt(allowed, "schedule_wake").ok).toBe(true)
    await expect(page.getByTestId("status-badge")).toContainText("Waiting")
    await expect(page.getByTestId("schedule-banner")).toBeVisible()
    await expect(page.getByTestId("schedule-notice")).toHaveCount(1)
    const { schedules: armed } = await (await request.get(`/api/threads/${threadID}/schedules`)).json()
    expect(armed).toHaveLength(1)
    scheduleID = armed[0].id
    expect(fixture.sources.map((value) => value.text)).toEqual([deniedText, allowedText])
    expect(fixture.errors).toEqual([])
    await page.screenshot({ path: info.outputPath("human-allowed-wake-card.png"), animations: "disabled" })
    await writeFile(info.outputPath("human-wake-policy-evidence.json"), JSON.stringify({ denied, allowed, sources: fixture.sources }, null, 2), { mode: 0o600 })
  } finally {
    try { if (scheduleID) await request.delete(`/api/schedules/${scheduleID}`, { timeout: 5000 }) }
    finally { await fixture.close() }
  }
})

test("a prohibited scheduled recadence leaves the consumed one-shot without a new wait card", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_HUMAN_WAKE_POLICY !== "1", "requires the keyless wake policy fixture")
  const fixture = await startWakePolicyFixture()
  let scheduleID = ""
  try {
    await configureFixture(request, fixture.url)
    await freshConversation(page)
    const threadID = (await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id"))!
    const created = await request.post("/api/schedules", { data: { kind: "thread", thread_id: threadID, prompt: "Inspect the current work state.", delay_s: 3600 } })
    expect(created.status()).toBe(201)
    const { schedule } = await created.json()
    scheduleID = schedule.id
    const denied = await sendHuman(page, request, threadID, deniedText)
    expect(receipt(denied, "schedule_wake").ok).toBe(false)
    const { schedule: unchanged } = await (await request.get(`/api/schedules/${scheduleID}`)).json()
    expect(unchanged).toEqual(schedule)
    const fired = await request.post(`/api/schedules/${scheduleID}/run`)
    expect(fired.ok()).toBe(true)
    const { turn } = await fired.json()
    let checked: Trace | undefined
    await expect.poll(async () => {
      checked = await (await request.get(`/api/trace/${turn.id}`)).json()
      return checked?.turn.status
    }).toBe("done")
    const source = denied.events.find((event) => event.kind === "user_message")!
    expect(receipt(checked!, "report_schedule")).toMatchObject({ ok: false, code: "thread_wake_prohibited", source_seq: source.seq, source_quote: deniedText })
    expect(checked!.events.some((event) => event.kind === "schedule")).toBe(false)
    const { schedule: consumed } = await (await request.get(`/api/schedules/${scheduleID}`)).json()
    expect(consumed).toMatchObject({ id: scheduleID, status: "done", delay_s: 3600, every_s: 0 })
    await reloadOpenConversation(page)
    await expect(page.getByTestId("transcript")).not.toContainText(waitingClaim)
    await expectNoWait(page)
    await expect(page.getByTestId("schedule-notice")).toHaveCount(2)
    expect(fixture.errors).toEqual([])
    await page.screenshot({ path: info.outputPath("prohibited-recadence-no-new-card.png"), animations: "disabled" })
    await writeFile(info.outputPath("human-recadence-evidence.json"), JSON.stringify({ denied, checked, schedule, consumed, sources: fixture.sources }, null, 2), { mode: 0o600 })
  } finally {
    try { if (scheduleID) await request.delete(`/api/schedules/${scheduleID}`, { timeout: 5000 }) }
    finally { await fixture.close() }
  }
})
