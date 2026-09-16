import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { Composer } from "./composer"
import { TooltipProvider } from "@/components/ui/tooltip"
import { IME_KEYCODE } from "@/lib/ime"
import type { Attachment, ModelInfo } from "@/lib/types"

function savedFile(name: string): Attachment {
  return {
    id: "att_1",
    thread_id: "th_1",
    name,
    rel_path: `uploads/${name}`,
    size: 1,
    created_at: "2026-01-01T00:00:00Z",
  }
}

function renderComposer(props: Partial<Parameters<typeof Composer>[0]> = {}) {
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
        onSend={vi.fn()}
        onStop={vi.fn()}
        onUpload={vi.fn(async (): Promise<Attachment[]> => [])}
        focusSignal={0}
        {...props}
      />
    </TooltipProvider>,
  )
}

describe("Composer model switcher", () => {
  it("stays a switcher when only one model is ready", () => {
    renderComposer()
    expect(screen.getByLabelText("Model")).toBeTruthy()
  })
})

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

function frame(): Promise<void> {
  return new Promise((resolve) => requestAnimationFrame(() => resolve()))
}

describe("Composer Enter vs IME", () => {
  it("sends on Enter once the draft is settled", () => {
    const onSend = vi.fn()
    renderComposer({ onSend })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "draft" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSend).toHaveBeenCalledTimes(1)
    expect(onSend).toHaveBeenCalledWith("draft", undefined, { steer: false })
  })

  it("does not send Shift+Enter", () => {
    const onSend = vi.fn()
    renderComposer({ onSend })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "draft" } })
    fireEvent.keyDown(input, { key: "Enter", shiftKey: true, keyCode: 13 })
    expect(onSend).not.toHaveBeenCalled()
  })

  it("does not send while the IME is composing", () => {
    const onSend = vi.fn()
    renderComposer({ onSend })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "draft" } })
    fireEvent.compositionStart(input)
    fireEvent.keyDown(input, { key: "Enter", isComposing: true, keyCode: IME_KEYCODE })
    expect(onSend).not.toHaveBeenCalled()
    expect(input).toHaveValue("draft")
  })

  // WebKit: compositionend, then the Enter that kept leftover Latin, with
  // isComposing already false. That Enter belongs to the IME.
  it("does not send on the Enter that confirmed the IME", () => {
    const onSend = vi.fn()
    renderComposer({ onSend })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "draft" } })
    fireEvent.compositionStart(input)
    fireEvent.compositionEnd(input)
    fireEvent.keyDown(input, { key: "Enter", isComposing: false, keyCode: 13 })
    expect(onSend).not.toHaveBeenCalled()
    expect(input).toHaveValue("draft")
  })

  it("sends on the next Enter after the IME has settled", async () => {
    const onSend = vi.fn()
    renderComposer({ onSend })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "draft" } })
    fireEvent.compositionStart(input)
    fireEvent.compositionEnd(input)
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSend).not.toHaveBeenCalled()
    await frame()
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSend).toHaveBeenCalledTimes(1)
    expect(onSend).toHaveBeenCalledWith("draft", undefined, { steer: false })
  })

  it("steers immediately on ⌘Enter", () => {
    const onSend = vi.fn()
    renderComposer({ onSend, running: true })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "draft" } })
    fireEvent.keyDown(input, { key: "Enter", metaKey: true, keyCode: 13 })
    expect(onSend).toHaveBeenCalledWith("draft", undefined, { steer: true })
  })
})

describe("Composer follow-up queue", () => {
  it("shows queued messages above the box", () => {
    renderComposer({
      running: true,
      followups: [
        {
          id: "fu_1",
          thread_id: "th_1",
          seq: 1,
          text: "after this",
          created_at: "2026-09-16T00:00:00Z",
        },
      ],
    })
    expect(screen.getByTestId("followup-queue")).toHaveTextContent("1 Queued")
    expect(screen.getByTestId("composer-input")).toHaveAttribute(
      "placeholder",
      "Working… Enter queues, ⌘Enter steers",
    )
  })
})

describe("Composer quotes", () => {
  it("prefixes selected text onto the send and clears the chip", () => {
    const onSend = vi.fn()
    const onQuotesChange = vi.fn()
    renderComposer({
      onSend,
      quotes: [{ id: "q1", text: "alpha" }],
      onQuotesChange,
    })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "do this" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSend).toHaveBeenCalledWith("Selected text:\nalpha\n\ndo this", undefined, {
      steer: false,
    })
    expect(onQuotesChange).toHaveBeenCalledWith([])
  })

  it("sends quotes alone when the box is empty", () => {
    const onSend = vi.fn()
    renderComposer({
      onSend,
      quotes: [{ id: "q1", text: "alpha" }],
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(onSend).toHaveBeenCalledWith("Selected text:\nalpha", undefined, { steer: false })
  })
})

describe("Composer chrome", () => {
  // A hairline turns the box into a docked toolbar. The fade is the join
  // that makes it sit on the transcript instead.
  it("sits on the transcript with a fade instead of a hairline", () => {
    renderComposer()
    expect(screen.getByTestId("composer-fade")).toBeInTheDocument()
    expect(screen.getByTestId("composer").className).not.toMatch(/border-t/)
    expect(
      screen.getByTestId("composer").querySelector(".content-column"),
    ).not.toBeNull()
  })
})

describe("Composer pasted images", () => {
  const png = new File([new Uint8Array([1, 2, 3, 4])], "clip.png", {
    type: "image/png",
  })

  function stubObjectURLs() {
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      writable: true,
      value: () => "blob:preview",
    })
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      writable: true,
      value: () => {},
    })
  }

  function pasteInto(el: HTMLElement, file: File) {
    fireEvent.paste(el, {
      clipboardData: {
        items: [{ kind: "file", type: file.type, getAsFile: () => file }],
        files: [file],
      },
    })
  }

  it("shows a thumbnail that can be dropped before send", async () => {
    stubObjectURLs()
    const onSend = vi.fn()
    renderComposer({ onSend })
    pasteInto(screen.getByTestId("composer-input"), png)
    expect(screen.getByTestId("composer-images")).toBeInTheDocument()
    expect(screen.getByAltText("clip.png")).toHaveAttribute("src", "blob:preview")
    fireEvent.click(screen.getByRole("button", { name: "Remove clip.png" }))
    expect(screen.queryByTestId("composer-images")).toBeNull()
    expect(onSend).not.toHaveBeenCalled()
  })

  it("sends the pixels as vision input, not a workspace upload", async () => {
    stubObjectURLs()
    const onSend = vi.fn()
    const onUpload = vi.fn(async (): Promise<Attachment[]> => [])
    renderComposer({ onSend, onUpload })
    pasteInto(screen.getByTestId("composer-input"), png)
    fireEvent.change(screen.getByTestId("composer-input"), {
      target: { value: "what is this" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await waitFor(() => expect(onSend).toHaveBeenCalledTimes(1))
    expect(onUpload).not.toHaveBeenCalled()
    const [text, images] = onSend.mock.calls[0]
    expect(text).toBe("what is this")
    expect(images).toHaveLength(1)
    expect(images[0].name).toBe("clip.png")
    expect(images[0].mime).toBe("image/png")
    expect(typeof images[0].data).toBe("string")
    expect(images[0].data.length).toBeGreaterThan(0)
  })

  it("sends an image with no caption", async () => {
    stubObjectURLs()
    const onSend = vi.fn()
    renderComposer({ onSend })
    pasteInto(screen.getByTestId("composer-input"), png)
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await waitFor(() => expect(onSend).toHaveBeenCalledTimes(1))
    const [text, images] = onSend.mock.calls[0]
    expect(text).toBe("")
    expect(images).toHaveLength(1)
  })
})

describe("Composer file drop", () => {
  const png = new File([new Uint8Array([1, 2, 3, 4])], "shot.png", {
    type: "image/png",
  })
  const notes = new File([new Uint8Array([1, 2, 3])], "notes.txt", {
    type: "text/plain",
  })

  function stubObjectURLs() {
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      writable: true,
      value: () => "blob:preview",
    })
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      writable: true,
      value: () => {},
    })
  }

  function fileTransfer(files: File[]) {
    return {
      types: ["Files"],
      files,
      items: files.map((file) => ({
        kind: "file",
        type: file.type,
        getAsFile: () => file,
      })),
    }
  }

  function dropOn(files: File[]) {
    fireEvent.drop(screen.getByTestId("composer-drop"), {
      dataTransfer: fileTransfer(files),
    })
  }

  it("shows a drop overlay while files hover the box, then clears it", () => {
    renderComposer()
    const zone = screen.getByTestId("composer-drop")
    fireEvent.dragEnter(zone, { dataTransfer: fileTransfer([notes]) })
    expect(screen.getByTestId("composer-drop-overlay")).toHaveTextContent(
      "Drop files to attach",
    )
    fireEvent.dragLeave(zone, { dataTransfer: fileTransfer([notes]) })
    expect(screen.queryByTestId("composer-drop-overlay")).toBeNull()
  })

  it("does not arm the overlay for dragged text", () => {
    renderComposer()
    fireEvent.dragEnter(screen.getByTestId("composer-drop"), {
      dataTransfer: { types: ["text/plain"], files: [], items: [] },
    })
    expect(screen.queryByTestId("composer-drop-overlay")).toBeNull()
  })

  it("drops an image as a vision thumb and a file as a workspace chip", async () => {
    stubObjectURLs()
    const onSend = vi.fn()
    const onUpload = vi.fn(async () => [savedFile("notes.txt")])
    renderComposer({ onSend, onUpload })
    dropOn([png, notes])
    expect(screen.getByAltText("shot.png")).toHaveAttribute("src", "blob:preview")
    expect(screen.getByTestId("composer-attachments")).toHaveTextContent("notes.txt")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await waitFor(() => expect(onSend).toHaveBeenCalledTimes(1))
    expect(onUpload).toHaveBeenCalledTimes(1)
    expect(onUpload).toHaveBeenCalledWith([notes])
    const [text, images, opts] = onSend.mock.calls[0]
    expect(text).toBe("")
    expect(images).toHaveLength(1)
    expect(images[0].name).toBe("shot.png")
    expect(opts.files).toEqual(["uploads/notes.txt"])
  })

  it("sends a file with no caption", async () => {
    const onSend = vi.fn()
    const onUpload = vi.fn(async () => [savedFile("notes.txt")])
    renderComposer({ onSend, onUpload })
    dropOn([notes])
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await waitFor(() => expect(onSend).toHaveBeenCalledTimes(1))
    const [text, images, opts] = onSend.mock.calls[0]
    expect(text).toBe("")
    expect(images).toBeUndefined()
    expect(opts.files).toEqual(["uploads/notes.txt"])
  })

  it("does not send when the upload returns nothing", async () => {
    const onSend = vi.fn()
    const onUpload = vi.fn(async (): Promise<Attachment[]> => [])
    renderComposer({ onSend, onUpload })
    dropOn([notes])
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await waitFor(() => expect(onUpload).toHaveBeenCalledTimes(1))
    expect(onSend).not.toHaveBeenCalled()
  })

  it("keeps the chip and does not send when the upload fails", async () => {
    const onSend = vi.fn()
    const onUpload = vi.fn(async (): Promise<Attachment[]> => {
      throw new Error("disk full")
    })
    renderComposer({ onSend, onUpload })
    dropOn([notes])
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await waitFor(() => expect(onUpload).toHaveBeenCalledTimes(1))
    expect(onSend).not.toHaveBeenCalled()
    expect(screen.getByTestId("composer-attachments")).toHaveTextContent("notes.txt")
  })

  it("lets a chip be dropped before send", () => {
    renderComposer()
    dropOn([notes])
    fireEvent.click(screen.getByRole("button", { name: "Remove notes.txt" }))
    expect(screen.queryByTestId("composer-attachments")).toBeNull()
  })

  it("ignores a drop while the composer is disabled", () => {
    stubObjectURLs()
    renderComposer({ disabled: true })
    dropOn([png, notes])
    expect(screen.queryByTestId("composer-images")).toBeNull()
    expect(screen.queryByTestId("composer-attachments")).toBeNull()
  })
})

describe("Composer context meter", () => {
  it("shows a ring once a turn has billed tokens", () => {
    renderComposer({
      models: [
        {
          id: "default\tm",
          provider_id: "default",
          provider_label: "Offline",
          label: "m",
          model: "m",
          ready: true,
          default: true,
          context_window: 256000,
        },
      ],
      usage: {
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
      },
    })
    expect(screen.getByTestId("context-meter")).toBeTruthy()
    expect(screen.getByLabelText("28% context used")).toBeTruthy()
  })

  it("still paints an arc when the selected model never reported a window", () => {
    renderComposer({
      contextBudget: 80000,
      usage: {
        context_tokens: 54100,
        context_window: 0,
        turn: {
          prompt_tokens: 54100,
          completion_tokens: 100,
          cached_tokens: 0,
          reasoning_tokens: 0,
          total_tokens: 54200,
          calls: 1,
        },
        thread: {
          prompt_tokens: 54100,
          completion_tokens: 100,
          cached_tokens: 0,
          reasoning_tokens: 0,
          total_tokens: 54200,
          calls: 1,
        },
      },
    })
    expect(screen.getByLabelText("54.1K tokens used")).toBeTruthy()
    expect(
      Number(
        screen.getByTestId("context-meter").querySelector("[data-fill]")?.getAttribute("data-fill"),
      ),
    ).toBeGreaterThan(0)
  })
})

describe("Composer slash commands", () => {
  it("lists built-in commands when the box is just a slash", () => {
    renderComposer()
    fireEvent.change(screen.getByTestId("composer-input"), {
      target: { value: "/" },
    })
    expect(screen.getByTestId("slash-menu")).toBeTruthy()
    expect(screen.getByTestId("slash-command-goal")).toBeTruthy()
    expect(screen.getByTestId("slash-command-compact")).toBeTruthy()
  })

  it("filters as the name is typed", () => {
    renderComposer()
    fireEvent.change(screen.getByTestId("composer-input"), {
      target: { value: "/comp" },
    })
    expect(screen.getByTestId("slash-command-compact")).toBeTruthy()
    expect(screen.queryByTestId("slash-command-goal")).toBeNull()
  })

  it("runs compact from the menu without sending a message", () => {
    const onSend = vi.fn()
    const onCompact = vi.fn()
    renderComposer({ onSend, onCompact })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "/" } })
    fireEvent.click(screen.getByTestId("slash-command-compact"))
    expect(onCompact).toHaveBeenCalledTimes(1)
    expect(onSend).not.toHaveBeenCalled()
    expect(screen.queryByTestId("slash-menu")).toBeNull()
  })

  it("waits for the objective after picking goal", () => {
    const onSend = vi.fn()
    const onSetGoal = vi.fn()
    renderComposer({ onSend, onSetGoal })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "/" } })
    fireEvent.click(screen.getByTestId("slash-command-goal"))
    expect(screen.queryByTestId("slash-menu")).toBeNull()
    expect(input).toHaveAttribute(
      "placeholder",
      "Standing objective for this conversation",
    )
    fireEvent.change(input, { target: { value: "keep going" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSetGoal).toHaveBeenCalledWith("keep going")
    expect(onSend).not.toHaveBeenCalled()
  })

  it("submits /goal with an argument in one go", () => {
    const onSend = vi.fn()
    const onSetGoal = vi.fn()
    renderComposer({ onSend, onSetGoal })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "/goal keep going" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSetGoal).toHaveBeenCalledWith("keep going")
    expect(onSend).not.toHaveBeenCalled()
  })

  it("shows the standing objective and can clear it", () => {
    const onClearGoal = vi.fn()
    renderComposer({ goal: "keep going", onClearGoal })
    expect(screen.getByTestId("goal-banner").textContent).toContain("keep going")
    expect(screen.getByTestId("goal-banner").textContent).toContain("Pursuing")
    fireEvent.click(screen.getByRole("button", { name: "Clear goal" }))
    expect(onClearGoal).toHaveBeenCalled()
  })

  it("starts a standing objective from the banner", () => {
    const onResumeGoal = vi.fn()
    renderComposer({ goal: "keep going", onResumeGoal })
    fireEvent.click(screen.getByRole("button", { name: "Start goal" }))
    expect(onResumeGoal).toHaveBeenCalled()
  })
})
