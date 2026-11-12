import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { api } from "@/lib/api"
import type { MCPServerInput, MCPServerView } from "@/lib/mcp-settings"
import type { Settings, ToolDescriptor } from "@/lib/types"
import { useApp } from "@/store/app"

import { ComposerTools } from "./composer-tools"

vi.mock("@/lib/api", () => ({ api: {
  tools: vi.fn(), mcpServers: vi.fn(), settings: vi.fn(), saveSettings: vi.fn(), saveMCPServer: vi.fn(),
} }))

const catalog: ToolDescriptor[] = [
  { name: "read", title: "Read file", summary: "Read a file", group: "files", network: false, default_off: false },
  { name: "python_runner", title: "Run Python", summary: "Run Python", group: "shell", network: false, default_off: true },
  { name: "web_search", title: "Web search", summary: "Search", group: "web", network: true, default_off: false },
]

const server: MCPServerView = {
  id: "desk", name: "Desk", transport: "stdio", command: "runner", args: ["--serve"],
  url: "", enabled: true, timeout_seconds: 30, env_keys: ["TOKEN"], header_keys: [],
}

function toolSettings(patch: Partial<Settings["tools"]> = {}): Settings["tools"] {
  return {
    disabled: [], enabled: [], proxy: { http: "http://127.0.0.1:9", https: "", no_proxy: "" },
    web_search_max_results: 8, web_execution: "local", result_max_chars: 12000, ...patch,
  }
}

describe("Composer tool switches", () => {
  beforeEach(() => {
    useApp.setState({ locale: "en" })
    vi.mocked(api.tools).mockReset().mockResolvedValue({ catalog, enabled: ["read", "web_search"] })
    vi.mocked(api.mcpServers).mockReset().mockResolvedValue([server])
    vi.mocked(api.settings).mockReset().mockResolvedValue({ tools: toolSettings() } as Settings)
    vi.mocked(api.saveSettings).mockReset().mockImplementation(async (patch) => ({ tools: patch.tools } as Settings))
    vi.mocked(api.saveMCPServer).mockReset().mockImplementation(async (input: MCPServerInput) => ({
      ...server, ...input, env_keys: server.env_keys, header_keys: [],
    }))
  })

  it("starts checked and writes the next request's tool list when one is cleared", async () => {
    const user = userEvent.setup()
    render(<ComposerTools />)
    await user.click(screen.getByRole("button", { name: "Tools" }))
    const read = await screen.findByRole("menuitemcheckbox", { name: "Read file (read)" })
    const python = screen.getByRole("menuitemcheckbox", { name: "Run Python (python_runner)" })
    const desk = screen.getByRole("menuitemcheckbox", { name: "Desk" })
    expect(read).toHaveAttribute("aria-checked", "true")
    expect(python).toHaveAttribute("aria-checked", "false")
    expect(desk).toHaveAttribute("aria-checked", "true")
    expect(screen.getByText(/next model request/)).toBeVisible()

    await user.click(read)
    await waitFor(() => expect(api.saveSettings).toHaveBeenCalledWith({
      tools: expect.objectContaining({
        disabled: ["read"], enabled: [], web_execution: "local",
        proxy: { http: "http://127.0.0.1:9", https: "", no_proxy: "" },
      }),
    }))
    expect(read).toHaveAttribute("aria-checked", "false")

    await user.click(desk)
    await waitFor(() => expect(api.saveMCPServer).toHaveBeenCalledWith(expect.objectContaining({
      id: "desk", enabled: false, command: "runner", args: ["--serve"],
    })))
    expect(api.saveMCPServer).toHaveBeenCalledWith(expect.not.objectContaining({ env: expect.anything() }))
    expect(desk).toHaveAttribute("aria-checked", "false")
  })

  it("keeps the previous check when the save fails", async () => {
    vi.mocked(api.saveSettings).mockRejectedValueOnce(new Error("offline"))
    const user = userEvent.setup()
    render(<ComposerTools />)
    await user.click(screen.getByRole("button", { name: "Tools" }))
    const read = await screen.findByRole("menuitemcheckbox", { name: "Read file (read)" })
    await user.click(read)
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not update tools")
    await waitFor(() => expect(read).toHaveAttribute("aria-checked", "true"))
  })
})
