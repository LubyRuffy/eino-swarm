import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { ToolRow } from "./tool-row"
import type { Block } from "@/lib/transcript"
import { useApp } from "@/store/app"

function toolBlock(partial: Partial<NonNullable<Block["tool"]>>): Block {
  return {
    id: "b1",
    kind: "tool",
    agentId: "manager",
    text: partial.name ?? "exec",
    tool: {
      callId: "c1",
      name: "exec",
      args: `{"command":"printf x"}`,
      pending: false,
      ...partial,
    },
    turnId: "t1",
    seq: 1,
    at: new Date().toISOString(),
  }
}

const searchHits = JSON.stringify({
  results: [{ title: "One", url: "https://example.invalid/one", summary: "first hit" }],
})

describe("ToolRow", () => {
  it.each([["en", "Inspect history schema"], ["zh", "查看历史数据结构"]] as const)("labels history discovery in %s", (locale, label) => {
    const previous = useApp.getState().locale
    useApp.setState({ locale })
    const rendered = render(<ToolRow block={toolBlock({ name: "query_history", args: "{}", result: '{"ok":true,"tables":[]}' })} />)
    try {
      expect(screen.getByRole("button", { name: new RegExp(`query_history.*${label}`) })).toBeInTheDocument()
    } finally {
      rendered.unmount()
      useApp.setState({ locale: previous })
    }
  })

  it("marks a refused history query red without discarding its repairable error", () => {
    render(<ToolRow block={toolBlock({ name: "query_history", args: '{"sql":"DELETE FROM messages"}', result: '{"ok":false,"error":{"code":"query_failed","message":"Only read-only queries are allowed"}}' })} />)
    const toggle = screen.getByRole("button", { name: /query_history/ })
    expect(toggle).toHaveAttribute("aria-invalid", "true")
    expect(toggle).toHaveTextContent("Only read-only queries are allowed")
    fireEvent.click(toggle)
    expect(screen.getByRole("alert")).toHaveTextContent("Only read-only queries are allowed")
    expect(screen.getByTestId("tool-output")).toHaveTextContent('"code":"query_failed"')
  })

  it("keeps a skill patch collapsed and opens its original change after completion", () => {
    const block = toolBlock({
      name: "skill_manage", pending: true,
      args: JSON.stringify({ action: "patch", name: "release-checklist", old_text: "  old step", new_text: "  new step" }),
    })
    const { rerender } = render(<ToolRow block={block} />)
    const toggle = screen.getByRole("button", { name: /skill_manage/ })
    expect(toggle).toHaveAttribute("aria-expanded", "false")
    expect(toggle).toHaveTextContent("+1")
    expect(toggle).toHaveTextContent("−1")
    expect(screen.queryByTestId("file-diff")).not.toBeInTheDocument()
    fireEvent.click(toggle)
    rerender(<ToolRow block={{ ...block, tool: { ...block.tool!, pending: false, result: JSON.stringify({ success: true, name: "release-checklist" }) } }} />)
    const diff = screen.getByTestId("file-diff")
    expect(toggle).toHaveAttribute("aria-expanded", "true")
    expect(diff.querySelector('[data-diff="del"]')).toHaveTextContent("old step")
    expect(diff.querySelector('[data-diff="add"]')).toHaveTextContent("new step")
    expect(screen.queryByTestId("tool-output")).not.toBeInTheDocument()
    fireEvent.click(toggle)
    expect(screen.queryByTestId("file-diff")).not.toBeInTheDocument()
  })

  it("retains a failed skill patch's error when the change is expanded", () => {
    render(<ToolRow block={toolBlock({
      name: "skill_manage",
      args: JSON.stringify({ action: "patch", name: "release-checklist", old_text: "missing step", new_text: "new step" }),
      result: JSON.stringify({ success: false, error: "text not found" }),
    })} />)
    const toggle = screen.getByRole("button", { name: /skill_manage/ })
    expect(toggle).toHaveAttribute("aria-invalid", "true")
    expect(toggle).toHaveTextContent("text not found")
    fireEvent.click(toggle)
    expect(screen.getByRole("alert")).toHaveTextContent("text not found")
    expect(screen.getByTestId("file-diff")).toHaveTextContent("missing step")
  })

  it("opens a pending non-exec call so live output is visible", () => {
    render(
      <ToolRow
        block={toolBlock({
          name: "web_search",
          args: `{"query":"alpha"}`,
          pending: true,
          result: searchHits,
        })}
      />,
    )
    expect(screen.getByText("One")).toBeInTheDocument()
  })

  it("keeps a finished call collapsed until the user opens it", () => {
    render(
      <ToolRow
        block={toolBlock({
          result: JSON.stringify({
            exit_code: 0,
            stdout: "done",
            stderr: "",
            failed: false,
          }),
        })}
      />,
    )
    expect(screen.queryByTestId("tool-output")).not.toBeInTheDocument()
  })

  it("opens a collapsed exec when find reveals it", () => {
    render(
      <ToolRow
        block={toolBlock({
          result: JSON.stringify({
            exit_code: 0,
            stdout: "done",
            stderr: "",
            failed: false,
          }),
        })}
        reveal
      />,
    )
    expect(screen.getByTestId("tool-output").textContent).toBe("done")
  })

  // A long exec used to force the body open. The dump is the thing they
  // came to hide; the latest line on the row is enough to see it is alive.
  it("does not dump exec stdout until the row is opened", () => {
    render(
      <ToolRow
        block={toolBlock({
          pending: true,
          result: JSON.stringify({ stdout: "chunk-one\nchunk-two", stderr: "" }),
        })}
      />,
    )
    expect(screen.queryByTestId("tool-output")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: /exec/ })).toHaveTextContent("chunk-two")
    fireEvent.click(screen.getByRole("button", { name: /exec/ }))
    expect(screen.getByTestId("tool-output").textContent).toBe("chunk-one\nchunk-two")
  })

  it("keeps a finished exec collapsed when the reader never opened it", () => {
    const { rerender } = render(
      <ToolRow
        block={toolBlock({
          pending: true,
          result: JSON.stringify({ stdout: "chunk-one", stderr: "" }),
        })}
      />,
    )
    expect(screen.queryByTestId("tool-output")).not.toBeInTheDocument()
    rerender(
      <ToolRow
        block={toolBlock({
          result: JSON.stringify({
            exit_code: 0,
            stdout: "chunk-one\ndone",
            stderr: "",
            failed: false,
          }),
        })}
      />,
    )
    expect(screen.queryByTestId("tool-output")).not.toBeInTheDocument()
  })

  it("keeps exec open after it finishes if the reader opened it", () => {
    const { rerender } = render(
      <ToolRow
        block={toolBlock({
          pending: true,
          result: JSON.stringify({ stdout: "chunk-one", stderr: "" }),
        })}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: /exec/ }))
    expect(screen.getByTestId("tool-output").textContent).toBe("chunk-one")
    rerender(
      <ToolRow
        block={toolBlock({
          result: JSON.stringify({
            exit_code: 0,
            stdout: "done",
            stderr: "",
            failed: false,
          }),
        })}
      />,
    )
    expect(screen.getByTestId("tool-output").textContent).toBe("done")
  })

  it("keeps a finished edit collapsed until the user opens it", () => {
    const args = JSON.stringify({
      file_path: "pkg/alpha.go",
      search_block: "return 0",
      replace_block: "return 1",
    })
    render(
      <ToolRow
        block={toolBlock({
          name: "edit",
          args,
          result: "ok: replaced block in pkg/alpha.go",
        })}
      />,
    )
    expect(screen.getByRole("button", { name: /edit/ })).toHaveTextContent("+1")
    expect(screen.getByRole("button", { name: /edit/ })).toHaveTextContent("−1")
    expect(screen.getByTestId("marquee")).toHaveTextContent("pkg/alpha.go")
    expect(screen.getByTestId("marquee")).not.toHaveTextContent("+1")
    const counts = screen.getByTestId("edit-counts")
    expect(counts).toHaveTextContent("+1")
    expect(counts).toHaveTextContent("−1")
    expect(screen.getByText("+1")).toHaveClass("group-hover:text-diff-add")
    expect(screen.getByText("+1")).not.toHaveClass("text-diff-add")
    expect(screen.getByText("−1")).toHaveClass("group-hover:text-diff-del")
    expect(screen.getByText("−1")).not.toHaveClass("text-diff-del")
    expect(screen.queryByTestId("file-diff")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: /edit/ }))
    expect(screen.getByTestId("file-diff")).toBeInTheDocument()
  })

  it("keeps a finished write collapsed until the user opens it", () => {
    render(
      <ToolRow
        block={toolBlock({
          name: "write",
          args: JSON.stringify({
            file_path: "pkg/alpha.go",
            content: "package alpha\n",
          }),
          result: "Updated file /resolved/pkg/alpha.go (32 bytes)",
        })}
      />,
    )
    expect(screen.getByRole("button", { name: /write/ })).toHaveTextContent("+1")
    expect(screen.getByTestId("edit-counts")).toHaveTextContent("+1")
    expect(screen.getByTestId("edit-counts")).not.toHaveTextContent("−")
    expect(screen.queryByTestId("file-diff")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: /write/ }))
    expect(screen.getByTestId("file-diff")).toBeInTheDocument()
  })

  it("does not dump a pending write hunk until the row is opened", () => {
    render(
      <ToolRow
        block={toolBlock({
          name: "write",
          args: JSON.stringify({
            file_path: "pkg/alpha.go",
            content: "package alpha\n",
          }),
          pending: true,
        })}
      />,
    )
    expect(screen.queryByTestId("file-diff")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: /write/ })).toHaveTextContent("+1")
    fireEvent.click(screen.getByRole("button", { name: /write/ }))
    expect(screen.getByTestId("file-diff")).toBeInTheDocument()
  })

  it("keeps write open after it finishes if the reader opened it", () => {
    const args = JSON.stringify({
      file_path: "pkg/alpha.go",
      content: "package alpha\n",
    })
    const { rerender } = render(
      <ToolRow
        block={toolBlock({
          name: "write",
          args,
          pending: true,
        })}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: /write/ }))
    expect(screen.getByTestId("file-diff")).toBeInTheDocument()
    rerender(
      <ToolRow
        block={toolBlock({
          name: "write",
          args,
          result: "Updated file /resolved/pkg/alpha.go (32 bytes)",
        })}
      />,
    )
    expect(screen.getByTestId("file-diff")).toBeInTheDocument()
  })

  it("folds a non-exec tool when it finishes unless the reader opened it", () => {
    const { rerender } = render(
      <ToolRow
        block={toolBlock({
          name: "web_search",
          args: `{"query":"alpha"}`,
          pending: true,
          result: searchHits,
        })}
      />,
    )
    expect(screen.getByText("One")).toBeInTheDocument()
    rerender(
      <ToolRow
        block={toolBlock({
          name: "web_search",
          args: `{"query":"alpha"}`,
          result: searchHits,
        })}
      />,
    )
    expect(screen.queryByText("One")).not.toBeInTheDocument()
  })
})
