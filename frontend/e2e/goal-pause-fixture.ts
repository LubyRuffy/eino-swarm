import { createServer, type ServerResponse } from "node:http"

type Call = { function: { name: string; arguments: string } }
type Message = { role: string; content?: string; tool_calls?: Call[] }
type Input = { messages: Message[]; tools?: { function: { name: string } }[]; stream?: boolean }

// Make one counted tool call per turn, leaving the goal open so the real cap
// supplies the pause. No worker or external action is needed for this fixture.
export async function startGoalPauseFixture() {
  const errors: string[] = []
  let nextCall = 0
  let held = false
  const pending = new Set<() => void>()
  const reply = (response: ServerResponse, input: Input, delta: object, finish = "stop") => {
    if (!input.stream) {
      response.writeHead(200, { "Content-Type": "application/json" })
      response.end(JSON.stringify({ choices: [{ index: 0, message: delta, finish_reason: finish }] }))
      return
    }
    response.writeHead(200, { "Content-Type": "text/event-stream" })
    response.end(`data: ${JSON.stringify({ id: "goal-pause-fixture", choices: [{ index: 0, delta, finish_reason: null }] })}\n\ndata: ${JSON.stringify({ id: "goal-pause-fixture", choices: [{ index: 0, delta: {}, finish_reason: finish }] })}\n\ndata: [DONE]\n\n`)
  }
  const endpoint = createServer((incoming, response) => {
    let body = ""
    incoming.setEncoding("utf8")
    incoming.on("data", (part: string) => { body += part })
    incoming.on("end", () => {
      try {
        if (incoming.headers.authorization) throw new Error("The fixture must be keyless")
        const input = JSON.parse(body) as Input
        if (input.messages[0]?.content?.startsWith("## Schedule answer claims")) {
          reply(response, input, { role: "assistant", content: JSON.stringify({ claims: [] }) })
          return
        }
        if (input.messages[0]?.content?.startsWith("## Thread wake authorization classification")) {
          reply(response, input, { role: "assistant", content: JSON.stringify({ decision: "unchanged", quote: "" }) })
          return
        }
        if (!input.tools?.some((tool) => tool.function.name === "ls")) throw new Error("The real file listing tool is missing")
        const respond = () => {
          const lastUser = input.messages.findLastIndex((message) => message.role === "user")
          const called = input.messages.slice(lastUser + 1).some((message) => message.tool_calls?.some((call) => call.function.name === "ls"))
          if (called) reply(response, input, { role: "assistant", content: "The requested status has been reported." })
          else reply(response, input, { role: "assistant", tool_calls: [{ index: 0, id: `pause-${++nextCall}`, type: "function", function: { name: "ls", arguments: '{"path":"."}' } }] }, "tool_calls")
        }
        if (held) {
          pending.add(respond)
          response.once("close", () => pending.delete(respond))
        } else respond()
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
    url: `http://127.0.0.1:${address.port}`, errors,
    hold: () => { held = true },
    release: () => {
      held = false
      for (const respond of pending) respond()
      pending.clear()
    },
    close: () => new Promise<void>((resolve, reject) => {
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    }),
  }
}
