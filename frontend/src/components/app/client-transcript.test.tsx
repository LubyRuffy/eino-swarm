import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { useApp } from "@/store/app"

import { ClientChat } from "./client-transcript"

vi.mock("@/lib/api", () => ({
  api: {
    clientTask: async () => ({
      id: "claude:s1",
      title: "open session",
      status: "running",
      entries: [
        { role: "user", text: "open session" },
        { role: "thinking", text: "checked the path" },
        { role: "tool", text: "read" },
        { role: "tool", text: "grep" },
        { role: "assistant", text: "found the file" },
      ],
    }),
  },
}))

describe("ClientChat", () => {
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
})
