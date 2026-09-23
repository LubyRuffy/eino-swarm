import { describe, expect, it } from "vitest"

import {
  absorbChunk,
  catalogNames,
  chatRequestBody,
  completionURL,
  errorText,
  httpBaseURL,
  ModelCallError,
  modelsURL,
  normalizeApiStyle,
  normalizeReasoning,
  pieceFromData,
  redact,
  replyFromJSON,
  responsesRequestBody,
  takeSSE,
  type WireTurn,
} from "./openai-wire"

const turns: WireTurn[] = [
  { role: "user", parts: [{ kind: "text", text: "hello" }] },
  { role: "assistant", parts: [{ kind: "text", text: "hi" }] },
]

describe("openai wire", () => {
  it("keeps only http(s) base URLs", () => {
    expect(httpBaseURL("  https://endpoint.invalid/v1/ ")).toBe("https://endpoint.invalid/v1")
    expect(httpBaseURL("http://endpoint.invalid/v1")).toBe("http://endpoint.invalid/v1")
    expect(httpBaseURL("javascript:alert(1)")).toBeNull()
    expect(httpBaseURL("file:///tmp/x")).toBeNull()
    expect(httpBaseURL("not a url")).toBeNull()
    expect(httpBaseURL("")).toBeNull()
  })

  it("appends the resource the style asks for", () => {
    expect(modelsURL("https://endpoint.invalid/v1/")).toBe("https://endpoint.invalid/v1/models")
    expect(completionURL("https://endpoint.invalid/v1", "chat")).toBe(
      "https://endpoint.invalid/v1/chat/completions",
    )
    expect(completionURL("https://endpoint.invalid/v1", "responses")).toBe(
      "https://endpoint.invalid/v1/responses",
    )
    expect(normalizeApiStyle("responses")).toBe("responses")
    expect(normalizeApiStyle("nope")).toBe("chat")
  })

  it("omits thinking unless it is a known level, and adds no system turn", () => {
    expect(normalizeReasoning(" HIGH ")).toBe("high")
    expect(normalizeReasoning("max")).toBe("")
    const plain = chatRequestBody("m", turns, "")
    expect(plain).not.toHaveProperty("reasoning_effort")
    expect(plain.messages).toEqual([
      { role: "user", content: "hello" },
      { role: "assistant", content: "hi" },
    ])
    expect(JSON.stringify(plain)).not.toContain("system")
    const effort = chatRequestBody("m", turns, "low")
    expect(effort.reasoning_effort).toBe("low")
  })

  it("sends an image as image_url and a file as file_data", () => {
    const body = chatRequestBody(
      "m",
      [
        {
          role: "user",
          parts: [
            { kind: "text", text: "see" },
            { kind: "image", mediaType: "image/png", dataUrl: "data:image/png;base64,aa" },
            {
              kind: "file",
              name: "a.bin",
              mediaType: "application/octet-stream",
              dataUrl: "data:application/octet-stream;base64,bb",
            },
          ],
        },
      ],
      "",
    )
    const messages = body.messages as { content: { type: string }[] }[]
    expect(messages[0].content.map((part) => part.type)).toEqual(["text", "image_url", "file"])
  })

  it("uses input_text for the user and output_text for the assistant", () => {
    const body = responsesRequestBody(
      "m",
      [
        ...turns,
        {
          role: "user",
          parts: [{ kind: "image", mediaType: "image/png", dataUrl: "data:image/png;base64,aa" }],
        },
      ],
      "medium",
    )
    expect(body.reasoning).toEqual({ effort: "medium" })
    const input = body.input as { role: string; content: { type: string }[] }[]
    expect(input[0].content[0].type).toBe("input_text")
    expect(input[1].content[0].type).toBe("output_text")
    expect(input[2].content[0].type).toBe("input_image")
    expect(responsesRequestBody("m", turns, "")).not.toHaveProperty("reasoning")
  })

  it("reads model ids and ignores a duplicate", () => {
    expect(
      catalogNames({
        data: [{ id: "one" }, { id: "one" }, { id: "  " }, { id: "two" }, { nope: true }],
      }),
    ).toEqual(["one", "two"])
    expect(catalogNames({})).toEqual([])
  })

  it("appends chat deltas and stops on DONE", () => {
    const taken = takeSSE(
      'data: {"choices":[{"delta":{"content":"ab","reasoning_content":"th"}}]}\n\n' +
        "data: [DONE]\n\n" +
        "data: tr",
    )
    expect(taken.events).toHaveLength(2)
    expect(pieceFromData(taken.events[0].data)).toEqual({ text: "ab", reasoning: "th" })
    expect(pieceFromData(taken.events[1].data)).toBe("done")
    expect(taken.rest).toBe("data: tr")
  })

  it("replaces a resent buffer and still appends a short token", () => {
    const sentence = "abcdefghijkl"
    expect(absorbChunk(sentence, sentence)).toBe(sentence)
    expect(absorbChunk(sentence, sentence + "m")).toBe(sentence + "m")
    expect(absorbChunk("ab", "ab")).toBe("abab")
    expect(absorbChunk("", "ab")).toBe("ab")
  })

  it("reads a content array and a completed response envelope", () => {
    expect(
      pieceFromData('{"choices":[{"delta":{"content":[{"type":"text","text":"ab"}]}}]}'),
    ).toEqual({ text: "ab", reasoning: "" })
    expect(
      replyFromJSON({
        type: "response.completed",
        response: {
          output: [{ type: "message", content: [{ type: "output_text", text: "pong" }] }],
        },
      }),
    ).toEqual({ text: "pong", reasoning: "" })
    expect(replyFromJSON({ type: "response.output_text.done", text: "yo" })).toEqual({
      text: "yo",
      reasoning: "",
    })
  })

  it("reads responses deltas and skips the completed snapshot", () => {
    expect(
      pieceFromData('{"type":"response.output_text.delta","delta":"yo"}'),
    ).toEqual({ text: "yo", reasoning: "" })
    expect(
      pieceFromData('{"type":"response.reasoning_summary_text.delta","delta":"because"}'),
    ).toEqual({ text: "", reasoning: "because" })
    expect(pieceFromData('{"type":"response.output_text.done","text":"yo"}')).toBeNull()
  })

  it("reads a finished JSON body from either wire", () => {
    expect(
      replyFromJSON({ choices: [{ message: { content: "pong", reasoning_content: "think" } }] }),
    ).toEqual({ text: "pong", reasoning: "think" })
    expect(
      replyFromJSON({
        output: [
          { type: "reasoning", summary: [{ text: "think" }] },
          { type: "message", content: [{ type: "output_text", text: "pong" }] },
        ],
      }),
    ).toEqual({ text: "pong", reasoning: "think" })
  })

  it("surfaces an error payload and strips a key", () => {
    expect(() => pieceFromData('{"error":{"message":"nope secret"}}')).toThrow(ModelCallError)
    expect(errorText('{"error":{"message":"nope"}}', 400)).toBe("nope")
    expect(errorText("plain failure", 500)).toBe("plain failure")
    expect(redact("bearer secret-value refused", "secret-value")).toBe("bearer ••• refused")
    expect(redact("bearer secret-value refused", "")).toBe("bearer secret-value refused")
  })
})
