import { createServer, type ServerResponse } from "node:http"

type Call = { id: string; type: string; function: { name: string; arguments: string } }
type Message = { role: string; content?: string; tool_calls?: Call[] }
type Input = { messages: Message[]; tools?: { function: { name: string } }[] }
type Worker = { agent_id: string; role: string; status: string }

// This fixture rejects misleading model input at the real HTTP adapter. It
// does no work outside the loopback test server or the engine's tool graph.
export async function startRosterFixture() {
  const errors: string[] = []
  const snapshots: Worker[][] = []
  const authorization: boolean[] = []
  let completed = 0
  let spawnedID = ""
  let nextCall = 0
  const reply = (response: ServerResponse, content: string, call?: { name: string; args: object }) => {
    const delta = call ? { role: "assistant", tool_calls: [{ index: 0, id: `fixture-${++nextCall}`, type: "function", function: { name: call.name, arguments: JSON.stringify(call.args) } }] } : { role: "assistant", content }
    response.writeHead(200, { "Content-Type": "text/event-stream" })
    response.end(`data: ${JSON.stringify({ id: "fixture", object: "chat.completion.chunk", choices: [{ index: 0, delta, finish_reason: null }] })}\n\ndata: ${JSON.stringify({ id: "fixture", object: "chat.completion.chunk", choices: [{ index: 0, delta: {}, finish_reason: call ? "tool_calls" : "stop" }] })}\n\ndata: [DONE]\n\n`)
  }
  const endpoint = createServer((incoming, response) => {
    authorization.push(Boolean(incoming.headers.authorization))
    let body = ""
    incoming.setEncoding("utf8")
    incoming.on("data", (part: string) => { body += part })
    incoming.on("end", () => {
      try {
        const input = JSON.parse(body) as Input
        const manager = input.tools?.some((tool) => tool.function.name === "spawn_agent")
        if (!manager) { reply(response, "Verification complete."); return }
        const lastUser = input.messages.findLastIndex((message) => message.role === "user")
        const current = input.messages.slice(lastUser + 1)
        if (completed === 0) {
          const receipt = current.find((message) => message.role === "tool" && message.content?.includes('"agent_id"'))
          if (!receipt) {
            reply(response, "", { name: "spawn_agent", args: { role: "verifier", task: "Verify the assigned inputs and report the result." } })
          } else if (!current.some((message) => message.tool_calls?.some((call) => call.function.name === "wait_agents"))) {
            spawnedID = JSON.parse(receipt.content ?? "{}").agent_id
            reply(response, "", { name: "wait_agents", args: { agent_ids: [spawnedID], timeout_s: 10 } })
          } else {
            completed++
            reply(response, "Verification complete.")
          }
          return
        }
        // A prior successful spawn has both role and task. The old roster
        // reconstruction had only role, and therefore fails this assertion.
        const naked = input.messages.flatMap((message) => message.tool_calls ?? []).some((call) => call.function.name === "spawn_agent" && !JSON.parse(call.function.arguments).task)
        const rosters = input.messages.flatMap((message) => {
          if (message.role !== "system") return []
          try {
            const parsed = JSON.parse(message.content ?? "")
            return parsed.source === "runtime_worker_roster" ? [parsed.workers as Worker[]] : []
          } catch { return [] }
        })
        if (naked || rosters.length !== 1 || rosters[0].length !== 1 || rosters[0][0].agent_id !== spawnedID || rosters[0][0].status !== "done") {
          throw new Error("Finished history must be one explicit state snapshot, with no fabricated spawn calls")
        }
        snapshots.push(rosters[0])
        if (!current.some((message) => message.tool_calls?.some((call) => call.function.name === "wait_agents"))) {
          reply(response, "", { name: "wait_agents", args: { agent_ids: [spawnedID], timeout_s: 0 } })
        } else if (input.messages[lastUser].content?.startsWith("This turn is a scheduled check.") && !current.some((message) => message.tool_calls?.some((call) => call.function.name === "report_schedule"))) {
          reply(response, "", { name: "report_schedule", args: { findings: "Existing worker verified; no new worker was created.", keep: false } })
        } else {
          completed++
          reply(response, "Existing worker verified; no new worker was created.")
        }
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
    url: `http://127.0.0.1:${address.port}`, errors, snapshots, authorization,
    close: () => new Promise<void>((resolve, reject) => {
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    }),
  }
}
