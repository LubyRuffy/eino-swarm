import { render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ClientChat } from "./client-transcript"

vi.mock("@/lib/api", () => ({
  api: {
    clientTask: async () => ({
      id: "claude:s1",
      title: "open session",
      status: "running",
      entries: [
        { role: "user", text: "open session" },
        { role: "assistant", text: "found the file" },
        { role: "tool", text: "read" },
      ],
    }),
  },
}))

describe("ClientChat", () => {
  it("renders the session in the conversation column", async () => {
    render(<ClientChat id="claude:s1" />)
    expect(await screen.findByTestId("client-chat")).toBeTruthy()
    await waitFor(() => {
      expect(screen.getByTestId("user-message").textContent).toContain("open session")
    })
    expect(screen.getByTestId("assistant-message").textContent).toContain("found the file")
    expect(screen.getByTestId("client-tool").textContent).toContain("read")
  })
})
