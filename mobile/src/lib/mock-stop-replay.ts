type Step = { kind: string; text?: string; err?: string; agentID?: string; role?: string; callID?: string }

export function stopReplayStart(): Step[] {
  return [
    { kind: "user_message", text: "Continue the assigned work." },
    ...["reader-1", "writer-2", "done-3", "failed-4"].map((agentID) => ({ kind: "spawned", agentID, role: "worker" })),
    { kind: "finished", agentID: "done-3", text: "Verified result." },
    { kind: "finished", agentID: "failed-4", err: "provider unavailable" },
    { kind: "tool_call", agentID: "reader-1", callID: "read", text: "read({})" },
  ]
}

export function stopReplayEnd(order: string | null): Step[] {
  const finished = ["reader-1", "writer-2"].map((agentID) => ({
    kind: "finished", agentID, err: `swarm: agent ${agentID} cancelled: context canceled`,
  }))
  const cleanup = { kind: "cleanup", text: "Stopped unfinished workers." }
  const terminal = { kind: "error", err: "interrupted" }
  const ends = order === "before_cleanup" ? [...finished, cleanup, terminal]
    : order === "after_cleanup" ? [cleanup, ...finished, terminal] : [cleanup, terminal, ...finished]
  return [{ kind: "user_stop_requested", text: "Stop requested by the user." }, ...ends]
}
