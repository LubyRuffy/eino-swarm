import type { RemoteEvent } from "./rpc"

export const ASK_TOOL = "ask_user"
export const ASK_OTHER_ID = "other"
export const ASK_OTHER_LABEL = "Other"

export type AskOption = {
  id: string
  label: string
  description?: string
}

export type AskQuestion = {
  id: string
  header?: string
  prompt: string
  options: AskOption[]
}

export function splitToolCall(raw: string): { name: string; args: string } {
  const s = raw.trim()
  const open = s.indexOf("(")
  if (open < 0) return { name: s, args: "" }
  let args = s.slice(open + 1)
  if (args.endsWith(")")) args = args.slice(0, -1)
  return { name: s.slice(0, open).trim(), args }
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
  const id = typeof row.id === "string" ? hostAskIdent(row.id) : ""
  const prompt = typeof row.prompt === "string" ? row.prompt.trim() : ""
  if (!id || !prompt || !Array.isArray(row.options)) return null
  const options: AskOption[] = []
  for (const o of row.options) {
    if (!o || typeof o !== "object") continue
    const opt = o as { id?: unknown; label?: unknown; description?: unknown }
    const label = typeof opt.label === "string" ? opt.label.trim() : ""
    const oid = (typeof opt.id === "string" ? hostAskIdent(opt.id) : "") || hostAskIdent(label)
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

// The host normalizes model-provided ids before validating answer keys.
function hostAskIdent(raw: string): string {
  return raw.trim().toLowerCase()
    .replace(/[-\p{White_Space}]/gu, "_")
    .replace(/[^\p{L}\p{N}_]/gu, "")
    .replace(/^_+|_+$/g, "")
}

export type AskAnswers = Record<string, { answers: string[] }>

export function parseAskResult(text: string): Record<string, string> {
  try {
    const body = JSON.parse(text) as { answers?: Record<string, { answers?: unknown }> }
    const out: Record<string, string> = {}
    for (const [id, row] of Object.entries(body.answers ?? {})) {
      const first = Array.isArray(row?.answers) ? row.answers[0] : undefined
      if (typeof first === "string" && first.trim()) out[id] = first.trim()
    }
    return out
  } catch { return {} }
}

export function parseAskRecovery(ev: RemoteEvent) {
  if (ev.kind !== "ask_recovered" || ev.agent_id !== "manager" || !ev.thread_id ||
      !ev.turn_id || !ev.tool_call_id || !Number.isSafeInteger(ev.seq) || ev.seq <= 0) return null
  const questions = parseAskToolArgs(ev.text)
  return questions ? { callId: ev.tool_call_id, turnId: ev.turn_id, questions, recoveredSeq: ev.seq } : null
}

export function structuredAnswers(
  questions: AskQuestion[],
  picks: Record<string, string>,
  other: Record<string, string>,
): AskAnswers | null {
  const out: AskAnswers = {}
  for (const q of questions) {
    const optId = picks[q.id]
    if (!optId) return null
    if (optId === ASK_OTHER_ID) {
      const text = (other[q.id] ?? "").trim()
      if (!text) return null
      out[q.id] = { answers: [text] }
      continue
    }
    const label = q.options.find((o) => o.id === optId)?.label
    if (!label) return null
    out[q.id] = { answers: [label] }
  }
  return out
}

export function completeAskAnswers(questions: AskQuestion[], answers?: Record<string, string>): boolean {
  return Boolean(questions.length && answers && Object.keys(answers).length === questions.length &&
    questions.every((q) => typeof answers[q.id] === "string" && answers[q.id].trim()))
}

export function confirmedAskAnswers(questions: AskQuestion[], text: string, err?: string): Record<string, string> | undefined {
  if (err) return undefined
  try {
    const body = JSON.parse(text) as { answers?: Record<string, unknown> }
    // FormatAskResult has one answers object. Error/unknown envelopes are not human receipts.
    if (!body || Object.keys(body).length !== 1 || !body.answers || Array.isArray(body.answers) ||
        Object.keys(body.answers).length !== questions.length) return undefined
    const answers = parseAskResult(text)
    return completeAskAnswers(questions, answers) ? answers : undefined
  } catch { return undefined }
}
