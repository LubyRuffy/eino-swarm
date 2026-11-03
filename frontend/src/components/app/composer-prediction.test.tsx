import { fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import { emptyTranscript } from "@/lib/transcript"
import type { ModelInfo } from "@/lib/types"
import { useApp } from "@/store/app"

import { Composer } from "./composer"

const models: ModelInfo[] = [
  {
    id: "default\tmock",
    provider_id: "default",
    provider_label: "Offline",
    label: "mock",
    model: "m",
    ready: true,
    default: true,
  },
]

const suggestion = "Continue from the latest reply"

function renderComposer(onSend = vi.fn()) {
  return render(
    <TooltipProvider>
      <Composer
        running={false}
        models={models}
        provider="default"
        model="m"
        onModelChange={vi.fn()}
        reasoning=""
        reasoningLevels={["low", "medium", "high"]}
        onReasoningChange={vi.fn()}
        onSend={onSend}
        onStop={vi.fn()}
        onUpload={vi.fn(async () => [])}
        focusSignal={0}
      />
    </TooltipProvider>,
  )
}

afterEach(() => {
  useApp.setState({ transcript: emptyTranscript(), meta: undefined })
})

describe("Composer next step", () => {
  it("inserts the placeholder on Tab and does not send it", () => {
    useApp.setState({
      transcript: { ...emptyTranscript(), prediction: { text: suggestion, turnId: "tn_1" } },
    })
    const onSend = vi.fn()
    renderComposer(onSend)
    const input = screen.getByTestId("composer-input")
    expect(input).toHaveAttribute("placeholder", suggestion)
    expect(input).toHaveAttribute("data-prediction", "true")
    fireEvent.keyDown(input, { key: "Tab" })
    expect(input).toHaveValue(suggestion)
    expect(onSend).not.toHaveBeenCalled()
    fireEvent.change(input, { target: { value: "" } })
    expect(input).toHaveAttribute("data-prediction", "true")
    fireEvent.keyDown(input, { key: "Escape" })
    expect(input).not.toHaveAttribute("data-prediction")
    expect(input).toHaveAttribute("placeholder", "Describe what you want done. It will delegate as needed.")
  })

  it("hides the suggestion when the setting is off or the draft is not empty", () => {
    useApp.setState({
      transcript: { ...emptyTranscript(), prediction: { text: suggestion, turnId: "tn_1" } },
      meta: { swarm: { composer_predictions: false } } as never,
    })
    renderComposer()
    const input = screen.getByTestId("composer-input")
    expect(input).not.toHaveAttribute("data-prediction")
    expect(input).toHaveAttribute("placeholder", "Describe what you want done. It will delegate as needed.")
  })
})
