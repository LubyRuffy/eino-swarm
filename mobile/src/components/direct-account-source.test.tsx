import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { DirectChatScreen } from "./direct-chat-screen"
import type { DirectProvider } from "@/lib/direct-provider"
import type { streamCompletion } from "@/lib/openai-client"
import { ModelCallError } from "@/lib/openai-wire"
import { saveThreads, newThread } from "@/lib/direct-threads"

const server: DirectProvider = { id: "gateway:owner", sourceKind: "gateway", label: "Server", baseURL: "https://server.invalid/v1", apiKey: "mobile-secret", api: "chat", model: "shared", catalog: ["shared"], timeoutSeconds: 300 }
const personal: DirectProvider = { ...server, id: "p-personal", sourceKind: "phone", label: "Personal", apiKey: "private-secret", baseURL: "https://personal.invalid/v1" }
const props = { hosts: [], activeFingerprint: "", path: "relay", onSelectHost: vi.fn(), onAddHost: vi.fn(), onUnlink: vi.fn(), onModels: vi.fn() }

describe("direct account source", () => {
  it("cancels an active server response when its account source disappears", async () => {
    let signal: AbortSignal | undefined
    const complete: typeof streamCompletion = vi.fn(async (input) => {
      signal = input.signal
      input.onDelta({ text: "received", reasoning: "" })
      await new Promise<void>((_resolve, reject) => input.signal?.addEventListener("abort", () => reject(new ModelCallError("abort", "abort")), { once: true }))
    })
    const view = render(<DirectChatScreen {...props} gatewaySourceID={server.id} providers={[server, personal]} complete={complete} />)
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "test" } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await screen.findByText("received")
    view.rerender(<DirectChatScreen {...props} gatewaySourceID={server.id} providers={[personal]} complete={complete} />)
    await waitFor(() => expect(signal?.aborted).toBe(true))
    expect(screen.getByText("received")).toBeVisible()
    expect(complete).toHaveBeenCalledTimes(1)
  })
  it("hides another account's server history while retaining personal and current account conversations", () => {
    const a = newThread({ providerId: "gateway:other", model: "shared", reasoning: "" }); a.title = "Other account private history"
    const own = newThread({ providerId: server.id, model: "shared", reasoning: "" }); own.title = "Current account history"
    const local = newThread({ providerId: personal.id, model: "shared", reasoning: "" }); local.title = "Phone personal history"
    saveThreads([a, own, local])
    render(<DirectChatScreen {...props} gatewaySourceID={server.id} providers={[server, personal]} />)
    expect(screen.queryByText(a.title)).not.toBeInTheDocument()
    expect(screen.getByText(own.title)).toBeVisible()
    expect(screen.getByText(local.title)).toBeVisible()
  })

  it("does not route a saved server thread to a private same-named model", async () => {
    const thread = newThread({ providerId: server.id, model: "shared", reasoning: "" })
    thread.title = "Server conversation"
    saveThreads([thread])
    const complete = vi.fn<typeof streamCompletion>()
    render(<DirectChatScreen {...props} gatewaySourceID={server.id} providers={[personal]} complete={complete} />)
    fireEvent.click(screen.getByRole("button", { name: "Open Server conversation" }))
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "test" } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)) })
    expect(complete).not.toHaveBeenCalled()
    expect(screen.getByLabelText("Message")).toHaveValue("test")
  })
})
