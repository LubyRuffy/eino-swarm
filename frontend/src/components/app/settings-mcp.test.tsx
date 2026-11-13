import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import type { MCPServerInput, MCPServerView, MCPToolCatalog } from "@/lib/mcp-settings"
import { MCPTab } from "./settings-mcp"

vi.mock("@/lib/api", () => ({ api: {
  mcpServers: vi.fn(), mcpPolicy: vi.fn(), saveMCPPolicy: vi.fn(), saveMCPServer: vi.fn(), deleteMCPServer: vi.fn(), inspectMCPServer: vi.fn(),
  importMCPServers: vi.fn(),
} }))

const saved: MCPServerView = {
  id: "local", name: "Local tools", transport: "stdio", command: "runner", args: ["--serve"],
  url: "", enabled: true, timeout_seconds: 30, env_keys: ["TOKEN"], header_keys: [],
}

describe("MCP connection settings", () => {
  beforeEach(() => {
    vi.mocked(api.mcpServers).mockReset().mockResolvedValue([])
    vi.mocked(api.mcpPolicy).mockReset().mockResolvedValue(5)
    vi.mocked(api.saveMCPPolicy).mockReset().mockImplementation(async (percent: number) => percent)
    vi.mocked(api.saveMCPServer).mockReset().mockImplementation(async (input: MCPServerInput) => ({
      ...input, env_keys: Object.keys(input.env ?? {}), header_keys: Object.keys(input.headers ?? {}),
    }))
    vi.mocked(api.deleteMCPServer).mockReset().mockResolvedValue({})
    vi.mocked(api.inspectMCPServer).mockReset().mockResolvedValue({ tools: [] })
    vi.mocked(api.importMCPServers).mockReset().mockResolvedValue([])
  })

  it("adds a local connection with exact argument values and clears the write-only draft after save", async () => {
    render(<MCPTab />)
    await screen.findByText(/No MCP servers yet/)
    expect(screen.queryByRole("heading", { name: "Tool catalog" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Add server" }))
    expect(screen.queryByLabelText("Server ID")).toBeNull()
    fireEvent.change(screen.getByLabelText("Display name"), { target: { value: "New local" } })
    fireEvent.change(screen.getByLabelText("Command"), { target: { value: "runner" } })
    fireEvent.change(screen.getByLabelText("Arguments (JSON array)"), { target: { value: '["--path","a b "]' } })
    fireEvent.change(screen.getByLabelText("Environment variables (JSON object)"), { target: { value: '{"TOKEN":"private-token"}' } })
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }))
    await screen.findByText("New local")
    const row = screen.getByText("New local").closest("[data-settings-row]")
    expect(row?.querySelector("[data-mcp-status='on']")).toHaveClass("text-done")
    expect(row?.querySelector("[data-mcp-status='unchecked']")).toHaveClass("text-muted-foreground")
    expect(api.saveMCPServer).toHaveBeenCalledWith(expect.objectContaining({
      name: "New local", args: ["--path", "a b "], env: { TOKEN: "private-token" }, enabled: true,
    }), true)
    const created = vi.mocked(api.saveMCPServer).mock.calls[0][0]
    expect(created.id).toMatch(/^[a-f0-9]{32}$/)
    expect(document.body).not.toHaveTextContent(created.id)
    expect(screen.queryByLabelText("Environment variables (JSON object)")).toBeNull()
    expect(document.body).not.toHaveTextContent("private-token")
  })

  it("refuses invalid JSON without discarding the editor and preserves saved secrets by omission", async () => {
    vi.mocked(api.mcpServers).mockResolvedValue([saved])
    render(<MCPTab />)
    fireEvent.click(await screen.findByRole("button", { name: "Edit Local tools" }))
    expect(screen.queryByLabelText("Server ID")).toBeNull()
    expect(screen.getByLabelText("Environment variables (JSON object)")).toHaveValue("")
    expect(screen.getByText(/Saved keys: TOKEN/)).toBeVisible()
    fireEvent.change(screen.getByLabelText("Arguments (JSON array)"), { target: { value: "[1]" } })
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }))
    expect(screen.getByRole("alert")).toHaveTextContent("Arguments must be a JSON array of strings.")
    expect(api.saveMCPServer).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText("Arguments (JSON array)"), { target: { value: "[]" } })
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }))
    await waitFor(() => expect(api.saveMCPServer).toHaveBeenCalledOnce())
    expect(vi.mocked(api.saveMCPServer).mock.calls[0][0].id).toBe("local")
    expect(vi.mocked(api.saveMCPServer).mock.calls[0][0]).not.toHaveProperty("env")
    expect(vi.mocked(api.saveMCPServer).mock.calls[0][0]).not.toHaveProperty("headers")
    expect(vi.mocked(api.saveMCPServer).mock.calls[0][1]).toBe(false)
  })

  it("configures a remote transport with write-only request headers", async () => {
    render(<MCPTab />)
    await screen.findByText(/No MCP servers yet/)
    fireEvent.click(screen.getByRole("button", { name: "Add server" }))
    const name = screen.getByLabelText("Display name")
    expect(name).toHaveAttribute("autocorrect", "off")
    expect(name).toHaveAttribute("autocapitalize", "off")
    expect(name).toHaveAttribute("spellcheck", "false")
    fireEvent.change(name, { target: { value: "Remote tools" } })
    fireEvent.click(screen.getByLabelText("Transport"))
    fireEvent.click(screen.getByRole("option", { name: "Streamable HTTP" }))
    expect(screen.queryByLabelText("Command")).toBeNull()
    const url = screen.getByLabelText("Server URL")
    expect(url).toHaveAttribute("type", "text")
    expect(url).toHaveAttribute("autocorrect", "off")
    expect(url).toHaveAttribute("spellcheck", "false")
    fireEvent.change(url, { target: { value: "https://tools.invalid/mcp" } })
    const headers = screen.getByLabelText("Request headers (JSON object)")
    expect(headers).toHaveAttribute("autocorrect", "off")
    expect(headers).toHaveAttribute("spellcheck", "false")
    fireEvent.change(headers, { target: { value: '{"Authorization":"private"}' } })
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }))
    await screen.findByText("Remote tools")
    expect(api.saveMCPServer).toHaveBeenCalledWith(expect.objectContaining({
      name: "Remote tools", transport: "streamable_http", url: "https://tools.invalid/mcp", headers: { Authorization: "private" },
    }), true)
    expect(vi.mocked(api.saveMCPServer).mock.calls[0][0].id).toMatch(/^[a-f0-9]{32}$/)
  })

  it("keeps an editor after save failure so the user can retry", async () => {
    vi.mocked(api.mcpServers).mockResolvedValue([saved])
    vi.mocked(api.saveMCPServer).mockRejectedValueOnce(new Error("storage unavailable"))
    render(<MCPTab />)
    fireEvent.click(await screen.findByRole("button", { name: "Edit Local tools" }))
    fireEvent.change(screen.getByLabelText("Display name"), { target: { value: "Revised local" } })
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("storage unavailable")
    expect(screen.getByLabelText("Display name")).toHaveValue("Revised local")
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }))
    await screen.findByText("Revised local")
  })

  it("assigns a new id beside an existing connection and keeps the typed name", async () => {
    const existing = { ...saved, id: "srv-kept" }
    vi.mocked(api.mcpServers).mockResolvedValue([existing])
    render(<MCPTab />)
    fireEvent.click(await screen.findByRole("button", { name: "Add server" }))
    fireEvent.change(screen.getByLabelText("Display name"), { target: { value: "remote.example" } })
    fireEvent.change(screen.getByLabelText("Command"), { target: { value: "other-runner" } })
    expect(screen.getByRole("button", { name: "Edit Local tools" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Remove Local tools" })).toBeDisabled()
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }))
    await screen.findByText("remote.example")
    expect(screen.getByText("Local tools")).toBeVisible()
    const [input, createOnly] = vi.mocked(api.saveMCPServer).mock.calls[0]
    expect(createOnly).toBe(true)
    expect(input.name).toBe("remote.example")
    expect(input.id).toMatch(/^[a-f0-9]{32}$/)
    expect(input.id).not.toBe(existing.id)
    expect(document.body).not.toHaveTextContent(input.id)
    expect(document.body).not.toHaveTextContent(existing.id)
  })

  it("hides an internal id from the list and from settings search", async () => {
    vi.mocked(api.mcpServers).mockResolvedValue([{ ...saved, id: "srv-hidden", name: "Desk" }])
    const view = render(<MCPTab />)
    expect(await screen.findByText("Desk")).toBeVisible()
    expect(screen.getByText("Local command (stdio)")).toBeVisible()
    expect(document.body).not.toHaveTextContent("srv-hidden")
    view.rerender(<MCPTab query="srv-hidden" />)
    expect(screen.queryByText("Desk")).toBeNull()
    view.rerender(<MCPTab query="stdio" />)
    expect(screen.getByText("Desk")).toBeVisible()
  })

  it("retries a new connection with the same id and does not rewrite the name", async () => {
    vi.mocked(api.saveMCPServer).mockRejectedValueOnce(new Error("storage unavailable"))
    render(<MCPTab />)
    await screen.findByText(/No MCP servers yet/)
    fireEvent.click(screen.getByRole("button", { name: "Add server" }))
    fireEvent.change(screen.getByLabelText("Display name"), { target: { value: "remote.example" } })
    fireEvent.change(screen.getByLabelText("Command"), { target: { value: "runner" } })
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("storage unavailable")
    expect(screen.getByLabelText("Display name")).toHaveValue("remote.example")
    const first = vi.mocked(api.saveMCPServer).mock.calls[0][0].id
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }))
    await screen.findByText("remote.example")
    expect(vi.mocked(api.saveMCPServer).mock.calls[1][0]).toMatchObject({ id: first, name: "remote.example" })
  })

  it("tests even disabled connections and shows discovered descriptions and input schemas", async () => {
    vi.mocked(api.mcpServers).mockResolvedValue([{ ...saved, enabled: false }])
    vi.mocked(api.inspectMCPServer).mockResolvedValue({
      server_name: "Fixture server", server_version: "1",
      tools: [{ name: "lookup", description: "Read a record", input_schema: { type: "object", properties: { id: { type: "string" } } } }],
    })
    render(<MCPTab />)
    fireEvent.click(await screen.findByRole("button", { name: "Test Local tools and list tools" }))
    await screen.findByText("Connected · 1 tools")
    const catalog = screen.getByRole("region", { name: "Local tools Tool catalog" })
    fireEvent.click(within(catalog).getByText("lookup"))
    expect(within(catalog).getByText("Read a record")).toBeVisible()
    const schema = catalog.querySelector("pre")
    expect(schema).toHaveTextContent('"properties"')
    expect(schema).toHaveTextContent('"id"')
    expect(document.querySelector("[data-mcp-status='off']")).toHaveClass("text-muted-foreground")
    expect(document.querySelector("[data-mcp-status='connected']")).toHaveClass("text-done")
    await waitFor(() => expect(schema?.querySelector("[style*='--shiki-light']")).toBeTruthy(), { timeout: 5000 })
    expect(api.inspectMCPServer).toHaveBeenCalledWith("local", expect.any(AbortSignal))
  })

  it("retains a connection error and lets the user retest to an empty catalog", async () => {
    vi.mocked(api.mcpServers).mockResolvedValue([saved])
    vi.mocked(api.inspectMCPServer).mockRejectedValueOnce(new Error("connection refused"))
    render(<MCPTab />)
    const inspect = await screen.findByRole("button", { name: "Test Local tools and list tools" })
    fireEvent.click(inspect)
    expect(await screen.findByRole("alert")).toHaveTextContent("connection refused")
    expect(document.querySelector("[data-mcp-status='failed']")).toHaveClass("text-destructive")
    await waitFor(() => expect(inspect).toBeEnabled())
    fireEvent.click(inspect)
    await screen.findByText("This server has no tools.")
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("requires confirmation before deleting and reloads after a list failure", async () => {
    vi.mocked(api.mcpServers).mockRejectedValueOnce(new Error("read unavailable")).mockResolvedValue([saved])
    render(<MCPTab />)
    fireEvent.click(await screen.findByRole("button", { name: "Retry" }))
    fireEvent.click(await screen.findByRole("button", { name: "Remove Local tools" }))
    expect(api.deleteMCPServer).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(api.deleteMCPServer).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Remove Local tools" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove server" }))
    await screen.findByText(/No MCP servers yet/)
    expect(api.deleteMCPServer).toHaveBeenCalledWith("local")
  })

  it("cancels discovery on leaving the page and ignores a late result", async () => {
    vi.mocked(api.mcpServers).mockResolvedValue([saved])
    let finish!: (catalog: MCPToolCatalog) => void
    vi.mocked(api.inspectMCPServer).mockReturnValue(new Promise((resolve) => { finish = resolve }))
    const view = render(<MCPTab />)
    fireEvent.click(await screen.findByRole("button", { name: "Test Local tools and list tools" }))
    expect(document.querySelector("[data-mcp-status='checking']")).toHaveClass("text-running")
    const signal = vi.mocked(api.inspectMCPServer).mock.calls[0][1]!
    expect(signal.aborted).toBe(false)
    view.unmount()
    expect(signal.aborted).toBe(true)
    await act(async () => finish({ tools: [{ name: "late" }] }))
    expect(screen.queryByText("late")).toBeNull()
  })

  it("applies JSON text without showing saved credentials", async () => {
    vi.mocked(api.mcpServers).mockResolvedValue([saved])
    vi.mocked(api.importMCPServers).mockResolvedValue([{ ...saved, name: "Renamed", command: "next" }])
    render(<MCPTab />)
    await screen.findByText("Local tools")
    fireEvent.click(screen.getByRole("button", { name: "Edit JSON" }))
    const text = screen.getByLabelText<HTMLTextAreaElement>("MCP JSON")
    expect(text.value).toContain('"command": "runner"')
    expect(text.value).not.toContain("TOKEN")
    await waitFor(() => expect(text.parentElement?.querySelector("pre [style*='--shiki-light']")).toBeTruthy())
    expect(screen.queryByRole("button", { name: "Add server" })).toBeNull()
    expect(screen.queryByText("Local tools")).toBeNull()
    const content = '{"mcpServers":{"local":{"command":"next","args":["--serve"]}}}'
    fireEvent.change(text, { target: { value: content } })
    fireEvent.click(screen.getByRole("button", { name: "Apply JSON" }))
    await screen.findByText("Renamed")
    expect(api.importMCPServers).toHaveBeenCalledWith(content)
    expect(screen.queryByLabelText("MCP JSON")).toBeNull()
    expect(document.body).not.toHaveTextContent("TOKEN")
  })

  it("keeps the JSON text when apply fails", async () => {
    vi.mocked(api.mcpServers).mockResolvedValue([saved])
    vi.mocked(api.importMCPServers).mockRejectedValueOnce(new Error("mcp: invalid JSON"))
    render(<MCPTab />)
    await screen.findByText("Local tools")
    fireEvent.click(screen.getByRole("button", { name: "Edit JSON" }))
    fireEvent.click(screen.getByRole("button", { name: "Apply JSON" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("mcp: invalid JSON")
    expect(screen.getByLabelText("MCP JSON")).toBeVisible()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(screen.queryByLabelText("MCP JSON")).toBeNull()
    expect(api.importMCPServers).toHaveBeenCalledOnce()
  })

  it("saves the direct-load percent without changing connections", async () => {
    render(<MCPTab />)
    const input = await screen.findByLabelText("Direct load (%)")
    expect(input).toHaveValue(5)
    fireEvent.change(input, { target: { value: "0" } })
    fireEvent.blur(input)
    await waitFor(() => expect(api.saveMCPPolicy).toHaveBeenCalledWith(0))
    expect(api.saveMCPServer).not.toHaveBeenCalled()
  })
})
