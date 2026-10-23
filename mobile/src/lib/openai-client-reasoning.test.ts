import { describe, expect, it, vi } from "vitest"

import { blankProvider } from "./direct-provider"
import { streamCompletion } from "./openai-client"
import type { StreamPiece } from "./openai-wire"

const thought = "检查当前证据 before answering."
const answer = "Verified answer."
const cases = [
  { name: "legacy field", fields: { reasoning_content: thought } },
  { name: "current field", fields: { reasoning: thought } },
  { name: "empty legacy fallback", fields: { reasoning_content: "", reasoning: thought } },
  { name: "null legacy fallback", fields: { reasoning_content: null, reasoning: thought } },
  { name: "empty current field", fields: { reasoning_content: thought, reasoning: "" } },
  { name: "null current field", fields: { reasoning_content: thought, reasoning: null } },
  { name: "legacy precedence", fields: { reasoning_content: thought, reasoning: "Unused alternative thought." } },
  { name: "mirrored aliases", fields: { reasoning_content: thought, reasoning: thought } },
]

function fragmentedResponse(raw: string, contentType: string): Response {
  const bytes = new TextEncoder().encode(raw)
  return new Response(new ReadableStream({
    start(controller) {
      // Split both JSON field names and multibyte text across transport reads.
      for (let offset = 0; offset < bytes.length; offset += 7) {
        controller.enqueue(bytes.slice(offset, offset + 7))
      }
      controller.close()
    },
  }), { headers: { "Content-Type": contentType } })
}

for (const wire of ["SSE", "JSON"] as const) {
  describe(`direct chat reasoning through the ${wire} HTTP reader`, () => {
    it.each(cases)("shows $name before the answer and retains it once", async ({ fields }) => {
      const message = { role: "assistant", content: answer, ...fields }
      const raw = wire === "JSON"
        ? JSON.stringify({ choices: [{ message, finish_reason: "stop" }] })
        : [
          { choices: [{ delta: { role: "assistant", ...fields } }] },
          { choices: [{ delta: { content: answer }, finish_reason: "stop" }] },
          // Some gateways append a snapshot after the deltas. It is not another thought.
          { choices: [{ message, finish_reason: "stop" }] },
        ].map((frame) => `data: ${JSON.stringify(frame)}\r\n\r\n`).join("") + "data: [DONE]\r\n\r\n"
      const fetchImpl = vi.fn(async (_url: RequestInfo | URL, _init?: RequestInit) =>
        fragmentedResponse(raw, wire === "JSON" ? "application/json" : "text/event-stream"),
      )
      const seen: StreamPiece[] = []

      await streamCompletion({
        provider: { ...blankProvider(), baseURL: "https://endpoint.invalid/v1", model: "fixture" },
        model: "fixture",
        reasoning: "",
        turns: [{ role: "user", parts: [{ kind: "text", text: "Inspect the current input." }] }],
        fetchImpl,
        onDelta: (piece) => seen.push(piece),
      })

      expect(seen).toEqual([
        { text: "", reasoning: thought },
        { text: answer, reasoning: thought },
      ])
      expect(fetchImpl).toHaveBeenCalledTimes(1)
      expect(String(fetchImpl.mock.calls[0][0])).toBe("https://endpoint.invalid/v1/chat/completions")
    })
  })
}
