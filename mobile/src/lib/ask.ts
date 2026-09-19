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

export type AskAnswers = Record<string, { answers: string[] }>

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
