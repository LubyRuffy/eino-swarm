import { describe, expect, it } from "vitest"

import {
  DIFF_LINE_CAP,
  clipDiff,
  parseEditDiff,
  parseFileChange,
  parseWriteDiff,
} from "./edit-diff"

describe("parseEditDiff", () => {
  it("turns a search/replace into a line hunk with shared context kept", () => {
    const diff = parseEditDiff(
      JSON.stringify({
        file_path: "pkg/alpha.go",
        search_block: "func Alpha() {\n\treturn 0\n}\n",
        replace_block: "func Alpha() {\n\treturn 1\n}\n",
      }),
    )
    expect(diff?.path).toBe("pkg/alpha.go")
    expect(diff?.added).toBe(1)
    expect(diff?.removed).toBe(1)
    expect(diff?.hunks).toHaveLength(1)
    expect(diff?.hunks[0].lines.map((l) => [l.op, l.text])).toEqual([
      ["eq", "func Alpha() {"],
      ["del", "\treturn 0"],
      ["add", "\treturn 1"],
      ["eq", "}"],
    ])
  })

  it("reads an apply_patch body as the hunks it already is", () => {
    const patch = [
      "*** Begin Patch",
      "*** Update File: pkg/alpha.go",
      "@@",
      " func Alpha() {",
      "-        return 0",
      "+        return 1",
      " }",
      "*** End Patch",
    ].join("\n")
    const diff = parseEditDiff(JSON.stringify({ file_path: "pkg/alpha.go", patch }))
    expect(diff?.added).toBe(1)
    expect(diff?.removed).toBe(1)
    expect(diff?.hunks[0].lines.map((l) => l.op)).toEqual(["eq", "del", "add", "eq"])
    expect(diff?.hunks[0].lines[1].text).toBe("        return 0")
    expect(diff?.hunks[0].lines[2].text).toBe("        return 1")
  })

  it("does not invent a hunk from a path-only call", () => {
    expect(parseEditDiff(`{"file_path":"pkg/alpha.go"}`)).toBeUndefined()
    expect(parseEditDiff("")).toBeUndefined()
    expect(parseEditDiff("not json")).toBeUndefined()
  })

  it("does not treat the status line as the change", () => {
    const diff = parseEditDiff(
      JSON.stringify({
        file_path: "pkg/alpha.go",
        search_block: "old",
        replace_block: "new",
      }),
    )
    const texts = diff?.hunks.flatMap((h) => h.lines.map((l) => l.text)).join("\n") ?? ""
    expect(texts).not.toMatch(/ok:/)
    expect(texts).not.toContain("search_block")
    expect(texts).not.toContain("replaced block")
  })

  it("treats an empty replace_block as a deletion hunk, not a missing edit", () => {
    const diff = parseEditDiff(
      JSON.stringify({
        file_path: "notes.txt",
        search_block: "beta\n",
        replace_block: "",
      }),
    )
    expect(diff?.added).toBe(0)
    expect(diff?.removed).toBe(1)
    expect(diff?.hunks[0].lines.map((l) => [l.op, l.text])).toEqual([["del", "beta"]])
  })

  it("normalises CRLF in a search/replace so the hunk is the same as LF", () => {
    const diff = parseEditDiff(
      JSON.stringify({
        file_path: "pkg/alpha.go",
        search_block: "alpha\r\nbeta\r\n",
        replace_block: "alpha\r\ngamma\r\n",
      }),
    )
    expect(diff?.hunks[0].lines.map((l) => [l.op, l.text])).toEqual([
      ["eq", "alpha"],
      ["del", "beta"],
      ["add", "gamma"],
    ])
  })
})

describe("parseWriteDiff", () => {
  it("treats the written body as added lines, not the status sentence", () => {
    const diff = parseWriteDiff(
      JSON.stringify({
        file_path: "pkg/alpha.go",
        content: "package alpha\nfunc Alpha() {}\n",
      }),
    )
    expect(diff?.path).toBe("pkg/alpha.go")
    expect(diff?.added).toBe(2)
    expect(diff?.removed).toBe(0)
    expect(diff?.hunks[0].lines.every((l) => l.op === "add")).toBe(true)
    expect(diff?.hunks[0].lines.map((l) => l.text)).toEqual(["package alpha", "func Alpha() {}"])
    const texts = diff?.hunks.flatMap((h) => h.lines.map((l) => l.text)).join("\n") ?? ""
    expect(texts).not.toMatch(/Updated file/)
    expect(texts).not.toContain("content")
  })

  it("keeps an empty write as a zero-line hunk so the path still shows", () => {
    const diff = parseWriteDiff(JSON.stringify({ file_path: "pkg/alpha.go", content: "" }))
    expect(diff?.path).toBe("pkg/alpha.go")
    expect(diff?.added).toBe(0)
    expect(diff?.hunks[0].lines).toEqual([])
  })

  it("does not invent a hunk from a path-only call", () => {
    expect(parseWriteDiff(`{"file_path":"pkg/alpha.go"}`)).toBeUndefined()
    expect(parseWriteDiff("")).toBeUndefined()
  })
})

describe("parseFileChange", () => {
  it("dispatches edit and write, and ignores other tools", () => {
    const edit = parseFileChange(
      "edit",
      JSON.stringify({ file_path: "pkg/alpha.go", search_block: "a", replace_block: "b" }),
    )
    expect(edit?.removed).toBe(1)
    expect(edit?.added).toBe(1)
    const write = parseFileChange(
      "write",
      JSON.stringify({ file_path: "pkg/alpha.go", content: "one\n" }),
    )
    expect(write?.added).toBe(1)
    expect(write?.removed).toBe(0)
    expect(parseFileChange("exec", `{"command":"true"}`)).toBeUndefined()
  })
})

describe("clipDiff", () => {
  it("keeps the first cap lines and reports the rest as hidden", () => {
    const lines = Array.from({ length: 5 }, (_, i) => ({ op: "add" as const, text: `L${i}` }))
    const clipped = clipDiff({ path: "pkg/alpha.go", hunks: [{ lines }], added: 5, removed: 0 }, 3)
    expect(clipped.hunks[0].lines.map((l) => l.text)).toEqual(["L0", "L1", "L2"])
    expect(clipped.hidden).toBe(2)
  })

  it("stops mid-file when a later hunk would overflow the cap", () => {
    const hunk = (n: number, prefix: string) => ({
      header: `@@ ${prefix}`,
      lines: Array.from({ length: n }, (_, i) => ({ op: "eq" as const, text: `${prefix}${i}` })),
    })
    const clipped = clipDiff(
      { path: "pkg/alpha.go", hunks: [hunk(3, "a"), hunk(3, "b")], added: 0, removed: 0 },
      4,
    )
    expect(clipped.hunks).toHaveLength(2)
    expect(clipped.hunks[0].lines).toHaveLength(3)
    expect(clipped.hunks[1].header).toBe("@@ b")
    expect(clipped.hunks[1].lines.map((l) => l.text)).toEqual(["b0"])
    expect(clipped.hidden).toBe(2)
  })

  it("does not clip a hunk that already fits", () => {
    const diff = parseWriteDiff(
      JSON.stringify({ file_path: "pkg/alpha.go", content: "one\ntwo\n" }),
    )!
    const clipped = clipDiff(diff)
    expect(clipped.hidden).toBe(0)
    expect(clipped.hunks[0].lines).toHaveLength(2)
    expect(DIFF_LINE_CAP).toBeGreaterThan(2)
  })
})
