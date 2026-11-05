import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { streamCompletion } from "@/lib/openai-client"
import { DirectChatScreen } from "./direct-chat-screen"
import { maxAttachmentBytes } from "@/lib/attachments"
import { saveProviders, type DirectProvider } from "@/lib/direct-provider"
import { setLocale, t } from "@/lib/i18n"
import { ModelCallError, type StreamPiece, type WireTurn } from "@/lib/openai-wire"

const provider: DirectProvider = {
  id: "p-1",
  label: "Desk",
  baseURL: "https://endpoint.invalid/v1",
  apiKey: "",
  api: "chat",
  model: "one",
  catalog: ["one", "two"],
  timeoutSeconds: 300,
}

function harness(complete: typeof streamCompletion = async (input) => {
  input.onDelta({ text: "pong", reasoning: "" })
}) {
  saveProviders([provider])
  return render(
    <DirectChatScreen
      hosts={[]}
      activeFingerprint=""
      path="relay"
      providers={[provider]}
      onSelectHost={vi.fn()}
      onAddHost={vi.fn()}
      onUnlink={vi.fn()}
      onModels={vi.fn()}
      complete={complete}
    />,
  )
}

describe("DirectChatScreen", () => {
  it("moves a new chat onto a remaining endpoint when the selected one is removed", () => {
    setLocale("en")
    const keep: DirectProvider = {
      ...provider,
      id: "p-keep",
      label: "Keep",
      model: "two",
      catalog: ["two"],
    }
    const view = render(
      <DirectChatScreen
        hosts={[]}
        activeFingerprint=""
        path="relay"
        providers={[provider, keep]}
        onSelectHost={vi.fn()}
        onAddHost={vi.fn()}
        onUnlink={vi.fn()}
        onModels={vi.fn()}
      />,
    )
    expect(screen.getByLabelText(t("composer.model"))).toHaveTextContent("one")
    view.rerender(
      <DirectChatScreen
        hosts={[]}
        activeFingerprint=""
        path="relay"
        providers={[keep]}
        onSelectHost={vi.fn()}
        onAddHost={vi.fn()}
        onUnlink={vi.fn()}
        onModels={vi.fn()}
      />,
    )
    expect(screen.getByLabelText(t("composer.model"))).toHaveTextContent("two")
    expect(screen.queryByText(t("account.sourceUnavailable"))).not.toBeInTheDocument()
  })

  it("keeps an open chat on a removed endpoint instead of switching it", async () => {
    setLocale("en")
    const keep: DirectProvider = {
      ...provider,
      id: "p-keep",
      label: "Keep",
      model: "two",
      catalog: ["two"],
    }
    const view = render(
      <DirectChatScreen
        hosts={[]}
        activeFingerprint=""
        path="relay"
        providers={[provider, keep]}
        onSelectHost={vi.fn()}
        onAddHost={vi.fn()}
        onUnlink={vi.fn()}
        onModels={vi.fn()}
        complete={async (input) => {
          input.onDelta({ text: "pong", reasoning: "" })
        }}
      />,
    )
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "start" } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await screen.findByText("pong")
    view.rerender(
      <DirectChatScreen
        hosts={[]}
        activeFingerprint=""
        path="relay"
        providers={[keep]}
        onSelectHost={vi.fn()}
        onAddHost={vi.fn()}
        onUnlink={vi.fn()}
        onModels={vi.fn()}
        complete={async (input) => {
          input.onDelta({ text: "pong", reasoning: "" })
        }}
      />,
    )
    expect(screen.getByLabelText(t("composer.model"))).toHaveTextContent("one")
    expect(screen.getByText(t("account.sourceUnavailable"))).toBeInTheDocument()
  })

  it.each([
    ["gateway_login_required", "Sign in to the Gateway to use cloud Web tools, or switch Web tools off for ordinary model chat."],
    ["gateway_access_denied", "Account access could not be confirmed. Refresh or sign in again."],
    ["cloud_tools_model_unsupported", "This model does not support function tools. Switch Web tools off or choose a model that supports tools, then retry."],
  ])("explains %s with a recoverable Web setting action", async (code, message) => {
    setLocale("en")
    harness(async () => { throw new ModelCallError(code, "http") })
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "start" } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(await screen.findByText(message)).toBeVisible()
    expect(screen.queryByText(code)).not.toBeInTheDocument()
    expect(screen.getByLabelText("Message")).toBeEnabled()
  })

  it("aborts a streaming reply from the composer and retains received text without an error", async () => {
    setLocale("en")
    let signal: AbortSignal | undefined
    harness(async (input) => {
      signal = input.signal
      input.onDelta({ text: "received text", reasoning: "" })
      await new Promise<void>((_resolve, reject) => {
        input.signal?.addEventListener("abort", () => reject(new ModelCallError("abort", "abort")), { once: true })
      })
    })
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "start" } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await screen.findByText("received text")
    const form = within(screen.getByLabelText("Message").closest("form")!)
    const stop = form.getByRole("button", { name: "Stop" })
    expect(stop).toBeEnabled()
    fireEvent.click(stop)
    await waitFor(() => expect(screen.getByLabelText("Message")).toBeEnabled())
    expect(signal?.aborted).toBe(true)
    expect(screen.getByText("received text")).toBeVisible()
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
    expect(form.getByRole("button", { name: "Send" })).toBeDisabled()
    expect(form.queryByRole("button", { name: "Stop" })).not.toBeInTheDocument()
  })

  it("offers the saved models and thinking levels, then shows the reply", async () => {
    setLocale("en")
    const complete = vi.fn(async (input: { onDelta: (piece: StreamPiece) => void }) => {
      input.onDelta({ text: "pong", reasoning: "because" })
    })
    harness(complete)
    expect(screen.getByRole("tab", { name: t("chat.tab") })).toHaveAttribute("aria-selected", "true")
    fireEvent.click(screen.getByRole("button", { name: t("composer.model") }))
    expect(screen.getByRole("radio", { name: "one" })).toHaveAttribute("aria-checked", "true")
    expect(screen.getByRole("radio", { name: "two" })).toBeInTheDocument()
    fireEvent.keyDown(window, { key: "Escape" })
    expect(screen.getByRole("option", { name: t("reason.high") })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText(t("composer.thinking")), { target: { value: "high" } })
    fireEvent.change(screen.getByLabelText(t("thread.message")), { target: { value: "ping" } })
    fireEvent.click(screen.getByRole("button", { name: t("thread.send") }))
    await waitFor(() => expect(screen.getByText("pong")).toBeInTheDocument())
    expect(screen.queryByTestId("phone-thought")).not.toBeInTheDocument()
    expect(screen.queryByTestId("work-fold")).not.toBeInTheDocument()
    expect(screen.queryByTestId("phone-thought")).not.toBeInTheDocument()
    expect(screen.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", "false")
    fireEvent.click(screen.getByTestId("phone-thought-toggle"))
    expect(screen.getByTestId("phone-thought")).toHaveTextContent("because")
    expect(complete.mock.calls[0]?.[0]).toMatchObject({ model: "one", reasoning: "high" })
  })

  it("sends an image and a text file with the turn", async () => {
    setLocale("en")
    const complete = vi.fn(async (input: { onDelta: (piece: StreamPiece) => void; turns: WireTurn[] }) => {
      input.onDelta({ text: "saw", reasoning: "" })
    })
    harness(complete)
    const image = new File([Uint8Array.from([1, 2, 3])], "shot.png", { type: "image/png" })
    const notes = new File(["alpha"], "notes.txt", { type: "text/plain" })
    fireEvent.change(screen.getByTestId("file-input"), { target: { files: [image, notes] } })
    fireEvent.click(screen.getByRole("button", { name: t("thread.send") }))
    await waitFor(() => expect(complete).toHaveBeenCalled())
    const turns = complete.mock.calls[0]?.[0].turns ?? []
    const kinds = turns[0]?.parts.map((part) => part.kind)
    expect(kinds).toContain("image")
    expect(turns[0]?.parts.some((part) => part.kind === "text" && part.text.includes("alpha"))).toBe(true)
    expect(screen.getAllByText("shot.png").length).toBeGreaterThan(0)
    expect(screen.getByText("saw")).toBeInTheDocument()
  })

  it("keeps the draft and says when a file is too large", async () => {
    setLocale("en")
    const complete = vi.fn()
    harness(complete)
    const big = new File([new Uint8Array(8)], "big.bin", { type: "application/octet-stream" })
    Object.defineProperty(big, "size", { value: maxAttachmentBytes + 1 })
    fireEvent.change(screen.getByTestId("file-input"), { target: { files: [big] } })
    fireEvent.click(screen.getByRole("button", { name: t("thread.send") }))
    await waitFor(() => expect(screen.getByText(t("chat.tooBig"))).toBeInTheDocument())
    expect(complete).not.toHaveBeenCalled()
    expect(screen.getByText("big.bin")).toBeInTheDocument()
  })

  it("stops a live reply when leaving, so the composer is not stuck", async () => {
    setLocale("en")
    const complete = vi.fn(
      (input: { signal?: AbortSignal; onDelta: (piece: StreamPiece) => void; turns: WireTurn[] }) =>
        new Promise<void>((_resolve, reject) => {
          if (input.signal?.aborted) {
            reject(new ModelCallError("abort", "abort"))
            return
          }
          input.signal?.addEventListener("abort", () => {
            reject(new ModelCallError("abort", "abort"))
          })
        }),
    )
    harness(complete)
    fireEvent.change(screen.getByLabelText(t("thread.message")), { target: { value: "ping" } })
    fireEvent.click(screen.getByRole("button", { name: t("thread.send") }))
    await waitFor(() => expect(within(screen.getByRole("banner")).getByRole("button", { name: t("thread.stop") })).toBeInTheDocument())
    fireEvent.click(screen.getByRole("button", { name: t("thread.back") }))
    await waitFor(() => expect(screen.getByLabelText(t("thread.message"))).toBeEnabled())
    expect(screen.queryByRole("button", { name: t("thread.stop") })).not.toBeInTheDocument()
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText(t("thread.message")), { target: { value: "again" } })
    expect(screen.getByRole("button", { name: t("thread.send") })).toBeEnabled()
  })

  it("shows the thought while the reply is still arriving", async () => {
    setLocale("en")
    let release!: () => void
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    const encoder = new TextEncoder()
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        const stream = new ReadableStream({
          async start(controller) {
            controller.enqueue(
              encoder.encode('data: {"choices":[{"delta":{"reasoning_content":"because"}}]}\n\n'),
            )
            await gate
            controller.enqueue(
              encoder.encode('data: {"choices":[{"delta":{"content":"pong"}}]}\n\ndata: [DONE]\n\n'),
            )
            controller.close()
          },
        })
        return new Response(stream)
      }),
    )
    try {
      harness(streamCompletion)
      fireEvent.change(screen.getByLabelText(t("thread.message")), { target: { value: "ping" } })
      fireEvent.click(screen.getByRole("button", { name: t("thread.send") }))
      await waitFor(() => expect(screen.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", "false"))
      expect(screen.queryByText("pong")).not.toBeInTheDocument()
      expect(screen.queryByTestId("phone-thought")).not.toBeInTheDocument()
      release()
      await waitFor(() => expect(screen.getByText("pong")).toBeInTheDocument())
      expect(screen.queryByTestId("work-fold")).not.toBeInTheDocument()
      expect(screen.queryByTestId("phone-thought")).not.toBeInTheDocument()
      expect(screen.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", "false")
      fireEvent.click(screen.getByTestId("phone-thought-toggle"))
      expect(screen.getByTestId("phone-thought")).toHaveTextContent("because")
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it("shows the endpoint's refusal on the reply", async () => {
    setLocale("en")
    const complete = vi.fn(async () => {
      throw new ModelCallError("nope", "http")
    })
    harness(complete)
    fireEvent.change(screen.getByLabelText(t("thread.message")), { target: { value: "ping" } })
    fireEvent.click(screen.getByRole("button", { name: t("thread.send") }))
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("nope"))
  })
})
