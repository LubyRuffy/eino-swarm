import { act, fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { setLocale } from "@/lib/i18n"
import { Composer } from "./composer"

describe("Composer", () => {
  it("sends the trimmed text and clears the box", () => {
    setLocale("en")
    const onSubmit = vi.fn()
    render(<Composer label="Message" sendLabel="Send" onSubmit={onSubmit} />)
    const box = screen.getByLabelText("Message")
    fireEvent.change(box, { target: { value: "  hello  " } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(onSubmit).toHaveBeenCalledWith("hello")
    expect(box).toHaveValue("")
  })

  // Nothing to send is the common state; an enabled button there is a lie.
  it("keeps send off until there is something to send", () => {
    setLocale("en")
    render(<Composer label="Message" sendLabel="Send" onSubmit={vi.fn()} />)
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled()
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "x" } })
    expect(screen.getByRole("button", { name: "Send" })).toBeEnabled()
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "   " } })
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled()
  })

  it("sends on Enter but keeps Shift+Enter for a new line", () => {
    setLocale("en")
    const onSubmit = vi.fn()
    render(<Composer label="Message" sendLabel="Send" onSubmit={onSubmit} />)
    const box = screen.getByLabelText("Message")
    fireEvent.change(box, { target: { value: "one" } })
    fireEvent.keyDown(box, { key: "Enter", shiftKey: true })
    expect(onSubmit).not.toHaveBeenCalled()
    fireEvent.keyDown(box, { key: "Enter" })
    expect(onSubmit).toHaveBeenCalledWith("one")
  })

  // A Pinyin or Kana candidate list commits on Enter. Sending there would
  // ship half a word.
  it("does not send while an IME is composing", () => {
    setLocale("en")
    const onSubmit = vi.fn()
    render(<Composer label="Message" sendLabel="Send" onSubmit={onSubmit} />)
    const box = screen.getByLabelText("Message")
    fireEvent.change(box, { target: { value: "ni" } })
    fireEvent.keyDown(box, { key: "Enter", isComposing: true })
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it("offers steer beside send while a turn is live", () => {
    setLocale("en")
    const onSteer = vi.fn()
    const onSubmit = vi.fn()
    render(
      <Composer
        label="Message"
        sendLabel="Follow-up"
        steerLabel="Steer"
        onSteer={onSteer}
        onSubmit={onSubmit}
      />,
    )
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "nudge" } })
    fireEvent.click(screen.getByRole("button", { name: "Steer" }))
    expect(onSteer).toHaveBeenCalledWith("nudge")
    expect(onSubmit).not.toHaveBeenCalled()
    expect(screen.getByLabelText("Message")).toHaveValue("")
  })

  it("has no steer control on an idle thread", () => {
    setLocale("en")
    render(<Composer label="Message" sendLabel="Send" onSubmit={vi.fn()} />)
    expect(screen.queryByRole("button", { name: "Steer" })).not.toBeInTheDocument()
  })

  it("refuses to send while the link is down", () => {
    setLocale("en")
    const onSubmit = vi.fn()
    render(<Composer label="Message" sendLabel="Send" disabled onSubmit={onSubmit} />)
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "x" } })
    fireEvent.keyDown(screen.getByLabelText("Message"), { key: "Enter" })
    expect(onSubmit).not.toHaveBeenCalled()
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled()
  })

  it("grows with the text instead of scrolling a one-line slot", () => {
    setLocale("en")
    render(<Composer label="Message" sendLabel="Send" onSubmit={vi.fn()} />)
    const box = screen.getByLabelText("Message") as HTMLTextAreaElement
    Object.defineProperty(box, "scrollHeight", { value: 68, configurable: true })
    fireEvent.change(box, { target: { value: "a\nb\nc" } })
    expect(box.style.height).toBe("68px")
    // Past the cap the transcript would lose its last answer to the keyboard.
    Object.defineProperty(box, "scrollHeight", { value: 900, configurable: true })
    fireEvent.change(box, { target: { value: "a\n".repeat(40) } })
    expect(box.style.height).toBe("132px")
  })

  it("renders the hint and whatever sits above the box", () => {
    setLocale("en")
    render(
      <Composer
        label="Message"
        sendLabel="Send"
        hint="queued"
        above={<p>chips</p>}
        onSubmit={vi.fn()}
      />,
    )
    expect(screen.getByText("queued")).toBeInTheDocument()
    expect(screen.getByText("chips")).toBeInTheDocument()
  })

  it("offers only the levels it was given and sends a file with no text", () => {
    setLocale("en")
    const onSubmit = vi.fn()
    render(
      <Composer
        label="Message"
        sendLabel="Send"
        models={[
          { provider_id: "a", provider_label: "Alpha", model: "one", default: true },
          { provider_id: "b", provider_label: "Beta", model: "two" },
        ]}
        reasoningLevels={["low"]}
        onSubmit={onSubmit}
      />,
    )
    // A select's minimum is the option text. Left at that width, the model
    // and the thinking level push the box off a phone.
    expect(screen.getByLabelText("Model")).toHaveClass("min-w-0")
    expect(screen.getByLabelText("Thinking level")).toHaveClass("min-w-0")
    expect(screen.getByLabelText("Message")).toHaveClass("min-w-0")
    expect(screen.getByRole("option", { name: "one" })).toBeInTheDocument()
    expect(screen.getByRole("option", { name: "two" })).toBeInTheDocument()
    expect(screen.getByRole("option", { name: "Low thinking" })).toBeInTheDocument()
    expect(screen.queryByRole("option", { name: "Medium thinking" })).not.toBeInTheDocument()
    const file = new File(["x"], "notes.txt", { type: "text/plain" })
    fireEvent.change(screen.getByTestId("file-input"), { target: { files: [file] } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(onSubmit).toHaveBeenCalledWith(
      "",
      expect.objectContaining({ files: [file], images: [], model: "one" }),
    )
  })

  it("can send a quote alone and remove it before sending", () => {
    const onSubmit = vi.fn()
    const onQuotesChange = vi.fn()
    const { rerender } = render(
      <Composer label="Message" sendLabel="Send" quotes={["alpha"]} onQuotesChange={onQuotesChange} onSubmit={onSubmit} />,
    )
    expect(screen.getByRole("button", { name: "Send" })).toBeEnabled()
    fireEvent.click(screen.getByRole("button", { name: "Remove quote 1" }))
    expect(onQuotesChange).toHaveBeenCalledWith([])
    rerender(<Composer label="Message" sendLabel="Send" quotes={[]} onQuotesChange={onQuotesChange} onSubmit={onSubmit} />)
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled()
    rerender(<Composer label="Message" sendLabel="Send" quotes={["alpha"]} onQuotesChange={onQuotesChange} onSubmit={onSubmit} />)
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(onSubmit).toHaveBeenCalledWith("<selected_text>\nalpha\n</selected_text>")
  })

  it("lets the user read and edit a selected quote before sending", () => {
    const onSubmit = vi.fn()
    const onQuotesChange = vi.fn()
    const { rerender } = render(
      <Composer label="Message" sendLabel="Send" quotes={["long original quote"]} onQuotesChange={onQuotesChange} onSubmit={onSubmit} />,
    )
    fireEvent.change(screen.getByRole("textbox", { name: "Edit quote 1" }), { target: { value: "corrected quote" } })
    expect(onQuotesChange).toHaveBeenCalledWith(["corrected quote"])
    rerender(<Composer label="Message" sendLabel="Send" quotes={["corrected quote"]} onQuotesChange={onQuotesChange} onSubmit={onSubmit} />)
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(onSubmit).toHaveBeenCalledWith("<selected_text>\ncorrected quote\n</selected_text>")
  })

  it("does not enable sending an emptied quote", () => {
    const onSubmit = vi.fn()
    render(<Composer label="Message" sendLabel="Send" quotes={["  "]} onQuotesChange={vi.fn()} onSubmit={onSubmit} />)
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled()
  })

  it("restores the text and quote after a failed send", async () => {
    const onSubmit = vi.fn().mockRejectedValue(new Error("offline"))
    const onQuotesChange = vi.fn()
    render(<Composer label="Message" sendLabel="Send" quotes={["alpha"]} onQuotesChange={onQuotesChange} onSubmit={onSubmit} />)
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "explain" } })
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Send" }))
    })
    await vi.waitFor(() => expect(onQuotesChange).toHaveBeenLastCalledWith(["alpha"]))
    expect(screen.getByLabelText("Message")).toHaveValue("explain")
  })
})
