import type { Step } from "./mock-turn"
import type { ThreadDetail } from "./rpc"

export type MockGoalFields = {
  goal?: string
  goalComplete?: boolean
  goalCapped?: boolean
  goalBlocked?: boolean
  goalBlockReason?: string
  goalIdle?: boolean
}

/** The offline host keeps the same pause in its detail replies and events. */
export function mockGoalFixture(history: string | null): { fields: MockGoalFields; events: Step[] } | undefined {
  if (!["goal-complete", "goal-paused", "goal-blocked", "goal-idle"].includes(history ?? "")) return undefined
  const goal = history === "goal-complete" ? "Verify current work" : "Verify the required artifact."
  const fields: MockGoalFields = { goal }
  const events: Step[] = [{ kind: "goal", text: goal }]
  if (history === "goal-paused") {
    fields.goalCapped = true
    events.push({ kind: "goal_capped", text: '{"auto_turns":1,"cap":1}' })
  } else if (history === "goal-blocked") {
    fields.goalBlocked = true
    fields.goalBlockReason = "Required evidence is unavailable."
    events.push({ kind: "goal_blocked", text: JSON.stringify({ reason: fields.goalBlockReason }) })
  } else if (history === "goal-idle") {
    fields.goalIdle = true
    events.push({ kind: "goal_idle" })
  }
  return { fields, events }
}

export function mockGoalDetail(fields: MockGoalFields): Pick<ThreadDetail,
  "goal" | "goal_on" | "goal_complete" | "goal_capped" | "goal_blocked" | "goal_block_reason" | "goal_idle"
> {
  return {
    goal: fields.goal,
    goal_on: Boolean(fields.goal),
    goal_complete: fields.goalComplete,
    goal_capped: fields.goalCapped,
    ...(fields.goalBlocked === undefined ? {} : { goal_blocked: fields.goalBlocked }),
    ...(fields.goalBlockReason === undefined ? {} : { goal_block_reason: fields.goalBlockReason }),
    ...(fields.goalIdle === undefined ? {} : { goal_idle: fields.goalIdle }),
  }
}

export function resumeMockGoal(fields: MockGoalFields): void {
  fields.goalCapped = false
  fields.goalComplete = false
  if (fields.goalBlocked !== undefined) fields.goalBlocked = false
  if (fields.goalBlockReason !== undefined) fields.goalBlockReason = ""
  if (fields.goalIdle !== undefined) fields.goalIdle = false
}
