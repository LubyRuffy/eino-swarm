import { createServer, type ServerResponse } from "node:http"

type Call = { function: { name: string; arguments: string } }
type Message = { role: string; content?: string; tool_calls?: Call[] }
type Input = { messages: Message[]; tools?: { function: { name: string } }[]; stream?: boolean; tool_choice?: string }

// Both model attempts stay open until the test inspects their actual UI. This
// reproduces an output-budget interruption without relying on timing or a mock
// of stored events, then resumes the same worker through the manager tool graph.
export async function startGenerationInterruptionFixture() {
  const errors: string[] = []
  let workerID = ""
  let workerRequests = 0
  let nextCall = 0
  let first: ServerResponse | undefined
  let second: ServerResponse | undefined
  const chunk = (response: ServerResponse, delta: object, finish: string | null = null) => {
    response.write(`data: ${JSON.stringify({ id: `generation-fixture-${workerRequests}`, object: "chat.completion.chunk", choices: [{ index: 0, delta, finish_reason: finish }] })}\n\n`)
  }
  const end = (response: ServerResponse, finish: string) => {
    chunk(response, {}, finish)
    response.end("data: [DONE]\n\n")
  }
  const reply = (response: ServerResponse, content: string, name?: string, args?: object) => {
    response.writeHead(200, { "Content-Type": "text/event-stream" })
    chunk(response, name ? { role: "assistant", tool_calls: [{ index: 0, id: `generation-call-${++nextCall}`, type: "function", function: { name, arguments: JSON.stringify(args) } }] } : { role: "assistant", content })
    end(response, name ? "tool_calls" : "stop")
  }
  const endpoint = createServer((incoming, response) => {
    let body = ""
    incoming.setEncoding("utf8")
    incoming.on("data", (part: string) => { body += part })
    incoming.on("end", () => {
      try {
        if (incoming.headers.authorization) throw new Error("The fixture must be keyless")
        const input = JSON.parse(body) as Input
        if (input.messages[0]?.role === "system" && input.messages[0].content?.split("\n", 1)[0] === "## Schedule answer claims") {
          if (input.stream || input.tools?.length || input.tool_choice !== "none" || input.messages.length !== 2 || input.messages[1].role !== "user") {
            throw new Error("Schedule answer policy must remain a separate non-streaming, tool-free request")
          }
          response.writeHead(200, { "Content-Type": "application/json" })
          response.end(JSON.stringify({ choices: [{ index: 0, message: { role: "assistant", content: '{"claims":[]}' }, finish_reason: "stop" }] }))
          return
        }
        const manager = input.tools?.some((tool) => tool.function.name === "spawn_agent")
        if (!manager) {
          workerRequests++
          response.writeHead(200, { "Content-Type": "text/event-stream" })
          if (workerRequests === 1) {
            first = response
            chunk(response, { role: "assistant", reasoning_content: "Inspecting the first assigned input before the interrupted output." })
          } else if (workerRequests === 2) {
            second = response
            chunk(response, { role: "assistant", reasoning_content: "Inspecting the remaining input after recovery." })
          } else throw new Error("Recovery must resume the existing worker once")
          return
        }
        const calls = input.messages.flatMap((message) => message.tool_calls ?? [])
        const count = (name: string) => calls.filter((call) => call.function.name === name).length
        const receipt = input.messages.find((message) => message.role === "tool" && message.content?.includes('"agent_id"'))
        if (!receipt) reply(response, "", "spawn_agent", { role: "verifier", task: "Inspect the assigned inputs and report a verified result." })
        else {
          workerID = JSON.parse(receipt.content ?? "{}").agent_id
          if (count("wait_agents") === 0) reply(response, "", "wait_agents", { agent_ids: [workerID], timeout_s: 60 })
          else if (count("resume_agent") === 0) reply(response, "", "resume_agent", { agent_id: workerID, task: "Continue the unfinished verification and report the result." })
          else if (count("wait_agents") === 1) reply(response, "", "wait_agents", { agent_ids: [workerID], timeout_s: 60 })
          else reply(response, "The existing worker recovered and verified the assigned inputs.")
        }
      } catch (error) {
        errors.push(String(error))
        if (!response.headersSent) response.writeHead(400, { "Content-Type": "application/json" })
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
    url: `http://127.0.0.1:${address.port}`, errors,
    state: () => ({ workerID, workerRequests }),
    interruptFirst: () => { if (!first) throw new Error("First attempt has not started"); end(first, "length") },
    finishRecovery: () => {
      if (!second) throw new Error("Recovery has not started")
      chunk(second, { content: "The assigned inputs were verified after recovery." })
      end(second, "stop")
    },
    close: () => new Promise<void>((resolve, reject) => {
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    }),
  }
}
