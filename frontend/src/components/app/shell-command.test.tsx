import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { ShellCommand } from "./shell-command"

describe("ShellCommand", () => {
  it("wraps the full command instead of truncating it", () => {
    const command = "cd /tmp/workspace/pkg && for d in alpha beta; do echo $d; done"
    render(<ShellCommand command={command} />)
    const el = screen.getByTestId("shell-command")
    expect(el.textContent).toContain(command)
    expect(el.className).toMatch(/whitespace-pre-wrap/)
    expect(el.className).not.toMatch(/\btruncate\b/)
  })

  it("keeps heredoc bodies on their own lines", () => {
    const command = "cat <<END\nline one\nline two\nEND"
    render(<ShellCommand command={command} />)
    expect(screen.getByTestId("shell-command").textContent).toContain("line one")
    expect(screen.getByTestId("shell-command").textContent).toContain("line two")
  })

  it("paints keywords and strings onto tokens, not the whole line", () => {
    render(<ShellCommand command={`echo "ok" && ls -la`} />)
    expect(screen.getByText("echo")).toHaveClass("text-syntax-command")
    expect(screen.getByText(`"ok"`)).toHaveClass("text-syntax-string")
    expect(screen.getByText("&&")).toHaveClass("text-syntax-operator")
    expect(screen.getByText("-la")).toHaveClass("text-syntax-flag")
  })

  it("truncates on one line in the collapsed header", () => {
    render(<ShellCommand command="echo hi && ls -la" compact />)
    const el = screen.getByTestId("shell-command-preview")
    expect(el.className).toMatch(/\btruncate\b/)
    expect(el.className).not.toMatch(/whitespace-pre-wrap/)
    expect(el.textContent).not.toMatch(/^\$/)
  })
})
