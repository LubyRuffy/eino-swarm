import { describe, expect, it } from "vitest"

import { mockListedFiles, mockOutputFiles } from "./mock-files"

describe("mock workspace listing", () => {
  const files = mockOutputFiles("")

  it("returns only the root folder until that folder is opened", () => {
    expect(mockListedFiles(files).map((file) => file.path)).toEqual(["outputs"])
    const opened = mockListedFiles(files, "outputs").map((file) => file.path)
    expect(opened).toContain("outputs/report.md")
    expect(opened.every((path) => !path.slice("outputs/".length).includes("/"))).toBe(true)
  })

  it("returns every search match and leaves other files out", () => {
    expect(mockListedFiles(files, "outputs", "report").map((file) => file.path)).toEqual(["outputs/report.md"])
  })
})
