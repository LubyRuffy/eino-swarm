import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { emptyTranscript, reduceEvents } from "@/lib/transcript"
import { collectionEvent, stoppedCollectionEvents } from "@/test/worker-collection-events"
import { Transcript } from "./transcript"

describe("stopped worker collection transcript", () => {
  it("shows the same stopped conversation after Reload without an internal JSON card", () => {
    const events = stoppedCollectionEvents()
    const live = reduceEvents(reduceEvents(emptyTranscript(), events.slice(0, 5)), events.slice(5))
    const view = render(<Transcript state={live} loaded onSelectAgent={() => {}} />)
    const before = view.container.textContent
    view.rerender(<Transcript state={reduceEvents(emptyTranscript(), events)} loaded onSelectAgent={() => {}} />)
    expect(view.container.textContent).toBe(before)
    expect(view.container.textContent).not.toContain('"wait_id"')
    expect(view.container.textContent).not.toContain('"worker_ids"')
    expect(view.container.textContent).not.toContain('"running":true')
    expect(screen.getByText("Stopped unfinished workers.")).toBeVisible()
    expect(screen.getByTestId("iteration-limit").textContent).not.toContain("Continue")
    expect(screen.queryByRole("button", { name: /Continue this turn|Stop this turn/i })).not.toBeInTheDocument()
  })

  it("keeps unknown user-visible notices in the rendered fallback", () => {
    const state = reduceEvents(emptyTranscript(), [collectionEvent(20, "future_user_notice", { text: "A visible future notice." })])
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.getByText("A visible future notice.")).toBeVisible()
  })
})
