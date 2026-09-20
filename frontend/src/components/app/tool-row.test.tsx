import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { ToolRow } from "./tool-row"
import type { Block } from "@/lib/transcript"

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
          result: "Updated file pkg/alpha.go",
        })}
      />,
    )
    expect(screen.getByRole("button", { name: /write/ })).toHaveTextContent("+1")
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
          result: "Updated file pkg/alpha.go",
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
