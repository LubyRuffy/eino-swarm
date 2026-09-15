import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { Composer } from "./composer"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { ModelInfo } from "@/lib/types"

function renderComposer(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  const models: ModelInfo[] = [
    { id: "default", label: "Default", model: "m", ready: true },
  ]
  return render(
    <TooltipProvider>
      <Composer
        running={false}
        models={models}
        provider="default"
        onProviderChange={vi.fn()}
        reasoning=""
        reasoningLevels={["low", "medium", "high"]}
        onReasoningChange={vi.fn()}
        onSend={vi.fn()}
        onStop={vi.fn()}
        onUpload={vi.fn(async () => {})}
        focusSignal={0}
        {...props}
      />
    </TooltipProvider>,
  )
}

describe("Composer thinking level", () => {
  // The trigger has to show the current level, or a user cannot tell whether a
  // conversation is thinking hard or on its default before they send.
  it("labels the empty default as Default and a set level by name", () => {
    renderComposer({ reasoning: "" })
    expect(screen.getByLabelText("Thinking level").textContent).toContain(
      "Default",
    )
  })

  it("labels a set level by name", () => {
    renderComposer({ reasoning: "high" })
    expect(screen.getByLabelText("Thinking level").textContent).toContain("High")
  })

  // An endpoint that reports no levels (older server) must not render a control
  // that would send an empty, meaningless choice.
  it("hides the control when the server offers no levels", () => {
    renderComposer({ reasoningLevels: [] })
    expect(screen.queryByLabelText("Thinking level")).toBeNull()
  })
})
