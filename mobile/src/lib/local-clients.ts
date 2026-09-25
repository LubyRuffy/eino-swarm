import type { ClientTool } from "./rpc"

/** Recent page replaces itself. Rows already pulled with More stay. */
export function mergeClientTools(
  prev: ClientTool[],
  incoming: ClientTool[],
  mode: "replace" | "append",
): ClientTool[] {
  if (mode === "append") {
    return prev.map((tool) => {
      const add = incoming.find((item) => item.id === tool.id)
      if (!add) return tool
      const seen = new Set(tool.tasks.map((task) => task.id))
      const extra = add.tasks
        .filter((task) => !seen.has(task.id))
        .map((task) => ({ ...task, older: true }))
      return { ...tool, tasks: [...tool.tasks, ...extra], more: add.more, next: add.next }
    })
  }
  return incoming.map((tool) => {
    const old = prev.find((item) => item.id === tool.id)
    const kept = (old?.tasks ?? []).filter(
      (task) => task.older && !tool.tasks.some((next) => next.id === task.id),
    )
    return { ...tool, tasks: [...tool.tasks, ...kept] }
  })
}

export function clientToolTitle(id: string): "home.clientClaude" | "home.clientCodex" | "home.clientCursor" | "home.clients" {
  if (id === "claude") return "home.clientClaude"
  if (id === "codex") return "home.clientCodex"
  if (id === "cursor") return "home.clientCursor"
  return "home.clients"
}
