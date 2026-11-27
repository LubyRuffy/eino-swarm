export type InspectorScope = "thread" | "project"

export const builtinInspectorScopes = {
  agents: "thread",
  files: "thread",
  trace: "thread",
  memory: "project",
} as const satisfies Record<string, InspectorScope>
