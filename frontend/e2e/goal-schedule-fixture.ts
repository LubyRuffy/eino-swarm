import { createServer, type ServerResponse } from "node:http"

type Call = { function: { name: string; arguments: string } }
type Message = { role: string; content?: string; tool_calls?: Call[] }
type Input = { messages: Message[]; tools?: { function: { name: string } }[] }

// Exercise contradictory lifecycle calls through the real keyless HTTP adapter.
// The script is task agnostic; it never performs work outside the test engine.
export async function startGoalScheduleFixture(automatic: boolean) {
  const errors: string[] = []
  let finish = false
  let nextCall = 0
  const reply = (response: ServerResponse, call?: { name: string; args: object }) => {
    const delta = call
      ? { role: "assistant", tool_calls: [{ index: 0, id: `goal-fixture-${++nextCall}`, type: "function", function: { name: call.name, arguments: JSON.stringify(call.args) } }] }
      : { role: "assistant", content: finish ? "The objective is satisfied." : "The check finished; the objective remains open." }
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
        const input = JSON.parse(body) as Input
        if (input.messages[0]?.role === "system" && input.messages[0].content?.startsWith("## Thread wake authorization classification")) {
          if (input.messages.length !== 2 || input.tools?.length) throw new Error("Policy classification must remain a separate tool-free request")
          response.writeHead(200, { "Content-Type": "application/json" })
          response.end(JSON.stringify({ choices: [{ index: 0, message: { role: "assistant", content: JSON.stringify({ decision: "unchanged", quote: "" }) }, finish_reason: "stop" }] }))
          return
        }
        const lastUser = input.messages.findLastIndex((message) => message.role === "user")
        const current = input.messages.slice(lastUser + 1)
        const called = (name: string) => current.some((message) => message.tool_calls?.some((call) => call.function.name === name))
        const scheduled = input.messages[lastUser]?.content?.startsWith("This turn is a scheduled check.")
        if (!scheduled) {
          if (!called("schedule_wake")) {
            reply(response, { name: "schedule_wake", args: { prompt: "Inspect the current objective state.", ...(automatic ? { delay_s: 1 } : { every_s: 3600 }) } })
          } else { reply(response) }
          return
        }
        if (!called("report_schedule")) {
          reply(response, { name: "report_schedule", args: finish ? { findings: "All required work is verified.", keep: false } : { findings: "Required work remains.", keep: true, next_in_s: 3600 } })
        } else if (!called("complete_goal")) {
          if (!input.tools?.some((tool) => tool.function.name === "complete_goal")) throw new Error("A pursuing scheduled turn must expose the goal tool")
          reply(response, { name: "complete_goal", args: { summary: finish ? "Objective verified." : "One check finished." } })
        } else { reply(response) }
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
    finish: () => { finish = true },
    close: () => new Promise<void>((resolve, reject) => {
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    }),
  }
}
