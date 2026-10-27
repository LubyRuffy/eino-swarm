import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ClientGroups } from "./client-groups"

describe("ClientGroups", () => {
  it("does not reopen a task when its read finishes after Back", async () => {
    let finish: (view: { id: string; title: string; status: string; entries: [] }) => void = () => undefined
    const pending = new Promise<{ id: string; title: string; status: string; entries: [] }>((resolve) => { finish = resolve })
    render(<ClientGroups
      tools={[{ id: "codex", more: false, tasks: [{ id: "c1", title: "task", status: "done", updated_at: "2026-09-25T00:00:00Z" }] }]}
      onMore={() => undefined}
      onRead={() => pending}
    />)
    fireEvent.click(screen.getByRole("button", { name: "task" }))
    expect(screen.getByTestId("client-transcript")).toBeInTheDocument()
    await act(async () => expect(window.__zwaiAndroidBack?.()).toBe(true))
    await act(async () => finish({ id: "c1", title: "task", status: "done", entries: [] }))
    expect(screen.queryByTestId("client-transcript")).not.toBeInTheDocument()
  })

  it("shows a spinner and disables Clients More while its page is loading", () => {
    const onMore = vi.fn()
    render(
      <ClientGroups
        tools={[
          { id: "codex", more: true, next: "42", tasks: [] },
          { id: "claude", more: true, next: "38", tasks: [] },
        ]}
        loadingMore="codex"
        onMore={onMore}
      />,
    )
    const button = screen.getByTestId("client-more-codex")
    expect(button).toHaveAttribute("aria-busy", "true")
    expect(button).toBeDisabled()
    expect(button).toHaveTextContent("Loading more")
    expect(button.querySelector("svg.motion-safe\\:animate-spin")).not.toBeNull()
    expect(screen.getByTestId("client-more-claude")).toBeDisabled()
    fireEvent.click(button)
    expect(onMore).not.toHaveBeenCalled()
  })

  it("shows a running light and does not open the task", () => {
    const onMore = vi.fn()
    render(
      <ClientGroups
        tools={[
          {
            id: "codex",
            more: false,
            tasks: [{ id: "c1", title: "fresh task", status: "running", updated_at: "2026-09-25T00:00:00Z" }],
          },
        ]}
        onMore={onMore}
      />,
    )
    expect(screen.getByTestId("client-status")).toHaveAttribute("data-status", "running")
    fireEvent.click(screen.getByRole("button", { name: "fresh task" }))
    expect(onMore).not.toHaveBeenCalled()
    expect(screen.getByTestId("client-transcript")).toBeTruthy()
    expect(screen.getByLabelText("Message")).toBeDisabled()
  })

  it("keeps task-detail touches out of the inbox pull gesture", () => {
    const onInboxTouch = vi.fn()
    render(
      <div onTouchStart={onInboxTouch}>
        <ClientGroups
          tools={[{ id: "codex", more: false, tasks: [{ id: "c1", title: "open session", status: "done", updated_at: "2026-09-25T00:00:00Z" }] }]}
          onMore={() => undefined}
        />
      </div>,
    )
    fireEvent.click(screen.getByRole("button", { name: "open session" }))
    fireEvent.touchStart(screen.getByTestId("client-transcript"))
    expect(onInboxTouch).not.toHaveBeenCalled()
  })

  it("folds a tool group and hides its tasks", () => {
    render(
      <ClientGroups
        tools={[
          {
            id: "claude",
            more: false,
            tasks: [{ id: "c1", title: "open session", status: "done", updated_at: "2026-09-25T00:00:00Z" }],
          },
        ]}
        onMore={() => undefined}
      />,
    )
    expect(screen.getByTestId("client-task")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Claude" }))
    expect(screen.queryByTestId("client-task")).toBeNull()
  })

  it("folds adjacent thinking and tools until the row is opened", async () => {
    render(
      <ClientGroups
        tools={[
          {
            id: "cursor",
            more: false,
            tasks: [{ id: "c1", title: "open session", status: "done", updated_at: "2026-09-25T00:00:00Z" }],
          },
        ]}
        onMore={() => undefined}
        onRead={async () => ({
          id: "c1",
          title: "open session",
          status: "done",
          entries: [
            { role: "user", text: "the request" },
            { role: "thinking", text: "checked the path" },
            { role: "tool", text: "read" },
            { role: "assistant", text: "done" },
          ],
        })}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "open session" }))
    const fold = await screen.findByTestId("work-fold")
    expect(screen.getByTestId("client-request")).toHaveTextContent("the request")
    expect(fold).toHaveTextContent("Thought · 1 tool")
    expect(screen.queryByText("read")).toBeNull()
    expect(screen.getByText("done")).toBeTruthy()
    fireEvent.click(fold)
    await waitFor(() => expect(screen.getByText("read")).toBeTruthy())
    expect(screen.queryByTestId("phone-thought")).not.toBeInTheDocument()
    expect(screen.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", "false")
    fireEvent.click(screen.getByTestId("phone-thought-toggle"))
    expect(screen.getByText("checked the path")).toBeTruthy()
  })

  it("loads an earlier page and keeps the live reply", async () => {
    const onRead = vi.fn(async (_id: string, before?: number) => {
      if (before) {
        return {
          id: "c1",
          title: "open session",
          status: "done",
          older: false,
          entries: [
            { role: "user", text: "the request", at: 0 },
            { role: "assistant", text: "earlier reply", at: 10 },
          ],
        }
      }
      return {
        id: "c1",
        title: "open session",
        status: "done",
        older: true,
        before: 40,
        entries: [
          { role: "user", text: "the request", at: 0 },
          { role: "assistant", text: "latest reply", at: 40 },
        ],
      }
    })
    render(
      <ClientGroups
        tools={[
          {
            id: "cursor",
            more: false,
            tasks: [{ id: "c1", title: "open session", status: "done", updated_at: "2026-09-25T00:00:00Z" }],
          },
        ]}
        onMore={() => undefined}
        onRead={onRead}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "open session" }))
    expect(await screen.findByText("latest reply")).toBeTruthy()
    fireEvent.click(screen.getByTestId("client-earlier"))
    expect(await screen.findByText("earlier reply")).toBeTruthy()
    expect(screen.getByText("latest reply")).toBeTruthy()
    expect(onRead).toHaveBeenCalledWith("c1", 40)
  })
  it.each([
    { at: 200, n: 0 },
    { at: 100, n: 1 },
  ])("keeps a new thinking identity closed after a tail poll ($at, $n)", async ({ at, n }) => {
    const doc = (thinkingAt: number, thinkingN: number, text: string) => ({
      id: "c1",
      title: "open session",
      status: "running",
      entries: [
        { role: "user", text: "the request", at: 1, n: 0 },
        { role: "thinking", text, at: thinkingAt, n: thinkingN },
      ],
    })
    const onRead = vi.fn()
      .mockResolvedValueOnce(doc(100, 0, "earlier analysis"))
      .mockResolvedValueOnce(doc(at, n, "next analysis"))
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] })
    const { unmount } = render(
      <ClientGroups
        tools={[{ id: "cursor", more: false, tasks: [{ id: "c1", title: "open session", status: "running", updated_at: "2026-09-25T00:00:00Z" }] }]}
        onMore={() => undefined}
        onRead={onRead}
      />,
    )
    try {
      fireEvent.click(screen.getByRole("button", { name: "open session" }))
      fireEvent.click(await screen.findByTestId("work-fold"))
      fireEvent.click(screen.getByTestId("phone-thought-toggle"))
      expect(screen.getByTestId("phone-thought")).toHaveTextContent("earlier analysis")
      await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
      expect(onRead).toHaveBeenCalledTimes(2)
      expect(onRead).toHaveBeenLastCalledWith("c1")
      expect(screen.getByTestId("work-fold")).toHaveAttribute("aria-expanded", "true")
      expect(screen.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", "false")
      expect(screen.queryByTestId("phone-thought")).not.toBeInTheDocument()
    } finally {
      unmount()
      vi.useRealTimers()
    }
  })

  it("preserves the chosen thinking state while the same identity grows in tail polls", async () => {
    const doc = (text: string, status = "running") => ({
      id: "c1",
      title: "open session",
      status,
      entries: [
        { role: "user", text: "the request", at: 1, n: 0 },
        { role: "thinking", text, at: 100, n: 2 },
      ],
    })
    const onRead = vi.fn()
      .mockResolvedValueOnce(doc("initial analysis"))
      .mockResolvedValueOnce(doc("initial analysis with a detail"))
      .mockResolvedValueOnce(doc("initial analysis with a detail and conclusion", "done"))
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] })
    const { unmount } = render(
      <ClientGroups
        tools={[{ id: "cursor", more: false, tasks: [{ id: "c1", title: "open session", status: "running", updated_at: "2026-09-25T00:00:00Z" }] }]}
        onMore={() => undefined}
        onRead={onRead}
      />,
    )
    try {
      fireEvent.click(screen.getByRole("button", { name: "open session" }))
      fireEvent.click(await screen.findByTestId("work-fold"))
      fireEvent.click(screen.getByTestId("phone-thought-toggle"))
      expect(screen.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", "true")
      await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
      expect(onRead).toHaveBeenCalledTimes(2)
      expect(screen.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", "true")
      expect(screen.getByTestId("phone-thought")).toHaveTextContent("initial analysis with a detail")
      fireEvent.click(screen.getByTestId("phone-thought-toggle"))
      await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
      expect(onRead).toHaveBeenCalledTimes(3)
      expect(screen.getByTestId("work-fold")).toHaveAttribute("aria-expanded", "true")
      expect(screen.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", "false")
      expect(screen.queryByTestId("phone-thought")).not.toBeInTheDocument()
    } finally {
      unmount()
      vi.useRealTimers()
    }
  })
})
