import { createServer, type ServerResponse } from "node:http"
import { fileURLToPath } from "node:url"

type Message = { role: string; content?: string; tool_calls?: { function: { name: string; arguments: string } }[] }
type Input = { messages: Message[]; tools?: { function: { name: string } }[]; stream?: boolean }
type ReviewIssue = {
  code: string; index: number; usage?: string; over_by?: number; current_entries?: string[]
  available_chars?: number; max_content_chars?: number; proposed_usage?: string
  repair?: { invalid_evidence?: { source_text?: string }[]; unsupported_numbers?: string[]; supported_numbers?: string[] }
}

export const capacityRepairCase = {
  related: "For audit exports only, the 64MiB cap is mandatory. Do not rewrite timestamps or remove owner IDs. Repeat: keep this export rule limited to audit exports.",
  unrelated: "Release branches require an independent review before merging.",
  request: "For audit exports only, encrypt exported files; retain the 64MiB cap and do not rewrite timestamps or remove owner IDs.",
  compacted: "Audit exports only: encrypt files; cap 64MiB; never rewrite timestamps or remove owner IDs.",
}

const managerAnswer = "The requested check is complete."

/** Real, keyless HTTP adapter fixture. All rejections pass through the engine's
 *  evidence, numeric and bounded NotesDraft gates. No UI state is fabricated. */
export async function startMemoryReviewFixture(port = 0) {
  const errors: string[] = []
  const calls: { review: boolean; attempt: number; authorization: boolean }[] = []
  let mode: "reject" | "repair" | "compact" = "reject"
  const repairReceipts: ReviewIssue[][] = []
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
        if (input.messages[0]?.role === "system" && input.messages[0].content?.startsWith("## Schedule answer claims")) {
          if (input.stream || input.messages.length !== 2 || input.tools?.length) throw new Error("Answer classification must remain a separate tool-free request")
          const candidate = JSON.parse(input.messages[1].content ?? "") as { messages?: unknown }
          if (!Array.isArray(candidate.messages) || candidate.messages.length !== 1 || candidate.messages[0] !== managerAnswer) throw new Error("Unknown answer classification candidate")
          response.writeHead(200, { "Content-Type": "application/json" })
          response.end(JSON.stringify({ choices: [{ index: 0, message: { role: "assistant", content: JSON.stringify({ claims: [] }) }, finish_reason: "stop" }] }))
          return
        }
        const review = input.tools?.some((tool) => tool.function.name === "apply_memory_review") ?? false
        const attempt = input.messages.filter((message) => message.role === "assistant" && message.tool_calls?.some((call) => call.function.name === "apply_memory_review")).length + 1
        calls.push({ review, attempt, authorization: Boolean(incoming.headers.authorization) })
        if (!review) { reply(response, managerAnswer); return }
        if (held) await held
        const source = input.messages.filter((message) => message.role === "user").map((message) => message.content ?? "").join("\n")
        const match = /\[seq=(\d+)[^\n]*\] human: ([^\n]+)/.exec(source)
        if (!match) throw new Error("Reviewer did not receive a sequence-bound human source")
        const evidence = [{ seq: Number(match[1]), quote: match[2] }]
        let changes: object[]
        if (mode === "compact") {
          if (match[2] !== capacityRepairCase.request) throw new Error("Unknown capacity-repair source")
          const previous = input.messages.filter((message) => message.role === "tool").at(-1)
          if (!previous) {
            changes = [{ target: "memory", action: "add", content: `${match[2]} Export cap 91MiB.`, evidence: [{ seq: Number(match[1]), quote: "An absent quotation." }, ...evidence] }]
          } else {
            const receipt = JSON.parse(previous.content ?? "") as { success?: boolean; issues?: ReviewIssue[] }
            if (receipt.success !== false || !Array.isArray(receipt.issues)) throw new Error("Missing aggregated repair receipt")
            const issues = receipt.issues
            const codes = issues.map((issue) => issue.code).sort()
            if (codes.join(",") !== "capacity,evidence,numeric" || issues.some((issue) => issue.index !== 0)) throw new Error("First refusal did not return every independent problem")
            const citation = issues.find((issue) => issue.code === "evidence")!
            if (!citation.repair?.invalid_evidence?.some((invalid) => invalid.source_text === match[2])) throw new Error("Repair lost the exact visible source")
            const numeric = issues.find((issue) => issue.code === "numeric")!
            if (numeric.repair?.unsupported_numbers?.join(",") !== "91MiB" || !numeric.repair?.supported_numbers?.includes("64MiB")) throw new Error("Numeric repair admitted unsupported spelling")
            const capacity = issues.find((issue) => issue.code === "capacity")!
            const entries = [capacityRepairCase.related, capacityRepairCase.unrelated]
            const limit = entries.join("\n\n").length
            const growth = `${match[2]} Export cap 91MiB.`.length + 2
            if (!capacity.current_entries || capacity.usage !== `${limit}/${limit}` || capacity.proposed_usage !== `${limit + growth}/${limit}` || capacity.over_by !== growth || capacity.available_chars !== 0 || capacity.max_content_chars !== 0 || JSON.stringify(capacity.current_entries) !== JSON.stringify(entries)) throw new Error("Capacity repair lacks exact candidate and available-space accounting")
            if (capacityRepairCase.compacted.length >= capacityRepairCase.related.length) throw new Error("Repair did not free related memory")
            repairReceipts.push(issues)
            changes = [{ target: "memory", action: "replace", old_text: capacity.current_entries[0], content: capacityRepairCase.compacted, evidence }]
          }
        } else if (attempt === 1) {
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
    url: `http://127.0.0.1:${address.port}`, errors, calls, repairReceipts,
    setMode: (next: "reject" | "repair" | "compact") => { mode = next },
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
