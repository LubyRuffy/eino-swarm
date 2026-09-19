import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { AskCardView } from "./ask-card"
import type { AskCard } from "@/lib/transcript-ask"

const answerAsk = vi.fn(async () => undefined)

vi.mock("@/store/app", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/store/app")>()
  const useApp = Object.assign(
    (sel: (s: { answerAsk: typeof answerAsk } & Record<string, unknown>) => unknown) =>
      sel({ ...actual.useApp.getState(), answerAsk }),
    actual.useApp,
  )
  return { ...actual, useApp }
})

function card(partial: Partial<AskCard> = {}): AskCard {
  return {
    callId: "tc_1",
    pending: true,
    questions: [
      {
        id: "approach",
        prompt: "Which approach should this work take?",
        options: [
          { id: "safer", label: "Prefer the safer path" },
          { id: "faster", label: "Prefer the faster path" },
          { id: "other", label: "Other" },
        ],
      },
    ],
    ...partial,
  }
}

describe("AskCardView", () => {
  beforeEach(() => {
    answerAsk.mockClear()
  })

  it("posts the chosen label after Submit", async () => {
    render(<AskCardView card={card()} />)
    expect(screen.getByTestId("ask-card").textContent).toContain("Asking question")
    expect(screen.getByTestId("ask-submit")).toBeDisabled()
    fireEvent.click(screen.getByTestId("ask-option-safer"))
    expect(answerAsk).not.toHaveBeenCalled()
    fireEvent.submit(screen.getByTestId("ask-card").querySelector("form")!)
    await waitFor(() =>
      expect(answerAsk).toHaveBeenCalledWith("tc_1", {
        approach: { answers: ["Prefer the safer path"] },
      }),
    )
  })

  it("lets a digit pick the matching row", async () => {
    render(<AskCardView card={card()} />)
    fireEvent.keyDown(screen.getByRole("radiogroup"), { key: "2" })
    fireEvent.click(screen.getByTestId("ask-submit"))
    await waitFor(() =>
      expect(answerAsk).toHaveBeenCalledWith("tc_1", {
        approach: { answers: ["Prefer the faster path"] },
      }),
    )
  })

  it("keeps Other closed until that row is chosen", async () => {
    render(<AskCardView card={card()} />)
    expect(screen.queryByTestId("ask-other-approach")).toBeNull()
    fireEvent.click(screen.getByTestId("ask-option-other"))
    expect(screen.getByTestId("ask-other-approach")).toBeTruthy()
    fireEvent.click(screen.getByTestId("ask-submit"))
    expect(answerAsk).not.toHaveBeenCalled()
    fireEvent.change(screen.getByTestId("ask-other-approach"), {
      target: { value: "a different path" },
    })
    fireEvent.click(screen.getByTestId("ask-submit"))
    await waitFor(() =>
      expect(answerAsk).toHaveBeenCalledWith("tc_1", {
        approach: { answers: ["a different path"] },
      }),
    )
  })

  it("spans the conversation column instead of a dialog width", () => {
    render(<AskCardView card={card()} />)
    const el = screen.getByTestId("ask-card")
    const classes = el.className.split(/\s+/)
    expect(classes).toContain("w-full")
    // max-w-lg is the modal default; a cap here leaves a gutter beside the
    // rest of the transcript. The column already has --content-max.
    expect(classes.some((c) => c.startsWith("max-w-"))).toBe(false)
  })

  it("shows the settled answer instead of buttons", () => {
    render(
      <AskCardView
        card={card({
          pending: false,
          answers: { approach: "Prefer the safer path" },
        })}
      />,
    )
    expect(screen.queryByTestId("ask-option-safer")).toBeNull()
    expect(screen.getByTestId("ask-card").textContent).toContain(
      "Prefer the safer path",
    )
  })
})
