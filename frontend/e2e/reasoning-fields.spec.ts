import { expect, test } from "@playwright/test"
import { mkdir, writeFile } from "node:fs/promises"
import { createServer, type ServerResponse } from "node:http"

import { composer, freshConversation, reloadOpenConversation, statusBadge, waitForIdle } from "./session"

type TraceEvent = { thread_id: string; turn_id: string; agent_id?: string; message_id?: string; seq: number; kind: string; text?: string }
type CommentaryEvent = TraceEvent & { tool_call_id?: string }
type FixtureRequest = { method?: string; path?: string; hasAuthorization: boolean; body: Record<string, unknown> }
type PolicyInput = { messages?: { role?: string; content?: string }[]; stream?: boolean; tools?: unknown[] }

for (const field of ["reasoning_content", "reasoning"] as const) {
  test(`${field} streams before the answer and survives same-turn Trace replay`, async ({ page, request }, info) => {
    test.skip(process.env.ZWAI_E2E_REASONING_FIELDS !== "1", "requires the keyless loopback reasoning endpoint")
    const firstThought = "Inspecting the supplied inputs. "
    const secondThought = "The requested checks are complete."
    const thought = firstThought + secondThought
    const answer = "The assigned inputs passed verification."
    const requests: FixtureRequest[] = []
    const attempts: FixtureRequest[] = []
    const policyAttempts: { request: FixtureRequest; result: { claims: never[] } }[] = []
    const errors: string[] = []
    let stream: ServerResponse | undefined
    let turnID = ""
    const chunk = (delta: object, finish: string | null = null) => {
      if (!stream) throw new Error("The model stream has not started")
      stream.write(`data: ${JSON.stringify({ id: "reasoning-fixture", object: "chat.completion.chunk", choices: [{ index: 0, delta, finish_reason: finish }] })}\n\n`)
    }
    const endpoint = createServer(async (incoming, response) => {
      try {
        const chunks: Buffer[] = []
        for await (const part of incoming) chunks.push(Buffer.from(part))
        const body = JSON.parse(Buffer.concat(chunks).toString()) as Record<string, unknown>
        const observed = { method: incoming.method, path: incoming.url, hasAuthorization: Boolean(incoming.headers.authorization), body }
        requests.push(observed)
        if (observed.method !== "POST" || observed.path !== "/chat/completions" || observed.hasAuthorization) {
          throw new Error("The reasoning fixture accepts only keyless Chat Completions POST requests")
        }
        const input = body as PolicyInput
        // Match the separate read-only protocol used by schedule-answer-fixture;
        // the scripted assistant answer is known data, not human-task wording.
        if (input.messages?.[0]?.role === "system" && input.messages[0].content?.split("\n", 1)[0] === "## Schedule answer claims") {
          if (input.stream || input.tools?.length || input.messages.length !== 2 || input.messages[1].role !== "user") {
            throw new Error("Claim policy must remain a separate read-only request")
          }
          const candidate = JSON.parse(input.messages[1].content ?? "") as { messages?: unknown; now?: unknown }
          if (Object.keys(candidate).sort().join(",") !== "messages,now" ||
              !Array.isArray(candidate.messages) || candidate.messages.length !== 1 || candidate.messages[0] !== answer ||
              typeof candidate.now !== "string" || !Number.isFinite(Date.parse(candidate.now))) {
            throw new Error("Unknown or malformed schedule-answer candidate")
          }
          const result = { claims: [] as never[] }
          policyAttempts.push({ request: observed, result })
          if (policyAttempts.length !== 1) throw new Error("Only one classification may inspect the scripted answer")
          response.writeHead(200, { "Content-Type": "application/json" })
          response.end(JSON.stringify({ choices: [{ index: 0, message: { role: "assistant", content: JSON.stringify(result) }, finish_reason: "stop" }] }))
          return
        }
        if (input.stream !== true || input.messages?.[0]?.role !== "system") throw new Error("Unknown reasoning fixture protocol")
        attempts.push(observed)
        if (attempts.length !== 1) throw new Error("Only the explicit conversation turn may request the fixture")
        stream = response
        response.writeHead(200, { "Content-Type": "text/event-stream" })
        // Each later frame waits for the browser's assertion. Buffered model
        // output or an answer-only UI cannot accidentally pass these checks.
        chunk({ role: "assistant", [field]: firstThought })
      } catch (error) {
        errors.push(String(error))
        if (!response.headersSent) response.writeHead(400, { "Content-Type": "application/json" })
        response.end(JSON.stringify({ error: { message: String(error), type: "invalid_request_error" } }))
      }
    })
    await new Promise<void>((resolve, reject) => {
      endpoint.once("error", reject)
      endpoint.listen(0, "127.0.0.1", () => { endpoint.off("error", reject); resolve() })
    })
    try {
      const address = endpoint.address()
      if (!address || typeof address === "string") throw new Error("The reasoning fixture did not listen")
      const { settings } = await (await request.get("/api/settings")).json()
      expect((await request.put("/api/settings", { data: {
        models: { default: "reasoning-fixture", providers: [{ id: "reasoning-fixture", label: "Offline reasoning regression", base_url: `http://127.0.0.1:${address.port}`, model: "fixture-model", api_key: "", api_mode: "chat_completions" }] },
        swarm: { ...settings.swarm, auto_title: false, composer_predictions: false },
        memory: { ...settings.memory, auto_review: false },
        ui: { ...settings.ui, transcript_mode: "developer" },
      } })).ok()).toBe(true)
      await freshConversation(page)
      const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
      expect(threadID).toBeTruthy()
      const accepted = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === `/api/threads/${threadID}/steer`)
      await composer(page).fill("Perform the requested check")
      await composer(page).press("Enter")
      const started = await accepted
      expect(started.status()).toBe(202)
      turnID = (await started.json()).turn.id
      const transcript = page.getByTestId("transcript")
      const thoughts = transcript.getByTestId("thought-toggle")
      await expect(thoughts).toHaveCount(1)
      await expect(transcript.getByTestId("thought-scroll")).toContainText(firstThought.trim())
      await expect(transcript).not.toContainText(secondThought)
      await expect(transcript).not.toContainText(answer)
      chunk({ [field]: secondThought })
      await expect(transcript.getByTestId("thought-scroll")).toHaveText(thought)
      await expect(transcript).not.toContainText(answer)
      await page.screenshot({ path: info.outputPath("reasoning-before-answer.png"), animations: "disabled" })
      chunk({ content: answer })
      chunk({}, "stop")
      stream?.end("data: [DONE]\n\n")
      await waitForIdle(page)

      const traceReply = await request.get(`/api/trace/${turnID}`)
      expect(traceReply.ok()).toBe(true)
      const trace = await traceReply.json()
      expect(trace.turn).toMatchObject({ id: turnID, thread_id: threadID, status: "done" })
      expect(trace.llm_calls.filter((call: { agent_id: string }) => call.agent_id === "manager")).toHaveLength(1)
      expect(trace.llm_calls.filter((call: { agent_id: string }) => call.agent_id === "schedule-answer-policy")).toHaveLength(1)
      expect(trace.llm_calls).toHaveLength(2)
      const savedThoughts = (trace.events as TraceEvent[]).filter((event) => event.kind === "reasoning")
      expect(savedThoughts).toHaveLength(1)
      expect(savedThoughts[0]).toMatchObject({ thread_id: threadID, turn_id: turnID, agent_id: "manager", text: thought })
      expect(savedThoughts[0].message_id).toBeTruthy()
      expect(savedThoughts[0].seq).toBeGreaterThan(0)
      expect((trace.events as TraceEvent[]).some((event) => event.text === answer && event.turn_id === turnID)).toBe(true)
      expect(attempts).toHaveLength(1)
      expect(policyAttempts).toHaveLength(1)
      expect(requests).toHaveLength(2)
      expect(attempts[0]).toMatchObject({ method: "POST", path: "/chat/completions", hasAuthorization: false, body: { stream: true, model: "fixture-model" } })
      expect(errors).toEqual([])

      for (let replay = 0; replay < 2; replay++) {
        await expect(transcript.getByText(answer, { exact: true })).toHaveCount(1)
        await expect(transcript.getByText(answer, { exact: true })).toBeVisible()
        await expect(thoughts).toHaveCount(1)
        if (await thoughts.getAttribute("aria-expanded") === "false") await thoughts.click()
        await expect(transcript.getByTestId("thought-scroll")).toHaveText(thought)
        await page.getByRole("tab", { name: "Trace", exact: true }).click()
        await expect(page.getByRole("tabpanel").getByText(turnID, { exact: true })).toBeVisible()
        await page.getByTestId("trace-log-toggle").click()
        await expect(page.getByTestId("trace-log")).toContainText(thought)
        await expect(page.getByTestId("trace-log")).toContainText(answer)
        await page.screenshot({ path: info.outputPath(`reasoning-trace-${replay}.png`), animations: "disabled" })
        if (replay === 0) {
          await reloadOpenConversation(page)
          await waitForIdle(page)
        }
      }
      const replayed = await (await request.get(`/api/trace/${turnID}`)).json()
      expect((replayed.events as TraceEvent[]).filter((event) => event.kind === "reasoning")).toEqual(savedThoughts)
      expect(attempts).toHaveLength(1)
      expect(policyAttempts).toHaveLength(1)
      expect(requests).toHaveLength(2)
      await writeFile(info.outputPath("reasoning-trace.json"), JSON.stringify({ field, trace, attempts, policyAttempts }, null, 2), { mode: 0o600 })
    } finally {
      await writeFile(info.outputPath("reasoning-fixture.json"), JSON.stringify({ field, requests, attempts, policyAttempts, errors }, null, 2), { mode: 0o600 })
      await new Promise<void>((resolve, reject) => {
        endpoint.close((error) => error ? reject(error) : resolve())
        endpoint.closeAllConnections()
      })
    }
  })
}

test("manager commentary splits the work folds live and in paged replay", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_REASONING_FIELDS !== "1", "requires the keyless loopback reasoning endpoint")
  const rounds = [
    { id: "read-first", file: "first-input.txt", data: "First file evidence", thought: "Identify the first input.", prose: "I will inspect the first file." },
    { id: "read-second", file: "second-input.txt", data: "Second file evidence", thought: "Identify the comparison input.", prose: "I will compare the second file." },
  ]
  const answer = "Both files were inspected."
  await mkdir(info.outputDir, { recursive: true })
  await Promise.all(rounds.map((round) => writeFile(info.outputPath(round.file), round.data, { mode: 0o600 })))
  const errors: string[] = []
  let managerRequests = 0
  let finish = () => {}
  const finalGate = new Promise<void>((resolve) => { finish = resolve })
  const endpoint = createServer(async (incoming, response) => {
    try {
      const chunks: Buffer[] = []
      for await (const chunk of incoming) chunks.push(Buffer.from(chunk))
      const input = JSON.parse(Buffer.concat(chunks).toString()) as {
        messages?: { role?: string; content?: string; tool_call_id?: string }[]
        tools?: { function?: { name?: string } }[]
        stream?: boolean
      }
      if (incoming.method !== "POST" || incoming.url !== "/chat/completions" || incoming.headers.authorization) {
        throw new Error("Only keyless Chat Completions POST requests are permitted")
      }
      const messages = input.messages ?? []
      if (messages[0]?.content?.split("\n", 1)[0] === "## Schedule answer claims") {
        const candidate = JSON.parse(messages[1]?.content ?? "{}") as { messages?: string[] }
        const allowed = [...rounds.map((round) => round.prose), answer]
        if (input.stream || input.tools?.length || !candidate.messages?.length ||
            candidate.messages.some((text) => !allowed.includes(text))) {
          throw new Error("Unexpected private classification request")
        }
        response.writeHead(200, { "Content-Type": "application/json" })
        response.end(JSON.stringify({ choices: [{ index: 0, message: { role: "assistant", content: '{"claims":[]}' }, finish_reason: "stop" }] }))
        return
      }
      const generation = managerRequests++
      if (input.stream !== true || generation > rounds.length || !input.tools?.some((tool) => tool.function?.name === "read")) {
        throw new Error("Unexpected manager request")
      }
      const previous = rounds[generation - 1]
      if (previous && !messages.some((message) => message.role === "tool" &&
          message.tool_call_id === previous.id && message.content?.includes(previous.data))) {
        throw new Error("The next generation did not receive the real file read result")
      }
      const round = rounds[generation]
      if (!round) await finalGate
      if (response.destroyed) return
      response.writeHead(200, { "Content-Type": "text/event-stream" })
      const chunk = (delta: object, finishReason: string | null = null) => {
        response.write(`data: ${JSON.stringify({ id: `commentary-${generation}`, object: "chat.completion.chunk",
          choices: [{ index: 0, delta, finish_reason: finishReason }] })}\n\n`)
      }
      if (round) {
        chunk({ role: "assistant", reasoning_content: round.thought })
        chunk({ content: round.prose })
        chunk({ tool_calls: [{ index: 0, id: round.id, type: "function",
          function: { name: "read", arguments: JSON.stringify({ file_path: info.outputPath(round.file) }) } }] })
      } else {
        chunk({ role: "assistant", content: answer })
      }
      chunk({}, round ? "tool_calls" : "stop")
      response.end("data: [DONE]\n\n")
    } catch (error) {
      errors.push(String(error))
      if (!response.headersSent) response.writeHead(400, { "Content-Type": "application/json" })
      response.end(JSON.stringify({ error: { message: String(error), type: "invalid_request_error" } }))
    }
  })
  await new Promise<void>((resolve, reject) => {
    endpoint.once("error", reject)
    endpoint.listen(0, "127.0.0.1", () => { endpoint.off("error", reject); resolve() })
  })
  try {
    const address = endpoint.address()
    if (!address || typeof address === "string") throw new Error("The commentary fixture did not listen")
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "commentary-fixture", providers: [{ id: "commentary-fixture", label: "Offline commentary regression",
        base_url: `http://127.0.0.1:${address.port}`, model: "fixture-model", api_key: "", api_mode: "chat_completions" }] },
      swarm: { ...settings.swarm, auto_title: false, composer_predictions: false },
      memory: { ...settings.memory, auto_review: false },
      ui: { ...settings.ui, transcript_mode: "user" },
    } })).ok()).toBe(true)
    await page.addInitScript(() => {
      const NativeEventSource = window.EventSource
      const events: CommentaryEvent[] = []
      ;(window as Window & { __managerCommentaryEvents?: CommentaryEvent[] }).__managerCommentaryEvents = events
      window.EventSource = class extends NativeEventSource {
        constructor(url: string | URL, options?: EventSourceInit) {
          super(url, options)
          for (const kind of ["agent_message", "tool_call_delta", "tool_call"]) {
            this.addEventListener(kind, (event) => { events.push(JSON.parse(event.data)) })
          }
        }
      }
    })
    await freshConversation(page)
    const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
    expect(threadID).toBeTruthy()
    const started = page.waitForResponse((response) => response.request().method() === "POST" &&
      new URL(response.url()).pathname === `/api/threads/${threadID}/steer`)
    await composer(page).fill("Compare the two local inputs")
    await composer(page).press("Enter")
    const accepted = await started
    expect(accepted.status()).toBe(202)
    const { turn } = await accepted.json()
    const transcript = page.getByTestId("transcript")
    const rows = transcript.locator('[data-testid="single-work"],[data-testid="work-fold"],[data-testid="assistant-message"]')
    const assertOrder = async (finished: boolean) => {
      await expect(rows).toHaveCount(finished ? 6 : 5)
      const firstThought = rows.nth(0).getByTestId("thought-toggle")
      await expect(firstThought).toBeVisible()
      await expect(rows.nth(1)).toContainText(rounds[0].prose)
      const mixed = rows.nth(2)
      await expect(mixed).toHaveAttribute("data-testid", "work-fold")
      await expect(rows.nth(3)).toContainText(rounds[1].prose)
      await expect(rows.nth(4).getByRole("button", { name: /second-input\.txt/ })).toBeVisible()
      await mixed.click()
      const read = transcript.getByRole("button", { name: /first-input\.txt/ })
      await expect(read).toBeVisible()
      const secondThought = transcript.getByTestId("thought-toggle").nth(1)
      await expect(secondThought).toBeVisible()
      if (await secondThought.getAttribute("aria-expanded") === "false") await secondThought.click()
      await expect(transcript.getByTestId("thought-scroll").filter({ hasText: rounds[1].thought })).toBeVisible()
      await mixed.click()
      if (finished) await expect(rows.nth(5)).toContainText(answer)
    }

    // The final response is held so late end-of-run publication cannot pass.
    await expect.poll(() => managerRequests).toBe(3)
    await expect(statusBadge(page)).toContainText("Working")
    const expectedLive = rounds.flatMap((round) => [round.prose, round.id])
    await expect.poll(async () => {
      const events = await page.evaluate(() => (window as Window & { __managerCommentaryEvents?: CommentaryEvent[] }).__managerCommentaryEvents ?? [])
      const live = events.filter((event) => event.turn_id === turn.id && event.agent_id === "manager")
      const order = live.filter((event) => event.kind !== "tool_call_delta")
        .map((event) => event.kind === "agent_message" ? event.text : event.tool_call_id)
      const previewsFollowProse = rounds.every((round) => {
        const proseIndex = live.findIndex((event) => event.kind === "agent_message" && event.text === round.prose)
        return live.every((event, index) => event.kind !== "tool_call_delta" || event.tool_call_id !== round.id ||
          (proseIndex >= 0 && proseIndex < index))
      })
      return { order, previewsFollowProse }
    }).toEqual({ order: expectedLive, previewsFollowProse: true })
    await assertOrder(false)
    await expect(transcript).not.toContainText(answer)
    finish()
    await waitForIdle(page)
    await assertOrder(true)

    const trace = await (await request.get(`/api/trace/${turn.id}`)).json()
    const durable = (trace.events as CommentaryEvent[])
      .filter((event) => event.agent_id === "manager")
      .flatMap((event) => event.kind === "reasoning" || event.kind === "agent_message" ? [event.text] :
        event.kind === "tool_call" ? [event.tool_call_id] : [])
    expect(durable).toEqual([...rounds.flatMap((round) => [round.thought, round.prose, round.id]), answer])
    await reloadOpenConversation(page)
    await assertOrder(true)

    // Keep the real log endpoint and only reduce its first-page limit.
    const logPath = `/api/threads/${threadID}/log`
    await page.route((url) => url.pathname === logPath, async (route) => {
      const url = new URL(route.request().url())
      if (!url.searchParams.has("before") && !url.searchParams.has("after")) url.searchParams.set("limit", "3")
      await route.continue({ url: url.href })
    })
    const older = page.waitForResponse((response) => {
      const url = new URL(response.url())
      return url.pathname === logPath && url.searchParams.has("before")
    })
    await reloadOpenConversation(page)
    expect((await older).ok()).toBe(true)
    await assertOrder(true)
    expect(managerRequests).toBe(3)
    expect(errors).toEqual([])
  } finally {
    finish()
    await new Promise<void>((resolve, reject) => {
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    })
  }
})
