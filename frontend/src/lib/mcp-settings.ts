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
