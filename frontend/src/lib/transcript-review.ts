import type { ReviewOutcome, SwarmEvent } from "./types"

/** Read a review's outcome out of an event. A malformed one is dropped: the
 *  review is an extra, and half a summary is worse than none. */
export function parseReview(ev: SwarmEvent): ReviewOutcome | undefined {
  let raw: unknown
  try {
    raw = JSON.parse(ev.text ?? "")
  } catch {
    return undefined
  }
  if (!raw || typeof raw !== "object") return undefined
  const body = raw as Partial<ReviewOutcome>
  return {
    changed: body.changed === true,
    notes: typeof body.notes === "object" && body.notes ? body.notes : undefined,
    skills: Array.isArray(body.skills) ? body.skills : undefined,
    changes: Array.isArray(body.changes) ? body.changes : undefined,
    note: typeof body.note === "string" ? body.note : undefined,
    err: typeof body.err === "string" && body.err ? body.err : ev.err || undefined,
    notify: notifyOf(body.notify),
  }
}

function notifyOf(raw: unknown): ReviewOutcome["notify"] {
  return raw === "off" || raw === "on" || raw === "verbose" ? raw : undefined
}

/** What the review's tool actions are called in the transcript. */
const NOTE_VERBS: Record<string, string> = {
  add: "stored",
  replace: "revised",
  remove: "removed",
}
const SKILL_VERBS: Record<string, string> = {
  create: "recorded",
  patch: "updated",
  delete: "removed",
}

/** What the Memory panel says after someone asked for a review.
 *
 *  Auto-review stays quiet when it kept nothing — a line after every answer
 *  would train people to ignore the ones that matter. A click is a request
 *  for an answer, so the panel always says what happened. */
export function reviewPanelHint(outcome?: ReviewOutcome): string {
  if (outcome?.err) return `Memory review failed: ${outcome.err}`
  if (!outcome?.changed) return "Review finished — nothing new to keep."
  return "Review finished."
}

export function reviewNotice(outcome?: ReviewOutcome): string | undefined {
  if (!outcome) return undefined
  if (outcome.err) return `Memory review failed: ${outcome.err}`
  if (!outcome.changed) return undefined
  const parts: string[] = []
  for (const action of Object.keys(NOTE_VERBS)) {
    const n = outcome.notes?.[action] ?? 0
    if (n > 0) parts.push(`${n} ${n === 1 ? "note" : "notes"} ${NOTE_VERBS[action]}`)
  }
  for (const s of outcome.skills ?? []) {
    if (!s?.name) continue
    parts.push(`skill "${s.name}" ${SKILL_VERBS[s.action] ?? s.action}`)
  }
  const head =
    parts.length > 0 ? `Memory updated: ${parts.join(", ")}.` : "Memory updated."
  if (outcome.notify !== "verbose") return head
  const preview = (outcome.changes ?? [])
    .map(changePreview)
    .filter(Boolean)
    .join("\n")
  return preview ? `${head}\n${preview}` : head
}

function changePreview(c: { action?: string; name?: string; text?: string; target?: string }): string {
  const mark =
    c.action === "add" || c.action === "create"
      ? "+"
      : c.action === "remove" || c.action === "delete"
        ? "−"
        : "~"
  const label = c.name ? `skill ${c.name}` : "note"
  const text = (c.text ?? "").trim()
  return text ? `${mark} ${label}: ${text}` : `${mark} ${label}`
}
