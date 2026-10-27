import { afterEach, expect, it, vi } from "vitest"
import { emptyTranscript } from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"
import { dropQueued, queueEvent, type StreamSnapshot } from "./app-stream"
import { resetThreadHistory } from "./thread-history"

vi.mock("@/lib/api", () => ({ api: { turns: async () => [] } }))
afterEach(() => { dropQueued(); resetThreadHistory() })

it("only clears the live collection pause for its current turn and wait identity", () => {
  let state: StreamSnapshot = {
    activeId: "thread", transcript: emptyTranscript(), status: { running: false }, threads: [], followups: [], schedules: [],
    refreshFiles: async () => {}, refreshThreads: async () => {}, refreshFollowups: async () => {}, refreshSchedules: async () => {},
  }
  const emit = (seq: number, kind: string, text: string, turn = "turn") => {
    const event: SwarmEvent = { thread_id: "thread", turn_id: turn, seq, agent_id: "manager", kind, text, created_at: "2026-01-01T00:00:00Z" }
    queueEvent((patch) => { state = { ...state, ...typeof patch === "function" ? patch(state) : patch } }, () => state, "thread", event)
  }
  emit(1, "user_message", "Inspect the inputs.")
  emit(2, "max_iterations", '{"reason":"workers_uncollected","wait_id":"current","automatic_resume":true}')
  for (const [seq, body, turn] of [[3, '{"wait_id":"old"}', "turn"], [4, '{}', "turn"], [5, '{"wait_id":"current"}', "other-turn"]] as const) {
    emit(seq, "workers_collection_resumed", body, turn)
    expect(state.status.awaiting_continue).toBe(true)
    expect(state.transcript.agents.manager.blocks.find((block) => block.kind === "confirm")?.confirm?.pending).toBe(true)
  }
  emit(6, "workers_collection_resumed", '{"wait_id":"current"}')
  expect(state.status.awaiting_continue).toBe(false)
  expect(state.transcript.agents.manager.blocks.find((block) => block.kind === "confirm")?.confirm).toMatchObject({ pending: false, continued: true })
  emit(6, "workers_collection_resumed", '{"wait_id":"current"}')
  expect(state.transcript.agents.manager.blocks.filter((block) => block.kind === "confirm")).toHaveLength(1)
})
