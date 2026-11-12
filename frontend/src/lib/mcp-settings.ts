export type MCPTransport = "stdio" | "streamable_http" | "sse"

export interface MCPServerView {
  id: string
  name: string
  transport: MCPTransport
  command: string
  args: string[]
  url: string
  enabled: boolean
  timeout_seconds: number
  env_keys: string[]
  header_keys: string[]
}

export interface MCPServerInput extends Omit<MCPServerView, "env_keys" | "header_keys"> {
  env?: Record<string, string>
  headers?: Record<string, string>
}

export interface MCPToolCatalog {
  server_name?: string
  server_version?: string
  tools: Array<{ name: string; original_name?: string; description?: string; input_schema?: unknown }>
}

export interface MCPDraft extends Omit<MCPServerInput, "args" | "env" | "headers"> {
  args: string
  env: string
  headers: string
}

export type MCPDraftField = "args" | "env" | "headers"

export class MCPDraftError extends Error {
  constructor(readonly field: MCPDraftField) {
    super(field)
  }
}

const mcpServerIDPattern = /^[a-zA-Z0-9_-]{1,32}$/

function randomMCPIDBytes(): Uint8Array {
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  return bytes
}

// Settings assigns the id. A retry keeps the id already stored on the draft;
// anything else, including a display name, is not an id.
export function assignMCPServerID(
  current: string,
  used: Iterable<string>,
  nextBytes: () => Uint8Array = randomMCPIDBytes,
): string {
  const taken = new Set(used)
  if (mcpServerIDPattern.test(current) && !taken.has(current)) return current
  for (let attempt = 0; attempt < 8; attempt++) {
    const id = [...nextBytes()].map((byte) => byte.toString(16).padStart(2, "0")).join("")
    if (mcpServerIDPattern.test(id) && !taken.has(id)) return id
  }
  throw new Error("mcp id allocation failed")
}

function quoteMCPCommandToken(command: string): string {
  if (!/[\s"'\\]/.test(command)) return command
  return `"${command.replace(/[\\"]/g, (char) => `\\${char}`)}"`
}

// The text uses the same mcpServers object other clients edit directly.
// Credentials stay out of it. A program path with spaces and no arguments is
// quoted so applying the text does not split the path.
export function mcpServersDocument(servers: MCPServerView[]): string {
  const mcpServers: Record<string, Record<string, unknown>> = {}
  for (const server of servers) {
    const entry: Record<string, unknown> = {}
    if (server.name !== server.id) entry.name = server.name
    if (server.transport === "stdio") {
      entry.type = "stdio"
      entry.command = server.args.length === 0 ? quoteMCPCommandToken(server.command) : server.command
      entry.args = server.args
    } else {
      entry.type = server.transport === "sse" ? "sse" : "http"
      entry.url = server.url
    }
    entry.enabled = server.enabled
    if (server.timeout_seconds) entry.timeout_seconds = server.timeout_seconds
    mcpServers[server.id] = entry
  }
  return JSON.stringify({ mcpServers }, null, 2)
}

export function mcpTransportKey(transport: MCPTransport): "mcp.stdio" | "mcp.http" | "mcp.sse" {
  if (transport === "streamable_http") return "mcp.http"
  if (transport === "sse") return "mcp.sse"
  return "mcp.stdio"
}

export function createMCPDraft(server?: MCPServerView): MCPDraft {
  return {
    id: server?.id ?? "", name: server?.name ?? "", enabled: server?.enabled ?? true,
    transport: server?.transport ?? "stdio", command: server?.command ?? "",
    args: JSON.stringify(server?.args ?? []), url: server?.url ?? "",
    timeout_seconds: server?.timeout_seconds ?? 0, env: "", headers: "",
  }
}

function readSecrets(raw: string, field: "env" | "headers"): Record<string, string> | undefined {
  if (!raw.trim()) return undefined
  try {
    const value: unknown = JSON.parse(raw)
    if (!value || typeof value !== "object" || Array.isArray(value) ||
      Object.values(value).some((entry) => typeof entry !== "string")) throw new Error()
    return value as Record<string, string>
  } catch { throw new MCPDraftError(field) }
}

export function mcpDraftInput(draft: MCPDraft): MCPServerInput {
  let args: unknown
  try {
    args = JSON.parse(draft.args)
    if (!Array.isArray(args) || args.some((arg) => typeof arg !== "string")) throw new Error()
  } catch { throw new MCPDraftError("args") }
  const env = readSecrets(draft.env, "env")
  const headers = readSecrets(draft.headers, "headers")
  return {
    id: draft.id, name: draft.name, enabled: draft.enabled, transport: draft.transport,
    command: draft.command, args: args as string[], url: draft.url,
    timeout_seconds: draft.timeout_seconds,
    ...(env === undefined ? {} : { env }), ...(headers === undefined ? {} : { headers }),
  }
}
