import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { Textarea } from "./textarea"

describe("Textarea", () => {
  // An empty notes box with no chrome looks like leftover whitespace.
  // Same tokens as Input so a form field is recognisable before it has text.
  it("shows the same border as a one-line input", () => {
    render(<Textarea aria-label="notes" />)
    expect(screen.getByLabelText("notes").className).toMatch(/\bborder-input\b/)
  })
})
