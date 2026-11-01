import { describe, expect, it } from "vitest"
import { ToolContinuation, ToolRoundCollector } from "./openai-tool-wire"

describe("complete cloud function rounds", () => {
  it("assembles fragmented Chat identities and arguments before admitting a call", () => {
    const collector = new ToolRoundCollector("chat")
    collector.accept(JSON.stringify({ choices: [{ delta: { tool_calls: [{ index: 0, id: "call-", function: { name: "web_", arguments: '{"que' } }] } }] }))
    collector.accept(JSON.stringify({ choices: [{ delta: { tool_calls: [{ index: 0, id: "one", function: { name: "search", arguments: 'ry":"x"}' } }] }, finish_reason: "tool_calls" }] }))
    expect(collector.finish().calls).toEqual([{ id: "call-one", name: "web_search", arguments: '{"query":"x"}' }])
  })

  it("reconciles Responses deltas and final snapshots without duplicating calls or arguments", () => {
    const collector = new ToolRoundCollector("responses")
    collector.accept(JSON.stringify({ type: "response.output_item.added", output_index: 1, item: { type: "function_call", id: "item", call_id: "call", name: "web_fetch", arguments: "" } }))
    collector.accept(JSON.stringify({ type: "response.function_call_arguments.delta", item_id: "item", delta: '{"url":' }))
    collector.accept(JSON.stringify({ type: "response.function_call_arguments.delta", item_id: "item", delta: '"https://public.invalid/"}' }))
    collector.accept(JSON.stringify({ type: "response.completed", response: { id: "response", status: "completed", output: [{ type: "reasoning", id: "reason", summary: [] }, { type: "function_call", id: "item", call_id: "call", name: "web_fetch", arguments: '{"url":"https://public.invalid/"}' }] } }))
    expect(collector.finish().calls).toEqual([{ id: "call", name: "web_fetch", arguments: '{"url":"https://public.invalid/"}' }])
  })

  it.each(["truncated", "unknown", "oversized", "duplicate", "finish-limit", "invalid-field"])("fails closed on %s Chat calls", (mode) => {
    const collector = new ToolRoundCollector("chat")
    const call = { index: 0, id: "call", type: "function", function: { name: mode === "unknown" ? "exec" : "web_search", arguments: mode === "oversized" ? "x".repeat((64 << 10) + 1) : mode === "invalid-field" ? {} : '{"query":"x"}' } }
    const action = () => {
      collector.accept(JSON.stringify({ choices: [{ delta: { tool_calls: mode === "duplicate" ? [call, { ...call, index: 1 }] : [call] }, ...(mode === "truncated" ? {} : { finish_reason: mode === "finish-limit" ? "length" : "tool_calls" }) }] }))
      collector.finish()
    }
    expect(action).toThrow()
  })

  it("does not execute a partial Responses function round at EOF", () => {
    const collector = new ToolRoundCollector("responses")
    collector.accept(JSON.stringify({ type: "response.output_item.added", output_index: 0, item: { type: "function_call", id: "item", call_id: "call", name: "web_search", arguments: '{"query":"x"}' } }))
    expect(() => collector.finish()).toThrow("cloud_tools_incomplete")
  })

  it("uses Responses state when provided and portable function output history otherwise", () => {
    for (const responseID of ["", "previous-response"]) {
      const continuation = new ToolContinuation()
      const calls = [{ id: "call", name: "web_search", arguments: '{"query":"x"}' }]
      continuation.append("responses", { calls, responseID, output: [] }, { text: "", reasoning: "" }, ["evidence"])
      const body: Record<string, unknown> = { input: [{ role: "user", content: "x" }] }
      continuation.request(body, "responses")
      if (responseID) {
        expect(body.previous_response_id).toBe(responseID)
        expect(body.input).toEqual([{ type: "function_call_output", call_id: "call", output: "evidence" }])
      } else {
        expect(body.previous_response_id).toBeUndefined()
        expect(body.input).toEqual(expect.arrayContaining([expect.objectContaining({ type: "function_call", call_id: "call" }), expect.objectContaining({ type: "function_call_output", call_id: "call" })]))
      }
      expect(body.tools).toEqual(expect.arrayContaining([expect.objectContaining({ type: "function", name: "web_fetch" })]))
    }
  })
})
