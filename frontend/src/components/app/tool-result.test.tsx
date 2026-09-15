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
      "2|func main() {}",
    ].join("\n")
    render(
      <ToolResultBody
        name="read"
        args={`{"file_path":"main.go"}`}
        result={result}
      />,
    )
    expect(screen.getByText("package main")).toBeInTheDocument()
    expect(screen.getByText("1")).toBeInTheDocument()
    expect(screen.getByText("2")).toBeInTheDocument()
  })

  it("shows command stdout, not the JSON envelope", () => {
    render(
      <ToolResultBody
        name="exec"
        args={`{"command":"echo ok"}`}
        result={JSON.stringify({ exit_code: 0, stdout: "ok\nline", stderr: "", failed: false })}
      />,
    )
    const pre = document.querySelector("pre")
    expect(pre?.textContent).toBe("ok\nline")
    expect(screen.queryByText(/exit_code/)).not.toBeInTheDocument()
    expect(screen.queryByText(/"command"/)).not.toBeInTheDocument()
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
