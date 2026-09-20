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

  // CJK IMEs write the committed string back into a controlled textarea
  // after we clear it. A leftover Enter would then queue the same draft
  // that just started the turn.
  it("does not queue when the IME restores a draft that was just sent", () => {
    const onSend = vi.fn()
    renderComposer({ onSend, running: true })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "draft" } })
    fireEvent.keyDown(input, { key: "Enter", metaKey: true, keyCode: 13 })
    expect(onSend).toHaveBeenCalledTimes(1)
    fireEvent.change(input, { target: { value: "draft" } })
    expect(input).toHaveValue("")
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSend).toHaveBeenCalledTimes(1)
  })

  it("sends a different draft after the previous one", () => {
    const onSend = vi.fn()
    renderComposer({ onSend, running: true })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "draft" } })
    fireEvent.keyDown(input, { key: "Enter", metaKey: true, keyCode: 13 })
    fireEvent.change(input, { target: { value: "next" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSend).toHaveBeenNthCalledWith(1, "draft", undefined, { steer: true })
    expect(onSend).toHaveBeenNthCalledWith(2, "next", undefined, { steer: false })
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

  it("names live compression above the box so a queued follow-up is not a freeze", () => {
    renderComposer({ running: true, compressing: true })
    expect(screen.getByTestId("compressing-banner")).toHaveTextContent(
      "Compressing conversation context…",
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
    expect(
      screen.getByTestId("composer").querySelector(".content-gutter"),
    ).not.toBeNull()
  })

  // Default Textarea is bg-background + rounded-md. That square fill
  // overflows the card's rounded-3xl and paints over the top corners.
  it("does not paint an opaque textarea over the rounded card border", () => {
    renderComposer()
    const input = screen.getByTestId("composer-input")
    expect(input.className).toMatch(/\bbg-transparent\b/)
    expect(input.className).not.toMatch(/\bbg-background\b/)
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
    expect(screen.getByTestId("slash-command-plan")).toBeTruthy()
    expect(screen.getByTestId("slash-command-compact")).toBeTruthy()
  })

  it("lists commands when the Slash key landed as a CJK punctuation comma", () => {
    renderComposer()
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "、" } })
    expect(input).toHaveValue("/")
    expect(screen.getByTestId("slash-menu")).toBeTruthy()
    expect(screen.getByTestId("slash-command-goal")).toBeTruthy()
  })

  it("lists commands when a slash is typed after existing text", () => {
    renderComposer()
    fireEvent.change(screen.getByTestId("composer-input"), {
      target: { value: "hello /" },
    })
    expect(screen.getByTestId("slash-menu")).toBeTruthy()
    expect(screen.getByTestId("slash-command-goal")).toBeTruthy()
  })

  it("keeps the preceding text when Escape dismisses an inline slash", () => {
    renderComposer()
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "hello /go" } })
    fireEvent.keyDown(input, { key: "Escape" })
    expect(input).toHaveValue("hello ")
    expect(screen.queryByTestId("slash-menu")).toBeNull()
  })

  it("puts the compact fill on the menu from the usage snapshot", () => {
    renderComposer({
      usage: {
        context_tokens: 25600,
        context_window: 256000,
        turn: {
          prompt_tokens: 25600,
          completion_tokens: 0,
          cached_tokens: 0,
          reasoning_tokens: 0,
          total_tokens: 25600,
          calls: 1,
        },
        thread: {
          prompt_tokens: 25600,
          completion_tokens: 0,
          cached_tokens: 0,
          reasoning_tokens: 0,
          total_tokens: 25600,
          calls: 1,
        },
      },
    })
    fireEvent.change(screen.getByTestId("composer-input"), {
      target: { value: "/" },
    })
    expect(screen.getByTestId("slash-command-compact").textContent).toMatch(/10%/)
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

  it("writes /goal into the box after picking from the menu", () => {
    const onSend = vi.fn()
    const onSetGoal = vi.fn()
    renderComposer({ onSend, onSetGoal })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "/" } })
    fireEvent.click(screen.getByTestId("slash-command-goal"))
    expect(screen.queryByTestId("slash-menu")).toBeNull()
    expect(input).toHaveValue("/goal ")
    expect(input).toHaveFocus()
    expect(input).toHaveAttribute("data-goal-draft", "true")
    expect(screen.getByTestId("composer-command-hint").textContent).toMatch(
      /Standing objective/,
    )
    fireEvent.change(input, { target: { value: "/goal keep going" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSetGoal).toHaveBeenCalledWith("keep going")
    expect(onSend).not.toHaveBeenCalled()
  })

  it("keeps preceding text when goal is picked after it", () => {
    const onSend = vi.fn()
    const onSetGoal = vi.fn()
    renderComposer({ onSend, onSetGoal })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "hello /" } })
    fireEvent.click(screen.getByTestId("slash-command-goal"))
    expect(onSetGoal).not.toHaveBeenCalled()
    expect(onSend).not.toHaveBeenCalled()
    expect(input).toHaveValue("hello /goal ")
    expect(screen.queryByTestId("slash-menu")).toBeNull()
  })

  it("keeps the preceding text when compact is picked after it", () => {
    const onSend = vi.fn()
    const onCompact = vi.fn()
    renderComposer({ onSend, onCompact })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "hello /" } })
    fireEvent.click(screen.getByTestId("slash-command-compact"))
    expect(onCompact).toHaveBeenCalledTimes(1)
    expect(onSend).not.toHaveBeenCalled()
    expect(input).toHaveValue("hello")
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

  it("submits a trailing /goal after existing text", () => {
    const onSend = vi.fn()
    const onSetGoal = vi.fn()
    renderComposer({ onSend, onSetGoal })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "hello /goal keep going" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSetGoal).toHaveBeenCalledWith("keep going")
    expect(onSend).not.toHaveBeenCalled()
  })

  it("submits /goal when the objective is glued on without a space", () => {
    const onSend = vi.fn()
    const onSetGoal = vi.fn()
    renderComposer({ onSend, onSetGoal })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "/goal持续推进" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSetGoal).toHaveBeenCalledWith("持续推进")
    expect(onSend).not.toHaveBeenCalled()
  })

  it("submits /goal when the slash is the fullwidth IME solidus", () => {
    const onSend = vi.fn()
    const onSetGoal = vi.fn()
    renderComposer({ onSend, onSetGoal })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "／goal keep going" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSetGoal).toHaveBeenCalledWith("keep going")
    expect(onSend).not.toHaveBeenCalled()
  })

  it("does not queue a /goal submit while a turn is running", () => {
    const onSend = vi.fn()
    const onSetGoal = vi.fn()
    renderComposer({ running: true, onSend, onSetGoal })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "/goal keep going" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSetGoal).toHaveBeenCalledWith("keep going")
    expect(onSend).not.toHaveBeenCalled()
  })

  it("keeps Send next to Stop after picking goal during a live turn", () => {
    renderComposer({ running: true })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "/" } })
    fireEvent.click(screen.getByTestId("slash-command-goal"))
    expect(input).toHaveValue("/goal ")
    expect(screen.getByRole("button", { name: "Stop" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled()
    fireEvent.change(input, { target: { value: "/goal keep going" } })
    expect(screen.getByRole("button", { name: "Send" })).not.toBeDisabled()
  })

  it("leaves a bare /goal prompt in the box instead of wiping it", () => {
    const onSetGoal = vi.fn()
    renderComposer({ onSetGoal })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "/goal " } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSetGoal).not.toHaveBeenCalled()
    expect(input).toHaveValue("/goal ")
  })

  it("shows the standing objective and can clear it", () => {
    const onClearGoal = vi.fn()
    renderComposer({ goal: "keep going", onClearGoal })
    expect(screen.getByTestId("goal-banner").textContent).toContain("keep going")
    expect(screen.getByTestId("goal-banner").textContent).toContain("Pursuing")
    fireEvent.click(screen.getByRole("button", { name: "Clear goal" }))
    expect(onClearGoal).toHaveBeenCalled()
  })

  it("does not offer start while a pursuing goal is idle between turns", () => {
    renderComposer({ goal: "keep going", onResumeGoal: vi.fn() })
    expect(screen.getByTestId("goal-banner").textContent).toContain("Pursuing")
    expect(screen.queryByRole("button", { name: "Start goal" })).toBeNull()
  })

  it("starts a held standing objective from the banner", () => {
    const onResumeGoal = vi.fn()
    renderComposer({ goal: "keep going", goalIdle: true, onResumeGoal })
    fireEvent.click(screen.getByRole("button", { name: "Start goal" }))
    expect(onResumeGoal).toHaveBeenCalled()
  })

  it("starts a paused standing objective from the banner", () => {
    const onResumeGoal = vi.fn()
    renderComposer({ goal: "keep going", goalCapped: true, onResumeGoal })
    expect(screen.getByTestId("goal-reason").textContent).toMatch(/not an error/)
    fireEvent.click(screen.getByRole("button", { name: "Start goal" }))
    expect(onResumeGoal).toHaveBeenCalled()
  })

  it("starts a completed standing objective from the banner", () => {
    const onResumeGoal = vi.fn()
    renderComposer({ goal: "keep going", goalComplete: true, onResumeGoal })
    expect(screen.getByTestId("goal-reason").textContent).toMatch(/too early/)
    fireEvent.click(screen.getByRole("button", { name: "Start goal" }))
    expect(onResumeGoal).toHaveBeenCalled()
  })

  it("writes /plan into the box after picking from the menu", () => {
    const onSend = vi.fn()
    const onSetPlan = vi.fn()
    renderComposer({ onSend, onSetPlan })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "/" } })
    fireEvent.click(screen.getByTestId("slash-command-plan"))
    expect(input).toHaveValue("/plan ")
    expect(screen.getByTestId("composer-command-hint").textContent).toMatch(
      /What should we plan/,
    )
    fireEvent.change(input, { target: { value: "/plan inspect then change" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSetPlan).toHaveBeenCalledWith("inspect then change")
    expect(onSend).not.toHaveBeenCalled()
  })

  it("submits /plan with an argument in one go", () => {
    const onSend = vi.fn()
    const onSetPlan = vi.fn()
    renderComposer({ onSend, onSetPlan })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "/plan inspect then change" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSetPlan).toHaveBeenCalledWith("inspect then change")
    expect(onSend).not.toHaveBeenCalled()
  })

  it("uses the ask placeholder while a question is waiting", () => {
    renderComposer({ awaitingAnswer: true })
    expect(screen.getByTestId("composer-input")).toHaveAttribute(
      "placeholder",
      "Type an answer, or pick a choice above",
    )
  })

  it("shows a planning banner and can implement", () => {
    const onImplementPlan = vi.fn()
    renderComposer({
      planMode: true,
      planMarkdown: "# Plan\n\nDo the work.",
      onImplementPlan,
    })
    expect(screen.getByTestId("plan-banner").textContent).toContain("Planning")
    fireEvent.click(screen.getByTestId("plan-implement"))
    expect(onImplementPlan).toHaveBeenCalled()
  })
})
