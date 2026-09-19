import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { AskCard } from "./ask-card"
import { parseAskToolArgs } from "@/lib/ask"

describe("AskCard", () => {
  it("submits the picked option label", () => {
    const onSubmit = vi.fn()
    const questions = parseAskToolArgs(
      '{"questions":[{"id":"q1","prompt":"Which?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}]}',
    )!
    render(<AskCard questions={questions} onSubmit={onSubmit} />)
    fireEvent.click(screen.getByRole("button", { name: "A" }))
    fireEvent.click(screen.getByTestId("ask-submit"))
    expect(onSubmit).toHaveBeenCalledWith({ q1: { answers: ["A"] } })
  })
})
