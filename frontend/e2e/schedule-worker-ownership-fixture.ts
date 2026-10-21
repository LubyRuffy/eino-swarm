import { createServer, type ServerResponse } from "node:http"

type Call = { function: { name: string; arguments: string } }
type Message = { role: string; content?: string; tool_calls?: Call[] }
type Input = { messages: Message[]; tools?: { function: { name: string } }[] }
type Worker = { agent_id: string; role: string; status: string }

// Keep one real worker HTTP stream open across manager turns. Explicit test
// handshakes decide when it finishes; an elapsed timer cannot prove survival.
export async function startScheduleWorkerOwnershipFixture() {
  const errors: string[] = []
  const rosters: Worker[][] = []
  let mode: "spawn" | "fail" | "collect" | "block" = "spawn"
  let nextCall = 0
  let workerID = ""
  let workerResponse: ServerResponse | undefined
  let released = false
  let workerCancelled = false
  let failedReplies = 0
  let workerRequests = 0
  let managerBlocked = false
  const chunk = (response: ServerResponse, delta: object, finish: string | null = null) => {
    response.write(`data: ${JSON.stringify({ id: "ownership-fixture", object: "chat.completion.chunk", choices: [{ index: 0, delta, finish_reason: finish }] })}\n\n`)
  }
  const finish = (response: ServerResponse, content: string) => {
    chunk(response, { role: "assistant", content })
    chunk(response, {}, "stop")
    response.end("data: [DONE]\n\n")
  }
  const reply = (response: ServerResponse, content: string, call?: { name: string; args: object }) => {
    response.writeHead(200, { "Content-Type": "text/event-stream" })
    if (!call) { finish(response, content); return }
    chunk(response, { role: "assistant", tool_calls: [{ index: 0, id: `ownership-${++nextCall}`, type: "function", function: { name: call.name, arguments: JSON.stringify(call.args) } }] })
    chunk(response, {}, "tool_calls")
    response.end("data: [DONE]\n\n")
  }
  const endpoint = createServer((incoming, response) => {
    let body = ""
    incoming.setEncoding("utf8")
    incoming.on("data", (part: string) => { body += part })
    incoming.on("end", () => {
      try {
        if (incoming.headers.authorization) throw new Error("The fixture must be keyless")
        const input = JSON.parse(body) as Input
        const manager = input.tools?.some((tool) => tool.function.name === "spawn_agent")
        if (!manager) {
          workerRequests++
          if (workerRequests !== 1) throw new Error("The existing worker must never be restarted")
          workerResponse = response
          response.writeHead(200, { "Content-Type": "text/event-stream" })
          chunk(response, { role: "assistant", reasoning_content: "Inspecting the assigned inputs while the manager checks progress." })
          response.on("close", () => { if (!released) workerCancelled = true })
          return
        }
        const lastUser = input.messages.findLastIndex((message) => message.role === "user")
        const current = input.messages.slice(lastUser + 1)
        const called = (name: string) => current.some((message) => message.tool_calls?.some((call) => call.function.name === name))
        if (mode === "block") {
          managerBlocked = true
          response.writeHead(200, { "Content-Type": "text/event-stream" })
          chunk(response, { role: "assistant", reasoning_content: "Inspecting the current task state." })
          return
        }
        if (mode === "fail") {
          failedReplies++
          reply(response, "The next check will inspect the current state.")
          return
        }
        if (mode === "spawn") {
          const receipt = current.find((message) => message.role === "tool" && message.content?.includes('"agent_id"'))
          if (!receipt) reply(response, "", { name: "spawn_agent", args: { role: "verifier", task: "Inspect the assigned inputs and preserve the findings." } })
          else {
            workerID = JSON.parse(receipt.content ?? "{}").agent_id
            reply(response, "The existing worker continues under the assigned task.")
          }
          return
        }
        const snapshots = input.messages.flatMap((message) => {
          if (message.role !== "system") return []
          try {
            const parsed = JSON.parse(message.content ?? "")
            return parsed.source === "runtime_worker_roster" ? [parsed.workers as Worker[]] : []
          } catch { return [] }
        })
        if (snapshots.length !== 1 || snapshots[0].length !== 1 || snapshots[0][0].agent_id !== workerID || snapshots[0][0].status !== "done") {
          throw new Error("The next check must receive the same naturally finished worker")
        }
        rosters.push(snapshots[0])
        if (!called("wait_agents")) reply(response, "", { name: "wait_agents", args: { agent_ids: [workerID], timeout_s: 0 } })
        else if (!called("report_schedule")) reply(response, "", { name: "report_schedule", args: { findings: "The existing worker completed and its result was collected.", keep: false } })
        else reply(response, "The existing worker completed and its result was collected.")
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
    url: `http://127.0.0.1:${address.port}`, errors, rosters,
    state: () => ({ workerID, workerRequests, workerCancelled, released, failedReplies, managerBlocked }),
    failNextCheck: () => { mode = "fail" },
    blockNextCheck: () => { mode = "block" },
    releaseWorker: () => {
      if (!workerResponse) throw new Error("The worker stream has not started")
      released = true
      finish(workerResponse, "The assigned inputs were verified successfully.")
    },
    collectNextCheck: () => { mode = "collect" },
    close: () => new Promise<void>((resolve, reject) => {
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    }),
  }
}
