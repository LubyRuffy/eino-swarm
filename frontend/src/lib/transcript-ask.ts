import type { SwarmEvent, ThreadStatus } from "./types"
import type { AgentState, TranscriptState } from "./transcript"
import { append, block } from "./transcript-blocks"

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
  recoveredSeq?: number
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
    const oid = hostAskIdent(typeof opt.id === "string" ? opt.id : "") || hostAskIdent(label)
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

/** Only the engine's durable, checkpoint-verified receipt can reopen a human wait. */
export function askRecoveryCard(ev: SwarmEvent): AskCard | null {
  if (ev.kind !== "ask_recovered" || ev.agent_id !== "manager" || !ev.thread_id ||
      !ev.turn_id || !ev.tool_call_id || !Number.isSafeInteger(ev.seq) || ev.seq <= 0) return null
  const questions = parseAskToolArgs(ev.text ?? "")
  return questions ? { callId: ev.tool_call_id, questions, pending: true, recoveredSeq: ev.seq } : null
}

export function applyAskRecovery(agent: AgentState, ev: SwarmEvent, card: AskCard) {
  const existing = agent.blocks.find((b) => b.turnId === ev.turn_id && b.question?.callId === card.callId)
  if (!agent.blocks.some((b) => b.askRecovery?.thread_id === ev.thread_id && b.askRecovery.seq === ev.seq)) {
    append(agent, { ...block(ev, "notice", "ask_recovered"), quiet: true, askRecovery: ev })
  }
  // Accepted answers win even when a delayed recovery or old unknown arrives later.
  if (existing?.question) {
    if (completeAskAnswers(existing.question.questions, existing.question.answers) || (existing.question.recoveredSeq ?? 0) >= ev.seq) return
    agent.blocks = agent.blocks.map((b) => b === existing
      ? { ...b, question: { ...b.question!, pending: true, recoveredSeq: ev.seq } } : b)
  } else append(agent, { ...block(ev, "question", ASK_TOOL), question: card })
}

export function settleAskCard(card: AskCard, ev: SwarmEvent): AskCard {
  if (completeAskAnswers(card.questions, card.answers)) return card
  const answers = confirmedAskAnswers(card.questions, ev.text ?? "", ev.err)
  if (answers) return { ...card, pending: false, answers }
  // Stored results preceding this recovery may arrive after it over live catch-up.
  if (ev.seq > 0 && ev.seq < (card.recoveredSeq ?? 0)) return card
  return { ...card, pending: false, answers: undefined }
}

export function askStatusFromTranscript(state: TranscriptState, status: ThreadStatus): ThreadStatus {
  if (!status.running) return status
  const questions = state.agents.manager?.blocks.filter((b) => b.question && (!status.turn_id || b.turnId === status.turn_id)) ?? []
  return questions.length ? { ...status, awaiting_answer: questions.some((b) => b.question?.pending) } : status
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
