import { describe, expect, it } from "vitest"

import {
  SELECTED_TEXT_LABEL,
  SELECTED_TEXT_TAG,
  USER_REQUEST_TAG,
  annotationLabel,
  appendQuote,
  displayQuotedText,
  editQuote,
  formatQuotedMessage,
  parseQuotedMessage,
  plainUserText,
  removeQuote,
} from "./quote"

describe("appendQuote", () => {
  it("drops whitespace-only selections so the chip is never empty", () => {
    expect(appendQuote([], "   \n\t  ")).toEqual([])
  })

  it("keeps inner line breaks and assigns an id", () => {
    const next = appendQuote([], "alpha\nbeta", "q-fixed")
    expect(next).toEqual([{ id: "q-fixed", text: "alpha\nbeta" }])
  })
})

describe("editQuote", () => {
  const start = [{ id: "q1", text: "alpha" }]

  it("replaces the matching snippet", () => {
    expect(editQuote(start, "q1", "beta")).toEqual([{ id: "q1", text: "beta" }])
  })

  it("removes the snippet when the edit is empty", () => {
    expect(editQuote(start, "q1", "  ")).toEqual([])
  })
})

describe("removeQuote", () => {
  it("drops only the matching id", () => {
    expect(
      removeQuote(
        [
          { id: "q1", text: "a" },
          { id: "q2", text: "b" },
        ],
        "q1",
      ),
    ).toEqual([{ id: "q2", text: "b" }])
  })
})

describe("annotationLabel", () => {
  it("names the chip after the count, not the quoted words", () => {
    expect(annotationLabel(0)).toBe("")
    expect(annotationLabel(1)).toBe("1 annotation")
    expect(annotationLabel(2)).toBe("2 annotations")
  })
})

describe("formatQuotedMessage", () => {
  it("sends the draft unchanged when nothing was quoted", () => {
    expect(formatQuotedMessage([], "do this")).toBe("do this")
  })

  it("tags the quote and the request so the model can tell them apart", () => {
    expect(formatQuotedMessage(["alpha"], "do this")).toBe(
      `<${SELECTED_TEXT_TAG}>\nalpha\n</${SELECTED_TEXT_TAG}>\n\n<${USER_REQUEST_TAG}>\ndo this\n</${USER_REQUEST_TAG}>`,
    )
  })

  it("sends quotes alone when the box is empty", () => {
    expect(formatQuotedMessage(["alpha"], "  ")).toBe(
      `<${SELECTED_TEXT_TAG}>\nalpha\n</${SELECTED_TEXT_TAG}>`,
    )
  })

  it("joins more than one quote in the order they were added", () => {
    expect(formatQuotedMessage(["alpha", "beta"], "go")).toBe(
      `<${SELECTED_TEXT_TAG}>\nalpha\n</${SELECTED_TEXT_TAG}>\n\n<${SELECTED_TEXT_TAG}>\nbeta\n</${SELECTED_TEXT_TAG}>\n\n<${USER_REQUEST_TAG}>\ngo\n</${USER_REQUEST_TAG}>`,
    )
  })

  it("keeps a quote that contains the closing tag from breaking the wrapper", () => {
    const inner = `keep </${SELECTED_TEXT_TAG}> inside`
    const wrapped = formatQuotedMessage([inner], "go")
    expect(parseQuotedMessage(wrapped)).toEqual({ quotes: [inner], body: "go" })
  })

  it("names a label after the request, not the tags", () => {
    expect(plainUserText(formatQuotedMessage(["alpha"], "do this"))).toBe("do this")
    expect(plainUserText(formatQuotedMessage(["alpha"], ""))).toBe("alpha")
    expect(plainUserText("plain")).toBe("plain")
  })

  it("copies chips plus the request, not the wire tags", () => {
    expect(displayQuotedText(formatQuotedMessage(["alpha"], "do this"))).toBe(
      "alpha\n\ndo this",
    )
    expect(displayQuotedText(formatQuotedMessage(["alpha"], ""))).toBe("alpha")
    expect(displayQuotedText("plain")).toBe("plain")
    expect(displayQuotedText(formatQuotedMessage(["alpha"], "do this"))).not.toMatch(
      /<\/?selected_text>|<\/?user_request>/,
    )
  })

  // The wrapper is a protocol, not a task. A canned example leaking in here
  // would show up in every quoted send.
  it("does not bake a canned task into the wrapper", () => {
    const wrapped = formatQuotedMessage(["x"], "y")
    expect(wrapped).not.toMatch(/notes\.md|deadline|summary\.md|table/i)
    expect(wrapped).not.toMatch(/Selected text:/)
  })
})

describe("parseQuotedMessage", () => {
  it("leaves a plain instruction alone", () => {
    expect(parseQuotedMessage("do this")).toEqual({ quotes: [], body: "do this" })
  })

  it("round-trips a tagged send", () => {
    const wrapped = formatQuotedMessage(["alpha\nbeta"], "do this")
    expect(parseQuotedMessage(wrapped)).toEqual({
      quotes: ["alpha\nbeta"],
      body: "do this",
    })
  })

  it("reads the legacy Selected text prefix so old bubbles still split", () => {
    expect(
      parseQuotedMessage(`${SELECTED_TEXT_LABEL}:\nalpha\n\ndo this`),
    ).toEqual({ quotes: ["alpha"], body: "do this" })
  })

  it("reads more than one legacy quote", () => {
    expect(
      parseQuotedMessage(
        `${SELECTED_TEXT_LABEL}:\nalpha\n\n${SELECTED_TEXT_LABEL}:\nbeta\n\ngo`,
      ),
    ).toEqual({ quotes: ["alpha", "beta"], body: "go" })
  })
})
