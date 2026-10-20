import { createServer, type ServerResponse } from "node:http"
import { fileURLToPath } from "node:url"

type Message = { role: string; content?: string; tool_calls?: { function: { name: string; arguments: string } }[] }
type Input = { messages: Message[]; tools?: { function: { name: string } }[] }

/** Real, keyless HTTP adapter fixture. All rejections pass through the engine's
 *  evidence, numeric and bounded NotesDraft gates. No UI state is fabricated. */
export async function startMemoryReviewFixture(port = 0) {
  const errors: string[] = []
  const calls: { review: boolean; attempt: number; authorization: boolean }[] = []
  let mode: "reject" | "repair" = "reject"
  let callID = 0
  let held: Promise<void> | undefined
  let release: (() => void) | undefined
  const reply = (response: ServerResponse, content: string, args?: object) => {
    const delta = args ? { role: "assistant", tool_calls: [{ index: 0, id: `review-fixture-${++callID}`, type: "function", function: { name: "apply_memory_review", arguments: JSON.stringify(args) } }] } : { role: "assistant", content }
    response.writeHead(200, { "Content-Type": "text/event-stream" })
    response.end(`data: ${JSON.stringify({ id: "review-fixture", object: "chat.completion.chunk", choices: [{ index: 0, delta, finish_reason: null }] })}\n\ndata: ${JSON.stringify({ id: "review-fixture", object: "chat.completion.chunk", choices: [{ index: 0, delta: {}, finish_reason: args ? "tool_calls" : "stop" }] })}\n\ndata: [DONE]\n\n`)
  }
  const endpoint = createServer((incoming, response) => {
    let body = ""
    incoming.setEncoding("utf8")
    incoming.on("data", (part: string) => { body += part })
    incoming.on("end", async () => {
      try {
        const input = JSON.parse(body) as Input
        const review = input.tools?.some((tool) => tool.function.name === "apply_memory_review") ?? false
        const attempt = input.messages.filter((message) => message.role === "assistant" && message.tool_calls?.some((call) => call.function.name === "apply_memory_review")).length + 1
        calls.push({ review, attempt, authorization: Boolean(incoming.headers.authorization) })
        if (!review) { reply(response, "The requested check is complete."); return }
        if (held) await held
        const source = input.messages.filter((message) => message.role === "user").map((message) => message.content ?? "").join("\n")
        const match = /\[seq=(\d+)[^\n]*\] human: ([^\n]+)/.exec(source)
        if (!match) throw new Error("Reviewer did not receive a sequence-bound human source")
        const evidence = [{ seq: Number(match[1]), quote: match[2] }]
        let changes: object[]
        if (attempt === 1) {
          changes = [{ target: "memory", action: "add", content: match[2], evidence: [{ seq: Number(match[1]), quote: "A quote absent from the source." }] }]
        } else if (mode === "repair") {
          changes = [{ target: "memory", action: "replace", old_text: "Saved convention:", content: match[2], evidence }]
        } else if (attempt === 2) {
          changes = [{ target: "memory", action: "add", content: `${match[2]} after 999 checks.`, evidence }]
        } else {
          changes = [{ target: "memory", action: "add", content: match[2], evidence }]
        }
        reply(response, "", { changes })
      } catch (error) {
        errors.push(String(error))
        response.writeHead(400, { "Content-Type": "application/json" })
        response.end(JSON.stringify({ error: { message: String(error), type: "invalid_request_error" } }))
      }
    })
  })
  await new Promise<void>((resolve, reject) => {
    endpoint.once("error", reject)
    endpoint.listen(port, "127.0.0.1", () => { endpoint.off("error", reject); resolve() })
  })
  const address = endpoint.address()
  if (!address || typeof address === "string") throw new Error("Memory fixture did not listen")
  return {
    url: `http://127.0.0.1:${address.port}`, errors, calls,
    setMode: (next: "reject" | "repair") => { mode = next },
    pauseReview: () => { held = new Promise<void>((resolve) => { release = resolve }) },
    resumeReview: () => { release?.(); held = undefined; release = undefined },
    close: () => new Promise<void>((resolve, reject) => {
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    }),
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const fixture = await startMemoryReviewFixture(Number(process.env.ZWAI_REVIEW_FIXTURE_PORT ?? 0))
  process.stdout.write(`${fixture.url}\n`)
}
