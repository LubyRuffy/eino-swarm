import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { setLocale } from "@/lib/i18n"

import { renderBlock, ThreadLog } from "./thread-blocks"

describe("thread blocks", () => {
  it("renders markdown in a user bubble instead of the source markers", () => {
    render(
      <>
        {renderBlock({
          id: "1",
          kind: "user",
          text: "see **alpha** and [docs](https://example.invalid/docs)",
        })}
      </>,
    )
    expect(screen.getByText("alpha").closest("strong")).toBeTruthy()
    expect(screen.queryByText(/\*\*alpha\*\*/)).not.toBeInTheDocument()
    expect(screen.getByRole("link", { name: "docs" })).toHaveAttribute(
      "href",
      "https://example.invalid/docs",
    )
  })

  it("renders markdown in a steer bubble", () => {
    render(<>{renderBlock({ id: "2", kind: "steer", text: "keep **going**" })}</>)
    expect(screen.getByText("going").closest("strong")).toBeTruthy()
  })

  // A bubble stretched to the column for three words is a banner, and the
  // eye loses which side of the conversation it is reading.
  it("keeps what you said hugging its own text on your side of the column", () => {
    const { container } = render(
      <>{renderBlock({ id: "u", kind: "user", text: "hi" })}</>,
    )
    const bubble = container.firstElementChild as HTMLElement
    expect(bubble.className).toContain("ml-auto")
    expect(bubble.className).toContain("w-fit")
    expect(bubble.className).not.toContain("max-w-full")
  })

  it("splits a quoted user bubble so the tags are not the message", () => {
    render(
      <>
        {renderBlock({
          id: "q",
          kind: "user",
          text: "<selected_text>\nalpha\n</selected_text>\n\n<user_request>\ndo **this**\n</user_request>",
        })}
      </>,
    )
    expect(screen.getByTestId("quoted-message")).toHaveTextContent("Selected text:")
    expect(screen.getByTestId("quoted-message")).toHaveTextContent("alpha")
    expect(screen.getByText("this").closest("strong")).toBeTruthy()
    expect(screen.queryByText(/<selected_text>/)).not.toBeInTheDocument()
  })

  it("localizes an armed-wait notice instead of painting the schedule JSON", () => {
    setLocale("zh")
    render(<>{renderBlock({ id: "3", kind: "notice", text: "A wait is armed." })}</>)
    expect(screen.getByText("已设置等待。")).toBeInTheDocument()
    expect(screen.queryByText(/sch_/)).not.toBeInTheDocument()
    setLocale("en")
  })

  it("shows report findings as a notice, not the tool name", () => {
    render(
      <>
        {renderBlock({
          id: "4",
          kind: "tool",
          toolName: "report_schedule",
          text: JSON.stringify({ ok: true, quiet: false }),
          args: JSON.stringify({ findings: "one thing changed", quiet: false }),
        })}
      </>,
    )
    expect(screen.queryByText("report_schedule")).not.toBeInTheDocument()
    expect(screen.getByText("one thing changed")).toBeInTheDocument()
    expect(screen.queryByText(/"ok":true/)).not.toBeInTheDocument()
  })

  it("summarises a wait roster as counts, not the elapsed_ms dump", () => {
    setLocale("en")
    const report = JSON.stringify({
      elapsed_ms: 0,
      agents: [
        { agent_id: "w1", role: "worker", status: "done", elapsed_ms: 0 },
        { agent_id: "w2", role: "helper", status: "failed", elapsed_ms: 0 },
      ],
    })
    render(
      <>
        {renderBlock({
          id: "5",
          kind: "tool",
          toolName: "wait_agents",
          text: report,
        })}
        {renderBlock({
          id: "6",
          kind: "user",
          text: "keep going\n" + report,
        })}
        {renderBlock({
          id: "7",
          kind: "notice",
          text: report,
        })}
      </>,
    )
    expect(screen.getByText("wait_agents")).toBeInTheDocument()
    expect(screen.getByText("1 done · 1 failed")).toBeInTheDocument()
    expect(screen.getByText("keep going")).toBeInTheDocument()
    expect(screen.queryByText(/elapsed_ms/)).not.toBeInTheDocument()
    expect(screen.queryByText(/"agent_id"/)).not.toBeInTheDocument()
    expect(screen.queryByText(/w1/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: /wait_agents/ }))
    expect(screen.getByText(/worker done/)).toBeInTheDocument()
    expect(screen.getByText(/helper failed/)).toBeInTheDocument()
    expect(screen.queryByText(/elapsed_ms/)).not.toBeInTheDocument()
  })

  it("hides a wake tool chip", () => {
    const { container } = render(
      <>
        {renderBlock({
          id: "w",
          kind: "tool",
          toolName: "schedule_wake",
          text: "",
          args: `{"every_s":30,"prompt":"Continue the wait."}`,
        })}
      </>,
    )
    expect(container).toBeEmptyDOMElement()
    expect(screen.queryByText("schedule_wake")).not.toBeInTheDocument()
  })

  it("hides close_agent bookkeeping", () => {
    const { container } = render(
      <>
        {renderBlock({
          id: "8",
          kind: "tool",
          toolName: "close_agent",
          text: '{"cancelled":false,"already_finished":true}',
        })}
      </>,
    )
    expect(container).toBeEmptyDOMElement()
    expect(screen.queryByText("close_agent")).not.toBeInTheDocument()
  })

  it("keeps a long tool preview inside the row so the log is not stretched", () => {
    render(
      <>
        {renderBlock({
          id: "path",
          kind: "tool",
          toolName: "exec",
          text: "E2=" + "x".repeat(120) + "/run",
          pending: true,
        })}
      </>,
    )
    const row = screen.getByRole("button")
    expect(row).toHaveClass("min-w-0")
    expect(row).toHaveClass("max-w-full")
  })

  it("wraps a long assistant line instead of stretching the thread", () => {
    render(
      <>
        {renderBlock({
          id: "a",
          kind: "answer",
          text: "see `" + "x".repeat(80) + "`",
        })}
      </>,
    )
    expect(document.querySelector(".md-body")).toHaveClass("break-words")
  })

  it("keeps a mid-turn answer on screen and folds only adjacent work", () => {
    setLocale("en")
    render(
      <ThreadLog
        blocks={[
          { id: "r", kind: "reasoning", text: "first pass" },
          {
            id: "k",
            kind: "tool",
            toolName: "read",
            text: "read",
            args: `{"file_path":"src/lib/appearance.ts"}`,
          },
          { id: "a", kind: "answer", text: "still working the layout" },
          { id: "k2", kind: "tool", toolName: "grep", text: "grep" },
          { id: "a2", kind: "answer", text: "here is the result" },
        ]}
      />,
    )
    expect(screen.getByText("still working the layout")).toBeInTheDocument()
    expect(screen.getByText("here is the result")).toBeInTheDocument()
    expect(screen.getAllByTestId("work-fold")).toHaveLength(2)
    expect(screen.queryByText("read")).not.toBeInTheDocument()
    expect(screen.queryByText("grep")).not.toBeInTheDocument()
    expect(screen.queryByText("first pass")).not.toBeInTheDocument()
    fireEvent.click(screen.getAllByTestId("work-fold")[0])
    expect(screen.getByTestId("phone-thought")).toHaveTextContent("first pass")
    expect(screen.getByText("read")).toBeInTheDocument()
  })

  it("merges however many adjacent thoughts and tools sit together", () => {
    setLocale("en")
    render(
      <ThreadLog
        blocks={[
          { id: "r", kind: "reasoning", text: "first pass" },
          { id: "k", kind: "tool", toolName: "read", text: "read" },
          { id: "r2", kind: "reasoning", text: "next" },
          { id: "k2", kind: "tool", toolName: "grep", text: "grep" },
          { id: "a", kind: "answer", text: "done" },
        ]}
      />,
    )
    expect(screen.getAllByTestId("work-fold")).toHaveLength(1)
    expect(screen.getByTestId("work-fold")).toHaveTextContent("Thought · 2 tools")
    expect(screen.getByText("done")).toBeInTheDocument()
  })

  it("tickers only the latest work row, and not once an answer is already on screen", () => {
    setLocale("en")
    render(
      <ThreadLog
        running
        blocks={[
          { id: "r", kind: "reasoning", text: "first pass" },
          {
            id: "k",
            kind: "tool",
            toolName: "read",
            text: "read",
            pending: false,
          },
          { id: "a", kind: "answer", text: "partial" },
          {
            id: "ex",
            kind: "tool",
            toolName: "exec",
            text: "exec",
            args: `{"command":"printf x"}`,
            pending: true,
          },
        ]}
      />,
    )
    const folds = screen.getAllByTestId("work-fold")
    expect(folds[0]).toHaveTextContent("Thought · 1 tool")
    expect(folds[0]).not.toHaveTextContent("Planning next moves")
    expect(folds[1]).toHaveTextContent("Exec printf x")
    expect(screen.getByText("partial")).toBeInTheDocument()
  })

  it("does not keep Planning on a fold once the answer is on screen", () => {
    setLocale("en")
    render(
      <ThreadLog
        running
        blocks={[
          { id: "r", kind: "reasoning", text: "first pass" },
          { id: "k", kind: "tool", toolName: "read", text: "read", pending: false },
          { id: "a", kind: "answer", text: "partial", streaming: true },
        ]}
      />,
    )
    expect(screen.getByTestId("work-fold")).toHaveTextContent("Thought · 1 tool")
    expect(screen.getByTestId("work-fold")).not.toHaveTextContent("Planning next moves")
    expect(screen.getByText("partial")).toBeInTheDocument()
    expect(screen.queryByTestId("planning-tail")).not.toBeInTheDocument()
  })

  it("puts Planning under a closed answer instead of on the fold above it", () => {
    setLocale("en")
    render(
      <ThreadLog
        running
        blocks={[
          { id: "r", kind: "reasoning", text: "first pass" },
          { id: "k", kind: "tool", toolName: "read", text: "read", pending: false },
          { id: "a", kind: "answer", text: "landed" },
        ]}
      />,
    )
    expect(screen.getByTestId("work-fold")).toHaveTextContent("Thought · 1 tool")
    expect(screen.getByTestId("work-fold")).not.toHaveTextContent("Planning next moves")
    const tail = screen.getByTestId("planning-tail")
    expect(tail).toHaveTextContent("Planning next moves")
    expect(screen.getByText("landed").compareDocumentPosition(tail) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })
})
