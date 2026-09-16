import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { ToolResultBody } from "./tool-result"

describe("ToolResultBody", () => {
  it("renders a markdown file as markdown, not a mashed one-liner", () => {
    const result = [
      "encoding=utf-8 path=notes.md offset=1 limit=200",
      "1|# heading",
      "2|a short paragraph",
    ].join("\n")
    render(
      <ToolResultBody
        name="read"
        args={`{"file_path":"notes.md"}`}
        result={result}
      />,
    )
    expect(screen.getByRole("heading", { name: "heading" })).toBeInTheDocument()
    expect(screen.getByText("a short paragraph")).toBeInTheDocument()
    expect(screen.queryByText(/encoding=utf-8/)).not.toBeInTheDocument()
    expect(screen.queryByText(/file_path/)).not.toBeInTheDocument()
  })

  it("shows numbered lines for a non-markdown file", () => {
    const result = [
      "encoding=utf-8 path=main.go offset=1 limit=200",
      "1|package main",
      "2|func Hello() {}",
    ].join("\n")
    render(
      <ToolResultBody
        name="read"
        args={`{"file_path":"main.go"}`}
        result={result}
      />,
    )
    expect(screen.getByText("package")).toHaveClass("text-syntax-keyword")
    expect(screen.getByText("main")).toHaveClass("text-syntax-command")
    expect(screen.getByText("func")).toHaveClass("text-syntax-keyword")
    expect(screen.getByText("Hello")).toHaveClass("text-syntax-command")
    expect(screen.getByText("1")).toBeInTheDocument()
    expect(screen.getByText("2")).toBeInTheDocument()
  })

  it("does not guess a language for an unknown suffix", () => {
    const result = [
      "encoding=utf-8 path=notes.txt offset=1 limit=200",
      "1|package main",
    ].join("\n")
    render(
      <ToolResultBody
        name="read"
        args={`{"file_path":"notes.txt"}`}
        result={result}
      />,
    )
    expect(screen.getByText("package main")).toBeInTheDocument()
    expect(document.querySelector(".text-syntax-keyword")).toBeNull()
  })

  it("shows command stdout, not the JSON envelope", () => {
    render(
      <ToolResultBody
        name="exec"
        args={`{"command":"echo ok"}`}
        result={JSON.stringify({ exit_code: 0, stdout: "ok\nline", stderr: "", failed: false })}
      />,
    )
    expect(screen.getByTestId("tool-output").textContent).toBe("ok\nline")
    expect(screen.queryByText(/exit_code/)).not.toBeInTheDocument()
    expect(screen.queryByText(/"command"/)).not.toBeInTheDocument()
  })

  it("shows the full wrapped command, not a truncated one-liner", () => {
    const command = "cd /tmp/workspace/pkg && for d in alpha beta gamma; do echo $d; done"
    render(
      <ToolResultBody
        name="exec"
        args={JSON.stringify({ command })}
        result={JSON.stringify({ exit_code: 0, stdout: "alpha\nbeta", stderr: "", failed: false })}
      />,
    )
    const cmd = screen.getByTestId("shell-command")
    expect(cmd.textContent).toContain(command)
    expect(cmd.className).toMatch(/whitespace-pre-wrap/)
    expect(cmd.className).not.toMatch(/\btruncate\b/)
    expect(screen.getByText("cd")).toHaveClass("text-syntax-command")
    expect(screen.getByText("&&")).toHaveClass("text-syntax-operator")
    expect(screen.getByTestId("tool-output").textContent).toBe("alpha\nbeta")
  })

  it("keeps a heredoc body when the row is expanded", () => {
    const command = "cat <<END\nline one\nline two\nEND"
    render(
      <ToolResultBody
        name="exec"
        args={JSON.stringify({ command })}
        result={JSON.stringify({ exit_code: 0, stdout: "ok", stderr: "", failed: false })}
      />,
    )
    expect(screen.getByTestId("shell-command").textContent).toContain("line one")
    expect(screen.getByTestId("shell-command").textContent).toContain("line two")
  })

  it("shows a failed command as an error, not a grey dump", () => {
    render(
      <ToolResultBody
        name="exec"
        args={`{"command":"nope"}`}
        result={JSON.stringify({
          exit_code: 127,
          stdout: "",
          stderr: "not found",
          failed: true,
          error: "exit status 127",
        })}
      />,
    )
    expect(screen.getByRole("alert")).toHaveTextContent("exit status 127")
    expect(screen.getByText("not found")).toBeInTheDocument()
    expect(screen.queryByText(/full_command/)).not.toBeInTheDocument()
  })

  it("lists search hits instead of the raw JSON", () => {
    render(
      <ToolResultBody
        name="web_search"
        args={`{"query":"alpha terms"}`}
        result={JSON.stringify({
          results: [{ title: "One", url: "https://example.invalid/one", summary: "first hit" }],
        })}
      />,
    )
    expect(screen.getByText("One")).toBeInTheDocument()
    expect(screen.getByText("first hit")).toBeInTheDocument()
    expect(screen.queryByText(/"title"/)).not.toBeInTheDocument()
  })
})
