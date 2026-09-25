/** One foreign session. older is set on rows loaded with More so a
 *  refresh of the recent page does not drop them. */
export type ClientTask = {
  id: string
  title: string
  cwd?: string
  updated_at: string
  status: "running" | "done"
  older?: boolean
}

export type ClientTool = {
  id: string
  tasks: ClientTask[]
  more: boolean
  next?: string
}

export type ClientEntry = { role: string; text: string }

export type ClientTranscript = {
  id: string
  title: string
  status: "running" | "done" | string
  entries: ClientEntry[]
  truncated?: boolean
}

export type ClientCatalog = {
  enabled: boolean
  tools: ClientTool[]
}

export function toolLabelKey(
  id: string,
): "sidebar.clientClaude" | "sidebar.clientCodex" | "sidebar.clientCursor" | "sidebar.clients" {
  if (id === "claude") return "sidebar.clientClaude"
  if (id === "codex") return "sidebar.clientCodex"
  if (id === "cursor") return "sidebar.clientCursor"
  return "sidebar.clients"
}

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
      return {
        ...tool,
        tasks: [...tool.tasks, ...extra],
        more: add.more,
        next: add.next,
      }
    })
  }
  return incoming.map((tool) => {
    const old = prev.find((item) => item.id === tool.id)
    const kept = (old?.tasks ?? []).filter(
      (task) => task.older && !tool.tasks.some((next) => next.id === task.id),
    )
    const tasks = [...tool.tasks, ...kept]
    // A refresh is page one. More already walked past that cursor, so the
    // button must not jump back to the first older page.
    if (old?.tasks.some((task) => task.older)) {
      return { ...tool, tasks, more: old.more, next: old.next }
    }
    return { ...tool, tasks }
  })
}
