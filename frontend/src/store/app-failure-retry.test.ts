import { beforeEach, expect, it } from "vitest"

import { fake } from "./app-test-fixture"
import { emptyTranscript, reduceEvents } from "@/lib/transcript"
import type { Thread } from "@/lib/types"
import { useApp } from "@/store/app"

const thread: Thread = {
  id: "thread", title: "A conversation", project_id: "", provider_id: "", reasoning_effort: "",
  archived: false, created_at: "", last_active_at: "", running: false,
}

function failed() {
  return reduceEvents(emptyTranscript(), [
    { thread_id: "thread", turn_id: "turn", agent_id: "manager", seq: 2, kind: "user_message", text: "Inspect the assigned work.", created_at: "2026-01-01T00:00:01Z" },
    { thread_id: "thread", turn_id: "turn", agent_id: "manager", seq: 3, kind: "tool_call", tool_call_id: "call-1", text: "exec({\"command\":\"printf ok\"})", created_at: "2026-01-01T00:00:02Z" },
    { thread_id: "thread", turn_id: "turn", agent_id: "manager", seq: 4, kind: "tool_result", tool_call_id: "call-1", text: "committed receipt", created_at: "2026-01-01T00:00:03Z" },
    { thread_id: "thread", turn_id: "turn", agent_id: "manager", seq: 5, kind: "error", err: "The service refused the request.", created_at: "2026-01-01T00:00:04Z" },
  ])
}

beforeEach(() => {
  fake.retries.length = 0
  fake.started.length = 0
  fake.retryImpl = undefined
  useApp.setState({
    activeId: "thread",
    loaded: true,
    threads: [thread],
    turns: [{ id: "turn", thread_id: "thread", seq: 1, status: "error", user_text: "Inspect the assigned work.", final: "", provider_id: "", model: "", started_at: "2026-01-01T00:00:00Z", duration_ms: 4000 }],
    transcript: failed(),
    status: { running: false },
    error: undefined,
    meta: undefined,
  })
})

it("continues the failed turn after its acknowledgement without erasing a live continuation", async () => {
  fake.retryImpl = async () => {
    useApp.setState((state) => ({
      transcript: reduceEvents(state.transcript, [{
        thread_id: "thread", turn_id: "turn", agent_id: "manager", seq: 6, kind: "failure_continued",
        text: "Continuing from the failed step.", created_at: "2026-01-01T00:00:05Z",
      }]),
      status: { running: true, turn_id: "turn", started_at: "2026-01-01T00:00:01Z" },
    }))
  }
  expect(await useApp.getState().continueFailed("turn")).toBe(true)
  const state = useApp.getState()
  const blocks = state.transcript.agents.manager.blocks
  expect(fake.retries).toEqual([{ id: "thread", turnId: "turn" }])
  expect(fake.started).toEqual([])
  expect(blocks.filter((block) => block.kind === "notice")).toHaveLength(1)
  expect(blocks.some((block) => block.kind === "error")).toBe(false)
  expect(blocks.some((block) => block.kind === "tool" && block.tool?.result === "committed receipt")).toBe(true)
  expect(state.status.started_at).toBe("2026-01-01T00:00:01Z")
  expect(state.threads.find((row) => row.id === "thread")?.running).toBe(true)
})

it("keeps the error card when continuation is refused", async () => {
  fake.retryImpl = async () => { throw new Error("unavailable") }
  expect(await useApp.getState().continueFailed("turn")).toBe(false)
  const blocks = useApp.getState().transcript.agents.manager.blocks
  expect(blocks.some((block) => block.kind === "error")).toBe(true)
  expect(blocks.some((block) => block.kind === "tool" && block.tool?.result === "committed receipt")).toBe(true)
  expect(useApp.getState().error).toBe("unavailable")
  expect(useApp.getState().status.running).toBe(false)
  expect(fake.started).toEqual([])
})

it("does not mark the conversation now on screen when the acknowledgement is late", async () => {
  let release!: () => void
  fake.retryImpl = () => new Promise((resolve) => { release = resolve })
  const pending = useApp.getState().continueFailed("turn")
  useApp.setState({ activeId: "other", status: { running: false } })
  release()
  expect(await pending).toBe(true)
  expect(useApp.getState().status.running).toBe(false)
  expect(useApp.getState().activeId).toBe("other")
  expect(useApp.getState().threads.find((row) => row.id === "thread")?.running).toBe(true)
})
