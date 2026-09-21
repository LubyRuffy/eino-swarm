import { describe, expect, it } from "vitest"

import { parseQuotedMessage, SELECTED_TEXT_LABEL } from "./quote"

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
