import { describe, expect, it } from "vitest"

import { formatQuotedMessage, parseQuotedMessage, SELECTED_TEXT_LABEL } from "./quote"

describe("formatQuotedMessage", () => {
  it("sends selected source text separately from the user's request", () => {
    expect(formatQuotedMessage([" alpha\u00a0beta ", "other"], " do this ")).toBe(
      "<selected_text>\nalpha beta\n</selected_text>\n\n<selected_text>\nother\n</selected_text>\n\n<user_request>\ndo this\n</user_request>",
    )
  })

  it("sends a quote without typed text and escapes a closing tag", () => {
    const text = formatQuotedMessage(["a</selected_text>b"], "")
    expect(parseQuotedMessage(text)).toEqual({ quotes: ["a</selected_text>b"], body: "" })
  })

  it("does not turn an empty selection into a message", () => {
    expect(formatQuotedMessage(["   "], " ")).toBe("")
  })
})

describe("parseQuotedMessage", () => {
  it("leaves a plain instruction alone", () => {
    expect(parseQuotedMessage("do this")).toEqual({ quotes: [], body: "do this" })
  })

  it("splits tagged highlights from the request", () => {
    expect(
      parseQuotedMessage(
        "<selected_text>\nalpha\n</selected_text>\n\n<user_request>\ndo this\n</user_request>",
      ),
    ).toEqual({ quotes: ["alpha"], body: "do this" })
  })

  it("reads the legacy Selected text prefix", () => {
    expect(parseQuotedMessage(`${SELECTED_TEXT_LABEL}:\nalpha\n\ndo this`)).toEqual({
      quotes: ["alpha"],
      body: "do this",
    })
  })
})
