import { act, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import type { ComponentProps } from "react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { Palette } from "./palette"
import { api } from "@/lib/api"
import { applyLocale } from "@/lib/i18n"
import type { SearchHit, Thread } from "@/lib/types"
import { useApp } from "@/store/app"

vi.mock("@/lib/api", () => ({ api: { search: vi.fn() } }))

const placeholder = "Search conversations, or type a command…"
const loading = "Searching conversations…"
const failure = "Conversation search failed. Showing matching local titles only."
type SearchResponse = Awaited<ReturnType<typeof api.search>>

function thread(partial: Partial<Thread> = {}): Thread {
  return {
    id: "th_1",
    title: "Field journal",
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

function props(overrides: Partial<ComponentProps<typeof Palette>> = {}): ComponentProps<typeof Palette> {
  return {
    open: true,
    threads: [thread()],
    onOpenChange: vi.fn(),
    onOpen: vi.fn(),
    onNew: vi.fn(),
    onSettings: vi.fn(),
    onToggleTheme: vi.fn(),
    onToggleLocale: vi.fn(),
    onToggleContentWidth: vi.fn(),
    onToggleTranscriptMode: vi.fn(),
    onToggleSidebar: vi.fn(),
    onFind: vi.fn(),
    onOpenTerminal: vi.fn(),
    ...overrides,
  }
}

function response(query: string, hits: SearchHit[] = []): SearchResponse {
  return { query, embedding: false, hits }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((accept, refuse) => { resolve = accept; reject = refuse })
  return { promise, resolve, reject }
}

describe("Palette search", () => {
  beforeEach(() => {
    vi.mocked(api.search).mockReset().mockImplementation(async (query) => response(query))
  })

  it("keeps actions and local conversations without querying when the box is blank", async () => {
    const user = userEvent.setup()
    render(<Palette {...props()} />)
    expect(screen.getByRole("option", { name: /New conversation/ })).toBeVisible()
    expect(screen.getByText("Field journal")).toBeInTheDocument()
    await user.type(screen.getByPlaceholderText(placeholder), "   ")
    expect(screen.getByText("Field journal")).toBeInTheDocument()
    expect(api.search).not.toHaveBeenCalled()
  })

  it("hides unrelated actions and opens the first body hit with Enter", async () => {
    vi.mocked(api.search).mockResolvedValue(response("quartz", [{
      thread_id: "body-hit", title: "Field journal", snippet: "quartz observations", score: 1, source: "fts",
    }]))
    const callbacks = props()
    const user = userEvent.setup()
    render(<Palette {...callbacks} />)
    await user.type(screen.getByPlaceholderText(placeholder), "quartz")
    await screen.findByText("quartz observations")
    expect(screen.queryByRole("option", { name: /New conversation/ })).not.toBeInTheDocument()
    expect(screen.queryByRole("option", { name: "Settings" })).not.toBeInTheDocument()
    await user.keyboard("{Enter}")
    expect(callbacks.onOpen).toHaveBeenCalledWith("body-hit")
    expect(callbacks.onNew).not.toHaveBeenCalled()
    expect(callbacks.onOpenChange).toHaveBeenCalledWith(false)
  })

  it("keeps a matching command selectable after the server returns no conversations", async () => {
    const callbacks = props()
    const user = userEvent.setup()
    render(<Palette {...callbacks} />)
    await user.type(screen.getByPlaceholderText(placeholder), "settings")
    await waitFor(() => expect(api.search).toHaveBeenCalledWith("settings"))
    await waitFor(() => expect(screen.queryByText(loading)).not.toBeInTheDocument())
    expect(screen.getAllByRole("option")).toHaveLength(1)
    expect(screen.getByRole("option", { name: "Settings" })).toBeVisible()
    expect(screen.queryByText("Nothing matches that.")).not.toBeInTheDocument()
    await user.keyboard("{Enter}")
    expect(callbacks.onSettings).toHaveBeenCalledOnce()
    expect(callbacks.onNew).not.toHaveBeenCalled()
  })

  it.each([
    { query: "在会话中查找", label: "在会话中查找", callback: "onFind" as const },
    { query: "浅色", label: "切换浅色 / 深色", callback: "onToggleTheme" as const },
    { query: "深色", label: "切换浅色 / 深色", callback: "onToggleTheme" as const },
  ])("finds a localized visible command label using $query and runs it with Enter", async ({ query, label, callback }) => {
    act(() => {
      applyLocale("zh")
      useApp.setState({ locale: "zh" })
    })
    const callbacks = props()
    const user = userEvent.setup()
    render(<Palette {...callbacks} />)
    await user.type(screen.getByRole("combobox"), query)
    await waitFor(() => expect(api.search).toHaveBeenCalledWith(query))
    await waitFor(() => expect(screen.getAllByRole("option")).toHaveLength(1))
    expect(screen.getByRole("option")).toHaveTextContent(label)
    await user.keyboard("{Enter}")
    expect(callbacks[callback]).toHaveBeenCalledOnce()
    expect(callbacks.onNew).not.toHaveBeenCalled()
    expect(callbacks.onOpenChange).toHaveBeenCalledWith(false)
  })

  it("distinguishes a pending request from a completed search with no matches", async () => {
    const pending = deferred<SearchResponse>()
    vi.mocked(api.search).mockReturnValue(pending.promise)
    const user = userEvent.setup()
    render(<Palette {...props()} />)
    await user.type(screen.getByPlaceholderText(placeholder), "quartz")
    await waitFor(() => expect(api.search).toHaveBeenCalledWith("quartz"))
    expect(screen.getByRole("status")).toHaveTextContent(loading)
    expect(screen.getByRole("listbox")).toHaveAttribute("aria-busy", "true")
    expect(screen.queryByText("Nothing matches that.")).not.toBeInTheDocument()
    expect(screen.queryByText("Field journal")).not.toBeInTheDocument()
    expect(screen.queryAllByRole("option")).toHaveLength(0)
    await act(async () => pending.resolve(response("quartz")))
    expect(screen.getByRole("listbox")).toHaveAttribute("aria-busy", "false")
    expect(screen.queryByText(loading)).not.toBeInTheDocument()
    expect(screen.getByText("Nothing matches that.")).toBeVisible()
    expect(screen.queryByText(failure)).not.toBeInTheDocument()
  })

  it("reports a failed search and only offers matching local titles", async () => {
    vi.mocked(api.search).mockRejectedValue(new Error("offline"))
    const callbacks = props({ threads: [thread({ title: "Orchard planning" }), thread({ id: "th_2", title: "Harbor log" })] })
    const user = userEvent.setup()
    render(<Palette {...callbacks} />)
    await user.type(screen.getByPlaceholderText(placeholder), "orchard")
    await screen.findByText(failure)
    expect(screen.getByRole("alert")).toHaveTextContent(failure)
    expect(screen.getByRole("listbox")).toHaveAttribute("aria-busy", "false")
    expect(screen.getByRole("option", { name: /Orchard planning/ })).toBeVisible()
    expect(screen.queryByText("Harbor log")).not.toBeInTheDocument()
    expect(screen.queryByRole("option", { name: /New conversation/ })).not.toBeInTheDocument()
    expect(screen.queryByText(loading)).not.toBeInTheDocument()
    expect(screen.queryByText("Nothing matches that.")).not.toBeInTheDocument()
    await user.keyboard("{Enter}")
    expect(callbacks.onOpen).toHaveBeenCalledWith("th_1")
  })

  it("does not disguise a failed request with no local matches as an empty search", async () => {
    vi.mocked(api.search).mockRejectedValue(new Error("offline"))
    const user = userEvent.setup()
    render(<Palette {...props()} />)
    await user.type(screen.getByPlaceholderText(placeholder), "quartz")
    await screen.findByText(failure)
    expect(screen.queryAllByRole("option")).toHaveLength(0)
    expect(screen.queryByText("Nothing matches that.")).not.toBeInTheDocument()
    expect(screen.queryByText(loading)).not.toBeInTheDocument()
  })

  it("preserves server rank for a semantic hit with a blank title and trailing query whitespace", async () => {
    vi.mocked(api.search).mockResolvedValue({
      query: "travel planning", embedding: true, hits: [
        { thread_id: "rank-first", title: "", snippet: "vessel departure", score: 2, source: "semantic" },
        { thread_id: "rank-second", title: "Travel planning notes", snippet: "regional connections", score: 1, source: "semantic" },
      ],
    })
    const callbacks = props()
    const user = userEvent.setup()
    render(<Palette {...callbacks} />)
    await user.type(screen.getByPlaceholderText(placeholder), "travel planning ")
    await screen.findByText("vessel departure")
    const rows = screen.getAllByRole("option")
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent("Untitled")
    expect(rows[0]).toHaveTextContent("vessel departure")
    expect(rows[1]).toHaveTextContent("Travel planning notes")
    await user.keyboard("{Enter}")
    expect(callbacks.onOpen).toHaveBeenCalledWith("rank-first")
  })

  it("ignores an old response after a newer query has resolved", async () => {
    const old = deferred<SearchResponse>()
    vi.mocked(api.search).mockImplementation((query) => query === "older" ? old.promise : Promise.resolve(response(query, [{
      thread_id: "fresh", title: "Fresh result", snippet: "newest material", score: 1, source: "fts",
    }])))
    const user = userEvent.setup()
    render(<Palette {...props()} />)
    const input = screen.getByPlaceholderText(placeholder)
    await user.type(input, "older")
    await waitFor(() => expect(api.search).toHaveBeenCalledWith("older"))
    await user.clear(input)
    await user.type(input, "newer")
    await screen.findByText("Fresh result")
    await act(async () => old.resolve(response("older", [{
      thread_id: "stale", title: "Stale result", snippet: "old material", score: 1, source: "fts",
    }])))
    expect(screen.getByText("Fresh result")).toBeInTheDocument()
    expect(screen.queryByText("Stale result")).not.toBeInTheDocument()
  })

  it("removes an already displayed hit as soon as another query starts", async () => {
    const pending = deferred<SearchResponse>()
    vi.mocked(api.search).mockImplementation((query) => query === "older"
      ? Promise.resolve(response(query, [{ thread_id: "stale", title: "Stale result", snippet: "old material", score: 1, source: "fts" }]))
      : pending.promise)
    const user = userEvent.setup()
    render(<Palette {...props()} />)
    const input = screen.getByPlaceholderText(placeholder)
    await user.type(input, "older")
    await screen.findByText("Stale result")
    await user.type(input, "x")
    expect(screen.queryByText("Stale result")).not.toBeInTheDocument()
    expect(screen.getByRole("status")).toHaveTextContent(loading)
    await waitFor(() => expect(api.search).toHaveBeenCalledWith("olderx"))
    await act(async () => pending.resolve(response("olderx", [{
      thread_id: "fresh", title: "Fresh result", snippet: "new material", score: 1, source: "fts",
    }])))
    expect(screen.getByText("Fresh result")).toBeInTheDocument()
    expect(screen.queryByText("Stale result")).not.toBeInTheDocument()
  })

  it.each(["success", "failure"])("ignores a pending request's late %s after closing and reopening", async (outcome) => {
    const pending = deferred<SearchResponse>()
    vi.mocked(api.search).mockReturnValue(pending.promise)
    const callbacks = props()
    const user = userEvent.setup()
    const view = render(<Palette {...callbacks} />)
    await user.type(screen.getByPlaceholderText(placeholder), "quartz")
    await waitFor(() => expect(api.search).toHaveBeenCalledWith("quartz"))
    view.rerender(<Palette {...callbacks} open={false} />)
    await act(async () => {
      if (outcome === "success") pending.resolve(response("quartz", [{
        thread_id: "stale", title: "Stale result", snippet: "old material", score: 1, source: "fts",
      }]))
      else pending.reject(new Error("late failure"))
    })
    view.rerender(<Palette {...callbacks} />)
    expect(screen.getByPlaceholderText(placeholder)).toHaveValue("")
    expect(screen.getByText("Field journal")).toBeInTheDocument()
    expect(screen.queryByText("Stale result")).not.toBeInTheDocument()
    expect(screen.queryByText(failure)).not.toBeInTheDocument()
    expect(screen.queryByText(loading)).not.toBeInTheDocument()
  })
})
