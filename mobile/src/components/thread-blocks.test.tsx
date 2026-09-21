import { render, screen } from "@testing-library/react"
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
})
