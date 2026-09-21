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

  it("shows live stdout on a pending call instead of the waiting placeholder", () => {
    render(
      <ToolResultBody
        name="exec"
        args={`{"command":"printf x"}`}
        result={JSON.stringify({ stdout: "chunk-one", stderr: "" })}
        pending
      />,
    )
    expect(screen.getByTestId("tool-output").textContent).toBe("chunk-one")
    expect(screen.queryByText(/running/i)).not.toBeInTheDocument()
  })

  it("paints an edit as a highlighted diff, not the status line", () => {
    render(
      <ToolResultBody
        name="edit"
        args={JSON.stringify({
          file_path: "pkg/alpha.go",
          search_block: "func Alpha() {\n\treturn 0\n}\n",
          replace_block: "func Alpha() {\n\treturn 1\n}\n",
        })}
        result="ok: replaced block in pkg/alpha.go"
      />,
    )
    const body = screen.getByTestId("file-diff")
    expect(body).toHaveTextContent("return 0")
    expect(body).toHaveTextContent("return 1")
    expect(body.querySelector('[data-diff="del"]')).toHaveTextContent("return 0")
    expect(body.querySelector('[data-diff="add"]')).toHaveTextContent("return 1")
    expect(screen.queryByText(/ok: replaced block/)).not.toBeInTheDocument()
    expect(screen.queryByText(/search_block/)).not.toBeInTheDocument()
    expect(screen.getByText("func")).toHaveClass("text-syntax-keyword")
  })

  it("paints a patch edit from the hunk lines", () => {
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
    render(
      <ToolResultBody
        name="edit"
        args={JSON.stringify({ file_path: "pkg/alpha.go", patch })}
        result="ok: patched pkg/alpha.go"
      />,
    )
    expect(screen.getByTestId("file-diff").querySelector('[data-diff="del"]')).toHaveTextContent(
      "return 0",
    )
    expect(screen.queryByText(/ok: patched/)).not.toBeInTheDocument()
  })

  it("keeps a failed edit's error above the attempted change", () => {
    render(
      <ToolResultBody
        name="edit"
        args={JSON.stringify({
          file_path: "pkg/alpha.go",
          search_block: "missing",
          replace_block: "next",
        })}
        result="error: search_block not found in file"
        failed
      />,
    )
    expect(screen.getByRole("alert")).toHaveTextContent("search_block not found in file")
    expect(screen.getByTestId("file-diff").querySelector('[data-diff="del"]')).toHaveTextContent(
      "missing",
    )
  })

  it("paints a write as added lines, not the status sentence", () => {
    render(
      <ToolResultBody
        name="write"
        args={JSON.stringify({
          file_path: "pkg/alpha.go",
          content: "package alpha\nfunc Alpha() {}\n",
        })}
        result="Updated file /resolved/pkg/alpha.go (32 bytes)"
      />,
    )
    const body = screen.getByTestId("file-diff")
    expect(body.querySelectorAll('[data-diff="add"]')).toHaveLength(2)
    expect(body.querySelector('[data-diff="del"]')).toBeNull()
    expect(screen.getByText("package")).toHaveClass("text-syntax-keyword")
    expect(screen.getByText("func")).toHaveClass("text-syntax-keyword")
    expect(screen.queryByText(/Updated file/)).not.toBeInTheDocument()
    expect(screen.queryByText(/"content"/)).not.toBeInTheDocument()
  })

  it("shows an empty write as an empty file, not a status line", () => {
    render(
      <ToolResultBody
        name="write"
        args={JSON.stringify({ file_path: "pkg/alpha.go", content: "" })}
        result="Updated file /resolved/pkg/alpha.go (32 bytes)"
      />,
    )
    expect(screen.getByTestId("file-diff")).toHaveTextContent("pkg/alpha.go")
    expect(screen.getByTestId("file-diff")).not.toHaveTextContent("+0")
    expect(screen.getByText("(empty file)")).toBeInTheDocument()
    expect(screen.queryByText(/Updated file/)).not.toBeInTheDocument()
  })

  it("clips a huge write so the transcript does not mount every line", () => {
    const content = Array.from({ length: 401 }, (_, i) => `L${i}`).join("\n") + "\n"
    render(
      <ToolResultBody
        name="write"
        args={JSON.stringify({ file_path: "pkg/alpha.go", content })}
        result="Updated file /resolved/pkg/alpha.go (32 bytes)"
      />,
    )
    const body = screen.getByTestId("file-diff")
    expect(body).toHaveTextContent("+401")
    expect(body.querySelectorAll('[data-diff="add"]')).toHaveLength(400)
    expect(screen.getByTestId("file-diff-more")).toHaveTextContent("1 more lines")
    expect(screen.queryByText("L400")).not.toBeInTheDocument()
  })

  it("keeps a failed write's error above the attempted body", () => {
    render(
      <ToolResultBody
        name="write"
        args={JSON.stringify({
          file_path: "pkg/alpha.go",
          content: "package alpha\n",
        })}
        result="error: failed to write file"
        failed
      />,
    )
    expect(screen.getByRole("alert")).toHaveTextContent("failed to write file")
    expect(screen.getByTestId("file-diff").querySelector('[data-diff="add"]')).toHaveTextContent(
      "package alpha",
    )
  })

  it("treats CR as overwrite in live output", () => {
    render(
      <ToolResultBody
        name="exec"
        args={`{"command":"printf x"}`}
        result={JSON.stringify({ stdout: "step 1\rstep 2", stderr: "" })}
        pending
      />,
    )
    expect(screen.getByTestId("tool-output").textContent).toBe("step 2")
  })
})
