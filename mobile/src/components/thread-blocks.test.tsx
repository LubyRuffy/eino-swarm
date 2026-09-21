import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { setLocale } from "@/lib/i18n"

import { renderBlock } from "./thread-blocks"

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

  it("summarises a packed report tool from findings, not the envelope", () => {
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
    expect(screen.getByText("report_schedule")).toBeInTheDocument()
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
})
