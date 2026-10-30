import { createServer, type ServerResponse } from "node:http"
import { setTimeout as delay } from "node:timers/promises"

export const unsupportedScheduleAnswer = "A new future check is already armed every 90 seconds."
export const verifiedScheduleAnswer = "A recurring check is confirmed for every 90 seconds."

type Input = { messages: { role: string; content?: string }[]; stream?: boolean; tools?: unknown[]; tool_choice?: string }

// An actor/step protocol, independent of the human task. The correction waits
// at the fixture boundary until the browser has observed its live SSE notice.
export async function startScheduleAnswerFixture({ secondPolicyDelayMS = 0, secondPolicyFailures = [], unavailable = false, uncorrected = false }: {
  secondPolicyDelayMS?: number
  secondPolicyFailures?: number[]
  unavailable?: boolean
  uncorrected?: boolean
} = {}) {
  const errors: string[] = []
  let managerRequests = 0
  let checks = 0
  let releaseArm!: () => void
  const arm = new Promise<void>((resolve) => { releaseArm = resolve })
  const response = (res: ServerResponse, delta: object, call = false) => {
    res.writeHead(200, { "Content-Type": "text/event-stream" })
    res.end(`data: ${JSON.stringify({ id: "answer-fixture", choices: [{ index: 0, delta, finish_reason: null }] })}\n\ndata: ${JSON.stringify({ id: "answer-fixture", choices: [{ index: 0, delta: {}, finish_reason: call ? "tool_calls" : "stop" }] })}\n\ndata: [DONE]\n\n`)
  }
  const policy = (res: ServerResponse, content: object) => {
    res.writeHead(200, { "Content-Type": "application/json" })
    res.end(JSON.stringify({ choices: [{ index: 0, message: { role: "assistant", content: JSON.stringify(content) }, finish_reason: "stop" }] }))
  }
  const endpoint = createServer((req, res) => {
    let raw = ""
    req.setEncoding("utf8")
    req.on("data", (part: string) => { raw += part })
    req.on("end", async () => {
      try {
        if (req.headers.authorization) throw new Error("The offline answer fixture must be keyless")
        const input = JSON.parse(raw) as Input
        const system = input.messages[0]?.content ?? ""
        if (system.startsWith("## Schedule answer claims")) {
          if (input.stream || input.tools?.length || input.tool_choice !== "none" || input.messages.length !== 2 || input.messages[0].role !== "system" || input.messages[1].role !== "user") throw new Error("Claim policy must remain a separate read-only request")
          checks += 1
          const failureStatus = unavailable ? 503 : checks > 1 ? secondPolicyFailures[checks - 2] : undefined
          if (failureStatus) {
            res.writeHead(failureStatus, { "Content-Type": "application/json" })
            res.end(JSON.stringify({ error: { message: "The offline classification service is temporarily unavailable.", type: "service_unavailable" } }))
            return
          }
          // Delay headers to exercise the configured non-streaming first-byte
          // budget, independently of the shorter response-body idle budget.
          if (checks === secondPolicyFailures.length + 2 && secondPolicyDelayMS > 0) await delay(secondPolicyDelayMS)
          policy(res, { claims: [{ quote: checks === 1 || uncorrected ? unsupportedScheduleAnswer : verifiedScheduleAnswer, scope: "unspecified", new: true, every_s: 90,
            stance: "affirmed", temporal: "current", attribution: "assistant" }] })
          return
        }
        if (system.startsWith("## Thread wake authorization classification")) {
          if (input.stream || input.tools?.length || input.messages.length !== 2) throw new Error("Wake policy must remain a separate read-only request")
          policy(res, { decision: "unchanged", quote: "" })
          return
        }
        managerRequests += 1
        if (uncorrected) {
          if (managerRequests > 6) throw new Error(`Unbounded goal correction: ${managerRequests}`)
          if (managerRequests === 2) await arm
          response(res, { role: "assistant", content: unsupportedScheduleAnswer })
          return
        }
        if (managerRequests === 1) {
          response(res, { role: "assistant", content: unsupportedScheduleAnswer })
        } else if (managerRequests === 2) {
          await arm
          response(res, { role: "assistant", tool_calls: [{ index: 0, id: "arrange-confirmed-check", type: "function", function: {
            name: "schedule_wake", arguments: JSON.stringify({ title: "Current task verification", prompt: "Inspect the current task state.", every_s: 90 }),
          } }] }, true)
        } else if (managerRequests === 3) {
          response(res, { role: "assistant", content: verifiedScheduleAnswer })
        } else { throw new Error(`Unbounded manager correction: ${managerRequests}`) }
      } catch (error) {
        errors.push(String(error))
        if (!res.headersSent) res.writeHead(400, { "Content-Type": "application/json" })
        res.end(JSON.stringify({ error: { message: String(error), type: "invalid_request_error" } }))
      }
    })
  })
  await new Promise<void>((resolve, reject) => {
    endpoint.once("error", reject)
    endpoint.listen(0, "127.0.0.1", () => { endpoint.off("error", reject); resolve() })
  })
  const address = endpoint.address()
  if (!address || typeof address === "string") throw new Error("Offline fixture did not listen")
  return { url: `http://127.0.0.1:${address.port}`, errors, allowArm: releaseArm,
    state: () => ({ managerRequests, checks }),
    close: () => new Promise<void>((resolve, reject) => {
      releaseArm()
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    }),
  }
}
