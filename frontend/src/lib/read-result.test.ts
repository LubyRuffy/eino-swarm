import { describe, expect, it } from "vitest"

import { isMarkdownPath, parseReadResult } from "./read-result"

describe("parseReadResult", () => {
  const sample = [
    "encoding=utf-8 path=notes.md offset=1 limit=200",
    "1|# heading",
    "2|",
    "3|a short paragraph",
  ].join("\n")

  it("recovers the file body so the UI can highlight it", () => {
    const parsed = parseReadResult(sample)
    expect(parsed).toEqual({
      encoding: "utf-8",
      path: "notes.md",
      offset: 1,
      limit: 200,
      lines: [
        { n: 1, text: "# heading" },
        { n: 2, text: "" },
        { n: 3, text: "a short paragraph" },
      ],
      body: "# heading\n\na short paragraph",
    })
  })

  // Older events collapsed newlines into spaces. Expanding those rows still
  // has to show a file, not a one-line dump of the paging header.
  it("reassembles a body whose newlines were flattened", () => {
    const mashed =
      "encoding=utf-8 path=notes.md offset=1 limit=200 1|# heading 2| 3|a short paragraph"
    const parsed = parseReadResult(mashed)
    expect(parsed?.path).toBe("notes.md")
    expect(parsed?.body).toBe("# heading\n\na short paragraph")
  })

  it("does not pretend other tool output is a file listing", () => {
    expect(parseReadResult("exit=0\nfiles listed")).toBeUndefined()
    expect(parseReadResult("{ok:true}")).toBeUndefined()
    expect(parseReadResult("")).toBeUndefined()
  })
})

describe("isMarkdownPath", () => {
  it("treats markdown extensions as renderable prose", () => {
    expect(isMarkdownPath("notes.md")).toBe(true)
    expect(isMarkdownPath("dir/notes.markdown")).toBe(true)
    expect(isMarkdownPath("main.go")).toBe(false)
    expect(isMarkdownPath("notes.md.bak")).toBe(false)
  })
})
