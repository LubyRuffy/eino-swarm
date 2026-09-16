import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { ContextMeter } from "./context-meter"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { UsageSnapshot } from "@/lib/types"

const sample: UsageSnapshot = {
  context_tokens: 71300,
  context_window: 256000,
  turn: {
    prompt_tokens: 12000,
    completion_tokens: 3100,
    cached_tokens: 4000,
    reasoning_tokens: 800,
    total_tokens: 15100,
    calls: 4,
  },
  thread: {
    prompt_tokens: 71300,
    completion_tokens: 3100,
    cached_tokens: 4000,
    reasoning_tokens: 800,
    total_tokens: 74400,
    calls: 4,
  },
}

describe("ContextMeter", () => {
  it("hides until there is something to count", () => {
    const { container } = render(
      <TooltipProvider>
        <ContextMeter
          usage={{
            ...sample,
            context_tokens: 0,
            turn: { ...sample.turn, calls: 0, total_tokens: 0, prompt_tokens: 0, completion_tokens: 0, cached_tokens: 0, reasoning_tokens: 0 },
            thread: { ...sample.thread, calls: 0, total_tokens: 0 },
          }}
          window={256000}
        />
      </TooltipProvider>,
    )
    expect(container.querySelector("[data-testid=context-meter]")).toBeNull()
  })

  it("names the percentage so a screen reader can find it", () => {
    render(
      <TooltipProvider>
        <ContextMeter usage={sample} window={256000} />
      </TooltipProvider>,
    )
    expect(screen.getByLabelText("28% context used")).toBeTruthy()
  })

  it("falls back to a count when the window is unknown", () => {
    render(
      <TooltipProvider>
        <ContextMeter
          usage={{ ...sample, context_window: 0 }}
          window={0}
          scale={80000}
        />
      </TooltipProvider>,
    )
    expect(screen.getByLabelText("71.3K tokens used")).toBeTruthy()
    const ring = document.querySelector("[data-fill]")
    expect(Number(ring?.getAttribute("data-fill"))).toBeGreaterThan(0)
  })

  it("keeps an empty ring when nothing can scale the arc", () => {
    render(
      <TooltipProvider>
        <ContextMeter usage={{ ...sample, context_window: 0 }} window={0} />
      </TooltipProvider>,
    )
    expect(document.querySelector("[data-fill]")?.getAttribute("data-fill")).toBe(
      "0.00",
    )
  })
})
