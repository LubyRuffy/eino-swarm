import { describe, expect, it } from "vitest"

import { pieceFromData, replyFromJSON } from "./openai-wire"

const thought = "检查当前证据 before answering."
const cases = [
  { name: "legacy field", fields: { reasoning_content: thought }, want: thought },
  { name: "current field", fields: { reasoning: thought }, want: thought },
  { name: "empty legacy fallback", fields: { reasoning_content: "", reasoning: thought }, want: thought },
  { name: "null legacy fallback", fields: { reasoning_content: null, reasoning: thought }, want: thought },
  { name: "empty current field", fields: { reasoning_content: thought, reasoning: "" }, want: thought },
  { name: "null current field", fields: { reasoning_content: thought, reasoning: null }, want: thought },
  {
    name: "different aliases keep legacy precedence",
    fields: { reasoning_content: thought, reasoning: "Unused alternative thought." },
    want: thought,
  },
  { name: "mirrored aliases are read once", fields: { reasoning_content: thought, reasoning: thought }, want: thought },
]

// vLLM changed the field name. Keep both response shapes covered at the
// decoder boundary, including gateways that publish both aliases together.
describe("chat reasoning field compatibility", () => {
  it.each(cases)("reads a streamed $name without duplicating the thought", ({ fields, want }) => {
    const raw = JSON.stringify({ choices: [{ delta: { content: "Verified answer.", ...fields } }] })
    expect(pieceFromData(raw)).toEqual({ text: "Verified answer.", reasoning: want })
  })

  it.each(cases)("reads a JSON $name without duplicating the thought", ({ fields, want }) => {
    expect(replyFromJSON({ choices: [{ message: { content: "Verified answer.", ...fields } }] }))
      .toEqual({ text: "Verified answer.", reasoning: want })
  })
})
