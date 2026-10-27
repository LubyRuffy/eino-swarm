import { createServer, type ServerResponse } from "node:http"

export const prematureWorkerAnswer = "The delegated operation is complete."
export const collectedWorkerAnswer = "The original delegated result has been collected."
export const workerCompletionResult = "The original worker verified the assigned inputs."

type Input = { messages: { role: string; content?: string; tool_call_id?: string }[]; stream?: boolean; tools?: { function: { name: string } }[] }

// The worker owns one real HTTP stream across the manager's premature yield.
// Release is controlled by observed UI state, never by matching the task text.
export async function startWorkerCompletionFixture(options: { repeatFinal?: boolean } = {}) {
  const errors: string[] = []
  let managerRequests = 0
  let workerRequests = 0
  let cancelled = false
  let workerID = ""
  let allowResult!: () => void
  let entered!: () => void
  let resultReleased = false
  const workerEntered = new Promise<void>((resolve) => { entered = resolve })
  const result = new Promise<void>((resolve) => { allowResult = () => { resultReleased = true; resolve() } })
  const reply = (res: ServerResponse, delta: object, call = false) => {
    if (!res.headersSent) res.writeHead(200, { "Content-Type": "text/event-stream" })
    res.end(`data: ${JSON.stringify({ choices: [{ index: 0, delta, finish_reason: null }] })}\n\ndata: ${JSON.stringify({ choices: [{ index: 0, delta: {}, finish_reason: call ? "tool_calls" : "stop" }] })}\n\ndata: [DONE]\n\n`)
  }
  const tool = (res: ServerResponse, id: string, name: string, args: object) => reply(res, {
    role: "assistant", tool_calls: [{ index: 0, id, type: "function", function: { name, arguments: JSON.stringify(args) } }],
  }, true)
  const endpoint = createServer((req, res) => {
    let raw = ""
    req.setEncoding("utf8")
    req.on("data", (part: string) => { raw += part })
    req.on("end", async () => {
      try {
        if (req.headers.authorization) throw new Error("Worker completion fixture must be keyless")
        const input = JSON.parse(raw) as Input
        if (input.messages[0]?.content?.startsWith("## Schedule answer claims")) {
          if (input.stream || input.tools?.length) throw new Error("Admission policy acquired tools or streaming")
          res.writeHead(200, { "Content-Type": "application/json" })
          res.end(JSON.stringify({ choices: [{ message: { role: "assistant", content: '{"claims":[]}' }, finish_reason: "stop" }] }))
          return
        }
        if (!input.tools?.some((entry) => entry.function.name === "spawn_agent")) {
          workerRequests += 1
          if (workerRequests !== 1) throw new Error("The worker was replaced instead of collected")
          res.writeHead(200, { "Content-Type": "text/event-stream" })
          res.write(`data: ${JSON.stringify({ choices: [{ index: 0, delta: { role: "assistant", reasoning_content: "Inspecting the assigned inputs." }, finish_reason: null }] })}\n\n`)
          res.on("close", () => { if (!resultReleased) cancelled = true })
          entered()
          await result
          if (!res.destroyed) reply(res, { content: workerCompletionResult })
          return
        }
        managerRequests += 1
        if (managerRequests === 1) {
          tool(res, "spawn-original-worker", "spawn_agent", { role: "verifier", task: "Inspect the assigned inputs." })
        } else if (managerRequests === 2) {
          workerID = JSON.parse(input.messages.find((message) => message.tool_call_id === "spawn-original-worker")?.content ?? "{}").agent_id ?? ""
          if (!workerID) throw new Error("Actual spawn receipt has no ID")
          await workerEntered
          reply(res, { role: "assistant", content: prematureWorkerAnswer })
        } else if (managerRequests === 3) {
          if (!input.messages.some((message) => message.role === "user" && message.content?.startsWith("The manager's completion attempt was withheld because worker results have not been collected"))) throw new Error("Missing runtime collection cue")
          if (input.messages.some((message) => message.role === "assistant" && message.content?.includes(prematureWorkerAnswer))) throw new Error("Rejected prose survived into the next input")
          if (options.repeatFinal) reply(res, { role: "assistant", content: prematureWorkerAnswer })
          else tool(res, "collect-original-worker", "wait_agents", { agent_ids: [workerID], timeout_s: 30 })
        } else if (managerRequests === 4 && options.repeatFinal) {
          if (!resultReleased) throw new Error("The manager resumed before the worker finished")
          tool(res, "collect-original-worker", "wait_agents", { agent_ids: [workerID], timeout_s: 30 })
        } else if (managerRequests === (options.repeatFinal ? 5 : 4)) {
          const receipt = JSON.parse(input.messages.find((message) => message.tool_call_id === "collect-original-worker")?.content ?? "{}")
          if (receipt.agents?.length !== 1 || receipt.agents[0].agent_id !== workerID || receipt.agents[0].status !== "done" || receipt.agents[0].result !== workerCompletionResult) throw new Error("The original worker result was not collected")
          reply(res, { role: "assistant", content: collectedWorkerAnswer })
        } else throw new Error(`Unbounded manager correction: ${managerRequests}`)
      } catch (error) {
        errors.push(String(error))
        if (!res.headersSent) res.writeHead(400, { "Content-Type": "application/json" })
        res.end(JSON.stringify({ error: { message: String(error), type: "invalid_request_error" } }))
      }
    })
  })
  await new Promise<void>((resolve, reject) => { endpoint.once("error", reject); endpoint.listen(0, "127.0.0.1", resolve) })
  const address = endpoint.address()
  if (!address || typeof address === "string") throw new Error("Worker fixture did not listen")
  return { url: `http://127.0.0.1:${address.port}`, errors, allowResult,
    state: () => ({ managerRequests, workerRequests, workerID, cancelled }),
    close: () => new Promise<void>((resolve, reject) => {
      allowResult()
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    }),
  }
}
