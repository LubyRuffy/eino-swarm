import { describe, expect, it } from "vitest"

import { applyCarriageReturns, lastLine } from "./carriage"

describe("applyCarriageReturns", () => {
  it("overwrites the current line on CR", () => {
    expect(applyCarriageReturns("step 1\rstep 2")).toBe("step 2")
  })

  it("keeps newlines as separate rows", () => {
    expect(applyCarriageReturns("one\ntwo\rthree")).toBe("one\nthree")
  })

  it("leaves plain text alone", () => {
    expect(applyCarriageReturns("ok\nline")).toBe("ok\nline")
  })
})

describe("lastLine", () => {
  it("returns the last non-empty line", () => {
    expect(lastLine("a\nstep 14/174\n")).toBe("step 14/174")
  })

  it("returns empty when there is nothing to show", () => {
    expect(lastLine("\n\n")).toBe("")
  })
})
