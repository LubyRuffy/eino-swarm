import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { AskCard } from "./ask-card"
import { parseAskToolArgs } from "@/lib/ask"

describe("AskCard", () => {
  it("submits the picked option label", async () => {
    const onSubmit = vi.fn()
    const questions = parseAskToolArgs(
      '{"questions":[{"id":"q1","prompt":"Which?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}]}',
    )!
    render(<AskCard questions={questions} onSubmit={onSubmit} />)
    fireEvent.click(screen.getByRole("button", { name: "A" }))
    fireEvent.click(screen.getByTestId("ask-submit"))
    expect(onSubmit).toHaveBeenCalledWith({ q1: { answers: ["A"] } })
    await screen.findByRole("status")
  })

  it("keeps an Other draft and shows a failed submission before retrying", async () => {
    const onSubmit = vi.fn()
      .mockRejectedValueOnce(new Error("PC rejected the answer"))
      .mockResolvedValueOnce(undefined)
    const questions = parseAskToolArgs(JSON.stringify({ questions: [{
      id: "test-window", prompt: "When?", options: [
        { id: "now", label: "Now" }, { id: "later", label: "Later" },
      ],
    }] }))!
    render(<AskCard questions={questions} onSubmit={onSubmit} />)
    fireEvent.click(screen.getByRole("button", { name: "Other" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Other" }), { target: { value: "After review" } })
    fireEvent.click(screen.getByTestId("ask-submit"))
    await screen.findByRole("alert")
    expect(screen.getByRole("alert")).toHaveTextContent("PC rejected the answer")
    expect(screen.getByRole("textbox", { name: "Other" })).toHaveValue("After review")
    fireEvent.click(screen.getByTestId("ask-submit"))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(2))
    expect(screen.getByTestId("ask-submit")).toBeDisabled()
  })
})
