import { cloudToolDefinitions, type CloudToolCall } from "./cloud-web-tools"
import { ModelCallError, type ApiStyle, type StreamPiece } from "./openai-wire"

const maxArguments = 64 << 10
const maxCallsPerRound = 4
const maxTerminalBytes = 1 << 20
type Call = CloudToolCall & { itemID?: string }
export type ToolRound = { calls: CloudToolCall[]; responseID: string; output: Record<string, unknown>[] }

/** Observe complete function arguments without executing a partial model round. */
export class ToolRoundCollector {
  private calls = new Map<number, Call>()
  private itemIndexes = new Map<string, number>()
  private terminal = false
  private finished = false
  private incomplete = false
  private responseID = ""
  private output: Record<string, unknown>[] = []
  seen = false
  get ended() { return this.finished }

  constructor(private style: ApiStyle) {}

  accept(data: string) {
    if (data.trim() === "[DONE]") { this.terminal = true; this.finished = true; return }
    let value: unknown
    try { value = JSON.parse(data) } catch { return }
    const root = record(value)
    if (!root) return
    if (this.terminal) {
      if (containsFunctionData(root)) fail("cloud_tools_incomplete")
      return
    }
    if (this.style === "chat") this.identity(root.id)
    else this.identity(record(root.response)?.id)
    if (this.style === "chat") this.chat(root)
    else this.responses(root, new TextEncoder().encode(data).length)
  }

  finish(): ToolRound {
    if (this.calls.size && (!this.terminal || this.incomplete)) fail("cloud_tools_incomplete")
    const calls = [...this.calls.entries()].sort(([a], [b]) => a - b).map(([, call]) => {
      if (!call.id || call.id.length > 256 || /\s/.test(call.id) || !["web_search", "web_fetch"].includes(call.name) || !call.arguments) fail("cloud_tools_arguments")
      return { id: call.id, name: call.name, arguments: call.arguments }
    })
    if (new Set(calls.map((call) => call.id)).size !== calls.length) fail("cloud_tools_arguments")
    return { calls, responseID: this.responseID, output: this.output }
  }

  private chat(root: Record<string, unknown>) {
    const choices = root.choices
    if (!Array.isArray(choices) || !choices.length) return
    const choice = record(choices[0])
    const delta = record(choice?.delta)
    const message = record(choice?.message)
    const rawCalls = delta?.tool_calls ?? message?.tool_calls
    if (Array.isArray(rawCalls)) {
      this.seen = true
      rawCalls.forEach((raw, position) => {
        const row = record(raw)
        if (!row || (row.type != null && row.type !== "function")) fail("cloud_tools_arguments")
        const index = delta ? row.index : position
        if (!Number.isInteger(index) || Number(index) < 0 || Number(index) >= maxCallsPerRound) fail("cloud_tools_limit")
        const fn = record(row.function)
        if (!fn) fail("cloud_tools_arguments")
        const call = this.obtain(Number(index))
        this.field(call, "id", row.id, Boolean(message))
        this.field(call, "name", fn.name, Boolean(message))
        this.arguments(call, fn.arguments, Boolean(message))
      })
    }
    if (typeof choice?.finish_reason === "string" && choice.finish_reason) {
      this.terminal = true
      this.incomplete = choice.finish_reason !== "stop" && choice.finish_reason !== "tool_calls"
    }
    if (message) { this.terminal = true; this.finished = true }
  }

  private responses(root: Record<string, unknown>, bytes: number) {
    const type = root.type
    if (type === "response.incomplete" || type === "response.failed") { this.incomplete = true; this.terminal = true; this.finished = true }
    if (type === "response.output_item.added" || type === "response.output_item.done") {
      const item = record(root.item)
      if (item?.type === "function_call") this.responseCall(item, root.output_index, type.endsWith(".done"))
    } else if (type === "response.function_call_arguments.delta" || type === "response.function_call_arguments.done") {
      this.seen = true
      const index = typeof root.item_id === "string" ? this.itemIndexes.get(root.item_id) : undefined
      const target = index ?? root.output_index
      if (!Number.isInteger(target) || Number(target) < 0 || Number(target) > 255) fail("cloud_tools_arguments")
      const call = this.obtain(Number(target))
      this.arguments(call, type.endsWith(".done") ? root.arguments : root.delta, type.endsWith(".done"))
    }
    const response = type === "response.completed" ? record(root.response) : (Array.isArray(root.output) ? root : null)
    if (!response) return
    this.terminal = true
    this.finished = true
    if (response.status != null && response.status !== "completed") this.incomplete = true
    this.identity(response.id)
    if (!Array.isArray(response.output)) {
      if (this.calls.size) fail("cloud_tools_incomplete")
      return
    }
    if (!this.calls.size && !response.output.some((item) => record(item)?.type === "function_call")) return
    if (bytes > maxTerminalBytes) fail("cloud_tools_output_limit")
    const priorIndexes = [...this.calls.keys()]
    const finalIndexes = new Set<number>()
    this.output = response.output.map(record).filter((item): item is Record<string, unknown> => Boolean(item))
    this.output.forEach((item, index) => {
      if (item.type === "function_call") {
        finalIndexes.add(index)
        if (typeof item.arguments !== "string" || typeof item.name !== "string" || typeof item.call_id !== "string") fail("cloud_tools_arguments")
        this.responseCall(item, index, true)
      }
    })
    if (priorIndexes.some((index) => !finalIndexes.has(index))) fail("cloud_tools_incomplete")
  }

  private identity(value: unknown) {
    if (value == null) return
    if (typeof value !== "string" || !value || value.length > 256 || /\s/.test(value) || this.responseID && this.responseID !== value) fail("cloud_tools_arguments")
    this.responseID = value
  }

  private responseCall(item: Record<string, unknown>, index: unknown, snapshot: boolean) {
    this.seen = true
    if (!Number.isInteger(index) || Number(index) < 0 || Number(index) > 255) fail("cloud_tools_arguments")
    const call = this.obtain(Number(index))
    this.field(call, "id", item.call_id, true)
    this.field(call, "name", item.name, true)
    if (typeof item.id === "string") this.itemIndexes.set(item.id, Number(index))
    this.arguments(call, item.arguments, snapshot)
  }

  private obtain(index: number): Call {
    const existing = this.calls.get(index)
    if (existing) return existing
    if (this.calls.size >= maxCallsPerRound) fail("cloud_tools_limit")
    const call = { id: "", name: "", arguments: "" }
    this.calls.set(index, call)
    return call
  }

  private field(call: Call, key: "id" | "name", value: unknown, snapshot: boolean) {
    if (value == null) return
    if (typeof value !== "string" || value.length > 256) fail("cloud_tools_arguments")
    if (snapshot) {
      if (call[key] && call[key] !== value) fail("cloud_tools_arguments")
      call[key] = value
    } else if (!call[key] || call[key] !== value) call[key] += value
    if (call[key].length > 256) fail("cloud_tools_arguments")
  }

  private arguments(call: Call, value: unknown, snapshot: boolean) {
    if (value == null) return
    if (typeof value !== "string") fail("cloud_tools_arguments")
    if (snapshot) {
      if (call.arguments && !value.startsWith(call.arguments)) fail("cloud_tools_arguments")
      call.arguments = value
    } else call.arguments += value
    if (new TextEncoder().encode(call.arguments).length > maxArguments) fail("cloud_tools_arguments_limit")
  }
}

export class ToolContinuation {
  private chat: Record<string, unknown>[] = []
  private responses: Record<string, unknown>[] = []
  private previousID = ""
  private previousOutputs: Record<string, unknown>[] = []
  private receipts = new Set<string>()
  private seenCallIDs = new Set<string>()
  private unidentifiedCallIDs = new Set<string>()
  calls = 0

  assertFresh(style: ApiStyle, round: ToolRound) {
    if (round.calls.some((call) => {
      const callID = JSON.stringify([style, call.id])
      return this.receipts.has(this.receipt(style, round, call)) || this.unidentifiedCallIDs.has(callID) || !round.responseID && this.seenCallIDs.has(callID)
    })) fail("cloud_tools_replay")
  }

  request(body: Record<string, unknown>, style: ApiStyle) {
    body.tools = cloudToolDefinitions(style)
    body.tool_choice = "auto"
    if (style === "chat") body.messages = [...body.messages as unknown[], ...this.chat]
    else if (this.previousID) {
      body.previous_response_id = this.previousID
      body.input = this.previousOutputs
    } else body.input = [...body.input as unknown[], ...this.responses]
  }

  append(style: ApiStyle, round: ToolRound, piece: StreamPiece, outputs: string[]) {
    for (const call of round.calls) {
      this.receipts.add(this.receipt(style, round, call))
      const callID = JSON.stringify([style, call.id])
      this.seenCallIDs.add(callID)
      if (!round.responseID) this.unidentifiedCallIDs.add(callID)
    }
    this.calls += round.calls.length
    this.chat.push({ role: "assistant", content: piece.text || null, ...(piece.reasoning ? { reasoning_content: piece.reasoning } : {}), tool_calls: round.calls.map((call) => ({ id: call.id, type: "function", function: { name: call.name, arguments: call.arguments } })) })
    this.chat.push(...round.calls.map((call, index) => ({ role: "tool", tool_call_id: call.id, content: outputs[index] })))
    if (style !== "responses") return
    this.previousOutputs = round.calls.map((call, index) => ({ type: "function_call_output", call_id: call.id, output: outputs[index] }))
    this.previousID = round.responseID
    this.responses.push(...(round.output.length ? round.output : round.calls.map((call) => ({ type: "function_call", call_id: call.id, name: call.name, arguments: call.arguments }))), ...this.previousOutputs)
  }

  private receipt(style: ApiStyle, round: ToolRound, call: CloudToolCall): string {
    // Without a response identity, reusing a call ID in this turn is ambiguous.
    // Distinct known responses may legitimately choose the same call ID.
    return JSON.stringify([style, round.responseID || null, call.id])
  }
}

function record(value: unknown): Record<string, unknown> | null {
  return value != null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null
}
function containsFunctionData(root: Record<string, unknown>): boolean {
  if (root.type === "response.function_call_arguments.delta" || root.type === "response.function_call_arguments.done") return true
  if (record(root.item)?.type === "function_call") return true
  if (Array.isArray(record(root.response)?.output) && (record(root.response)!.output as unknown[]).some((item) => record(item)?.type === "function_call")) return true
  if (!Array.isArray(root.choices)) return false
  return root.choices.some((choice) => {
    const row = record(choice)
    return Array.isArray(record(row?.delta)?.tool_calls) || Array.isArray(record(row?.message)?.tool_calls)
  })
}
function fail(code: string): never { throw new ModelCallError(code, "parse") }
