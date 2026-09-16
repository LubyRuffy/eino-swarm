import { describe, expect, it } from "vitest"

import { parseReadResult } from "./read-result"
import {
  languageFromPath,
  paintLines,
  tokenizeSource,
  tokensByLine,
  type SourceToken,
  type SourceTokenKind,
} from "./source-highlight"

function joined(tokens: SourceToken[]): string {
  return tokens.map((t) => t.text).join("")
}

function firstKind(tokens: SourceToken[], snippet: string): SourceTokenKind | undefined {
  return tokens.find((t) => t.text === snippet)?.kind
}

describe("languageFromPath", () => {
  it("picks a dialect from the suffix, not from the file body", () => {
    expect(languageFromPath("pkg/main.go")).toBe("go")
    expect(languageFromPath("src/app.tsx")).toBe("js")
    expect(languageFromPath("src/app.py")).toBe("python")
    expect(languageFromPath("conf.yaml")).toBe("yaml")
    expect(languageFromPath("Dockerfile")).toBe("docker")
    expect(languageFromPath("go.mod")).toBe("gomod")
  })

  it("leaves markdown and unknown suffixes uncoloured", () => {
    expect(languageFromPath("notes.md")).toBeUndefined()
    expect(languageFromPath("notes.markdown")).toBeUndefined()
    expect(languageFromPath("notes.txt")).toBeUndefined()
    expect(languageFromPath("notes.md.bak")).toBeUndefined()
    expect(languageFromPath("LICENSE")).toBeUndefined()
  })
})

describe("tokenizeSource", () => {
  it("reconstructs the original body, including spaces and newlines", () => {
    const src = `package main\n\nfunc Hello() {\n\treturn "ok"\n}\n`
    expect(joined(tokenizeSource(src, "go"))).toBe(src)
  })

  it("marks Go keywords, names, strings, comments and numbers", () => {
    const tokens = tokenizeSource(
      `package main\nfunc Hello() { return "ok" // note\n}`,
      "go",
    )
    expect(firstKind(tokens, "package")).toBe("keyword")
    expect(firstKind(tokens, "main")).toBe("command")
    expect(firstKind(tokens, "func")).toBe("keyword")
    expect(firstKind(tokens, "Hello")).toBe("command")
    expect(firstKind(tokens, `"ok"`)).toBe("string")
    expect(firstKind(tokens, "return")).toBe("keyword")
    expect(tokens.some((t) => t.kind === "comment" && t.text.includes("note"))).toBe(true)
  })

  it("keeps a raw string as one string so a wrap still shows every line", () => {
    const src = "x := `line one\nline two`"
    const tokens = tokenizeSource(src, "go")
    expect(joined(tokens)).toBe(src)
    expect(tokens.some((t) => t.kind === "string" && t.text.includes("line one"))).toBe(true)
  })

  it("marks a Python def name and a hash comment", () => {
    const tokens = tokenizeSource(`def run():\n    return True  # note`, "python")
    expect(joined(tokens)).toBe(`def run():\n    return True  # note`)
    expect(firstKind(tokens, "def")).toBe("keyword")
    expect(firstKind(tokens, "run")).toBe("command")
    expect(firstKind(tokens, "True")).toBe("keyword")
    expect(tokens.some((t) => t.kind === "comment" && t.text.includes("note"))).toBe(true)
  })

  it("marks JSON keywords and strings, not the punctuation as words", () => {
    const src = `{"ok": true, "n": 2}`
    const tokens = tokenizeSource(src, "json")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, `"ok"`)).toBe("string")
    expect(firstKind(tokens, "true")).toBe("keyword")
    expect(firstKind(tokens, "2")).toBe("flag")
  })

  it("marks yaml keys and comments", () => {
    const src = "name: alpha\n# note"
    const tokens = tokenizeSource(src, "yaml")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "name")).toBe("variable")
    expect(tokens.some((t) => t.kind === "text" && t.text.includes("alpha"))).toBe(true)
    expect(tokens.some((t) => t.kind === "comment" && t.text.includes("note"))).toBe(true)
  })

  it("marks HTML tags, attributes and comments", () => {
    const src = `<!-- note --><div class="wrap">ok</div>`
    const tokens = tokenizeSource(src, "html")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "div")).toBe("command")
    expect(firstKind(tokens, "class")).toBe("variable")
    expect(firstKind(tokens, `"wrap"`)).toBe("string")
    expect(tokens.some((t) => t.kind === "comment" && t.text.includes("note"))).toBe(true)
  })

  it("delegates a shell script to the exec tokenizer", () => {
    const src = `echo "ok" && ls -la`
    const tokens = tokenizeSource(src, "shell")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "echo")).toBe("command")
    expect(firstKind(tokens, `"ok"`)).toBe("string")
    expect(firstKind(tokens, "&&")).toBe("operator")
  })

  it("colours a SQL keyword regardless of case", () => {
    const src = "SELECT name FROM items"
    const tokens = tokenizeSource(src, "sql")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "SELECT")).toBe("keyword")
    expect(firstKind(tokens, "FROM")).toBe("keyword")
  })

  it("keeps a block comment across lines so the closer is not a new statement", () => {
    const src = "a /*\nnote\n*/ b"
    const tokens = tokenizeSource(src, "go")
    expect(joined(tokens)).toBe(src)
    expect(tokens.some((t) => t.kind === "comment" && t.text.includes("note"))).toBe(true)
    const lines = tokensByLine(tokens)
    expect(lines).toHaveLength(3)
    expect(lines[1].every((t) => t.kind === "comment")).toBe(true)
  })

  it("does not invent tokens when the input is empty", () => {
    expect(tokenizeSource("", "go")).toEqual([])
  })

  it("falls back to a single text token when a line is too long to paint", () => {
    const src = `package ${"x".repeat(5000)}`
    const tokens = tokenizeSource(src, "go")
    expect(joined(tokens)).toBe(src)
    expect(tokens).toEqual([{ kind: "text", text: src }])
  })

  it("paints a template interpolation without dropping characters", () => {
    const src = "`hi ${name}`"
    const tokens = tokenizeSource(src, "js")
    expect(joined(tokens)).toBe(src)
    expect(tokens.some((t) => t.kind === "string" && t.text.includes("hi"))).toBe(true)
    expect(firstKind(tokens, "${")).toBe("operator")
    expect(firstKind(tokens, "name")).toBe("text")
  })

  it("keeps a Python prefixed string as a string", () => {
    const src = `x = f"ok"`
    const tokens = tokenizeSource(src, "python")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, `f"ok"`)).toBe("string")
  })

  it("marks a dollar identifier in PHP", () => {
    const src = "$name = 1;"
    const tokens = tokenizeSource(src, "php")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "$name")).toBe("variable")
  })

  it("marks a rust lifetime as a variable, not a char", () => {
    const src = "fn run<'a>(x: &'a T) {}"
    const tokens = tokenizeSource(src, "rust")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "fn")).toBe("keyword")
    expect(firstKind(tokens, "run")).toBe("command")
    expect(firstKind(tokens, "'a")).toBe("variable")
  })

  it("marks a CSS hex colour as a number token", () => {
    const src = "color: #abc;"
    const tokens = tokenizeSource(src, "css")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "#abc")).toBe("flag")
  })

  it("treats a shebang as a comment so the rest still tokenizes", () => {
    const src = "#!/usr/bin/env python\nprint(1)\n"
    const tokens = tokenizeSource(src, "python")
    expect(joined(tokens)).toBe(src)
    expect(tokens[0].kind).toBe("comment")
    expect(tokens.some((t) => t.kind === "text" && t.text.includes("print"))).toBe(true)
  })

  it("leaves an unknown dialect as plain text", () => {
    const src = "package main"
    expect(tokenizeSource(src, "nope")).toEqual([{ kind: "text", text: src }])
  })

  it("keeps a triple-quoted Python string together", () => {
    const src = `x = """a\nb"""`
    const tokens = tokenizeSource(src, "python")
    expect(joined(tokens)).toBe(src)
    expect(tokens.some((t) => t.kind === "string" && t.text.includes("a"))).toBe(true)
  })

  it("keeps a Go rune as a string, not a lifetime", () => {
    const src = `x := 'a'`
    const tokens = tokenizeSource(src, "go")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, `'a'`)).toBe("string")
  })

  it("marks a toml key before equals", () => {
    const src = "name = true"
    const tokens = tokenizeSource(src, "toml")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "name")).toBe("variable")
    expect(firstKind(tokens, "true")).toBe("keyword")
  })

  it("colours a Dockerfile instruction regardless of case", () => {
    const src = "from alpine"
    const tokens = tokenizeSource(src, "docker")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "from")).toBe("keyword")
  })

  it("reads hex numbers and a C block comment with no closer", () => {
    const src = "int x = 0xFF; /* note"
    const tokens = tokenizeSource(src, "c")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "int")).toBe("keyword")
    expect(firstKind(tokens, "0xFF")).toBe("flag")
    expect(tokens.some((t) => t.kind === "comment" && t.text.includes("note"))).toBe(true)
  })

  it("paints a self-closing HTML tag and an unterminated comment", () => {
    const src = `<img src="a"/> <!-- note`
    const tokens = tokenizeSource(src, "html")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "img")).toBe("command")
    expect(firstKind(tokens, "src")).toBe("variable")
    expect(tokens.some((t) => t.kind === "comment" && t.text.includes("note"))).toBe(true)
  })

  it("reads a PHP interpolated dollar brace", () => {
    const src = "${name}"
    const tokens = tokenizeSource(src, "php")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "${name}")).toBe("variable")
  })

  it("keeps escaped ticks and an unclosed template as strings", () => {
    const src = '`a\\`b ${x + 1}`'
    const tokens = tokenizeSource(src, "js")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "${")).toBe("operator")
    expect(firstKind(tokens, "+")).toBe("operator")
    expect(joined(tokenizeSource("`open", "js"))).toBe("`open")
  })

  it("reads floats, exponents and a lone dollar", () => {
    const src = "x = .5 + 1e2 + $ "
    const tokens = tokenizeSource(src, "php")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, ".5")).toBe("flag")
    expect(firstKind(tokens, "1e2")).toBe("flag")
    expect(firstKind(tokens, "$")).toBe("variable")
  })

  it("does not treat fr as a string prefix when it is an identifier", () => {
    const src = "fr = 1"
    const tokens = tokenizeSource(src, "python")
    expect(joined(tokens)).toBe(src)
    expect(tokens.some((t) => t.kind === "text" && t.text.includes("fr"))).toBe(true)
  })

  it("paints a processing-instruction tag", () => {
    const src = `<?xml version="1"?>`
    const tokens = tokenizeSource(src, "html")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "xml")).toBe("command")
    expect(firstKind(tokens, "version")).toBe("variable")
  })

  it("marks a Ruby method name", () => {
    const src = "def run\nend"
    const tokens = tokenizeSource(src, "ruby")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "def")).toBe("keyword")
    expect(firstKind(tokens, "run")).toBe("command")
  })

  it("tokenizes a nested template object without dropping braces", () => {
    const src = '`${fn({a: "b"})}`'
    const tokens = tokenizeSource(src, "js")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "${")).toBe("operator")
    expect(firstKind(tokens, `"b"`)).toBe("string")
  })

  it("does not swallow the letter after an incomplete hex prefix", () => {
    const src = "0x + 0b10"
    const tokens = tokenizeSource(src, "c")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "0")).toBe("flag")
    expect(firstKind(tokens, "0b10")).toBe("flag")
  })

  it("keeps an unclosed quote as a string through the end of the file", () => {
    const src = `"open`
    const tokens = tokenizeSource(src, "js")
    expect(joined(tokens)).toBe(src)
    expect(tokens.some((t) => t.kind === "string")).toBe(true)
  })

  it("paints a boolean HTML attribute", () => {
    const src = "<input disabled>"
    const tokens = tokenizeSource(src, "html")
    expect(joined(tokens)).toBe(src)
    expect(firstKind(tokens, "input")).toBe("command")
    expect(firstKind(tokens, "disabled")).toBe("variable")
  })
})

describe("paintLines", () => {
  it("zips numbered rows with highlighted tokens", () => {
    const listing = parseReadResult(
      ["encoding=utf-8 path=main.go offset=1 limit=200", "1|package main", "2|func Hello() {}"].join(
        "\n",
      ),
    )
    expect(listing).toBeTruthy()
    const rows = paintLines(listing!.lines, listing!.body, listing!.path)
    expect(rows).toHaveLength(2)
    expect(rows[0].n).toBe(1)
    expect(firstKind(rows[0].tokens, "package")).toBe("keyword")
    expect(firstKind(rows[1].tokens, "Hello")).toBe("command")
    expect(rows.map((r) => r.tokens.map((t) => t.text).join("")).join("\n")).toBe(listing!.body)
  })

  it("leaves an unknown suffix as plain text so a log is not guessed as code", () => {
    const listing = parseReadResult(
      ["encoding=utf-8 path=notes.txt offset=1 limit=200", "1|package main"].join("\n"),
    )
    const rows = paintLines(listing!.lines, listing!.body, listing!.path)
    expect(rows[0].tokens).toEqual([{ kind: "text", text: "package main" }])
  })

  it("keeps a blank numbered row so a gap in the file is still a gap", () => {
    const listing = parseReadResult(
      ["encoding=utf-8 path=main.go offset=1 limit=200", "1|package main", "2|", "3|func Hello() {}"].join(
        "\n",
      ),
    )
    const rows = paintLines(listing!.lines, listing!.body, listing!.path)
    expect(rows).toHaveLength(3)
    expect(rows[1].tokens).toEqual([])
    expect(rows[2].tokens.some((t) => t.text === "Hello")).toBe(true)
  })

  it("falls back to plain rows when the body does not match the numbered lines", () => {
    const rows = paintLines([{ n: 1, text: "package main" }], "package main\nfunc Hello()", "main.go")
    expect(rows).toEqual([{ n: 1, tokens: [{ kind: "text", text: "package main" }] }])
  })
})
