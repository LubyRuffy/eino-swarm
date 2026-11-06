import { t } from "./i18n"
import type { CompactBlock } from "./transcript"

export type IterationConfirmation = {
  limit: number
  extendBy?: number
  pending: boolean
  continued?: boolean
}

export function parseIterationConfirmation(text: string): IterationConfirmation | undefined {
  let raw: unknown
  try { raw = JSON.parse(text) } catch { return }
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return
  const body = raw as { reason?: unknown; limit?: unknown; extend_by?: unknown }
  if (body.reason !== undefined && body.reason !== "iterations") return
  if (typeof body.limit !== "number" || !Number.isFinite(body.limit) || body.limit < 0) return
  if (body.extend_by !== undefined && (typeof body.extend_by !== "number" || !Number.isFinite(body.extend_by) || body.extend_by <= 0)) return
  return { limit: body.limit, extendBy: body.extend_by as number | undefined, pending: true }
}

export function iterationConfirmationText(pause: IterationConfirmation): string {
  if (!pause.pending) return t(pause.continued ? "thread.limitContinued" : "thread.limitStopped")
  return pause.extendBy === undefined ? t("thread.limitAskLegacy", { limit: pause.limit })
    : t("thread.limitAsk", { limit: pause.limit, extendBy: pause.extendBy })
}

export function pendingIterationLimit(blocks: CompactBlock[], turnId?: string): CompactBlock | undefined {
  return [...blocks].reverse().find((b) => !b.agentId && b.confirm?.pending && (!turnId || !b.turnId || b.turnId === turnId))
}

export function settleIterationLimits(blocks: CompactBlock[], turnId?: string, continued = false): CompactBlock[] {
  return blocks.map((block) => {
    if (!block.confirm?.pending || turnId && block.turnId && block.turnId !== turnId) return block
    const confirm = { ...block.confirm, pending: false, continued }
    return { ...block, confirm, text: iterationConfirmationText(confirm) }
  })
}
