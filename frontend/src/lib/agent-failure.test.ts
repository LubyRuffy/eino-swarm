import { describe, expect, it } from "vitest"

import { agentFailure, AGENT_TIMEOUT_SETTINGS_KEY, AGENT_MAX_TURNS_SETTINGS_KEY } from "./agent-failure"
import { MAX_COMPLETION_SETTINGS_KEY, OUTPUT_BUDGET_ERROR } from "./output-budget"

describe("actionable agent failures", () => {
  it.each([
    "swarm: agent writer-1 exceeded timeout 10m0s (1 consecutive watchdog failures): context deadline exceeded",
    "swarm: agent writer-1 exceeded timeout 1.5s: context deadline exceeded",
  ])("links this worker's recorded overall timeout to its limit: %s", (error) => {
    expect(agentFailure(error, "writer-1")).toEqual({
      kind: "timeout",
      duration: error.includes("10m0s") ? "10m0s" : "1.5s",
      section: "swarm",
      settingsKey: AGENT_TIMEOUT_SETTINGS_KEY,
    })
  })

  it("reuses the completion cap for an exact recorded output-budget error", () => {
    expect(agentFailure(OUTPUT_BUDGET_ERROR, "writer-1")).toEqual({
      kind: "output_budget", section: "swarm", settingsKey: MAX_COMPLETION_SETTINGS_KEY,
    })
  })

  it.each([
    "exceeds max iterations",
    "[NodeRunError] exceeds max iterations\n------------------------\nnode path: [worker, agent]",
    "[NodeRunError] run node[ChatModel] pre processor fail: exceeds max iterations\n------------------------\nnode path: [ChatModel]",
  ])("links the actual ADK worker round limit to max_turns: %s", (error) => {
    expect(agentFailure(error, "writer-1")).toEqual({ kind: "max_turns", section: "swarm", settingsKey: AGENT_MAX_TURNS_SETTINGS_KEY })
  })

  it.each([
    "provider: stream idle timeout after 2m0s",
    "[NodeRunError] provider: stream idle timeout after 2m0s\n------------------------\nnode path: [ChatModel]",
    "[NodeRunError] failed to receive stream chunk: provider: stream idle timeout after 1s\n------------------------\nnode path: [node_1, ChatModel]",
  ])("opens Models for an identified provider stream-idle timeout: %s", (error) => {
    expect(agentFailure(error, "writer-1")).toEqual({ kind: "provider_idle", duration: error.includes("2m0s") ? "2m0s" : "1s", section: "models", settingsKey: "" })
  })

  // A provider's idle deadline and a parent turn failing do not identify a
  // worker watchdog. Sending either to that setting would hide the real cause.
  it.each([
    undefined, "", "context deadline exceeded", "request timeout after 10m",
    "swarm: agent other-2 exceeded timeout 10m0s: context deadline exceeded",
    "swarm: agent writer-1 exceeded timeout sometime: context deadline exceeded",
    "swarm: agent writer-1 cancelled: context canceled",
    "swarm: wait timeout, 1 agents still running",
    `tool error: ${OUTPUT_BUDGET_ERROR}`,
    "tool error: exceeds max iterations", "provider: exceeds max iterations",
    "exceeds max steps",
    "provider: stream idle timeout after sometime",
    "Post https://example.invalid: timeout awaiting response headers",
  ])("does not invent a configuration fix for %s", (error) => {
    expect(agentFailure(error, "writer-1")).toEqual({ kind: "unknown" })
  })
})
