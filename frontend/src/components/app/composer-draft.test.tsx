import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { Composer } from "./composer"
import { TooltipProvider } from "@/components/ui/tooltip"
import { ComposerDraftSession, type ComposerDraftSnapshot } from "@/lib/composer-draft"
import * as composerDraftHook from "@/lib/use-composer-draft"

afterEach(() => vi.restoreAllMocks())

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

function renderComposer(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  return render(<TooltipProvider><Composer running={false} models={[]}
    onModelChange={vi.fn()} reasoningLevels={[]} onReasoningChange={vi.fn()}
    onSend={vi.fn()} onStop={vi.fn()} onUpload={vi.fn(async () => [])}
    focusSignal={0} {...props} /></TooltipProvider>)
}

describe("Composer accepted sends", () => {
  it("keeps the full draft until the server acknowledges the send", async () => {
    const sent = deferred<boolean>()
    const onQuotesChange = vi.fn()
    const onSend = vi.fn(() => sent.promise)
    renderComposer({ onSend, quotes: [{ id: "q1", text: "quoted context" }], onQuotesChange })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "unfinished request" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSend).toHaveBeenCalledOnce()
    expect(input).toHaveValue("unfinished request")
    expect(onQuotesChange).not.toHaveBeenCalled()
    await act(async () => { sent.resolve(true); await sent.promise })
    await waitFor(() => expect(input).toHaveValue(""))
    expect(onQuotesChange).toHaveBeenCalledWith([])
  })

  it("retains the text when the send was refused", async () => {
    renderComposer({ onSend: vi.fn(async () => false) })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "retryable request" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    await act(async () => {})
    expect(input).toHaveValue("retryable request")
  })
})

describe("Composer draft recovery actions", () => {
  it.each<[string, Partial<ComposerDraftSnapshot>]>([
    ["save failure", { error: "Storage unavailable", saved: false }],
    ["restore failure", { error: "Could not restore", loaded: false, saved: false }],
    ["conflict", { conflict: true }],
    ["unknown send", { draft: { key: "thread:recovery", revision: 1, text: "Unsent input", quotes: [], files: [], state: "uncertain" as const } }],
  ])("keeps %s actions outside the disabled input boundary", (_problem, patch) => {
    const snapshot: ComposerDraftSnapshot = {
      draft: { key: "thread:recovery", revision: 1, text: "Unsent input", quotes: [], files: [], state: "editing" },
      loaded: true, loading: false, saving: false, saved: true, ...patch,
    }
    vi.spyOn(composerDraftHook, "useComposerDraft").mockReturnValue({ session: new ComposerDraftSession(snapshot.draft.key), snapshot })
    const view = renderComposer()
    // aria-disabled on the whole dock disables its descendants in the browser,
    // even when a recovery button has no native disabled attribute of its own.
    const status = screen.getByTestId("composer-draft-status")
    expect(status.closest('[aria-disabled="true"]')).toBeNull()
    view.rerender(<TooltipProvider><Composer running={false} models={[]} disabled
      onModelChange={vi.fn()} reasoningLevels={[]} onReasoningChange={vi.fn()}
      onSend={vi.fn()} onStop={vi.fn()} onUpload={vi.fn(async () => [])} focusSignal={0} /></TooltipProvider>)
    expect(status.closest('[aria-disabled="true"]')).not.toBeNull()
    for (const button of within(status).getAllByRole("button")) expect(button).toBeDisabled()
  })
})
