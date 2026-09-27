import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { useApp } from "@/store/app"

import { ClientChat } from "./client-transcript"

const { clientTask } = vi.hoisted(() => ({ clientTask: vi.fn() }))

vi.mock("@/lib/api", () => ({
  api: { clientTask },
}))

const folded = {
  id: "claude:s1",
  title: "open session",
  status: "running",
  entries: [
    { role: "user", text: "open session", at: 0 },
    { role: "thinking", text: "checked the path", at: 20 },
    { role: "tool", text: "read", at: 30 },
    { role: "tool", text: "grep", at: 40 },
    { role: "assistant", text: "found the file", at: 50 },
  ],
}

describe("ClientChat", () => {
  beforeEach(() => {
    clientTask.mockReset()
    clientTask.mockResolvedValue(folded)
  })

  it("folds adjacent thinking and tools until the row is opened", async () => {
    useApp.setState({ transcriptMode: "user" })
    render(<ClientChat id="claude:s1" />)
    expect(await screen.findByTestId("client-chat")).toBeTruthy()
    await waitFor(() => {
      expect(screen.getByTestId("user-message").textContent).toContain("open session")
    })
    expect(screen.getByTestId("client-request")).toContainElement(screen.getByTestId("user-message"))
    expect(screen.getByTestId("assistant-message").textContent).toContain("found the file")
    const fold = screen.getByTestId("work-fold")
    expect(fold).toHaveTextContent("Thought · 2 tools")
    expect(screen.queryByTestId("client-tool")).toBeNull()
    fireEvent.click(fold)
    expect(screen.getByTestId("client-thought").textContent).toContain("checked the path")
    expect(screen.getAllByTestId("client-tool").map((n) => n.textContent)).toEqual(["read", "grep"])
  })

  it("loads the earlier page and keeps the live reply", async () => {
    clientTask.mockImplementation(async (_id: string, before?: number) => {
      if (before) {
        return {
          id: "claude:s1",
          title: "open session",
          status: "running",
          older: false,
          entries: [
            { role: "user", text: "open session", at: 0 },
            { role: "assistant", text: "earlier reply", at: 10 },
          ],
        }
      }
      return {
        id: "claude:s1",
        title: "open session",
        status: "running",
        older: true,
        before: 40,
        entries: [
          { role: "user", text: "open session", at: 0 },
          { role: "assistant", text: "latest reply", at: 40 },
        ],
      }
    })
    render(<ClientChat id="claude:s1" />)
    expect(await screen.findByText("latest reply")).toBeTruthy()
    expect(screen.queryByText("earlier reply")).toBeNull()
    fireEvent.click(screen.getByTestId("client-earlier"))
    expect(await screen.findByText("earlier reply")).toBeTruthy()
    expect(screen.getByText("latest reply")).toBeTruthy()
    expect(screen.queryByTestId("client-earlier")).toBeNull()
    expect(clientTask).toHaveBeenCalledWith("claude:s1", 40)
  })
})
