import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { emptyTranscript, type Block } from "@/lib/transcript"
import { useApp } from "@/store/app"
import { IterationLimitCard } from "./iteration-limit-card"

const before = useApp.getState()
afterEach(() => act(() => useApp.setState(before, true)))
const block: Block = { id: "pause", kind: "confirm", agentId: "manager", turnId: "turn", seq: 2,
  at: "2026-01-01T00:00:00Z", text: "", confirm: {
    limit: 80, extendBy: 2000, reason: "workers_uncollected", automaticResume: true, pending: true,
  } }

it("explains automatic collection waiting and preserves explicit Continue and Stop", () => {
  const extend = vi.fn()
  useApp.setState({ transcript: { ...emptyTranscript(), running: true }, extendTurn: extend })
  const { rerender } = render(<IterationLimitCard block={block} />)
  expect(screen.getByTestId("iteration-limit")).toHaveTextContent("resume once")
  expect(screen.getByTestId("iteration-limit")).not.toHaveTextContent(/80|2000|tool-round limit/)
  fireEvent.click(screen.getByRole("button", { name: "Continue to collect delegated results" }))
  expect(extend).toHaveBeenCalledWith(true)
  fireEvent.click(screen.getByRole("button", { name: "Stop waiting for delegated results" }))
  expect(extend).toHaveBeenCalledWith(false)
  rerender(<IterationLimitCard block={{ ...block, confirm: { ...block.confirm!, pending: false, continued: true } }} />)
  expect(screen.getByTestId("iteration-limit")).toHaveTextContent("Continuing to collect")
  expect(screen.queryByText(/resume once/)).not.toBeInTheDocument()
  expect(screen.queryByRole("button")).not.toBeInTheDocument()
})

it("does not promise automatic resumption for an exhausted collection allowance", () => {
  useApp.setState({ transcript: { ...emptyTranscript(), running: true } })
  render(<IterationLimitCard block={{ ...block, confirm: { ...block.confirm!, automaticResume: false } }} />)
  expect(screen.getByTestId("iteration-limit")).toHaveTextContent("Continue to collect them, or Stop")
  expect(screen.queryByText(/resume once/)).not.toBeInTheDocument()
  act(() => useApp.setState({ transcript: emptyTranscript() }))
  expect(screen.getByTestId("iteration-limit")).toHaveTextContent("Stopped before collecting")
})
