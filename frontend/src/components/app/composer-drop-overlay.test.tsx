import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { ComposerDropOverlay } from "./composer-drop-overlay"

describe("ComposerDropOverlay", () => {
  it("prompts to drop, then shows a loading state while files are added", () => {
    const { rerender } = render(<ComposerDropOverlay busy={false} />)
    const overlay = screen.getByTestId("composer-drop-overlay")
    expect(overlay).toHaveTextContent("Drop files to attach")
    expect(overlay).not.toHaveAttribute("aria-busy", "true")

    rerender(<ComposerDropOverlay busy />)
    expect(screen.getByTestId("composer-drop-overlay")).toHaveTextContent("Adding files")
    expect(screen.getByTestId("composer-drop-overlay")).toHaveAttribute("aria-busy", "true")
  })
})
