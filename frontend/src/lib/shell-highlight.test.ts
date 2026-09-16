import { describe, expect, it } from "vitest"

import { tokenizeShell, type ShellToken, type ShellTokenKind } from "./shell-highlight"

function joined(tokens: ShellToken[]): string {
  return tokens.map((t) => t.text).join("")
}

function firstKind(tokens: ShellToken[], snippet: string): ShellTokenKind | undefined {
  return tokens.find((t) => t.text === snippet)?.kind
}

describe("tokenizeShell", () => {
  it("reconstructs the original command, including spaces and newlines", () => {
    const src = `echo "hi there" && ls -la\n# trailing`
    expect(joined(tokenizeShell(src))).toBe(src)
  })

  it("marks keywords, commands, strings, flags and operators", () => {
    const tokens = tokenizeShell(`if true; then echo "ok" && ls -la; fi`)
    expect(firstKind(tokens, "if")).toBe("keyword")
    expect(firstKind(tokens, "then")).toBe("keyword")
    expect(firstKind(tokens, "fi")).toBe("keyword")
    expect(firstKind(tokens, "echo")).toBe("command")
    expect(firstKind(tokens, "ls")).toBe("command")
    expect(firstKind(tokens, `"ok"`)).toBe("string")
    expect(firstKind(tokens, "-la")).toBe("flag")
    expect(firstKind(tokens, "&&")).toBe("operator")
    expect(firstKind(tokens, ";")).toBe("operator")
  })

  it("marks variables and comments", () => {
    const tokens = tokenizeShell('echo "$HOME" ${USER} # note')
    expect(firstKind(tokens, `"$HOME"`)).toBe("string")
    expect(firstKind(tokens, "${USER}")).toBe("variable")
    expect(tokens.some((t) => t.kind === "comment" && t.text.includes("note"))).toBe(true)
  })

  it("keeps a heredoc body as a string so a wrapped expand still shows every line", () => {
    const src = "cat <<END\nline one\nline two\nEND"
    const tokens = tokenizeShell(src)
    expect(joined(tokens)).toBe(src)
    expect(tokens.some((t) => t.kind === "string" && t.text.includes("line one"))).toBe(true)
    expect(firstKind(tokens, "cat")).toBe("command")
  })

  it("treats a quoted heredoc delimiter as a string, not a second command", () => {
    const src = "cat <<'END'\nnot a command\nEND"
    const tokens = tokenizeShell(src)
    expect(joined(tokens)).toBe(src)
    expect(tokens.filter((t) => t.kind === "command").map((t) => t.text)).toEqual(["cat"])
  })

  it("starts a new command after a pipe", () => {
    const tokens = tokenizeShell(`ls | grep foo`)
    expect(firstKind(tokens, "ls")).toBe("command")
    expect(firstKind(tokens, "grep")).toBe("command")
    expect(firstKind(tokens, "|")).toBe("operator")
  })

  it("does not invent tokens when the input is empty", () => {
    expect(tokenizeShell("")).toEqual([])
  })

  it("falls back to a single text token when a line is too long to paint", () => {
    const src = `echo ${"x".repeat(5000)}`
    const tokens = tokenizeShell(src)
    expect(joined(tokens)).toBe(src)
    expect(tokens).toEqual([{ kind: "text", text: src }])
  })
})
