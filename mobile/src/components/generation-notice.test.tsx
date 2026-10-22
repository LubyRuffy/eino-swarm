import { render, screen } from "@testing-library/react"
import { expect, it } from "vitest"
import { ThreadLog } from "./thread-blocks"
import { setLocale } from "@/lib/i18n"
import { applyEvent } from "@/lib/transcript"

it.each(["pending", "recovered", "none"])("shows the phone generation reason and %s recovery", (recovery) => {
  setLocale("en")
  const blocks = applyEvent([], { thread_id: "th", turn_id: "turn", kind: recovery === "recovered" ? "generation_recovered" : "generation_interrupted", seq: 2, text: JSON.stringify({ reason: "output_limit", detail: "max tokens", recovery }), created_at: "" })
  render(<ThreadLog blocks={blocks} running />)
  expect(screen.getByTestId("generation-notice")).toHaveTextContent(/output budget/)
  expect(screen.getByTestId("generation-notice")).toHaveTextContent(recovery === "pending" ? /pending/ : recovery === "recovered" ? /resumed/ : /not scheduled/)
})
