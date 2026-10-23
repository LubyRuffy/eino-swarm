import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { createServer, type ServerResponse } from "node:http"

import { composer, freshConversation, reloadOpenConversation, waitForIdle } from "./session"

type TraceEvent = { thread_id: string; turn_id: string; agent_id?: string; message_id?: string; seq: number; kind: string; text?: string }

for (const field of ["reasoning_content", "reasoning"] as const) {
  test(`${field} streams before the answer and survives same-turn Trace replay`, async ({ page, request }, info) => {
    test.skip(process.env.ZWAI_E2E_REASONING_FIELDS !== "1", "requires the keyless loopback reasoning endpoint")
    const firstThought = "Inspecting the supplied inputs. "
    const secondThought = "The requested checks are complete."
    const thought = firstThought + secondThought
    const answer = "The assigned inputs passed verification."
    const attempts: { method?: string; path?: string; hasAuthorization: boolean; body: Record<string, unknown> }[] = []
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
        attempts.push({ method: incoming.method, path: incoming.url, hasAuthorization: Boolean(incoming.headers.authorization), body })
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
        swarm: { ...settings.swarm, auto_title: false },
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
      expect(trace.llm_calls).toHaveLength(1)
      const savedThoughts = (trace.events as TraceEvent[]).filter((event) => event.kind === "reasoning")
      expect(savedThoughts).toHaveLength(1)
      expect(savedThoughts[0]).toMatchObject({ thread_id: threadID, turn_id: turnID, agent_id: "manager", text: thought })
      expect(savedThoughts[0].message_id).toBeTruthy()
      expect(savedThoughts[0].seq).toBeGreaterThan(0)
      expect((trace.events as TraceEvent[]).some((event) => event.text === answer && event.turn_id === turnID)).toBe(true)
      expect(attempts).toHaveLength(1)
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
      await writeFile(info.outputPath("reasoning-trace.json"), JSON.stringify({ field, trace, attempts }, null, 2), { mode: 0o600 })
    } finally {
      await new Promise<void>((resolve, reject) => {
        endpoint.close((error) => error ? reject(error) : resolve())
        endpoint.closeAllConnections()
      })
    }
  })
}
