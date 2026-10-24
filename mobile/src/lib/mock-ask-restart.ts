type Step = { kind: string; text?: string; agentID?: string; callID?: string; turnID?: string }

/** A persisted unaccepted question and its explicit recovery; no model reissues it. */
export function restartAskHistory(): Step[] {
  const args = JSON.stringify({ questions: [{ id: "test-window", prompt: "When should this run?", options: [{ id: "now", label: "Now" }, { id: "later", label: "Later" }] }] })
  const owner = { agentID: "manager", callID: "ask-fixture", turnID: "restart-ask-turn" }
  return [
    { ...owner, kind: "tool_call", text: `ask_user(${args})` },
    { ...owner, kind: "tool_result", text: '{"outcome":"unknown","error":"outcome_unknown"}' },
    { ...owner, kind: "engine_restart_checkpoint", text: '{"pause":"ask","manager_history":["private seed"]}' },
    { ...owner, kind: "ask_recovered", text: args },
  ]
}
