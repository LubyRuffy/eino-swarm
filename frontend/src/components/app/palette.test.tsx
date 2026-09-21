import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest"

import { Palette } from "./palette"
import { api } from "@/lib/api"
import type { Thread } from "@/lib/types"

vi.mock("@/lib/api", () => ({
  api: {
    search: vi.fn(),
  },
}))

beforeAll(() => {
  // cmdk scrolls the selected row; jsdom has no layout.
  Element.prototype.scrollIntoView = vi.fn()
})

function thread(partial: Partial<Thread> = {}): Thread {
  return {
    id: "th_1",
    title: "gamma",
    project_id: "",
    provider_id: "default",
    reasoning_effort: "",
    archived: false,
    created_at: new Date().toISOString(),
    last_active_at: new Date().toISOString(),
    running: false,
    ...partial,
  }
}

const noop = () => undefined

describe("Palette search", () => {
  beforeEach(() => {
    vi.mocked(api.search).mockReset()
  })

  it("does not call the API while the box is empty", () => {
    render(
      <Palette
        open
        onOpenChange={noop}
        threads={[thread()]}
        onOpen={noop}
        onNew={noop}
        onSettings={noop}
        onToggleTheme={noop}
        onToggleLocale={noop}
        onToggleContentWidth={noop}
        onToggleSidebar={noop}
        onFind={noop}
        onOpenTerminal={noop}
      />,
    )
    expect(screen.getByText("gamma")).toBeInTheDocument()
    expect(api.search).not.toHaveBeenCalled()
  })

  it("lists a body hit the title does not contain", async () => {
    vi.mocked(api.search).mockResolvedValue({
      query: "unique-body",
      embedding: false,
      hits: [
        {
          thread_id: "th_1",
          title: "gamma",
          snippet: "unique-body needle",
          score: 1,
          source: "fts",
        },
      ],
    })
    const user = userEvent.setup()
    render(
      <Palette
        open
        onOpenChange={noop}
        threads={[thread()]}
        onOpen={noop}
        onNew={noop}
        onSettings={noop}
        onToggleTheme={noop}
        onToggleLocale={noop}
        onToggleContentWidth={noop}
        onToggleSidebar={noop}
        onFind={noop}
        onOpenTerminal={noop}
      />,
    )
    await user.type(
      screen.getByPlaceholderText("Search conversations, or type a command…"),
      "unique-body",
    )
    await waitFor(() => expect(api.search).toHaveBeenCalledWith("unique-body"))
    expect(screen.getByText("unique-body needle")).toBeInTheDocument()
  })

  it("keeps the local conversation list when search fails", async () => {
    vi.mocked(api.search).mockRejectedValue(new Error("offline"))
    const user = userEvent.setup()
    render(
      <Palette
        open
        onOpenChange={noop}
        threads={[thread()]}
        onOpen={noop}
        onNew={noop}
        onSettings={noop}
        onToggleTheme={noop}
        onToggleLocale={noop}
        onToggleContentWidth={noop}
        onToggleSidebar={noop}
        onFind={noop}
        onOpenTerminal={noop}
      />,
    )
    await user.type(
      screen.getByPlaceholderText("Search conversations, or type a command…"),
      "unique-body",
    )
    await waitFor(() => expect(api.search).toHaveBeenCalledWith("unique-body"))
    expect(screen.getByText("gamma")).toBeInTheDocument()
  })

  it("hides the local list while a search is in flight", async () => {
    let finish!: (value: {
      query: string
      embedding: boolean
      hits: {
        thread_id: string
        title: string
        snippet: string
        score: number
        source: "fts"
      }[]
    }) => void
    vi.mocked(api.search).mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    const user = userEvent.setup()
    render(
      <Palette
        open
        onOpenChange={noop}
        threads={[thread()]}
        onOpen={noop}
        onNew={noop}
        onSettings={noop}
        onToggleTheme={noop}
        onToggleLocale={noop}
        onToggleContentWidth={noop}
        onToggleSidebar={noop}
        onFind={noop}
        onOpenTerminal={noop}
      />,
    )
    await user.type(
      screen.getByPlaceholderText("Search conversations, or type a command…"),
      "unique-body",
    )
    await waitFor(() => expect(api.search).toHaveBeenCalledWith("unique-body"))
    expect(screen.queryByText("gamma")).not.toBeInTheDocument()
    finish({
      query: "unique-body",
      embedding: false,
      hits: [
        {
          thread_id: "th_1",
          title: "gamma",
          snippet: "unique-body needle",
          score: 1,
          source: "fts",
        },
      ],
    })
    await waitFor(() => expect(screen.getByText("unique-body needle")).toBeInTheDocument())
  })

  it("drops stale hits when the query changes", async () => {
    vi.mocked(api.search)
      .mockResolvedValueOnce({
        query: "alpha",
        embedding: false,
        hits: [
          {
            thread_id: "th_stale",
            title: "stale-title",
            snippet: "old hit",
            score: 1,
            source: "fts",
          },
        ],
      })
      .mockResolvedValueOnce({
        query: "alphax",
        embedding: false,
        hits: [
          {
            thread_id: "th_new",
            title: "fresh-title",
            snippet: "new hit",
            score: 1,
            source: "fts",
          },
        ],
      })
    const user = userEvent.setup()
    render(
      <Palette
        open
        onOpenChange={noop}
        threads={[thread({ title: "gamma" })]}
        onOpen={noop}
        onNew={noop}
        onSettings={noop}
        onToggleTheme={noop}
        onToggleLocale={noop}
        onToggleContentWidth={noop}
        onToggleSidebar={noop}
        onFind={noop}
        onOpenTerminal={noop}
      />,
    )
    await user.type(
      screen.getByPlaceholderText("Search conversations, or type a command…"),
      "alpha",
    )
    await waitFor(() => expect(screen.getByText("stale-title")).toBeInTheDocument())
    await user.type(
      screen.getByPlaceholderText("Search conversations, or type a command…"),
      "x",
    )
    await waitFor(() =>
      expect(screen.queryByText("stale-title")).not.toBeInTheDocument(),
    )
    await waitFor(() => expect(screen.getByText("fresh-title")).toBeInTheDocument())
  })

  it("hides local titles the server did not rank", async () => {
    vi.mocked(api.search).mockResolvedValue({
      query: "zzz",
      embedding: false,
      hits: [],
    })
    const user = userEvent.setup()
    render(
      <Palette
        open
        onOpenChange={noop}
        threads={[thread()]}
        onOpen={noop}
        onNew={noop}
        onSettings={noop}
        onToggleTheme={noop}
        onToggleLocale={noop}
        onToggleContentWidth={noop}
        onToggleSidebar={noop}
        onFind={noop}
        onOpenTerminal={noop}
      />,
    )
    await user.type(
      screen.getByPlaceholderText("Search conversations, or type a command…"),
      "zzz",
    )
    await waitFor(() => expect(api.search).toHaveBeenCalledWith("zzz"))
    expect(screen.queryByText("gamma")).not.toBeInTheDocument()
  })
})
