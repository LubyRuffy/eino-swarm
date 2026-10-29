export type Step = { kind: string; turnID?: string; messageID?: string; text?: string; callID?: string; err?: string; agentID?: string; role?: string; at?: string; sourceThreadID?: string; sourceTurnID?: string }

/** A turn the phone can replay: a thought, work, then an answer. Generic on
 *  purpose — this is a rendering fixture, not a task the product knows. */
export function scriptedTurn(prompt: string): Step[] {
  return [
    { kind: "user_message", text: prompt },
    { kind: "reasoning_delta", text: "Reading the current shape before touching it." },
    { kind: "reasoning", text: "Reading the current shape before touching it." },
    { kind: "tool_call", text: `read({"file_path":"src/lib/layout.ts"})`, callID: "" },
    { kind: "tool_result", text: "42 lines", callID: "" },
    { kind: "tool_call", text: `exec({"command":"npm test -- layout"})`, callID: "" },
    { kind: "tool_result", text: "12 passed", callID: "" },
    { kind: "delta", text: "Here is what changed" },
    {
      kind: "agent_message",
      text: [
        "Here is what changed:",
        "",
        "- the column keeps its width on a narrow screen",
        "- the tail stays pinned while older rows page in",
        "",
        "```chart",
        '{"type":"bar","title":"Counts","x":"item","y":"n","unit":"n","data":[{"item":"a","n":1},{"item":"b","n":3}]}',
        "```",
        "",
        "Run `npm test` to see it green.",
      ].join("\n"),
    },
    { kind: "done" },
  ]
}

/** Isolates the phone's reasoning disclosure from its tool aggregation. */
export function reasoningOnlyTurn(running: boolean): Step[] {
  return [
    // A ready/history snapshot carries durable events, not seq-zero live deltas.
    { kind: "reasoning", agentID: "manager", text: "Reading the current shape before touching it." },
    ...(running ? [] : [
      { kind: "agent_message", agentID: "manager", text: "Here is what changed:" },
      { kind: "done", agentID: "manager" },
    ]),
  ]
}
