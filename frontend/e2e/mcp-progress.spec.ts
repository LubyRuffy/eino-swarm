import { expect, test } from "@playwright/test"
import { createServer, type ServerResponse } from "node:http"

import { composer, freshConversation, waitForIdle } from "./session"

type ModelRequest = {
  stream?: boolean
  messages: { role: string; content?: string; tool_call_id?: string }[]
  tools?: { function: { name: string } }[]
}

type ProgressEvent = { turn_id: string; tool_call_id: string; kind: string; seq: number; text?: string }

const callID = "remote-inspection"
const phase = "Checking the source records"
const resultText = "Source records verified"

function json(response: ServerResponse, body: unknown) {
  response.writeHead(200, { "Content-Type": "application/json" })
  response.end(JSON.stringify(body))
}

function modelReply(response: ServerResponse, input: ModelRequest, message: object, finish = "stop") {
  if (!input.stream) {
    json(response, { choices: [{ index: 0, message, finish_reason: finish }] })
    return
  }
  response.writeHead(200, { "Content-Type": "text/event-stream" })
  const chunk = (delta: object, reason: string | null) => ({
    id: "mcp-progress-fixture", choices: [{ index: 0, delta, finish_reason: reason }],
  })
  response.end(`data: ${JSON.stringify(chunk(message, null))}\n\ndata: ${JSON.stringify(chunk({}, finish))}\n\ndata: [DONE]\n\n`)
}

test("MCP progress is visible in folded work before the remote call returns", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_MCP_PROGRESS !== "1", "requires the keyless loopback model and MCP endpoint")
  const serverID = "progress-fixture"
  const errors: string[] = []
  let token: string | number | undefined
  let calls = 0
  let finalSent = false
  let modelResult = ""
  let reportProgress!: () => void
  let finishCall!: () => void
  const progressGate = new Promise<void>((resolve) => { reportProgress = resolve })
  const resultGate = new Promise<void>((resolve) => { finishCall = resolve })
  const endpoint = createServer(async (incoming, response) => {
    try {
      const path = new URL(incoming.url ?? "/", "http://127.0.0.1").pathname
      if (incoming.method === "GET") {
        if (path === "/mcp") response.writeHead(405).end()
        else json(response, { data: [{ id: "fixture-model" }] })
        return
      }
      let raw = ""
      incoming.setEncoding("utf8")
      for await (const part of incoming) raw += part
      if (path === "/mcp") {
        const rpc = JSON.parse(raw) as {
          id?: number | string
          method: string
          params?: { protocolVersion?: string; name?: string; _meta?: { progressToken?: string | number } }
        }
        if (rpc.id === undefined) {
          response.writeHead(202).end()
          return
        }
        const reply = (result: unknown) => ({ jsonrpc: "2.0", id: rpc.id, result })
        switch (rpc.method) {
          case "server/discover":
            // This fixture uses the legacy initialize handshake.
            json(response, { jsonrpc: "2.0", id: rpc.id, error: { code: -32601, message: "Method not found" } })
            return
          case "initialize":
            json(response, reply({
              protocolVersion: rpc.params?.protocolVersion, capabilities: { tools: {} },
              serverInfo: { name: serverID, version: "1" },
            }))
            return
          case "tools/list":
            json(response, reply({
              tools: [{ name: "inspect_record", inputSchema: { type: "object", properties: {} } }],
            }))
            return
          case "tools/call":
            if (rpc.params?.name !== "inspect_record") throw new Error("Unexpected remote tool")
            calls++
            token = rpc.params._meta?.progressToken
            response.writeHead(200, { "Content-Type": "text/event-stream" })
            response.flushHeaders()
            await progressGate
            if (token !== undefined) {
              response.write(`data: ${JSON.stringify({
                jsonrpc: "2.0", method: "notifications/progress",
                params: { progressToken: token, progress: 1, total: 2, message: phase },
              })}\n\n`)
            }
            await resultGate
            finalSent = true
            response.end(`data: ${JSON.stringify(reply({ content: [{ type: "text", text: resultText }] }))}\n\n`)
            return
          default:
            throw new Error(`Unexpected MCP method ${rpc.method}`)
        }
      }
      if (path !== "/chat/completions" || incoming.headers.authorization) {
        throw new Error("Expected a keyless Chat Completions request")
      }
      const input = JSON.parse(raw) as ModelRequest
      const answer = (content: string) => modelReply(response, input, { role: "assistant", content })
      if (input.messages[0]?.content?.startsWith("## Schedule answer claims")) {
        answer(JSON.stringify({ claims: [] }))
        return
      }
      const completed = input.messages.find((message) => message.tool_call_id === callID)
      if (completed) {
        modelResult = completed.content ?? ""
        answer("The inspection is complete.")
        return
      }
      const listed = input.messages.some((message) => message.tool_call_id === "remote-catalog")
      const name = input.tools?.find((item) => item.function.name.includes(listed ? "_call_tool_" : "_list_tools_"))?.function.name
      if (!name) throw new Error("MCP proxy absent at the model boundary")
      const args = listed ? { name: "inspect_record", arguments: {} } : {}
      modelReply(response, input, { role: "assistant", tool_calls: [
        { index: 0, id: listed ? callID : "remote-catalog", type: "function", function: { name, arguments: JSON.stringify(args) } },
      ] }, "tool_calls")
    } catch (error) {
      errors.push(String(error))
      if (response.headersSent) response.destroy()
      else response.writeHead(500).end()
    }
  })
  await new Promise<void>((resolve, reject) => {
    endpoint.once("error", reject)
    endpoint.listen(0, "127.0.0.1", () => { endpoint.off("error", reject); resolve() })
  })
  try {
    const address = endpoint.address()
    if (!address || typeof address === "string") throw new Error("Fixture did not listen")
    const baseURL = `http://127.0.0.1:${address.port}`
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "fixture-provider", providers: [{
        id: "fixture-provider", label: "MCP progress fixture", base_url: baseURL, model: "fixture-model", api_key: "",
      }] },
      swarm: { ...settings.swarm, auto_title: false, composer_predictions: false },
      memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    expect((await request.put(`/api/mcp/${serverID}`, { data: {
      id: serverID, name: "Progress fixture", enabled: true, transport: "streamable_http",
      url: `${baseURL}/mcp`, timeout_seconds: 20,
    } })).ok()).toBe(true)
    await page.addInitScript(() => {
      const observed: ProgressEvent[] = []
      const Original = window.EventSource
      Object.assign(window, { __mcpDeltas: observed })
      window.EventSource = class extends Original {
        constructor(url: string | URL, init?: EventSourceInit) {
          super(url, init)
          this.addEventListener("tool_delta", (event) => observed.push(JSON.parse(event.data)))
        }
      }
    })
    await freshConversation(page)
    const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
    const accepted = page.waitForResponse((reply) => reply.request().method() === "POST"
      && new URL(reply.url()).pathname === `/api/threads/${threadID}/steer`)
    await composer(page).fill("Inspect the records")
    await composer(page).press("Enter")
    const { turn } = await (await accepted).json()
    await expect.poll(() => calls).toBe(1)
    expect(typeof token).toBe("string")
    const transcript = page.getByTestId("transcript")
    const collapsed = transcript.getByTestId("work-fold")
    await expect(collapsed).toHaveAttribute("aria-expanded", "false")
    await expect(collapsed).toContainText("Waiting for MCP progress")
    reportProgress()
    await expect(collapsed).toContainText(phase)
    const live = await page.evaluate(() => (window as Window & { __mcpDeltas?: ProgressEvent[] }).__mcpDeltas ?? [])
    expect(live).toContainEqual(expect.objectContaining({
      kind: "tool_delta", turn_id: turn.id, tool_call_id: callID, seq: 0,
      text: expect.stringContaining(phase),
    }))
    expect(finalSent).toBe(false)
    expect(modelResult).toBe("")
    await page.screenshot({ path: info.outputPath("mcp-folded-progress.png"), animations: "disabled" })
    await collapsed.click()
    const tool = transcript.getByRole("button", { name: /mcp_.*call_tool/ }).last()
    if (await tool.getAttribute("aria-expanded") === "false") await tool.click()
    await expect(transcript.getByTestId("mcp-tool-progress")).toContainText(phase)
    finishCall()
    await waitForIdle(page)
    expect(errors).toEqual([])
    expect(modelResult).toContain(resultText)
    expect(modelResult).not.toContain(phase)
    await expect(transcript).not.toContainText(phase)
    const trace = await (await request.get(`/api/trace/${turn.id}`)).json()
    expect(trace.events).toContainEqual(expect.objectContaining({
      kind: "tool_result", tool_call_id: callID, text: expect.stringContaining(resultText),
    }))
    expect((trace.events as ProgressEvent[]).some((event) => event.kind === "tool_delta" || event.text?.includes(phase))).toBe(false)
  } finally {
    reportProgress()
    finishCall()
    await request.delete(`/api/mcp/${serverID}`)
    await new Promise<void>((resolve, reject) => {
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    })
  }
})
