import { isOutputBudgetError, MAX_COMPLETION_SETTINGS_KEY } from "./output-budget"

export const AGENT_TIMEOUT_SETTINGS_KEY = "agent_timeout_seconds"
export const AGENT_MAX_TURNS_SETTINGS_KEY = "max_turns"

type AgentFailure =
  | { kind: "timeout"; duration: string; section: "swarm"; settingsKey: typeof AGENT_TIMEOUT_SETTINGS_KEY }
  | { kind: "max_turns"; section: "swarm"; settingsKey: typeof AGENT_MAX_TURNS_SETTINGS_KEY }
  | { kind: "output_budget"; section: "swarm"; settingsKey: typeof MAX_COMPLETION_SETTINGS_KEY }
  | { kind: "provider_idle"; duration: string; section: "models"; settingsKey: "" }
  | { kind: "unknown" }

/** Only error formats emitted by this worker identify a setting. A provider
 *  deadline or the parent turn's error does not prove its watchdog expired. */
export function agentFailure(error: string | undefined, agentId: string): AgentFailure {
  const text = error?.trim() ?? ""
  const duration = /^(?:\d+(?:\.\d+)?(?:ns|µs|μs|us|ms|s|m|h))+$/
  const timeout = /^swarm: agent (\S+) exceeded timeout (\S+?)(?: \(\d+ consecutive watchdog failures\))?: /.exec(text)
  if (timeout?.[1] === agentId && duration.test(timeout[2])) {
    return { kind: "timeout", duration: timeout[2], section: "swarm", settingsKey: AGENT_TIMEOUT_SETTINGS_KEY }
  }
  const modelError = /^(?:\[NodeRunError\] )?(?:run node\[ChatModel\] pre processor fail: )?([^\n]+)(?:\n-{2,}\nnode path: \[[^\]\n]+\])?$/.exec(text)?.[1]
  if (modelError === "exceeds max iterations") {
    return { kind: "max_turns", section: "swarm", settingsKey: AGENT_MAX_TURNS_SETTINGS_KEY }
  }
  const idle = /^(?:failed to receive stream chunk: )?provider: stream idle timeout after (\S+)$/.exec(modelError ?? "")
  if (idle && duration.test(idle[1])) {
    return { kind: "provider_idle", duration: idle[1], section: "models", settingsKey: "" }
  }
  if (isOutputBudgetError(text)) {
    return { kind: "output_budget", section: "swarm", settingsKey: MAX_COMPLETION_SETTINGS_KEY }
  }
  return { kind: "unknown" }
}
