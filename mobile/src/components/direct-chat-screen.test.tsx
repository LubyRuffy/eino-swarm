import { fireEvent, render, screen, waitFor } from "@testing-library/react"
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
    fireEvent.click(screen.getByTestId("work-fold"))
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
    await waitFor(() => expect(screen.getByRole("button", { name: t("thread.stop") })).toBeInTheDocument())
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
      await waitFor(() => expect(screen.getByTestId("work-fold")).toHaveTextContent("because"))
      expect(screen.queryByText("pong")).not.toBeInTheDocument()
      expect(screen.queryByTestId("phone-thought")).not.toBeInTheDocument()
      release()
      await waitFor(() => expect(screen.getByText("pong")).toBeInTheDocument())
      fireEvent.click(screen.getByTestId("work-fold"))
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
