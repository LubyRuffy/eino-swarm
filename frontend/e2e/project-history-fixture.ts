import { createServer, type ServerResponse } from "node:http"

type Call = { id: string; function: { name: string; arguments: string } }
type Message = { role: string; content?: string; tool_call_id?: string; tool_calls?: Call[] }
type Input = { messages: Message[]; tools?: { function: { name: string } }[]; stream?: boolean; tool_choice?: string }
type HistoryResult = { ok?: boolean; columns?: string[]; rows?: Record<string, unknown>[]; row_count?: number; truncated?: boolean; context?: { project_id?: string; thread_id?: string }; error?: { code?: string; message?: string } }

// This endpoint exercises the real model adapter and tool executor. It is a
// protocol fixture, not evidence that a model can interpret project history.
export async function startProjectHistoryFixture({ repairWrite = false, omitProjectFilter = false }: { repairWrite?: boolean; omitProjectFilter?: boolean } = {}) {
  const errors: string[] = []
  const requests: Input[] = []
  const sourceReplies = new Map<string, string>()
  const knownAnswers = new Set<string>(["History query tool is unavailable."])
  const receipts: HistoryResult[] = []
  let projectID = ""
  let threadID = ""
  let finalAnswer = ""
  let finalSourceTexts: string[] = []
  let sourceIDs: string[] = []
  const reply = (response: ServerResponse, input: Input, delta: object, finish = "stop") => {
    if (!input.stream) {
      response.writeHead(200, { "Content-Type": "application/json" })
      response.end(JSON.stringify({ choices: [{ index: 0, message: delta, finish_reason: finish }] }))
      return
    }
    response.writeHead(200, { "Content-Type": "text/event-stream" })
    response.end(`data: ${JSON.stringify({ id: "project-history-fixture", choices: [{ index: 0, delta, finish_reason: null }] })}\n\ndata: ${JSON.stringify({ id: "project-history-fixture", choices: [{ index: 0, delta: {}, finish_reason: finish }] })}\n\ndata: [DONE]\n\n`)
  }
  const call = (response: ServerResponse, input: Input, id: string, args: object) => reply(response, input, {
    role: "assistant", tool_calls: [{ index: 0, id, type: "function", function: { name: "query_history", arguments: JSON.stringify(args) } }],
  }, "tool_calls")
  const querySources = (response: ServerResponse, input: Input) => call(response, input, "history-project-rows", {
    sql: `SELECT t.id AS thread_id, m.id AS message_id, m.seq, m.role, m.content, m.created_at, m.source, m.reasoning, t.goal FROM messages m JOIN threads t ON t.id = m.thread_id WHERE ${omitProjectFilter ? "" : "t.project_id = ? AND "}t.id <> ? AND m.role = 'user' ORDER BY t.created_at, m.seq`,
    params: omitProjectFilter ? [threadID] : [projectID, threadID],
  })
  const endpoint = createServer((incoming, response) => {
    let raw = ""
    incoming.setEncoding("utf8")
    incoming.on("data", (part: string) => { raw += part })
    incoming.on("end", () => {
      try {
        if (incoming.headers.authorization) throw new Error("History fixture must be keyless")
        const input = JSON.parse(raw) as Input
        const system = input.messages[0]?.content ?? ""
        if (system.startsWith("## Schedule answer claims")) {
          if (input.stream || input.tools?.length || input.tool_choice !== "none" || input.messages.length !== 2) throw new Error("Unexpected policy protocol")
          const source = JSON.parse(input.messages[1]?.content ?? "{}") as { messages?: unknown[] }
          const candidate = source.messages?.[0]
          const hostReport = typeof candidate === "string" && finalSourceTexts.length === 2 && finalSourceTexts.every((text) => candidate.includes(text)) && sourceIDs.every((id) => candidate.includes(id) || candidate.includes(id.replaceAll("_", "\\_"))) && candidate.includes("原文证据") && [...sourceReplies.keys()].every((text) => finalSourceTexts.includes(text) || !candidate.includes(text))
          if (!Array.isArray(source.messages) || source.messages.length !== 1 || typeof candidate !== "string" || !knownAnswers.has(candidate) && !hostReport) throw new Error("Unknown answer cannot be approved by this fixture")
          reply(response, input, { role: "assistant", content: JSON.stringify({ claims: [] }) })
          return
        }
        const lastUser = input.messages.findLast((message) => message.role === "user")?.content ?? ""
        const seeded = sourceReplies.get(lastUser)
        if (seeded) {
          reply(response, input, { role: "assistant", content: seeded })
          return
        }
        requests.push(input)
        if (!input.tools?.some((tool) => tool.function.name === "query_history")) {
          errors.push("query_history is absent from the actual manager HTTP request")
          reply(response, input, { role: "assistant", content: "History query tool is unavailable." })
          return
        }
        if (!projectID || !threadID || !system.includes(projectID) || !system.includes(threadID)) throw new Error("Current project/thread identity did not reach model input")
        const latest = input.messages.findLast((message) => message.role === "tool")
        if (!latest) {
          call(response, input, "history-schema", {})
          return
        }
        const result = JSON.parse(latest.content ?? "{}") as HistoryResult
        receipts.push(result)
        if (result.context?.project_id !== projectID || result.context.thread_id !== threadID) throw new Error("History result has the wrong conversation context")
        if (latest.tool_call_id === "history-write-refused") {
          if (!repairWrite || result.ok !== false || result.error?.code !== "query_failed" || !result.error.message) throw new Error("The unsafe query did not return a repairable read-only refusal")
          querySources(response, input)
          return
        }
        if (result.ok !== true) throw new Error(`History query failed: ${latest.content}`)
        if (latest.tool_call_id === "history-schema") {
          if (!JSON.stringify(result).includes("messages") || !JSON.stringify(result).includes("project_id")) throw new Error("History schema omitted raw transcript relations")
          if (repairWrite) call(response, input, "history-write-refused", { sql: "DELETE FROM messages WHERE thread_id = ?", params: [sourceIDs[0]] })
          else querySources(response, input)
          return
        }
        if (latest.tool_call_id !== "history-project-rows") throw new Error("Unexpected tool result identity")
        if (result.truncated || result.row_count !== 2 || !Array.isArray(result.rows) || result.rows.length !== 2) throw new Error("Expected two complete raw source rows")
        const columns = result.columns ?? []
        const expectedColumns = ["thread_id", "message_id", "seq", "role", "content", "created_at", "source", "reasoning", "goal"]
        if (columns.length !== expectedColumns.length || columns.some((name, index) => name !== expectedColumns[index])) throw new Error("Query columns no longer identify the selected original fields")
        const records = result.rows
        if (records.some((row) => !row || typeof row !== "object" || Array.isArray(row) || Object.keys(row).length !== columns.length || columns.some((column) => !Object.hasOwn(row, column)))) throw new Error("History rows must preserve every selected column as an object field")
        if (records.some((row) => !sourceIDs.includes(String(row.thread_id)))) throw new Error("Query returned another project's conversation")
        if (new Set(records.map((row) => row.thread_id)).size !== sourceIDs.length || records.some((row) => row.role !== "user" || Number(row.message_id) <= 0 || Number(row.seq) <= 0 || typeof row.content !== "string" || !row.created_at)) throw new Error("Query did not return distinct original source identities")
        // Each source starts with one human message: its per-thread seq is
        // shared, while the database message primary keys must stay distinct.
        if (records.some((row) => row.seq !== 1) || new Set(records.map((row) => row.message_id)).size !== sourceIDs.length) throw new Error("Global message keys were confused with per-conversation sequence numbers")
        if (records.some((row) => !sourceReplies.has(String(row.content)))) throw new Error("Query did not preserve the complete original source text")
        if (records.some((row) => row.source !== "human" || row.reasoning !== "" || row.goal !== "")) throw new Error("Query did not preserve the original empty text fields and human attribution")
        finalSourceTexts = records.map((row) => String(row.content))
        finalAnswer = JSON.stringify({ version: 1, language: "zh", sources: records.map((row, index) => ({ key: `original-${index + 1}`, source: { kind: "message", thread_id: String(row.thread_id), seq: row.seq } })), proposals: [], questions: [] })
        knownAnswers.add(finalAnswer)
        reply(response, input, { role: "assistant", content: finalAnswer })
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
  if (!address || typeof address === "string") throw new Error("History fixture did not listen")
  return {
    url: `http://127.0.0.1:${address.port}`, errors, requests, receipts,
    addSource: (text: string) => {
      const answer = "已记录这项项目要求。"
      sourceReplies.set(text, answer)
      knownAnswers.add(answer)
    },
    target: (project: string, thread: string, sources: string[]) => { projectID = project; threadID = thread; sourceIDs = sources },
    answer: () => finalAnswer,
    close: () => new Promise<void>((resolve, reject) => {
      endpoint.close((error) => error ? reject(error) : resolve())
      endpoint.closeAllConnections()
    }),
  }
}
