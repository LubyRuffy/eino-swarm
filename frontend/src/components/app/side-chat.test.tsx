import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import { SideChat } from "./side-chat"
import { api } from "@/lib/api"
import { useApp } from "@/store/app"
import { useSideChat } from "@/store/app-side-chat"

afterEach(() => {
  cleanup()
  useSideChat.getState().close()
  vi.restoreAllMocks()
})

it("cancels a side request without losing the question and permits retry after an error", async () => {
  useApp.setState({ locale: "en" })
  const chat = useSideChat.getState()
  chat.open("task", { question: "Which part was checked?", quote: "Selected context" })
  let finish!: (value: { answer: string }) => void
  const pending = new Promise<{ answer: string }>((resolve) => { finish = resolve })
  const ask = vi.spyOn(api, "askSideChat")
    .mockReturnValueOnce(pending)
    .mockRejectedValueOnce(new Error("Model unavailable"))
    .mockResolvedValueOnce({ answer: "The first part." })
  render(<SideChat threadId="task" onClose={vi.fn()} />)
  const question = screen.getByRole("textbox", { name: "Side chat question" })

  fireEvent.compositionStart(question)
  fireEvent.keyDown(question, { key: "Enter" })
  expect(ask).not.toHaveBeenCalled()
  fireEvent.compositionEnd(question)
  fireEvent.click(screen.getByRole("button", { name: "Send side question" }))
  fireEvent.click(screen.getByRole("button", { name: "Cancel side question" }))
  expect(ask.mock.calls[0][2].aborted).toBe(true)
  expect(question).toHaveValue("Which part was checked?")
  await act(async () => {
    ask.mock.calls[0][3]("Late partial answer")
    finish({ answer: "Cancelled answer" })
    await pending
  })
  expect(screen.queryByText("Cancelled answer")).toBeNull()
  expect(screen.queryByText("Late partial answer")).toBeNull()
  expect(question).toHaveFocus()

  fireEvent.click(screen.getByRole("button", { name: "Send side question" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("Model unavailable")
  expect(question).toHaveValue("Which part was checked?")
  fireEvent.click(screen.getByRole("button", { name: "Send side question" }))
  expect(await screen.findByText("The first part.")).toBeInTheDocument()
  expect(question).toHaveValue("")
  expect(screen.queryByRole("alert")).toBeNull()
  expect(ask.mock.calls.map(([id, body]) => ({ id, body }))).toEqual(Array.from({ length: 3 }, () => ({
    id: "task", body: { question: "Which part was checked?", quote: "Selected context", history: [] },
  })))
})

it("shows partial response text before the reply finishes and saves only the final answer", async () => {
  useApp.setState({ locale: "en" })
  useSideChat.getState().open("task", { question: "What did you find?" })
  let stream!: ReadableStreamDefaultController<Uint8Array>
  const body = new ReadableStream<Uint8Array>({ start: (controller) => { stream = controller } })
  const fetch = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(body, {
    headers: { "Content-Type": "text/event-stream" },
  }))
  const encoder = new TextEncoder()
  render(<SideChat threadId="task" onClose={vi.fn()} />)
  fireEvent.click(screen.getByRole("button", { name: "Send side question" }))
  expect(fetch.mock.calls[0][1]?.headers).toMatchObject({ Accept: "text/event-stream" })
  stream.enqueue(encoder.encode('event: delta\ndata: {"text":"Found the first result"}\n\n'))
  expect(await screen.findByText("Found the first result")).toBeInTheDocument()
  expect(screen.getByRole("button", { name: "Side chat model" })).toBeDisabled()
  expect(useSideChat.getState().session?.exchanges).toEqual([])

  stream.enqueue(encoder.encode('event: done\ndata: {"answer":"Found the first result and checked it."}\n\n'))
  stream.close()
  expect(await screen.findByText("Found the first result and checked it.")).toBeInTheDocument()
  expect(screen.queryByText("Found the first result")).toBeNull()
  expect(useSideChat.getState().session?.exchanges).toEqual([
    { question: "What did you find?", answer: "Found the first result and checked it." },
  ])
})

it("limits requests by UTF-8 size and retains only the four most recent pairs in model context", async () => {
  const ask = vi.spyOn(api, "askSideChat").mockResolvedValue({ answer: "Reply" })
  const chat = useSideChat.getState()
  chat.open("task", { quote: "🐈".repeat(2000) })
  chat.edit("界".repeat(1400))
  await chat.ask()
  expect(ask).not.toHaveBeenCalled()

  for (let i = 1; i <= 6; i++) {
    chat.edit(`Question ${i}`)
    await chat.ask()
  }
  const [, request] = ask.mock.calls[5]
  expect(request.quote).toBe("🐈".repeat(1023) + "…")
  expect(new TextEncoder().encode(request.quote).length).toBeLessThanOrEqual(4096)
  expect(request.history?.map(({ question }) => question)).toEqual(["Question 2", "Question 3", "Question 4", "Question 5"])
  expect(useSideChat.getState().session?.exchanges).toHaveLength(6)
})
