import { expect, test } from "@playwright/test"
import { createServer } from "node:http"
import { writeFile } from "node:fs/promises"

import { composer, freshConversation, reloadOpenConversation, waitForIdle } from "./session"

test("Responses cloud search keeps citations and its own Trace after reload", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_BUILTIN_TOOLS !== "1", "requires the opt-in keyless loopback Responses endpoint")
  const attempts: { path?: string; body: Record<string, unknown> }[] = []
  const policyAttempts: { path?: string; body: Record<string, unknown> }[] = []
  const cloud = { type: "web_search_call", id: "cloud-search", status: "completed", action: { type: "search", query: "public reference" } }
  const message = { type: "message", id: "cloud-answer", role: "assistant", content: [{ type: "output_text", text: "Reference located.", annotations: [{ type: "url_citation", url: "https://example.com/reference", title: "Reference source", start_index: 0, end_index: 9 }] }] }
  const endpoint = createServer(async (incoming, response) => {
    const chunks: Buffer[] = []
    for await (const chunk of incoming) chunks.push(Buffer.from(chunk))
    const body = JSON.parse(Buffer.concat(chunks).toString()) as Record<string, unknown>
    const input = body.input as { role?: string; content?: { text?: string }[] }[]
    if (input[0]?.role === "system" && input[0].content?.some((part) => part.text?.startsWith("## Schedule answer claims"))) {
      policyAttempts.push({ path: incoming.url, body })
      response.writeHead(200, { "Content-Type": "application/json" })
      response.end(JSON.stringify({ id: "claim-response", status: "completed", output: [{ type: "message", id: "claim-answer", role: "assistant", content: [{ type: "output_text", text: JSON.stringify({ claims: [] }) }] }] }))
      return
    }
    attempts.push({ path: incoming.url, body })
    response.writeHead(200, { "Content-Type": "text/event-stream" })
    const send = (event: unknown) => response.write(`data: ${JSON.stringify(event)}\n\n`)
    send({ type: "response.output_item.added", output_index: 0, item: { ...cloud, status: "in_progress" } })
    send({ type: "response.output_item.done", output_index: 0, item: cloud })
    send({ type: "response.output_text.delta", output_index: 1, content_index: 0, delta: "Reference located." })
    send({ type: "response.completed", response: { id: "cloud-response", status: "completed", output: [cloud, message], usage: { input_tokens: 10, output_tokens: 5, total_tokens: 15 } } })
    response.end()
  })
  await new Promise<void>((resolve) => endpoint.listen(0, "127.0.0.1", resolve))
  try {
    const address = endpoint.address()
    if (!address || typeof address === "string") throw new Error("fixture did not listen")
    const { settings } = await (await request.get("/api/settings")).json()
    const configured = await request.put("/api/settings", { data: {
      models: { default: "cloud", providers: [{ id: "cloud", label: "Cloud fixture", base_url: `http://127.0.0.1:${address.port}`, model: "fixture", api_key: "", api_mode: "responses", builtin_tools: [{ type: "web_search", search_context_size: "low" }] }] },
      swarm: { ...settings.swarm, auto_title: false },
      memory: { ...settings.memory, auto_review: false },
    } })
    expect(configured.ok()).toBe(true)
    await freshConversation(page)
    const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
    const accepted = page.waitForResponse((reply) => reply.request().method() === "POST" && new URL(reply.url()).pathname === `/api/threads/${threadID}/steer`)
    await composer(page).fill("Perform the requested check")
    await composer(page).press("Enter")
    const { turn } = await (await accepted).json()
    await waitForIdle(page)
    const transcript = page.getByTestId("transcript")
    const source = transcript.getByRole("link", { name: "Reference source", exact: true })
    await expect(source).toHaveAttribute("href", "https://example.com/reference")
    await expect(source).toBeVisible()
    expect(attempts).toHaveLength(1)
    expect(attempts[0].path).toBe("/responses")
    const tools = attempts[0].body.tools as { type: string; search_context_size?: string }[]
    expect(tools).toContainEqual({ type: "web_search", search_context_size: "low" })
    expect(tools.some((tool) => tool.type === "function")).toBe(true)
    const trace = await (await request.get(`/api/trace/${turn.id}`)).json()
    expect(trace.turn.status).toBe("done")
    expect(trace.llm_calls.filter((call: { agent_id: string }) => call.agent_id === "manager")).toHaveLength(1)
    expect(trace.llm_calls.filter((call: { agent_id: string }) => call.agent_id === "schedule-answer-policy")).toHaveLength(1)
    expect(policyAttempts).toHaveLength(1)
    expect(policyAttempts[0].path).toBe("/responses")
    expect(policyAttempts[0].body).toMatchObject({ tool_choice: "none", stream: false })
    expect(policyAttempts[0].body.input).toHaveLength(2)
    expect((policyAttempts[0].body.tools as { type: string }[]).some((tool) => tool.type === "function")).toBe(false)
    const receipts = trace.events.filter((event: { kind: string }) => event.kind === "builtin_tool")
    const completed = receipts.filter((event: { text: string }) => JSON.parse(event.text).status === "completed")
    expect(completed).toHaveLength(1)
    expect(completed[0]).toMatchObject({ agent_id: "manager", turn_id: turn.id })
    expect(JSON.parse(completed[0].text)).toMatchObject({ provider_id: "cloud", model: "fixture", id: "cloud-search", type: "web_search_call" })
    expect(trace.events.some((event: { kind: string }) => ["tool_call", "tool_result"].includes(event.kind))).toBe(false)
    await page.getByRole("tab", { name: "Trace", exact: true }).click()
    await page.getByTestId("trace-log-toggle").click()
    await expect(page.getByTestId("trace-log")).toContainText("web_search")
    await page.screenshot({ path: info.outputPath("builtin-tools-trace.png"), animations: "disabled" })
    await reloadOpenConversation(page)
    await waitForIdle(page)
    await expect(source).toBeVisible()
    await page.getByRole("tab", { name: "Trace", exact: true }).click()
    await page.getByTestId("trace-log-toggle").click()
    await expect(page.getByTestId("trace-log")).toContainText("web_search")
    expect(attempts).toHaveLength(1)
    expect(policyAttempts).toHaveLength(1)
    await writeFile(info.outputPath("builtin-tools-trace.json"), JSON.stringify({ trace, attempts, policyAttempts }, null, 2))
  } finally {
    await new Promise<void>((resolve, reject) => {
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    })
  }
})
