import { describe, expect, it } from "vitest"

import {
  SELECTED_TEXT_LABEL,
  annotationLabel,
  appendQuote,
  editQuote,
  formatQuotedMessage,
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

  it("puts the quote ahead of the draft so the model sees both", () => {
    expect(formatQuotedMessage(["alpha"], "do this")).toBe(
      `${SELECTED_TEXT_LABEL}:\nalpha\n\ndo this`,
    )
  })

  it("sends quotes alone when the box is empty", () => {
    expect(formatQuotedMessage(["alpha"], "  ")).toBe(`${SELECTED_TEXT_LABEL}:\nalpha`)
  })

  it("joins more than one quote in the order they were added", () => {
    expect(formatQuotedMessage(["alpha", "beta"], "go")).toBe(
      `${SELECTED_TEXT_LABEL}:\nalpha\n\n${SELECTED_TEXT_LABEL}:\nbeta\n\ngo`,
    )
  })

  // The wrapper is a protocol, not a task. A canned example leaking in here
  // would show up in every quoted send.
  it("does not bake a canned task into the wrapper", () => {
    const wrapped = formatQuotedMessage(["x"], "y")
    expect(wrapped).not.toMatch(/notes\.md|deadline|summary\.md|table/i)
  })
})
