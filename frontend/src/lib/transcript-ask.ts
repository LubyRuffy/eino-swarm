import type { SwarmEvent } from "./types"

/** ask_user payload. Host injects Other; models must not send that id. */
export const ASK_TOOL = "ask_user"
export const ASK_OTHER_ID = "other"
export const ASK_OTHER_LABEL = "Other"

export interface AskOption {
  id: string
  label: string
  description?: string
}

export interface AskQuestion {
  id: string
  header?: string
  prompt: string
  options: AskOption[]
}

export interface AskCard {
  callId: string
  questions: AskQuestion[]
  pending: boolean
  answers?: Record<string, string>
}

export function parseAskToolArgs(args: string): AskQuestion[] | null {
  const raw = args.trim()
  if (!raw || raw === "{}") return null
  try {
    const body = JSON.parse(raw) as { questions?: unknown }
    if (!Array.isArray(body.questions) || body.questions.length === 0) return null
    const out: AskQuestion[] = []
    for (const row of body.questions) {
      const q = parseQuestion(row)
      if (!q) return null
      out.push(q)
    }
    return out.length ? out : null
  } catch {
    return null
  }
}

function parseQuestion(raw: unknown): AskQuestion | null {
  if (!raw || typeof raw !== "object") return null
  const row = raw as {
    id?: unknown
    header?: unknown
    prompt?: unknown
    options?: unknown
  }
  const id = typeof row.id === "string" ? row.id.trim() : ""
  const prompt = typeof row.prompt === "string" ? row.prompt.trim() : ""
  if (!id || !prompt || !Array.isArray(row.options)) return null
  const options: AskOption[] = []
  for (const o of row.options) {
    if (!o || typeof o !== "object") continue
    const opt = o as { id?: unknown; label?: unknown; description?: unknown }
    const oid = typeof opt.id === "string" ? opt.id.trim() : ""
    const label = typeof opt.label === "string" ? opt.label.trim() : ""
    if (!oid || !label || oid === ASK_OTHER_ID) continue
    options.push({
      id: oid,
      label,
      description: typeof opt.description === "string" ? opt.description : undefined,
    })
  }
  if (options.length < 2) return null
  options.push({ id: ASK_OTHER_ID, label: ASK_OTHER_LABEL })
  return {
    id,
    header: typeof row.header === "string" ? row.header.trim() : undefined,
    prompt,
    options,
  }
}

export function parseAskResult(text: string): Record<string, string> {
  try {
    const body = JSON.parse(text) as {
      answers?: Record<string, { answers?: unknown }>
      error?: unknown
    }
    const out: Record<string, string> = {}
    for (const [id, row] of Object.entries(body.answers ?? {})) {
      const first = Array.isArray(row?.answers) ? row.answers[0] : undefined
      if (typeof first === "string" && first.trim()) out[id] = first.trim()
    }
    return out
  } catch {
    return {}
  }
}

export function askCardFromEvent(
  ev: SwarmEvent,
  name: string,
  args: string,
): AskCard | null {
  if (name !== ASK_TOOL) return null
  const questions = parseAskToolArgs(args)
  if (!questions) return null
  return { callId: ev.tool_call_id ?? "", questions, pending: true }
}
