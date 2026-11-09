import { gatewayFetch } from "./gateway-fetch"
import { notifyGatewayRefusal } from "./gateway-authority"
import { normalizeGatewayOrigin, requireCurrentGatewaySession, type GatewaySession } from "./gateway-account"
import { ModelCallError, type ApiStyle } from "./openai-wire"
import { approvedWebSession, loadWebExecution, sameWebAuthority } from "./web-execution"

export type CloudToolCall = { id: string; name: string; arguments: string }
export type CloudToolEvent = { id: string; callId?: string; responseId?: string; name: string; status: "running" | "completed" | "failed" | "cancelled"; args: string; text: string; truncated?: boolean }
export type CloudWebTools = { session: GatewaySession; mode: "auto" | "cloud"; fetcher: typeof fetch }
const maxArgumentsBytes = 64 << 10
const maxResponseBytes = 1 << 20
const maxFailureBytes = 4 << 10
export const maxCloudToolOutputBytes = 128 << 10

export class CloudWebToolFailure extends ModelCallError {
  constructor(readonly code: string, readonly status: number, readonly retryable: boolean) {
    super("cloud_tools_unavailable", "http")
  }
}

// Stable standalone Gateway failures that represent unavailable Web evidence.
// Authority, admission, safety and journal errors are outside this set.
const fetchFailures = new Map<string, readonly number[]>([
  ["upstream_not_found", [404, 410]],
  ["response_too_large", [413]],
  ["browser_fetch_failed", [502]],
  ["fetch_failed", [502]],
  ["proxy_unavailable", [502]],
  ["browser_unavailable", [503]],
  ["fetch_retry_cooldown", [503]],
  ["proxy_pool_unavailable", [503]],
  ["fetch_timeout", [504]],
])
const fetchFailureStatuses = new Set([...fetchFailures.values()].flat())

const definitions = [
  { name: "web_search", description: "Search the web for current information.", parameters: { type: "object", properties: { query: { type: "string" } }, required: ["query"], additionalProperties: false } },
  { name: "web_fetch", description: "Read a public web page as Markdown. Request browser rendering when needed.", parameters: { type: "object", properties: { url: { type: "string" }, render: { type: "boolean" } }, required: ["url"], additionalProperties: false } },
]

export function cloudToolDefinitions(style: ApiStyle): Record<string, unknown>[] {
  return definitions.map((definition) => style === "chat" ? { type: "function", function: definition } : { type: "function", ...definition })
}

export function currentCloudWebTools(fetcher?: typeof fetch): CloudWebTools | null {
  const session = approvedWebSession()
  const mode = loadWebExecution()
  if (!session && mode === "cloud") throw new ModelCallError("gateway_login_required", "http")
  return !session || mode === "off" ? null : { session, mode, fetcher: fetcher ?? gatewayFetch }
}

export function assertCloudWebToolsCurrent(tools: CloudWebTools) {
  if (!sameWebAuthority(tools.session) || loadWebExecution(tools.session) !== tools.mode) throw new ModelCallError("abort", "abort")
  requireCurrentGatewaySession(tools.session)
}

export function validateCloudWebTool(call: CloudToolCall) { toolRequest(call) }

export function cloudToolTraceText(name: string, output: string): string {
  if (name !== "web_search") return output
  try {
    const value = record(JSON.parse(output))
    if (!Array.isArray(value?.results)) return output
    return value.results.map((item: unknown) => {
      const row = record(item)
      return [row?.title, row?.url, row?.summary].filter((part) => typeof part === "string" && part).join("\n")
    }).join("\n\n")
  } catch { return output }
}

export async function executeCloudWebTool(tools: CloudWebTools, call: CloudToolCall, signal: AbortSignal, timeoutMs: number): Promise<string> {
  const { path, body, retrySafe } = toolRequest(call)
  if (signal.aborted) throw new ModelCallError("abort", "abort")
  const controller = new AbortController()
  const abort = () => controller.abort(signal.reason ?? "stop")
  signal.addEventListener("abort", abort, { once: true })
  const deadline = performance.now() + timeoutMs
  const timer = setTimeout(() => controller.abort("timeout"), timeoutMs)
  try {
    try {
      return await executeCloudWebToolAttempt(tools, call, path, body, controller.signal)
    } catch (firstError) {
      if (!retrySafe || !(firstError instanceof CloudWebToolFailure && firstError.retryable) ||
          controller.signal.aborted || performance.now() >= deadline) throw firstError
      try {
        return await executeCloudWebToolAttempt(tools, call, path, body, controller.signal)
      } catch (retryError) {
        // A retry that used the last of the tool budget leaves the first
        // verified Gateway failure as the result of this tool call.
        if (retryError instanceof ModelCallError && retryError.kind === "timeout" &&
            controller.signal.aborted && controller.signal.reason === "timeout" && !signal.aborted) {
          assertCloudWebToolsCurrent(tools)
          throw firstError
        }
        throw retryError
      }
    }
  } finally {
    clearTimeout(timer)
    signal.removeEventListener("abort", abort)
    if (!controller.signal.aborted) controller.abort("complete")
  }
}

async function executeCloudWebToolAttempt(tools: CloudWebTools, call: CloudToolCall, path: string, body: unknown, signal: AbortSignal): Promise<string> {
  assertCloudWebToolsCurrent(tools)
  assertToolRequestActive(signal)
  let response: Response | undefined
  try {
    response = await tools.fetcher(normalizeGatewayOrigin(tools.session.origin) + path, {
      method: "POST", headers: { "Content-Type": "application/json", Accept: "application/json", Authorization: "Bearer " + tools.session.token },
      body: JSON.stringify(body), signal, redirect: "error", credentials: "omit", cache: "no-store",
    })
    assertToolRequestActive(signal)
    if (response.redirected || response.url && new URL(response.url).origin !== tools.session.origin) throw new ModelCallError("cloud_tools_unavailable", "http")
    notifyGatewayRefusal(tools.session, response.status)
    if (!response.ok) throw await cloudWebToolError(tools, call, response, signal)
    assertCloudWebToolsCurrent(tools)
    const raw = await boundedText(response, signal, maxResponseBytes)
    if (raw.includes(tools.session.token)) throw new ModelCallError("cloud_tools_invalid", "parse")
    const value: unknown = JSON.parse(raw)
    assertCloudWebToolsCurrent(tools)
    const result = record(value)
    const data = record(result?.data)
    if (result?.success !== true || !data) throw new ModelCallError("cloud_tools_unavailable", "http")
    let output: string
    if (call.name === "web_fetch") {
      if (typeof data.markdown !== "string") throw new ModelCallError("cloud_tools_invalid", "parse")
      output = data.markdown
    } else {
      if (!Array.isArray(data.results) || data.results.length > 20) throw new ModelCallError("cloud_tools_invalid", "parse")
      output = JSON.stringify({ message: "Cloud web search", results: data.results.map((item: unknown) => {
        const row = record(item)
        if (!row || typeof row.url !== "string" || typeof row.title !== "string" || (row.snippet != null && typeof row.snippet !== "string")) throw new ModelCallError("cloud_tools_invalid", "parse")
        return { title: row.title, url: row.url, summary: row.snippet ?? "" }
      }) })
    }
    if (new TextEncoder().encode(output).length > maxCloudToolOutputBytes) throw new ModelCallError("cloud_tools_output_limit", "parse")
    return output
  } catch (error) {
    if (error instanceof ModelCallError) throw error
    assertToolRequestActive(signal)
    throw new ModelCallError("cloud_tools_unavailable", "http")
  } finally {
    await response?.body?.cancel().catch(() => undefined)
  }
}

async function cloudWebToolError(tools: CloudWebTools, call: CloudToolCall, response: Response, signal: AbortSignal): Promise<ModelCallError> {
  const { status } = response
  if (status === 401) return new ModelCallError("cloud_tools_login", "http")
  if (status === 403) return new ModelCallError("cloud_tools_denied", "http")
  const isFetch = call.name === "web_fetch"
  const contentType = response.headers.get("Content-Type")?.split(";", 1)[0].trim().toLowerCase()
  if ((isFetch ? fetchFailureStatuses.has(status) : status === 502) && contentType === "application/json") {
    const raw = await boundedText(response, signal, maxFailureBytes)
    assertCloudWebToolsCurrent(tools)
    if (raw.includes(tools.session.token)) return new ModelCallError("cloud_tools_invalid", "parse")
    const value = record(JSON.parse(raw))
    const failure = record(value?.error)
    const code = failure?.code
    if (value?.success === false && !("data" in value) && typeof failure?.message === "string" && typeof code === "string" &&
        (isFetch ? fetchFailures.get(code)?.includes(status) : code === "web_search_failed")) {
      return new CloudWebToolFailure(code, status, code === "fetch_timeout" || code === "proxy_unavailable")
    }
  }
  return new ModelCallError("cloud_tools_unavailable", "http")
}

function toolRequest(call: CloudToolCall): { path: string; body: unknown; retrySafe: boolean } {
  if (new TextEncoder().encode(call.arguments).length > maxArgumentsBytes) throw new ModelCallError("cloud_tools_arguments_limit", "parse")
  let parsed: unknown
  try { parsed = JSON.parse(call.arguments) } catch { throw new ModelCallError("cloud_tools_arguments", "parse") }
  const args = record(parsed)
  if (!args) throw new ModelCallError("cloud_tools_arguments", "parse")
  if (call.name === "web_search") {
    if (Object.keys(args).some((key) => key !== "query") || typeof args.query !== "string" || !args.query.trim() || new TextEncoder().encode(args.query).length > 4096) throw new ModelCallError("cloud_tools_arguments", "parse")
    return { path: "/v1/web/search", body: { query: args.query }, retrySafe: false }
  }
  if (call.name !== "web_fetch" || Object.keys(args).some((key) => key !== "url" && key !== "render") || typeof args.url !== "string" || (args.render != null && typeof args.render !== "boolean")) throw new ModelCallError("cloud_tools_arguments", "parse")
  let url: URL
  try { url = new URL(args.url) } catch { throw new ModelCallError("cloud_tools_arguments", "parse") }
  if (!["https:", "http:"].includes(url.protocol) || url.username || url.password || args.url.length > 8192) throw new ModelCallError("cloud_tools_arguments", "parse")
  return { path: "/v1/web/fetch", body: { url: args.url, formats: ["markdown"], ...(args.render ? { actions: [{ type: "wait", milliseconds: 1 }] } : {}) }, retrySafe: !args.render }
}

function assertToolRequestActive(signal: AbortSignal) {
  if (!signal.aborted) return
  const kind = signal.reason === "timeout" ? "timeout" : "abort"
  throw new ModelCallError(kind, kind)
}

async function boundedText(response: Response, signal: AbortSignal, maxBytes: number): Promise<string> {
  if (Number(response.headers.get("Content-Length")) > maxBytes) throw new ModelCallError("cloud_tools_output_limit", "parse")
  if (!response.body) throw new ModelCallError("cloud_tools_invalid", "parse")
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let total = 0
  let text = ""
  const abort = () => { void reader.cancel().catch(() => undefined) }
  signal.addEventListener("abort", abort, { once: true })
  try {
    for (;;) {
      assertToolRequestActive(signal)
      const next = await reader.read()
      assertToolRequestActive(signal)
      if (next.done) break
      total += next.value.byteLength
      if (total > maxBytes) throw new ModelCallError("cloud_tools_output_limit", "parse")
      text += decoder.decode(next.value, { stream: true })
    }
    return text + decoder.decode()
  } finally {
    signal.removeEventListener("abort", abort)
    await reader.cancel().catch(() => undefined)
    reader.releaseLock()
  }
}

function record(value: unknown): Record<string, unknown> | null {
  return value != null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null
}
